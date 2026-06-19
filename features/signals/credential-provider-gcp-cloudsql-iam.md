# Feature Specification: GCP Cloud SQL IAM Credential Provider

- **Spec ID prefix:** `SIGNALS-AUTH-GCP-`
- **Lifecycle status:** `ACTIVE`
- **Tracking issue:** [#96](https://github.com/Elevarq/Signals/issues/96)
- **Derives from:** `credential-providers.md` (ACTIVE, #93). This spec is a
  behavioral sub-spec; it MUST conform to that abstraction's interface,
  invariants (INV001–INV007), failure taxonomy, and resolved design
  decisions. Where this spec is silent, the keystone governs.
- **Type:** Behavioral + Integration-mapping (GCP ADC identity + OAuth2 token endpoint)

## Purpose

Implement the `gcp_cloudsql_iam` credential provider: connect Elevarq
Signals to **Google Cloud SQL for PostgreSQL** **passwordlessly**, using a
short-lived Google OAuth2 access token obtained from the collector's
ambient Google identity (the attached service account on GCE/GKE/Cloud
Run via Workload Identity, or Application Default Credentials in dev). The
access token *is* the connection password; no secret is stored in Signals'
configuration.

This is the third provider on the credential-provider keystone (#93),
after `aws_rds_iam` (#94) and `azure_entra` (#95). It reuses the shared
scaffolding (the `auth_method` target field, the `credentialResolver`
dispatch wired into `BeforeConnect`, the per-target token cache, and the
config-validation rules) and adds only the GCP-specific behavior: an ADC
token minter behind a seam, the `gcp_cloudsql_iam` validation branch, and
the operator-guidance text.

## Connection path (keystone INV003 satisfier — decision #1 of #93)

The keystone requires a token method to run only over an encrypted,
server-identity-verified channel, and explicitly defers to #96 the choice
of *which* satisfier this provider uses:

- **Chosen (confirmed): direct libpq + `verify-full`.** The provider
  mints an OAuth2 access token and hands it to pgx as the connection
  password over a standard libpq connection that MUST be
  `sslmode=verify-full` (the operator supplies the Cloud SQL server CA via
  `sslrootcert_file`). This is identical in shape to `aws_rds_iam` and
  `azure_entra`: the same `BeforeConnect` seam, the same cache, the same
  validation floors — no dialer or TLS surgery, and a light dependency
  (`golang.org/x/oauth2/google` only). INV003 is satisfied by
  `verify-full`.
- **Alternative: Cloud SQL Go Connector (`cloud.google.com/go/cloudsqlconn`).**
  The connector dials the instance through a Google-managed mTLS tunnel
  and performs IAM auth + server-identity verification itself (CA mode /
  instance match), so operators need not manage `verify-full` or a CA
  bundle — but it replaces pgx's dialer, pulls a substantially larger
  dependency tree, and requires a new `instance_connection_name`
  (`project:region:instance`) input. INV003 is satisfied by the
  connector's verification mode.

This spec is written for the **direct-libpq** path, which is the confirmed
choice. The connector path is retained above only as the documented
alternative considered; if it were ever adopted the Inputs/Interfaces/
Invariants below would change accordingly (notably:
`instance_connection_name` would replace `sslrootcert_file` +
`verify-full`, and the seam would dial via the connector).

## Inputs

- **`auth_method: gcp_cloudsql_iam`** (per target) — selects this provider.
- **`user`** (existing field) — the PostgreSQL role mapped to the IAM
  principal. For Cloud SQL IAM this is the IAM user email **without** the
  `.gserviceaccount.com` suffix for service accounts (Google's documented
  truncation). Used verbatim as the token's DB user.
- **`gcp_impersonate_service_account`** (new, optional) — the email of a
  service account to impersonate when minting the token, for the case
  where one collector serves instances owned by different service
  accounts. Resolution order:
  1. explicit `gcp_impersonate_service_account` config value,
  2. unset → mint directly from the ambient ADC identity.
- **Ambient Google identity** — discovered via Application Default
  Credentials (`GOOGLE_APPLICATION_CREDENTIALS` → gcloud user ADC →
  GCE/GKE/Cloud Run metadata server / Workload Identity). Never configured
  as a secret in Signals.
- **`host`, `port`, `dbname`, `sslmode`, `sslrootcert_file`** — existing
  fields. `sslmode` must satisfy INV003 (see FC-GCP-004): `verify-full`
  on the direct-libpq path.

## Outputs

- A resolved **password-kind** `Credential` whose `Password` is the Google
  OAuth2 access token and whose `ExpiresAt` is the token's expiry (Google
  access tokens are typically valid ~60 minutes).
- Credential **metadata only** to logs / status surfaces (INV007):
  `auth_method=gcp_cloudsql_iam`, resolved `db_user`, the scope,
  `resolved_at`, `expires_at`, whether impersonation is in effect. The
  token value is never emitted.

## Interfaces

### Provider contract (conforms to keystone)

```
gcpCloudSQLProvider implements CredentialProvider:
  Resolve(ctx, target) -> (Credential{Kind: password, Password: <token>,
                                      ExpiresAt: <token expiry>}, error)
```

- Selected at startup when `auth_method == gcp_cloudsql_iam`; invoked from
  the existing `BeforeConnect` hook (and `BuildSafeDSN` for doctor/conntest).
- Token acquisition is behind a **seam interface** (a `gcpTokenMinter`)
  so unit tests inject a fake and no test makes a real GCP call. The
  production implementation uses `golang.org/x/oauth2/google` (ADC +
  optional impersonation) to source a token for the fixed scope.

### Integration mapping — GCP

| Concern | Binding |
|---|---|
| Identity | Application Default Credentials (service account / Workload Identity / gcloud ADC) |
| Token mint | Google OAuth2 access token for scope `https://www.googleapis.com/auth/sqlservice.login` |
| Applied as | `ConnConfig.Password` (password kind) |
| Transport | TLS `verify-full` required on the direct-libpq path (INV003) |

The token **scope is fixed** at
`https://www.googleapis.com/auth/sqlservice.login` (the dedicated Cloud
SQL login scope — narrower than `cloud-platform`) and is not
operator-configurable.

## Invariants (GCP-specific; keystone invariants also apply)

- **SIGNALS-AUTH-GCP-INV001**: A target with `auth_method:
  gcp_cloudsql_iam` carries no password source. (Keystone INV001; enforced
  by FC-GCP-003.)
- **SIGNALS-AUTH-GCP-INV002**: The token is never written to logs,
  errors, audit, metrics, the local DB, or exports — only its metadata.
  (Keystone INV002/INV007.)
- **SIGNALS-AUTH-GCP-INV003**: The token is cached per target and
  re-acquired before expiry at `max(60s, min(5m, ttl*0.20))` (keystone
  default; for a ~60-minute token this caps at the **5-minute** ceiling).
  Cache key = `target_id` + `auth_method` + `db_user` + host/instance
  identity. Never shared across targets. (Keystone NFR001.)
- **SIGNALS-AUTH-GCP-INV004**: The token scope is fixed at
  `https://www.googleapis.com/auth/sqlservice.login`; no code path or
  config may widen it.

## Failure Conditions

- **FC-GCP-001 (mint failure)**: the token endpoint returns an error or
  times out → the target's connection attempt fails with a **redacted**,
  actionable error. No partial token cached. Other targets unaffected.
  (Keystone FC003.)
- **FC-GCP-002 (expired at connect)**: a cached token within the refresh
  skew or already expired → re-acquire before handing to pgx; if
  re-acquire fails, treat as FC-GCP-001. (Keystone FC004.)
- **FC-GCP-003 (stored secret on token method)**: `gcp_cloudsql_iam` +
  any password source → **hard config error at startup** naming the
  target and stating the method is passwordless. (Keystone FC005.)
- **FC-GCP-004 (TLS too weak)**: on the direct-libpq path,
  `gcp_cloudsql_iam` on a target whose effective `sslmode` is not
  `verify-full` → **hard config error at startup**, regardless of `env`.
  (Keystone FC006 / INV003.)
- **FC-GCP-005 (identity undiscoverable)**: ADC yields no usable
  credentials (or impersonation of the configured service account is
  denied) → **connect-time, target-scoped** failure with an actionable
  error naming the ADC sources tried and the impersonation remediation.
  Other targets keep collecting. (Keystone FC002.)

## Non-Functional Requirements

- **SIGNALS-AUTH-GCP-NFR001 (dependency hygiene)**: Use
  `golang.org/x/oauth2/google` (direct-libpq path) at a pinned version;
  the additions MUST pass Trivy / govulncheck gates. The GCP SDK is linked
  only on this provider's path — non-GCP targets require no Google
  credentials at runtime.
- **SIGNALS-AUTH-GCP-NFR002 (latency)**: steady-state reconnects reuse
  the cached token; a cold acquisition completes within the per-target
  connection budget.
- **SIGNALS-AUTH-GCP-NFR003 (no test-time GCP calls)**: unit tests use
  the injected `gcpTokenMinter` fake and make no real GCP or network
  calls. The live path is exercised only by the env-gated smoke.

## Acceptance Rules

- **AC-GCP-001 (normal)**: target with `gcp_cloudsql_iam`, `verify-full`,
  and a PG role mapped to the IAM principal connects with **no password in
  config**; a token is acquired and applied; `ExpiresAt` is the token's
  expiry. The minter is called for the fixed scope.
- **AC-GCP-002 (boundary — cache & refresh)**: within the refresh skew the
  cached token is reused; once it reaches its skew window it is
  re-acquired; a token cached for one target is never presented for
  another (distinct cache key).
- **AC-GCP-003 (invalid)**: `gcp_cloudsql_iam` + a password source aborts
  startup with an actionable error (FC-GCP-003).
- **AC-GCP-004 (boundary — TLS floor)**: `gcp_cloudsql_iam` +
  `sslmode=require` (or weaker) aborts startup in every environment
  (FC-GCP-004), on the direct-libpq path.
- **AC-GCP-005 (failure — mint denied)**: token denial/timeout fails the
  target's connection with a redacted, actionable error; the token never
  appears in any output surface (FC-GCP-001 + INV002).
- **AC-GCP-006 (failure — identity)**: with no resolvable ADC identity (or
  denied impersonation) the target fails with an actionable error naming
  the ADC chain and the impersonation remediation (FC-GCP-005); other
  targets keep collecting.
- **AC-GCP-007 (normal — metadata only)**: a successful resolution logs
  `auth_method`, db_user, scope, resolved_at, expires_at — never the token
  (INV002/INV007).
- **AC-GCP-008 (live smoke, env-gated)**: with
  `SIGNALS_INTEGRATION_LIVE=1` against a real Cloud SQL for PostgreSQL
  instance whose role is mapped to the test principal, the collector
  connects passwordlessly and collects at least one snapshot; the token is
  re-acquired across a reconnect that crosses the refresh skew. Not run in
  default CI.
- **AC-GCP-009 (operator guidance)**: when the connection fails because
  the DB role is not mapped to an IAM principal, `signalsctl` surfaces the
  exact `gcloud sql users create <user> --instance=<inst> --type=cloud_iam_service_account`
  (or `cloud_iam_user`) guidance and the `GRANT`/role note for the target.
  (UX may be refined alongside #99; the snippet text is owned here.)

## Proposed Tests (derived; written failing before implementation)

| Test | Maps to | Kind |
|---|---|---|
| token acquired, `ExpiresAt` set, applied as password; fixed scope used | AC-GCP-001 | unit (fake minter) |
| reuse within skew; re-acquire after skew; per-target isolation | AC-GCP-002 | unit (fake minter + clock) |
| startup rejects `gcp_cloudsql_iam` + password source | AC-GCP-003 | unit (config validation) |
| startup rejects `gcp_cloudsql_iam` + non-`verify-full` sslmode | AC-GCP-004 | unit (config validation) |
| mint error → redacted actionable error; no token in log/err | AC-GCP-005 | unit (fake minter error + log capture) |
| identity unresolved/impersonation denied → actionable error | AC-GCP-006 | unit |
| success logs metadata, never the token | AC-GCP-007 | unit (log capture) |
| live passwordless connect + re-acquire across reconnect | AC-GCP-008 | smoke (env-gated, build-tagged) |
| IAM-user create / grant guidance emitted on mapping-missing failure | AC-GCP-009 | unit |

## Safety Impact

- [x] Read-only enforcement — preserved (keystone INV005); no change.
- [x] Credential handling — new token credential under never-store /
  never-log (INV002/INV007).
- [x] Network behavior — new, explicit, operator-selected outbound calls
  to the Google ADC / OAuth2 token endpoint (NFR001/NFR003).

## Resolved design decision — connection path & identity

_(Confirmed 2026-06-16: direct libpq + `verify-full`, and the optional
`gcp_impersonate_service_account` field. This resolves the connection-path
decision the keystone explicitly defers to #96.)_

- **Connection path**: direct libpq + `verify-full` (token as password),
  reusing the existing seam — recommended for uniformity with `aws_rds_iam`
  and `azure_entra` and minimal dependency weight. The Cloud SQL Go
  Connector is the documented alternative (no operator CA management, at
  the cost of a heavier dependency, a dialer replacement, and an
  `instance_connection_name` input).
- **Token scope**: fixed at `https://www.googleapis.com/auth/sqlservice.login`
  (narrower than `cloud-platform`).
- **Identity**: Application Default Credentials chain; optional
  `gcp_impersonate_service_account` disambiguates / impersonates when one
  collector serves instances owned by different service accounts.
- **At connect time**: if no usable identity can be resolved (or
  impersonation is denied), the target fails with FC-GCP-005. The failure
  is **target-scoped** and MUST NOT stop collection for unrelated targets.
