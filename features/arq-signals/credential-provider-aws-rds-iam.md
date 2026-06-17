# Feature Specification: AWS RDS/Aurora IAM Credential Provider

- **Spec ID prefix:** `ARQ-SIGNALS-AUTH-AWS-`
- **Lifecycle status:** `ACTIVE`
- **Tracking issue:** [#94](https://github.com/Elevarq/Arq-Signals/issues/94)
- **Derives from:** `credential-providers.md` (ACTIVE, #93). This spec is a
  behavioral sub-spec; it MUST conform to that abstraction's interface,
  invariants (INV001–INV007), failure taxonomy, and resolved design
  decisions. Where this spec is silent, the keystone governs.
- **Type:** Behavioral + Integration-mapping (AWS identity + RDS auth-token endpoint)

## Purpose

Implement the `aws_rds_iam` credential provider: connect Elevarq Signals
to Amazon RDS / Aurora PostgreSQL **passwordlessly**, using a short-lived
RDS IAM authentication token minted from the collector's ambient AWS
identity (EC2 instance profile, ECS task role, EKS IRSA / Pod Identity,
or the local AWS credential chain in dev). The token *is* the connection
password; no secret is stored in Signals' configuration.

Because this is the first provider to land, it also introduces the
**shared scaffolding** the abstraction requires — the `auth_method`
target field, the `CredentialProvider` interface, the `password` provider
that wraps today's `ResolvePassword`, the per-target token cache, and the
config-validation rules. Those shared pieces are specified by the
keystone; this spec only adds the AWS-specific behavior and is the first
consumer of the scaffolding. Sibling providers (#95–#98) reuse it.

## Inputs

- **`auth_method: aws_rds_iam`** (per target) — selects this provider.
- **`user`** (existing field) — the PostgreSQL role that has been granted
  `rds_iam` on the instance. Used verbatim as the token's DB user.
- **`region`** (new, optional, under the method's config) — AWS region of
  the target instance. When omitted, resolved in order:
  1. explicit `region` config value,
  2. `AWS_REGION` / `AWS_DEFAULT_REGION` environment,
  3. EC2/ECS instance metadata (IMDS) region.
  If none resolve, the target fails with an actionable error (FC-AWS-005).
- **Ambient AWS identity** — discovered via the AWS SDK default credential
  chain. Never configured as a secret in Signals.
- **`host`, `port`, `dbname`, `sslmode`, `sslrootcert_file`** — existing
  fields, unchanged. `sslmode` must satisfy INV003 (see FC-AWS-004).

## Outputs

- A resolved **password-kind** `Credential` whose `Password` is the RDS
  IAM auth token and whose `ExpiresAt` is the token's expiry (AWS RDS IAM
  tokens are valid 15 minutes from minting).
- Credential **metadata only** to logs / status surfaces (INV007):
  `auth_method=aws_rds_iam`, resolved `region`, `db_user`, `resolved_at`,
  `expires_at`. The token value is never emitted.

## Interfaces

### Provider contract (conforms to keystone)

```
awsRDSIAMProvider implements CredentialProvider:
  Resolve(ctx, target) -> (Credential{Kind: password, Password: <token>,
                                      ExpiresAt: <mint+15m>}, error)
```

- Selected at startup when `auth_method == aws_rds_iam`; invoked from the
  existing `BeforeConnect` hook (and from `BuildSafeDSN` for doctor/conntest).
- Token minting is behind a **seam interface** (a `tokenMinter` the
  provider depends on) so unit tests inject a fake and no test makes a
  real AWS call. The production implementation calls the AWS SDK v2 RDS
  auth-token builder.

### Integration mapping — AWS

| Concern | Binding |
|---|---|
| Identity | AWS SDK v2 default credential chain (env / shared config / instance profile / ECS task role / IRSA / Pod Identity) |
| Token mint | RDS IAM auth token for endpoint `host:port`, region, db user; presigned, 15-minute validity |
| Applied as | `ConnConfig.Password` (password kind) |
| Transport | TLS `verify-full` required (INV003); token never sent over weak/unverified TLS |

## Invariants (AWS-specific; keystone invariants also apply)

- **ARQ-SIGNALS-AUTH-AWS-INV001**: A target with `auth_method:
  aws_rds_iam` carries no password source (`password_file` /
  `password_env` / `pgpass_file`). (Instance of keystone INV001;
  enforced at startup by FC-AWS-003.)
- **ARQ-SIGNALS-AUTH-AWS-INV002**: The minted token is never written to
  logs, errors, audit, metrics, the local DB, or exports — only its
  metadata (region, db_user, resolved_at, expires_at). (Instance of
  keystone INV002/INV007.)
- **ARQ-SIGNALS-AUTH-AWS-INV003**: The token is cached per target and
  re-minted before expiry at `max(60s, min(5m, 15m*0.20))` = **3 minutes**
  before the 15-minute expiry (i.e. re-mint once a cached token is ~12
  minutes old). Cache key = `target_id` + `auth_method` + `db_user` +
  host/instance identity (reuses `TargetConfig.ConnIdentity()` plus the
  method). Never shared across targets. (Instance of keystone NFR001.)

## Failure Conditions

- **FC-AWS-001 (mint failure)**: STS / token builder returns an error or
  times out → the target's connection attempt fails with a **redacted**,
  actionable error (operation + status class, never the token or
  credentials). No partial token cached. Other targets unaffected.
  (Keystone FC003.)
- **FC-AWS-002 (expired at connect)**: a cached token is within the
  3-minute skew or already expired at use → re-mint before handing to
  pgx; if re-mint fails, treat as FC-AWS-001. A token is never knowingly
  presented expired. (Keystone FC004.)
- **FC-AWS-003 (stored secret on token method)**: `aws_rds_iam` combined
  with any password source → **hard config error at startup** naming the
  target and stating the method is passwordless. (Keystone FC005.)
- **FC-AWS-004 (TLS too weak)**: `aws_rds_iam` on a target whose effective
  `sslmode` is not `verify-full` → **hard config error at startup**,
  regardless of `env`, naming the target and the required mode. (Keystone
  FC006.)
- **FC-AWS-005 (region unresolved)**: no region from config, environment,
  or IMDS → **connect-time, target-scoped** failure with an actionable
  error naming the sources tried. Other targets keep collecting. At
  startup, a missing config+env region produces a **warning only**, never
  a hard error (see "Resolved design decision — region resolution").
  (Specialisation of keystone FC002.)
- **FC-AWS-006 (identity undiscoverable)**: the AWS credential chain
  yields no usable identity → connect-time failure for that target with
  an actionable error naming the chain; daemon continues for other
  targets. (Keystone FC002.)

## Non-Functional Requirements

- **ARQ-SIGNALS-AUTH-AWS-NFR001 (dependency hygiene)**: Use AWS SDK for Go
  v2 (`config` + the RDS auth-token helper) at pinned versions; the
  additions MUST pass the repo's Trivy / govulncheck gates. The AWS SDK
  is linked only on the AWS provider's path — core collection paths that
  don't use `aws_rds_iam` must not require AWS credentials at runtime.
- **ARQ-SIGNALS-AUTH-AWS-NFR002 (latency)**: steady-state reconnects
  reuse the cached token (no mint); a cold mint completes within the
  existing per-target connection budget.
- **ARQ-SIGNALS-AUTH-AWS-NFR003 (no test-time AWS calls)**: unit tests use
  the injected `tokenMinter` fake and make no real AWS or network calls,
  consistent with the repo's "no hidden external network calls" safety
  principle. The live path is exercised only by the env-gated smoke.

## Acceptance Rules

- **AC-AWS-001 (normal)**: target with `aws_rds_iam`, `verify-full`, a
  resolvable region, and a DB role granted `rds_iam` connects with **no
  password in config**; a token is minted and applied; `ExpiresAt` is
  ~15 minutes ahead.
- **AC-AWS-002 (boundary — cache & refresh)**: within the 3-minute skew
  the cached token is reused; once a cached token reaches ~12 minutes old
  it is re-minted before the next connection; a token cached for one
  target is never presented for another (distinct cache key).
- **AC-AWS-003 (invalid)**: `aws_rds_iam` + `password_file` (or
  `_env`/`pgpass_file`) aborts startup with an actionable error
  (FC-AWS-003).
- **AC-AWS-004 (boundary — TLS floor)**: `aws_rds_iam` + `sslmode=require`
  (or weaker) aborts startup in every environment (FC-AWS-004).
- **AC-AWS-005 (failure — mint denied)**: token mint denial/timeout fails
  the target's connection with a redacted, actionable error; the token /
  AWS credentials never appear in any output surface (FC-AWS-001 +
  INV002).
- **AC-AWS-006 (failure — region/identity)**: with no resolvable region
  the target fails with an actionable error listing the sources tried
  (FC-AWS-005); with no AWS identity the target fails per FC-AWS-006 and
  other targets keep collecting.
- **AC-AWS-007 (normal — metadata only)**: a successful resolution logs
  `auth_method`, region, db_user, resolved_at, expires_at — and never the
  token (INV002/INV007).
- **AC-AWS-008 (live smoke, env-gated)**: with
  `ARQ_SIGNALS_INTEGRATION_LIVE=1` against a real RDS/Aurora instance
  whose role has `rds_iam`, the collector connects passwordlessly and
  collects at least one snapshot; the token is re-minted across a
  reconnect that crosses the refresh skew. Not run in default CI.
- **AC-AWS-009 (operator guidance)**: when the connection fails because
  the DB role lacks `rds_iam` (or the IAM principal is unmapped),
  `arqctl` surfaces the exact `GRANT rds_iam TO "<user>"` and the minimal
  IAM `rds-db:connect` policy snippet for the target. (UX detail may be
  refined alongside #99; the grant/policy text is owned here.)

## Proposed Tests (derived; written failing before implementation)

| Test | Maps to | Kind |
|---|---|---|
| token resolved, `ExpiresAt` set, applied as password | AC-AWS-001 | unit (fake minter) |
| reuse within skew; re-mint after 12m; per-target isolation | AC-AWS-002 | unit (fake minter + clock) |
| startup rejects `aws_rds_iam` + password source | AC-AWS-003 | unit (config validation) |
| startup rejects `aws_rds_iam` + non-`verify-full` sslmode | AC-AWS-004 | unit (config validation) |
| mint error → redacted actionable error; no token in log/err | AC-AWS-005 | unit (fake minter returns error + log capture) |
| region resolver order; none → actionable error | AC-AWS-006 | unit (env fixtures) |
| success logs metadata, never the token | AC-AWS-007 | unit (log capture) |
| live passwordless connect + re-mint across reconnect | AC-AWS-008 | smoke (env-gated, build-tagged) |
| grant/policy snippet emitted on `rds_iam`-missing failure | AC-AWS-009 | unit |

## Safety Impact

- [x] Read-only enforcement — preserved (INV005 of keystone); no change.
- [x] Credential handling — new token credential under never-store /
  never-log (INV002/INV007).
- [x] Network behavior — new, explicit, operator-selected outbound calls
  to the AWS identity / RDS auth endpoint (NFR001/NFR003).

Per the repo safety rule, this spec + its derived failing tests land
before implementation; the spec reaches `ACTIVE` (and acceptance cases
are added to `acceptance-tests.md` / `traceability.md`) as the tests are
committed.

## Resolved design decision — region resolution

Confirmed at promotion to `ACTIVE`:

- If `region` is **explicitly configured**, use it.
- If **omitted**, resolve via the normal AWS SDK / default-environment /
  IMDS mechanisms (`AWS_REGION` / `AWS_DEFAULT_REGION` → instance
  metadata).
- **At startup**: if `region` is neither configured nor present in the
  environment, emit a **warning only** — do **not** fail the whole
  collector (fail-soft). The warning states region will be resolved from
  instance metadata at connect time.
- **At connect time**: if the region still cannot be resolved, fail
  **that target** with FC-AWS-005. The failure is **target-scoped** and
  MUST NOT stop collection for unrelated targets.

This split is reflected in FC-AWS-005 (connect-time, target-scoped) and
in the startup warning path (config validation).
