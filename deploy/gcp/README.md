# GCP onboarding — passwordless Elevarq Signals on Cloud SQL

Stands up a least-privilege, **passwordless** Signals collector against Cloud
SQL for PostgreSQL using `auth_method: gcp_cloudsql_iam`. The collector VM's
attached **service account** mints a short-lived Google OAuth2 token at connect
time — **no password is stored anywhere** (INV001/INV002), and the connection
is `verify-full` over the direct libpq path (INV003).

- [`terraform/`](terraform/) — `terraform apply`

Provisions a dedicated service account with `roles/cloudsql.instanceUser`,
optionally the IAM DB user, and a GCE VM (no public IP) running the collector
pre-wired for `gcp_cloudsql_iam`. See
[`docs/database-connections.md`](../../docs/database-connections.md) for the
full `auth_method` reference.

## Prerequisites

- A Cloud SQL for PostgreSQL instance with **IAM database authentication**
  enabled (`cloudsql.iam_authentication = on`).
- A VPC network + subnetwork that routes to the instance's **private IP** (the
  direct libpq path needs private connectivity).
- The instance's **server CA certificate** (public, not a secret):

  ```bash
  gcloud sql instances describe <instance> \
    --format='value(serverCaCert.cert)' > server-ca.pem
  ```

## Step 1 — IAM DB user + grant (one-time)

The module creates the service account and grants
`roles/cloudsql.instanceUser`. The IAM DB user can be created by the module
(set `instance_name`) or out-of-band:

```bash
# SA email without the .gserviceaccount.com suffix is the PG role name
gcloud sql users create signals-collector@<project>.iam.gserviceaccount.com \
  --instance=<instance> --type=cloud_iam_service_account
```

Then grant least-privilege monitoring, **as a privileged role** on the target
database (the role name is the SA email **without** `.gserviceaccount.com`):

```sql
GRANT pg_monitor TO "signals-collector@<project>.iam";
```

`terraform output collector_db_user` prints the exact role name. See
[`docs/postgres-role.md`](../../docs/postgres-role.md) for the role rationale.

## Step 2 — Terraform

```bash
cd terraform
terraform init
terraform apply \
  -var project_id=my-project \
  -var region=europe-west1 \
  -var zone=europe-west1-b \
  -var db_host=10.0.0.5 \
  -var db_name=appdb \
  -var instance_name=my-instance \
  -var network=projects/my-project/global/networks/my-vpc \
  -var subnetwork=projects/my-project/regions/europe-west1/subnetworks/collector \
  -var "db_server_ca_cert=$(cat server-ca.pem)"
```

To impersonate another service account, set
`-var gcp_impersonate_service_account=collector@my-proj.iam.gserviceaccount.com`
— the VM SA must hold **Service Account Token Creator** on it.

## Verify (live)

Live verification provisions real infrastructure and is operator-gated — it is
not part of default CI (mirrors the provider live smokes, #96). After apply,
SSH to the collector VM:

```bash
# the collector container should be running and collecting
docker logs signals 2>&1 | grep -iE "collector|snapshot|connected"

# the control-plane API requires a bearer token; cloud-init minted one and
# stored it root-only on the VM, outside the bind-mounted config.
TOKEN="$(sudo cat /root/signals-api-token)"

# confirm collector status and trigger an export over the loopback API
docker exec -e SIGNALS_API_TOKEN="$TOKEN" signals signalsctl status
docker exec -e SIGNALS_API_TOKEN="$TOKEN" signals signalsctl export --output /data/snapshot.zip
```

A healthy run connects with **no password in config**, mints a token from the
attached service account, and collects at least one snapshot. If the
connection is rejected, re-check Step 1 (IAM DB user exists, `pg_monitor`
granted to the truncated SA name) and that the VM's subnet routes to the
instance private IP.

## Security notes

- **No secrets** in any input or on disk — the token is minted from the
  attached service account at connect time and never persisted. The server CA
  certificate is public information.
- The service account is **dedicated** to the collector and granted only
  `roles/cloudsql.instanceUser`. Add `roles/cloudsql.client` only if you front
  the instance with the Cloud SQL connector.
- The VM has **no public IP**, runs **Shielded VM** (secure boot + vTPM +
  integrity monitoring), and the API listener binds to `127.0.0.1` only.
- The loopback **control-plane API token** is a separate credential class from
  the (non-existent) DB password: cloud-init mints a random 32-byte token,
  passes it to the container via the environment (`SIGNALS_API_TOKEN`), and
  stores it root-only at `/root/signals-api-token` — outside the bind-mounted
  config, so `signals.yaml` itself stays secret-free.
- TLS is **`verify-full`** against the instance server CA (written to
  `/etc/signals/cloudsql-ca.pem`).

## Reusing the identity elsewhere

If you run the collector on GKE instead of the bundled VM, take the
`collector_service_account_email` output and bind it through Workload Identity
— see the Helm chart (`deploy/helm/signals/`) and its `gcp_cloudsql_iam` /
GKE-workload-identity snippet (#114).

## Data durability and upgrades (#501)

The collector's entire store — `signals.db` at **`/data`** — holds the
instance identity (`meta.instance_id`), collection history, and the
cadence/incremental watermarks that let a replacement collector resume instead
of re-collecting from zero. That store lives on a **separate persistent disk**
(`google_compute_disk.data`, attached via the instance's `attached_disk`
block), not the boot disk and not a Docker named volume. Persistent disks are
encrypted at rest by default.

- **The persistent disk is independent of the instance.** Replacing the
  instance does not destroy the disk: it carries `prevent_destroy = true` and
  is re-attached at the same stable `device_name`.
- **Replacement re-attaches, never re-initializes.** The boot-time mount is
  **format-if-empty** (resolving the disk at
  `/dev/disk/by-id/google-<device_name>`): a populated disk keeps its
  `signals.db` untouched, so identity, history, and watermarks survive the
  instance/image upgrade. A fresh disk is formatted exactly once and owned by
  the non-root container user (uid 10001).
- **Credentials stay external.** No DB credential or API token is written onto
  `/data`; the control-plane token stays root-only at `/root/signals-api-token`,
  outside the data disk.
- **Backups.** Snapshot the persistent data disk (not the boot disk) as the
  backup of record for collection history.

### Transitioning an existing boot-disk install (migration)

Earlier revisions mounted `/data` from a Docker **named volume on the boot
disk** (`-v signals-data:/data`), so `signals.db` was lost on the next
instance replacement. To move to the durable disk **without losing history** —
and **never letting the new instance initialize an empty store over an existing
one** — migrate the store before re-applying:

1. SSH to the VM (over OS Login / IAP) and **stop the collector**:
   `sudo docker stop signals`.
2. Copy the live store off the named volume:
   `sudo docker run --rm -v signals-data:/src -v /root/signals-backup:/dst alpine \
   sh -c 'cp -a /src/. /dst/'`.
3. **Verify** the copy: it must contain `signals.db`, and
   `sqlite3 /root/signals-backup/signals.db 'select value from meta where key="instance_id"; select count(*) from snapshots;'`
   must show the expected identity and a non-zero history count.
4. `terraform apply` to provision the persistent data disk; on the instance,
   with the collector **stopped**, copy the verified store onto the mounted
   disk: `sudo cp -a /root/signals-backup/. /data/ && sudo chown -R 10001:10001 /data`.
5. Re-verify identity + history from `/data/signals.db`, start the collector,
   and confirm the next cycle is incremental (watermarks intact).

If you cannot complete and verify this copy, **do not cut over**. **Rollback /
recovery note:** if a pre-fix install already lost `/data` on a prior
replacement, that history and identity **cannot be recovered**; the first apply
of this fix seeds a fresh store on the durable disk.
