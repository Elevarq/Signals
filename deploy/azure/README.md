# Azure onboarding — passwordless Elevarq Signals on Flexible Server

Stands up a least-privilege, **passwordless** Signals collector against Azure
Database for PostgreSQL Flexible Server using `auth_method: azure_entra`. The
collector VM's **user-assigned managed identity** mints a short-lived Entra
OAuth2 token at connect time — **no password is stored anywhere**
(INV001/INV002), and the connection is `verify-full` (INV003).

Two equivalent implementations:

- [`terraform/`](terraform/) — `terraform apply`
- [`bicep/main.bicep`](bicep/main.bicep) — `az deployment group create`

Both provision the user-assigned managed identity, a NIC on an existing
subnet, and a Linux VM running the collector pre-wired for `azure_entra`. See
[`docs/database-connections.md`](../../docs/database-connections.md) for the
full `auth_method` reference.

## Prerequisites

- An Azure Database for PostgreSQL **Flexible Server** with **Microsoft Entra
  authentication** enabled and an Entra administrator configured.
- An existing VNet/subnet that routes to the server (the server's firewall /
  private access must allow the collector subnet).
- The `azure_client_id` of the created identity is produced as an output — you
  do not supply it.

## Step 1 — Identity + database principal (one-time)

Apply the module first so the managed identity exists, then map it. The PG
principal name **must equal the managed identity display name**
(`<name_prefix>-collector`, default `signals-collector`).

Run once against the target database, **as an Entra administrator**:

```sql
-- name MUST equal the managed identity display name
SELECT * FROM pgaadauth_create_principal('signals-collector', false, false);
GRANT pg_monitor TO "signals-collector";   -- least-privilege read-only monitoring
```

See [`docs/postgres-role.md`](../../docs/postgres-role.md) for the role
rationale and [`docs/database-connections.md`](../../docs/database-connections.md)
(the `azure_entra` section) for the Entra mapping detail.

## Step 2a — Terraform

```bash
cd terraform
terraform init
terraform apply \
  -var resource_group_name=my-rg \
  -var location=westeurope \
  -var db_host=myserver.postgres.database.azure.com \
  -var db_name=appdb \
  -var subnet_id=/subscriptions/<sub>/resourceGroups/my-rg/providers/Microsoft.Network/virtualNetworks/my-vnet/subnets/collector \
  -var 'admin_ssh_public_key=ssh-ed25519 AAAA... you@example.com'
```

`terraform output collector_identity_name` prints the exact name to use in
Step 1.

## Step 2b — Bicep

```bash
# edit bicep/main.bicepparam first (dbHost, dbName, subnetId, adminSshPublicKey)
az deployment group create \
  --resource-group my-rg \
  --template-file bicep/main.bicep \
  --parameters bicep/main.bicepparam
```

The deployment emits `collectorIdentityName` and `collectorIdentityClientId`
as outputs.

## Verify (live)

Live verification provisions real infrastructure and is operator-gated — it is
not part of default CI (mirrors the provider live smokes, #95). After
apply/deploy, SSH to the collector VM:

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
managed identity, and collects at least one snapshot. If the connection is
rejected, re-check Step 1 (principal name == identity display name) and that
the server's network rules admit the collector subnet.

## Security notes

- **No secrets** in any input or on disk — the token is minted from the
  managed identity at connect time and never persisted. The SSH public key is
  a public credential, not a secret.
- The VM uses **SSH key auth only** (`disablePasswordAuthentication: true`).
- The identity is **user-assigned and dedicated** to the collector; scope its
  database grant to `pg_monitor` only.
- TLS is **`verify-full`** against the Azure CA bundle (fetched to
  `/etc/signals/azure-ca.pem`); override `db_ca_cert_url` / `dbCaCertUrl` if your
  server chains to a different root.
- The API listener binds to `127.0.0.1` only.
- The loopback **control-plane API token** is a separate credential class from
  the (non-existent) DB password: cloud-init mints a random 32-byte token,
  passes it to the container via the environment (`SIGNALS_API_TOKEN`), and
  stores it root-only at `/root/signals-api-token` — outside the bind-mounted
  config, so `signals.yaml` itself stays secret-free.
- Network egress is governed by the **subnet's NSG** by default. To pin a
  NIC-level NSG (allowing only egress to the DB on `db_port`), pass
  `network_security_group_id` / `networkSecurityGroupId`.

## Reusing the identity elsewhere

If you run the collector on AKS instead of the bundled VM, take the
`collector_identity_client_id` output and wire it through workload identity —
see the Helm chart (`deploy/helm/signals/`) and its `azure_entra` /
AKS-workload-identity snippet (#114).

## Data durability and upgrades (#501)

The collector's entire store — `signals.db` at **`/data`** — holds the
instance identity (`meta.instance_id`), collection history, and the
cadence/incremental watermarks that let a replacement collector resume instead
of re-collecting from zero. That store lives on a **separate managed data
disk** (`azurerm_managed_disk.data` in Terraform / the `dataDisk` resource in
Bicep), not the VM OS disk and not a Docker named volume. Managed disks are
encrypted at rest with platform-managed keys by default.

- **The data disk is independent of the VM.** Replacing the VM (new size,
  new image) does not destroy the disk: Terraform carries
  `prevent_destroy = true`, and the Bicep attachment uses `deleteOption:
  Detach`, so the disk is retained and re-attached at the same LUN.
- **Replacement re-attaches, never re-initializes.** The disk attaches with
  `createOption: Attach` (never from image), and the boot-time mount is
  **format-if-empty**: a populated disk keeps its `signals.db` untouched, so
  identity, history, and watermarks survive the VM/image upgrade. A fresh disk
  is formatted exactly once and owned by the non-root container user (uid 10001).
- **Credentials stay external.** No DB credential or API token is written onto
  `/data`; the control-plane token stays root-only at `/root/signals-api-token`,
  outside the data disk.
- **Backups.** Snapshot the managed data disk (not the OS disk) as the backup
  of record for collection history.

### Transitioning an existing OS-disk install (migration)

Earlier revisions mounted `/data` from a Docker **named volume on the OS disk**
(`-v signals-data:/data`), so `signals.db` was lost on the next VM
replacement. To move to the durable disk **without losing history** — and
**never letting the new VM initialize an empty store over an existing one** —
migrate the store before re-deploying:

1. SSH to the VM and **stop the collector**: `sudo docker stop signals`.
2. Copy the live store off the named volume:
   `sudo docker run --rm -v signals-data:/src -v /root/signals-backup:/dst alpine \
   sh -c 'cp -a /src/. /dst/'`.
3. **Verify** the copy: it must contain `signals.db`, and
   `sqlite3 /root/signals-backup/signals.db 'select value from meta where key="instance_id"; select count(*) from snapshots;'`
   must show the expected identity and a non-zero history count.
4. Re-apply (Terraform) / redeploy (Bicep) to provision the managed data disk;
   on the VM, with the collector **stopped**, copy the verified store onto the
   mounted disk: `sudo cp -a /root/signals-backup/. /data/ && sudo chown -R 10001:10001 /data`.
5. Re-verify identity + history from `/data/signals.db`, start the collector,
   and confirm the next cycle is incremental (watermarks intact).

If you cannot complete and verify this copy, **do not cut over**. **Rollback /
recovery note:** if a pre-fix install already lost `/data` on a prior
replacement, that history and identity **cannot be recovered**; the first
deploy of this fix seeds a fresh store on the durable disk.
