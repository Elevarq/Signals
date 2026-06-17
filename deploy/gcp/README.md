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
gcloud sql users create arq-signals-collector@<project>.iam.gserviceaccount.com \
  --instance=<instance> --type=cloud_iam_service_account
```

Then grant least-privilege monitoring, **as a privileged role** on the target
database (the role name is the SA email **without** `.gserviceaccount.com`):

```sql
GRANT pg_monitor TO "arq-signals-collector@<project>.iam";
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
docker logs arq-signals 2>&1 | grep -iE "collector|snapshot|connected"
# trigger an export to confirm a successful passwordless connection
docker exec arq-signals arqctl export --output /data/snapshot.zip
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
- TLS is **`verify-full`** against the instance server CA (written to
  `/etc/arq/cloudsql-ca.pem`).

## Reusing the identity elsewhere

If you run the collector on GKE instead of the bundled VM, take the
`collector_service_account_email` output and bind it through Workload Identity
— see the Helm chart (`deploy/helm/arq-signals/`) and its `gcp_cloudsql_iam` /
GKE-workload-identity snippet (#114).
