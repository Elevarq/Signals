# Database connection authentication (`auth_method`)

How Elevarq Signals supplies the credential it uses to connect to
PostgreSQL. Each target picks one `auth_method`; the method decides
*how* the credential is obtained, never *what* the connection is
allowed to do — the read-only least-privilege model is identical for
every method.

Specs: `features/arq-signals/credential-providers.md`
(`ARQ-SIGNALS-AUTH-*`, ACTIVE) and the per-provider sub-specs
(#94–#98).

## The guarantees (read this first)

These hold for every method below:

- **No stored secret for the cloud-identity methods.**
  `aws_rds_iam`, `azure_entra`, and `gcp_cloudsql_iam` mint a
  short-lived credential from the collector's *ambient* cloud identity
  at connect time. The target config carries **no** password, password
  file, or pgpass reference — combining one with a token method is a
  hard startup error (INV001 / FC005).
- **The credential is never disclosed.** No token, fetched secret, or
  key material is ever written to logs, errors, audit events, metrics,
  the local database, or any export. Only non-secret metadata
  (`auth_method`, db user, `resolved_at`, `expires_at`/`ttl_present`,
  and a fingerprint where deriving it can't leak the secret) is emitted
  (INV002 / INV007).
- **Server identity is verified for credential-bearing methods.** Any
  method that sends a token or a fetched password to the server
  (`aws_rds_iam`, `azure_entra`, `gcp_cloudsql_iam`, `secret_store`)
  **requires `sslmode=verify-full` in every environment** — stricter
  than the general prod-only TLS rule. A weaker mode is a hard startup
  error (INV003 / FC006).
- **Rotation is automatic.** The credential is re-resolved on every new
  physical connection, so a refreshed token or a rotated secret is
  picked up on the next reconnect without restarting the daemon
  (INV004). Minted tokens are cached per target and refreshed *before*
  expiry by `max(60s, min(5m, ttl × 0.20))`.
- **Read-only is untouched.** `ValidateRoleSafety` still runs and the
  same collector approval applies regardless of `auth_method` (INV005).

## Prerequisite: the read-only role

Every method authenticates **as the Signals role** — a `LOGIN` role
granted `pg_monitor`. Create it once (see
[postgres-role.md](postgres-role.md) for the full rationale):

```sql
CREATE ROLE signals LOGIN;          -- add PASSWORD only for the password / secret_store methods
GRANT pg_monitor TO signals;
```

The recipes below add the method-specific binding on top of this role.

## Choosing a method

| `auth_method` | Use when | Credential | Status |
|---|---|---|---|
| `password` (default) | Self-managed PostgreSQL, credential supplied locally | Password from file / env / pgpass | Shipped |
| `aws_rds_iam` | Amazon RDS / Aurora PostgreSQL | RDS IAM auth token (15 min) | Shipped (#94) |
| `azure_entra` | Azure Database for PostgreSQL — Flexible Server | Entra OAuth2 token (~75 min) | Shipped (#95) |
| `gcp_cloudsql_iam` | Google Cloud SQL for PostgreSQL | Google OAuth2 token (~60 min) | Shipped (#96) |
| `secret_store` | Self-managed PostgreSQL whose password lives in a cloud vault | Password fetched from AWS/Azure/GCP vault | Shipped (#97) |
| `mtls` | Client-certificate auth (on-prem / self-managed) | Client cert + key | Shipped (#98) |

Omitting `auth_method` keeps the existing password behaviour exactly —
no migration is required for current deployments (NFR003).

---

## `password` (default)

The credential is a password supplied locally via exactly one of
`password_file`, `password_env`, or `pgpass_file`.

```sql
CREATE ROLE signals LOGIN PASSWORD 'use-a-strong-secret';
GRANT pg_monitor TO signals;
```

```yaml
targets:
  - name: prod-primary
    host: db.internal
    port: 5432
    dbname: appdb
    user: signals
    sslmode: verify-full
    sslrootcert_file: /etc/ssl/certs/db-ca.pem
    # auth_method: password   # default — may be omitted
    password_file: /run/secrets/signals_password
```

---

## `aws_rds_iam` — Amazon RDS / Aurora (passwordless)

The collector's ambient AWS identity (EC2 instance profile, ECS task
role, EKS IRSA / Pod Identity, or the local credential chain in dev)
mints a 15-minute RDS IAM auth token that is used as the connection
password.

**1. Database — enable IAM auth for the role:**

```sql
CREATE ROLE signals LOGIN;     -- no password
GRANT rds_iam TO signals;      -- enables RDS IAM authentication
GRANT pg_monitor TO signals;
```

**2. AWS IAM — allow the collector's principal to connect.** Attach to
the instance profile / task role / IRSA role:

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": "rds-db:connect",
    "Resource": "arn:aws:rds-db:<region>:<account-id>:dbuser:<db-resource-id>/signals"
  }]
}
```

`<db-resource-id>` is the instance/cluster `DbiResourceId` (e.g.
`db-ABCDEFGH…`), not the DB identifier.

**3. Target config:**

```yaml
  - name: rds-prod
    host: mydb.abc123.us-east-1.rds.amazonaws.com
    port: 5432
    dbname: appdb
    user: signals
    auth_method: aws_rds_iam
    region: us-east-1              # optional; inferred from AWS_REGION / IMDS when omitted
    sslmode: verify-full          # required
    sslrootcert_file: /etc/ssl/certs/rds-global-bundle.pem
    # no password_* / pgpass_file — token methods are passwordless
```

Download the RDS CA bundle for `sslrootcert_file` from the AWS
[RDS SSL/TLS docs](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/UsingWithRDS.SSL.html).
If `region` is neither configured nor in the environment, startup emits
a warning and the region is resolved from instance metadata at connect
time.

---

## `azure_entra` — Azure Database for PostgreSQL Flexible Server (passwordless)

The collector's ambient Azure identity (Managed Identity on VM/VMSS,
AKS workload identity, or the local Azure credential chain in dev)
obtains an Entra OAuth2 token (scope
`https://ossrdbms-aad.database.windows.net/.default`, fixed and not
configurable) used as the connection password.

**1. Database — map the PG role to the Entra principal.** Run as an
Entra administrator on the server. The PG role name **must match** the
Entra user / group / service-principal display name:

```sql
SELECT * FROM pgaadauth_create_principal('signals', false, false);
GRANT pg_monitor TO signals;
```

**2. Azure — ensure the collector's Managed Identity exists** and (for
a user-assigned identity on a host with more than one) note its client
ID for `azure_client_id`.

**3. Target config:**

```yaml
  - name: azure-flex-prod
    host: myserver.postgres.database.azure.com
    port: 5432
    dbname: appdb
    user: signals                       # must equal the Entra principal name
    auth_method: azure_entra
    azure_client_id: 00000000-0000-0000-0000-000000000000  # optional; user-assigned MI disambiguation
    sslmode: verify-full                    # required
    sslrootcert_file: /etc/ssl/certs/DigiCertGlobalRootCA.crt.pem
```

`azure_client_id` is also read from `AZURE_CLIENT_ID`; with a single or
system-assigned identity it is not needed. An undiscoverable or
ambiguous identity is a connect-time, target-scoped failure (other
targets keep collecting).

---

## `gcp_cloudsql_iam` — Google Cloud SQL (passwordless)

The collector's Application Default Credentials (attached service
account via Workload Identity on GCE/GKE/Cloud Run, or `gcloud` ADC in
dev) obtain a Google OAuth2 token (scope
`https://www.googleapis.com/auth/sqlservice.login`, fixed) used as the
connection password over a direct libpq connection with `verify-full`.

**1. Cloud SQL — create the IAM database user:**

```bash
# service account identity:
gcloud sql users create signals-collector@<project>.iam.gserviceaccount.com \
  --instance=<instance> --type=cloud_iam_service_account

# or a human/user identity:
gcloud sql users create user@example.com \
  --instance=<instance> --type=cloud_iam_user
```

**2. GCP IAM — grant the collector's identity** the
`roles/cloudsql.instanceUser` role on the project (and
`roles/cloudsql.client` if you front the instance with the Cloud SQL
connector).

**3. Database — grant `pg_monitor`** to the IAM user. For a service
account the PG role name is the email **without** the
`.gserviceaccount.com` suffix (Google's documented truncation):

```sql
GRANT pg_monitor TO "signals-collector@<project>.iam";
```

**4. Target config:**

```yaml
  - name: cloudsql-prod
    host: 10.0.0.5                           # private IP, or the proxy endpoint
    port: 5432
    dbname: appdb
    user: "signals-collector@<project>.iam"  # SA email without .gserviceaccount.com
    auth_method: gcp_cloudsql_iam
    gcp_impersonate_service_account: ""      # optional; impersonate when one collector serves many SAs
    sslmode: verify-full                     # required (direct libpq path)
    sslrootcert_file: /etc/ssl/certs/server-ca.pem
```

Download the instance's `server-ca.pem` from the Cloud SQL console for
`sslrootcert_file`. When `gcp_impersonate_service_account` is set, the
ambient ADC identity must hold **Service Account Token Creator** on it.

---

## `secret_store` — self-managed PostgreSQL, password in a cloud vault

For PostgreSQL you run yourself: keep the database password in a cloud
secret store instead of in Signals' config. At connect time the
collector authenticates to the vault with its ambient workload identity
and uses the fetched value as the password. The backend is **inferred
from the shape of `secret_ref`** — there is no separate selector:

| `secret_ref` shape | Backend | IAM permission |
|---|---|---|
| `arn:aws:secretsmanager:<region>:<acct>:secret:<name>` | AWS Secrets Manager | `secretsmanager:GetSecretValue` |
| `arn:aws:ssm:<region>:<acct>:parameter/<name>` | AWS Systems Manager Parameter Store | `ssm:GetParameter` (+ `kms:Decrypt` for a `SecureString`) |
| `https://<vault>.vault.azure.net/secrets/<name>[/<version>]` | Azure Key Vault | Key Vault **Secrets User** (get) |
| `projects/<proj>/secrets/<name>/versions/<version\|latest>` | GCP Secret Manager | `secretmanager.versions.access` |

For both AWS forms the **region is taken from the ARN**, never from the
environment, and the `secretsmanager` vs `ssm` ARN service segment
selects the backend. A `secret_ref` matching none of the four shapes is
a hard startup error (FC007).

**1. Database — a normal password role** whose password is the value
stored in the vault:

```sql
CREATE ROLE signals LOGIN PASSWORD 'matches-the-vault-value';
GRANT pg_monitor TO signals;
```

**2. Vault — grant the collector's workload identity** the read
permission from the table above on that one secret.

**3. Target config (AWS Secrets Manager example):**

```yaml
  - name: selfmanaged-prod
    host: db.internal
    port: 5432
    dbname: appdb
    user: signals
    auth_method: secret_store
    secret_ref: arn:aws:secretsmanager:us-east-1:123456789012:secret:prod/signals-AbCdEf
    secret_json_key: password     # optional; extract one key from a JSON secret
    max_cache_ttl: 15m            # optional; bounds reuse when the vault gives no TTL
    sslmode: verify-full          # required
    sslrootcert_file: /etc/ssl/certs/db-ca.pem
```

`secret_json_key` covers JSON secrets like an AWS RDS-managed
`{"username":"…","password":"…"}` — set it to `password`. Omit it when
the secret value *is* the password. Without a vault-supplied TTL and
without `max_cache_ttl`, the secret is re-fetched on every reconnect so
a rotation is picked up immediately.

### AWS Parameter Store — and the Azure / GCP equivalents

AWS has two stores that can hold the secret itself: **Secrets Manager**
and **Systems Manager Parameter Store** (a `SecureString` parameter holds
an encrypted value directly). Both are supported above — reference a
Parameter Store parameter by its ARN; a `SecureString` is fetched with
`GetParameter` + `WithDecryption=true`, and a plain `String` works too.

Azure and GCP have *config* stores that look comparable — **Azure App
Configuration** and **GCP Parameter Manager** — but they are built to
**reference** the dedicated vault, not to store the secret themselves:

- **Azure App Configuration** holds a *Key Vault reference*; the secret
  value stays in **Key Vault**.
- **GCP Parameter Manager** holds a `__REF__(…)` reference to a Secret
  Manager secret; the value stays in **Secret Manager**.

So on Azure and GCP there is nothing extra to configure in Signals: keep
the password in **Key Vault** / **Secret Manager** and point `secret_ref`
at the vault (the rows above). Do **not** store a database password as a
plaintext App Configuration key-value or Parameter Manager parameter —
that defeats the vault's protection, and Signals deliberately does not
read secrets from those config stores.

---

## `mtls` — client-certificate auth (self-managed)

For on-prem / self-managed PostgreSQL that mandates mutual TLS: the
collector presents a client X.509 certificate instead of a password or
token. The cert/key are local PEM files; the database maps the cert to
the role server-side (`pg_hba.conf` `clientcert=verify-full` +
`pg_ident.conf`).

**1. Database — trust + map the client cert** (`pg_hba.conf`):

```
hostssl  appdb  signals  <collector-cidr>  cert  clientcert=verify-full
```

Map the certificate's CN to the role in `pg_ident.conf` if the CN differs
from `signals`, then `GRANT pg_monitor TO signals;`.

**2. Target config:**

```yaml
  - name: onprem-mtls
    host: db.internal
    port: 5432
    dbname: appdb
    user: signals
    auth_method: mtls
    sslcert: /etc/signals/client.crt        # PEM client certificate
    sslkey: /etc/signals/client.key         # PEM private key
    sslkey_passphrase_file: /etc/signals/key.pass  # optional; for an encrypted key
    sslmode: verify-full                # required
    sslrootcert_file: /etc/signals/ca.pem   # verifies the server
```

The private key is read at connect time and **never logged or exported**;
a rotated cert/key is picked up on the next reconnect. `verify-full` is
required — a client certificate is only presented to a verified server.

---

## TLS, at a glance

| Methods | Required `sslmode` |
|---|---|
| `aws_rds_iam`, `azure_entra`, `gcp_cloudsql_iam`, `secret_store` | `verify-full`, in **every** environment |
| `password` | `prod`: `verify-ca`/`verify-full` (with `sslrootcert_file`); weaker only in `dev`/`lab` with `SIGNALS_ALLOW_INSECURE_PG_TLS=true` |

`verify-full` requires `sslrootcert_file` to point at the CA bundle
that signs the server certificate.

## Common errors

Each is actionable and redacted (the credential never appears):

- **`<method>` set with a password source** → startup error: token /
  secret methods are passwordless. Remove `password_file` /
  `password_env` / `pgpass_file` (FC005).
- **`<method>` with `sslmode` weaker than `verify-full`** → startup
  error in every environment (FC006).
- **No ambient identity / mint or fetch denied** → connect-time,
  target-scoped failure naming the identity sources tried and the
  required grant; other targets keep collecting (FC002 / FC003). For
  the cloud methods this is where the missing `rds_iam` grant,
  `pgaadauth_create_principal` mapping, IAM-user mapping, or vault
  permission shows up.
- **`secret_store` without / with an unrecognised `secret_ref`** →
  startup error naming the four accepted forms (FC007).

## See also

- [postgres-role.md](postgres-role.md) — the read-only role every
  method authenticates as.
- [authentication.md](authentication.md) — authentication for Signals'
  own HTTP API (a different concern from database connections).
- `features/arq-signals/credential-providers.md` — the authoritative
  specification.
