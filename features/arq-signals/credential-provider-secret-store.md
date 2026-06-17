# Feature Specification: Secret-Store Credential Provider

- **Spec ID prefix:** `ARQ-SIGNALS-AUTH-SECRET-`
- **Lifecycle status:** `ACTIVE`
- **Tracking issue:** [#97](https://github.com/Elevarq/Arq-Signals/issues/97)
- **Derives from:** `credential-providers.md` (ACTIVE, #93). This spec is a
  behavioral sub-spec; it MUST conform to that abstraction's interface,
  invariants (INV001–INV007), failure taxonomy (notably FC003 fetch
  failure, FC007 missing method config), and RULE008. Where this spec is
  silent, the keystone governs.
- **Type:** Behavioral + Integration-mapping (three vault backends:
  AWS Secrets Manager, Azure Key Vault, GCP Secret Manager)

## Purpose

Implement the `secret_store` credential provider: connect Elevarq Signals
to **self-managed PostgreSQL running in a cloud** using a database password
that lives in a cloud secret store rather than in Signals' configuration.
The operator points a target at a **secret reference** (an AWS Secrets
Manager ARN, an Azure Key Vault secret URI, or a GCP Secret Manager
resource name); at connection time the provider fetches the secret using
the collector's **ambient workload identity** and uses the fetched value as
the connection password. No password is ever stored in Signals' config.

This is the fourth provider on the credential-provider keystone (#93),
after `aws_rds_iam` (#94), `azure_entra` (#95), and `gcp_cloudsql_iam`
(#96). It reuses the shared scaffolding (the `auth_method` target field,
the `credentialResolver` dispatch wired into `BeforeConnect`, the
per-target credential cache, and the config-validation rules) and adds
only the secret-store-specific behavior: a vault-fetch seam, backend
routing inferred from the reference shape, optional JSON-key extraction,
TTL-/`max_cache_ttl`-bounded caching, the `secret_store` validation
branch, and the operator-guidance text.

Unlike the token providers, the fetched secret is a **static password**
that changes only when the operator rotates it; rotation is picked up via
the keystone's fresh-read-on-reconnect seam (INV004), not via short-lived
token expiry.

## Backend routing (confirmed decision — inferred from `secret_ref`)

The backend is **inferred from the shape of `secret_ref`**; there is no
separate provider-selector field. The reference forms are mutually
unambiguous:

| `secret_ref` shape | Backend |
|---|---|
| `arn:aws:secretsmanager:<region>:<acct>:secret:<name>` | AWS Secrets Manager |
| `https://<vault-name>.vault.azure.net/secrets/<name>[/<version>]` | Azure Key Vault |
| `projects/<proj>/secrets/<name>/versions/<version|latest>` | GCP Secret Manager |

A `secret_ref` that matches none of these shapes is a **hard config error
at startup** (FC-SECRET-007) naming the three accepted forms. Inference is
total and deterministic: a given reference routes to exactly one backend or
is rejected. For AWS, the ARN's region segment additionally pins the
Secrets Manager endpoint — it is never taken from the environment (see
Integration mapping — vault backends).

## Inputs

- **`auth_method: secret_store`** (per target) — selects this provider.
- **`secret_ref`** (new, required) — the vault reference, in one of the
  three forms above. Its shape selects the backend. Absent → FC-SECRET-007.
  The reference is **not** a secret (it names, but does not contain, the
  credential) and MAY appear in logs/status.
- **`secret_json_key`** (new, optional) — when set, the fetched secret
  value is parsed as a JSON object and this key's string value is used as
  the password (e.g. `secret_json_key: password` for an AWS RDS-managed
  secret `{"username":"…","password":"…"}`). When unset, the fetched value
  is used verbatim as the password.
- **`max_cache_ttl`** (new, optional duration) — bounds how long a fetched
  secret may be reused between reconnects when the vault supplies no
  TTL/lease of its own (see keystone INV004 / ARQ-SIGNALS-AUTH-SECRET-INV003).
- **`user`** (existing field) — the PostgreSQL role to authenticate as. The
  fetched secret (or its extracted JSON key) is the password for this role.
- **Ambient cloud identity** — the provider authenticates to the vault with
  the collector's workload identity (AWS instance profile / IRSA / Pod
  Identity; Azure Managed Identity / default credential; GCP Workload
  Identity / ADC), discovered from the environment. Never configured as a
  secret in Signals.
- **`host`, `port`, `dbname`, `sslmode`, `sslrootcert_file`** — existing
  fields. `sslmode` MUST be `verify-full` (see FC-SECRET-006).

## Outputs

- A resolved **password-kind** `Credential` whose `Password` is the fetched
  secret value (or the extracted `secret_json_key`). `ExpiresAt` is the
  vault-supplied TTL/lease expiry when present; otherwise the cache bound
  derived from `max_cache_ttl` (or zero — re-fetch each reconnect — when
  neither is set).
- Credential **metadata only** to logs / status surfaces (INV007):
  `auth_method=secret_store`, the inferred backend, the (non-secret)
  `secret_ref`, resolved `db_user`, `resolved_at`, a `ttl_present` boolean
  (and `expires_at` when a TTL is known), and whether a JSON key was
  extracted. The secret value is **never** emitted.

## Interfaces

### Provider contract (conforms to keystone)

```
secretStoreProvider implements CredentialProvider:
  Resolve(ctx, target) -> (Credential{Kind: password, Password: <secret>,
                                      ExpiresAt: <ttl-derived bound>}, error)
```

- Selected at startup when `auth_method == secret_store`; invoked from the
  existing `BeforeConnect` hook (and `BuildSafeDSN` for doctor/conntest).
- The vault fetch is behind a **seam interface** (a `secretFetcher`) so
  unit tests inject a fake and no test makes a real cloud call. The seam is
  selected per inferred backend; the production implementations use the AWS
  Secrets Manager SDK, the Azure Key Vault `azsecrets` SDK, and the GCP
  Secret Manager SDK respectively.

### Integration mapping — vault backends

| Backend | Identity | Fetch API | IAM permission |
|---|---|---|---|
| AWS Secrets Manager | instance profile / IRSA / Pod Identity | `GetSecretValue` | `secretsmanager:GetSecretValue` |
| Azure Key Vault | Managed Identity / default credential | `GetSecret` | Key Vault Secrets User (get) |
| GCP Secret Manager | Workload Identity / ADC | `AccessSecretVersion` | `secretmanager.versions.access` |

In all three the fetched payload is applied as `ConnConfig.Password`
(password kind). Transport to PostgreSQL is TLS `verify-full` (INV003,
applied to `secret_store` by the confirmed decision below).

**AWS region — derived authoritatively from the ARN.** For an AWS Secrets
Manager `secret_ref`, the region segment of the ARN
(`arn:aws:secretsmanager:<region>:<acct>:secret:<name>`) selects the
Secrets Manager endpoint. The provider MUST NOT consult `AWS_REGION`,
`AWS_DEFAULT_REGION`, the SDK's default region-resolution chain, or IMDS
region lookup for endpoint selection — the ARN is the single source of
truth. Ambient workload identity is still resolved normally for
*authentication*; only the *region/endpoint* is pinned, from the ARN. An
ARN whose region segment is empty or malformed is a startup error
(FC-SECRET-007). This deliberately differs from `aws_rds_iam` (#94), where
the region is fail-soft (config → env → IMDS): for `secret_store` the
region is never ambient, so there is no region warning and no IMDS
dependency.

## Invariants (secret-store-specific; keystone invariants also apply)

- **ARQ-SIGNALS-AUTH-SECRET-INV001**: A target with `auth_method:
  secret_store` carries no inline password source (`password_file`,
  `password_env`, `pgpass_file`) — the password comes only from the vault.
  (Keystone INV001; enforced by FC-SECRET-005.)
- **ARQ-SIGNALS-AUTH-SECRET-INV002**: The fetched secret is never written
  to logs, errors, audit, metrics, the local DB, or exports — only its
  metadata. (Keystone INV002/INV007.) The `secret_ref` itself is a
  non-secret reference and MAY be logged.
- **ARQ-SIGNALS-AUTH-SECRET-INV003 (cache / TTL / rotation)**: The secret
  is re-resolved on every new physical connection, so a rotated secret is
  picked up on the next reconnect without a daemon restart. A cached secret
  MUST NOT be reused past its bound, where the bound is:
  `min(vault TTL/lease if present, max_cache_ttl if set)`. When the vault
  supplies no TTL and `max_cache_ttl` is unset, the secret is **re-fetched
  on every reconnect** (cache bound = 0). Cache key = `target_id` +
  `auth_method` + `db_user` + host identity; never shared across targets.
  (Keystone INV004 / NFR001 / RULE008.)
- **ARQ-SIGNALS-AUTH-SECRET-INV004 (verify-full floor)**: A `secret_store`
  target's effective `sslmode` MUST be `verify-full`, in every environment.
  This applies the keystone INV003 transport floor (defined there for token
  methods) to `secret_store` as well — a confirmed local strengthening, not
  a weakening of any keystone rule. Enforced by FC-SECRET-006.
- **ARQ-SIGNALS-AUTH-SECRET-INV005 (backend isolation)**: Only the SDK for
  the inferred backend is invoked for a given target; a non-`secret_store`
  target invokes no vault SDK at all.

## Failure Conditions

- **FC-SECRET-001 (fetch failure)**: the vault returns an error or times
  out (access denied, secret not found, throttling, network) → the target's
  connection attempt fails with a **redacted**, actionable error naming the
  backend and the required IAM permission. No partial/stale secret is
  presented. Other targets unaffected. (Keystone FC003.)
- **FC-SECRET-002 (identity undiscoverable)**: no ambient workload identity
  can authenticate to the vault → **connect-time, target-scoped** failure
  with an actionable error naming the identity sources tried. Other targets
  keep collecting. (Keystone FC002.)
- **FC-SECRET-003 (payload extraction failure)**: `secret_json_key` is set
  but the fetched value is not valid JSON, or the key is absent, or its
  value is not a string → target-scoped fetch-time error. The raw secret
  value is **not** included in the error. (Subcase of keystone FC003.)
- **FC-SECRET-004 (empty secret)**: the fetched value (or extracted key) is
  empty → target-scoped fetch-time error; an empty password is never
  presented to pgx.
- **FC-SECRET-005 (inline password source)**: `secret_store` + any inline
  password source → **hard config error at startup** naming the target and
  stating the password must come from the vault. (Keystone FC005.)
- **FC-SECRET-006 (TLS too weak)**: `secret_store` on a target whose
  effective `sslmode` is not `verify-full` → **hard config error at
  startup**, regardless of `env`. (Keystone FC006 / INV003.)
- **FC-SECRET-007 (missing/unrecognised reference)**: `secret_store`
  without `secret_ref`, or with a `secret_ref` that matches none of the
  three accepted shapes → **hard config error at startup** naming the
  accepted forms. (Keystone FC007.)

## Non-Functional Requirements

- **ARQ-SIGNALS-AUTH-SECRET-NFR001 (dependency hygiene)**: the AWS Secrets
  Manager, Azure Key Vault (`azsecrets`), and GCP Secret Manager SDKs are
  pinned and MUST pass Trivy / govulncheck gates. Each backend SDK is
  linked only on this provider's path; non-`secret_store` targets require
  no vault credentials at runtime. (AWS SDK config / Azure `azidentity` are
  already vendored by #94/#95.)
- **ARQ-SIGNALS-AUTH-SECRET-NFR002 (latency)**: steady-state reconnects
  reuse a cached secret within its bound; a cold fetch completes within the
  per-target connection budget. With no cache bound, the per-reconnect
  fetch cost is the documented trade-off for immediate rotation pickup.
- **ARQ-SIGNALS-AUTH-SECRET-NFR003 (no test-time cloud calls)**: unit tests
  use the injected `secretFetcher` fake and make no real cloud or network
  calls. The live path is exercised only by the env-gated smoke.

## Acceptance Rules

- **AC-SECRET-001 (normal)**: a target with `secret_store`, `verify-full`,
  and a `secret_ref` for each backend (ARN / Key Vault URI / Secret Manager
  resource) connects with **no password in config**; the secret is fetched
  from the inferred backend and applied as the password.
- **AC-SECRET-002 (boundary — backend inference)**: each reference shape
  routes to the correct backend fetcher; an unrecognised shape aborts
  startup with an actionable error (FC-SECRET-007).
- **AC-SECRET-003 (boundary — JSON key)**: with `secret_json_key` set, the
  value is parsed as JSON and the named key extracted; without it, the raw
  value is used; invalid JSON / missing key / non-string value fails the
  fetch with an error that does not leak the raw value (FC-SECRET-003).
- **AC-SECRET-004 (boundary — cache, TTL & rotation)**: a vault-supplied
  TTL bounds reuse; `max_cache_ttl` bounds reuse when no vault TTL is
  present; with neither set the secret is re-fetched on each reconnect; a
  rotated secret is observed on the next reconnect; a secret cached for one
  target is never presented for another (distinct cache key).
- **AC-SECRET-005 (invalid — inline password source)**: `secret_store` + a
  password source aborts startup with an actionable error (FC-SECRET-005).
- **AC-SECRET-006 (invalid — TLS floor)**: `secret_store` +
  `sslmode=require` (or weaker) aborts startup in every environment
  (FC-SECRET-006).
- **AC-SECRET-007 (invalid — missing reference)**: `secret_store` without
  `secret_ref` aborts startup with an actionable error (FC-SECRET-007).
- **AC-SECRET-008 (failure — fetch denied)**: a vault fetch error/timeout
  fails the target's connection with a redacted, actionable error naming
  the backend and IAM permission; the secret never appears in any output
  surface; other targets keep collecting (FC-SECRET-001 + INV002).
- **AC-SECRET-009 (failure — identity)**: with no resolvable workload
  identity for the vault, the target fails with an actionable error naming
  the identity sources tried (FC-SECRET-002); other targets keep
  collecting.
- **AC-SECRET-010 (normal — metadata only)**: a successful resolution logs
  `auth_method`, backend, `secret_ref`, db_user, resolved_at, ttl_present —
  never the secret value (INV002/INV007).
- **AC-SECRET-011 (live smoke, env-gated — required content)**: with
  `SIGNALS_INTEGRATION_LIVE=1` against a real secret store and a
  self-managed PostgreSQL whose role's password is the stored secret, the
  collector **fetches the secret from the vault, connects passwordlessly,
  and collects at least one snapshot**. This is the mandatory live-
  validation path. Documented per backend; not run in default CI.
- **AC-SECRET-012 (operator guidance)**: when the fetch fails for
  permission/identity reasons, `signalsctl` surfaces the exact IAM grant for
  the inferred backend (`secretsmanager:GetSecretValue` / Key Vault Secrets
  User / `secretmanager.versions.access`) and the workload-identity note.
  (UX may be refined alongside #99; the snippet text is owned here.)
- **AC-SECRET-013 (live rotation validation, env-gated — optional)**: as an
  additional, opt-in step beyond AC-SECRET-011, rotate the stored secret,
  force a reconnect, and verify the new secret is picked up without a daemon
  restart (INV004). Rotation testing is operationally heavier — it mutates
  the live vault — and is **NOT required** for every live-validation
  environment. Rotation-on-reconnect remains fully covered in CI by the
  unit-level AC-SECRET-004 (fake fetcher + clock), so omitting this live
  step does not reduce guaranteed rotation coverage.

## Proposed Tests (derived; written failing before implementation)

| Test | Maps to | Kind |
|---|---|---|
| secret fetched & applied as password, per backend (fake fetcher) | AC-SECRET-001 | unit (fake fetcher) |
| each ref shape routes to the right backend; bad shape rejected at startup | AC-SECRET-002 / FC-SECRET-007 | unit (routing + config validation) |
| JSON-key extraction; raw default; invalid/missing key fails without leaking value | AC-SECRET-003 | unit |
| vault TTL bound; `max_cache_ttl` bound; no-TTL+no-max re-fetches; rotation on reconnect; per-target isolation | AC-SECRET-004 | unit (fake fetcher + clock) |
| startup rejects `secret_store` + inline password source | AC-SECRET-005 | unit (config validation) |
| startup rejects `secret_store` + non-`verify-full` sslmode | AC-SECRET-006 | unit (config validation) |
| startup rejects `secret_store` without `secret_ref` | AC-SECRET-007 | unit (config validation) |
| fetch error → redacted actionable error; no secret in log/err; sibling unaffected | AC-SECRET-008 | unit (fake fetcher error + log capture) |
| identity unresolved → actionable error; sibling unaffected | AC-SECRET-009 | unit |
| success logs metadata, never the secret | AC-SECRET-010 | unit (log capture) |
| live fetch + passwordless connect + one snapshot (required) | AC-SECRET-011 | smoke (env-gated, build-tagged) |
| IAM-grant guidance emitted on permission failure | AC-SECRET-012 | unit |
| live secret rotation + reconnect + pickup (optional) | AC-SECRET-013 | smoke (env-gated, build-tagged, opt-in) |

## Safety Impact

- [x] Read-only enforcement — preserved (keystone INV005); no change.
- [x] Credential handling — fetched DB password under never-store /
  never-log (INV002/INV007); no inline password permitted (INV001).
- [x] Network behavior — new, explicit, operator-selected outbound calls to
  the inferred vault backend (NFR001/NFR003). No call on non-`secret_store`
  targets.

## Resolved design decisions — backend routing, payload shape, TLS floor

_(Confirmed 2026-06-16, #97.)_

1. **Backend routing — inferred from `secret_ref` shape** (ARN / Key Vault
   URI / Secret Manager resource). No separate provider-selector field; the
   reference forms are unambiguous, so there is no way to mismatch a
   provider against a reference. An unrecognised shape is FC-SECRET-007.
2. **Payload shape — raw string by default, optional `secret_json_key`.**
   The fetched value is the password verbatim unless `secret_json_key` is
   set, in which case the value is parsed as JSON and that key extracted —
   covering both plain secrets (Azure/GCP/manual AWS) and AWS RDS-managed
   JSON secrets `{"username":…,"password":…}`.
3. **TLS floor — `verify-full` required** (INV004 above). The fetched
   secret is high-value in transit, so `secret_store` adopts the same
   transport floor the keystone INV003 mandates for token methods. This is
   a local strengthening (stricter than the keystone baseline for password
   methods), permitted because sub-specs may not weaken the keystone but
   may add stricter guarantees.
