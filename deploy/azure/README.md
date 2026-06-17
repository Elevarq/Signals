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
# trigger an export to confirm a successful passwordless connection
docker exec signals signalsctl export --output /data/snapshot.zip
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
- Network egress is governed by the **subnet's NSG** by default. To pin a
  NIC-level NSG (allowing only egress to the DB on `db_port`), pass
  `network_security_group_id` / `networkSecurityGroupId`.

## Reusing the identity elsewhere

If you run the collector on AKS instead of the bundled VM, take the
`collector_identity_client_id` output and wire it through workload identity —
see the Helm chart (`deploy/helm/signals/`) and its `azure_entra` /
AKS-workload-identity snippet (#114).
