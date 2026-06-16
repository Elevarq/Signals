# Feature Specification: Credential Providers (`auth_method`)

- **Spec ID prefix:** `ARQ-SIGNALS-AUTH-`
- **Lifecycle status:** `DRAFT`
- **Tracking issue:** [#93](https://github.com/Elevarq/Arq-Signals/issues/93)
  (keystone of epic [#92](https://github.com/Elevarq/Arq-Signals/issues/92))
- **Type:** Behavioral + Integration-mapping (cloud identity endpoints)

> This is the keystone specification for epic #92. It MUST reach
> `ACTIVE` before any of the implementation sub-issues (#94–#101) begin.
> The provider implementations each derive their own behavioral
> sub-spec from the abstraction defined here.

## Purpose

Elevarq Signals connects to PostgreSQL with a read-only least-privilege
role. Today the only way to supply that role's credential is a password
read from a file, an environment variable, or a pgpass file
(see Appendix B § Credential sources). For enterprise databases hosted on
AWS, Azure, and GCP, the simplest *and* most secure connection is
**passwordless** — the workload's cloud identity mints a short-lived
token that is the credential, so no secret is ever stored in Signals'
configuration.

This specification defines a **credential-provider abstraction**: a
single per-target `auth_method` selector and a provider interface that
resolves the connection credential at connect time. It generalises the
existing password resolution into one of several interchangeable
providers without changing the read-only safety model, and it preserves
the rotation-on-reconnect behaviour the daemon already relies on.

The abstraction is the contract; the individual providers
(`aws_rds_iam`, `azure_entra`, `gcp_cloudsql_iam`, `secret_store`,
`mtls`) are specified as derived behavioral specs in #94–#98 and MUST
conform to the interface, invariants, and failure taxonomy defined here.

## Scope

### In Scope
- A per-target `auth_method` enum and its per-method configuration block.
- The `CredentialProvider` resolution contract invoked at connection
  time (the seam the daemon already exposes via `BeforeConnect`).
- The credential kinds a provider may return (password, bearer token,
  client-certificate material) and how each is applied to the pgx
  connection.
- Auto-detection rules for selecting/validating a method from the
  ambient environment.
- The shared failure taxonomy and the shared security invariants every
  provider inherits.
- Backward-compatibility guarantees for existing password-based config.

### Out of Scope
- The concrete token-minting logic for each cloud (derived specs
  #94–#96), the vault-fetch logic (#97), and the mTLS wiring (#98).
- The guided `arqctl connect` UX (#99), IaC templates (#100), and
  end-user docs (#101) — these consume this abstraction but do not
  define it.
- Any change to which SQL collectors run or to the read-only enforcement
  model. Authentication changes *how* the connection is established,
  never *what* it is permitted to do.

## Inputs

- **`auth_method`** (per target, string enum). One of:
  `password` (default), `aws_rds_iam`, `azure_entra`,
  `gcp_cloudsql_iam`, `secret_store`, `mtls`. When omitted, the
  effective value is `password` (backward compatibility).
- **Existing connection parameters** — `host`, `port`, `dbname`, `user`,
  `sslmode`, `sslrootcert_file` (unchanged).
- **Method-specific configuration** (only the block matching the chosen
  `auth_method` is read):
  - `password` — existing `password_file` / `password_env` /
    `pgpass_file` (at most one).
  - `aws_rds_iam` — `region` (optional; inferred from environment when
    omitted). Identity comes from the ambient AWS credential chain.
  - `azure_entra` — no required fields; identity comes from the ambient
    Managed Identity / default Azure credential. Token scope is fixed
    (`https://ossrdbms-aad.database.windows.net/.default`).
  - `gcp_cloudsql_iam` — no required fields; identity comes from the
    attached service account / Application Default Credentials.
  - `secret_store` — `secret_ref` (ARN / Key Vault secret URI / Secret
    Manager resource name). The provider's own auth to the vault uses
    ambient workload identity.
  - `mtls` — `sslcert` (client cert path) and `sslkey` (client key
    path); optional key-passphrase source. (Field wiring specified in
    #98.)
- **Ambient cloud identity** — instance profile / IRSA / Pod Identity
  (AWS), Managed Identity (Azure), Workload Identity / ADC (GCP). Read
  from the environment by the provider; never configured as a secret in
  Signals.

## Outputs

- A **resolved credential** handed to pgx at connection time, of exactly
  one kind:
  - **password kind** — a string placed in `ConnConfig.Password`
    (covers `password`, token methods where the token *is* the password,
    and `secret_store`).
  - **certificate kind** — client cert + key material placed in the
    connection's TLS config (covers `mtls`).
- **Operational metrics / status only**: a provider MAY surface
  non-secret resolution outcomes (method name, success/failure,
  token-expiry timestamp, mint latency) to logs and the existing
  `collector_status` / metrics surfaces. It MUST NOT surface the
  credential value itself.

## Interfaces

### Resolution contract

The abstraction is expressed as a provider resolved once per target and
invoked on every new physical connection (the existing pgx
`BeforeConnect` seam):

```
CredentialProvider:
  Resolve(ctx, target) -> (Credential, error)

Credential:
  Kind        : password | certificate
  Password    : string         # set when Kind == password
  Certificate : cert+key bytes # set when Kind == certificate
  ExpiresAt   : time | nil      # advisory; nil for static credentials
```

- The daemon selects the provider for a target from `auth_method` at
  startup (after config validation) and invokes `Resolve` inside
  `BeforeConnect`, so every new connection in the pool re-resolves.
- `password` maps to the existing `ResolvePassword` behaviour wrapped as
  a provider — no behavioural change for existing deployments.

### Integration mapping — cloud identity endpoints

| `auth_method` | Identity source | Credential minted | Applied as |
|---|---|---|---|
| `aws_rds_iam` | AWS default credential chain (instance profile / ECS task role / IRSA / Pod Identity) | RDS SigV4 auth token (region-scoped) | password kind |
| `azure_entra` | Azure Managed Identity / default credential | Entra OAuth2 access token (scope `…ossrdbms-aad…/.default`) | password kind |
| `gcp_cloudsql_iam` | GCP service account / ADC | OAuth2 access token | password kind |
| `secret_store` | Ambient workload identity → vault | Fetched DB password from vault | password kind |
| `mtls` | Local cert/key files | n/a (no token) | certificate kind |

The exact request/response contract for each endpoint is fixed in the
corresponding derived spec; this table is the binding map between
`auth_method` values and the identity/credential model.

## Invariants

- **ARQ-SIGNALS-AUTH-INV001 (no stored secret for token methods)**: For
  `aws_rds_iam`, `azure_entra`, and `gcp_cloudsql_iam`, the target
  configuration MUST NOT contain a password, password file, or pgpass
  reference. The credential is minted from ambient identity at connect
  time and never persisted.
- **ARQ-SIGNALS-AUTH-INV002 (credential never disclosed)**: No provider
  may write a resolved password, token, fetched secret, or private-key
  material to logs, error messages, audit events, metrics, the local
  database, or any export artifact. This extends the existing
  "credentials never stored, exported, or logged" safety principle to
  tokens and certificate material. Only the SHA-256 fingerprint or the
  method name / expiry timestamp may be logged.
- **ARQ-SIGNALS-AUTH-INV003 (TLS floor for token methods)**: Any
  `auth_method` that transmits a bearer token to the server
  (`aws_rds_iam`, `azure_entra`, `gcp_cloudsql_iam`) MUST use an
  encrypted connection whose server identity is verified
  (`verify-full`, or the cloud connector that provides equivalent
  verification). A token method MUST NOT proceed over a connection that
  would expose the token (no `disable`/`allow`/`prefer`, and no
  unverified TLS), in any environment — this is stricter than the
  general `prod`-only TLS rule.
- **ARQ-SIGNALS-AUTH-INV004 (rotation on reconnect preserved)**: The
  credential is re-resolved on every new physical connection. A rotated
  secret, a refreshed token, or a replaced certificate is picked up on
  the next reconnect without a daemon restart.
- **ARQ-SIGNALS-AUTH-INV005 (read-only model untouched)**: The
  credential-provider abstraction changes only how the connection
  authenticates. It does not add write capability, does not relax role
  validation (`ValidateRoleSafety`), and does not bypass collector
  approval. A target authenticated by any method is subject to the same
  read-only enforcement as a password target.
- **ARQ-SIGNALS-AUTH-INV006 (single method per target)**: Exactly one
  `auth_method` is in effect per target, and only that method's
  configuration block is read. Configuration for a non-selected method
  is ignored (and, where it implies a stored secret on a token method,
  rejected — see FC005).

## Failure Conditions

- **ARQ-SIGNALS-AUTH-FC001 (unknown method)**: `auth_method` is not one
  of the enum values → hard config error at startup, naming the field
  and the allowed values. Daemon does not start.
- **ARQ-SIGNALS-AUTH-FC002 (identity undiscoverable)**: A token or
  secret_store method is configured but no ambient cloud identity can be
  discovered → the target's connection attempt fails with an actionable
  error (which identity source was tried). Other targets are unaffected;
  the daemon continues (consistent with per-target collection
  isolation).
- **ARQ-SIGNALS-AUTH-FC003 (credential mint/fetch failure)**: The
  identity endpoint or vault returns an error, times out, or denies the
  request → connection attempt fails with a redacted, actionable error
  (endpoint + status class, never the credential). The failure is
  recorded in the target's collector status; no partial credential is
  cached.
- **ARQ-SIGNALS-AUTH-FC004 (expired token at connect)**: A minted token
  has expired (or is within an unusable skew) at the moment of use →
  the provider re-mints before handing the credential to pgx; if
  re-mint fails, treat as FC003. A token is never knowingly presented
  expired.
- **ARQ-SIGNALS-AUTH-FC005 (stored secret on token method)**: A target
  sets a token method (`aws_rds_iam` / `azure_entra` /
  `gcp_cloudsql_iam`) together with `password_file` / `password_env` /
  `pgpass_file` → hard config error at startup (violates INV001). The
  message states that token methods are passwordless.
- **ARQ-SIGNALS-AUTH-FC006 (TLS too weak for token method)**: A token
  method is configured on a target whose effective `sslmode` does not
  meet INV003 → hard config error at startup, regardless of `env`,
  naming the target and the required mode.
- **ARQ-SIGNALS-AUTH-FC007 (missing method config)**: The selected
  method's required fields are absent (e.g. `secret_store` without
  `secret_ref`, `mtls` without `sslcert`/`sslkey`) → hard config error
  at startup naming the missing field.

## Non-Functional Requirements

- **ARQ-SIGNALS-AUTH-NFR001 (mint latency budget)**: Credential
  resolution at `BeforeConnect` MUST complete within the existing
  per-target connection budget. Token minting SHOULD be cached in memory
  and refreshed shortly *before* expiry so steady-state reconnects do
  not pay mint latency on every connection.
- **ARQ-SIGNALS-AUTH-NFR002 (minimal outbound surface)**: A provider's
  only new outbound calls are to its cloud identity / vault endpoint.
  No telemetry, no third-party calls. (Consistent with the "no hidden
  external network calls" safety principle — these calls are explicit,
  documented, and operator-selected via `auth_method`.)
- **ARQ-SIGNALS-AUTH-NFR003 (backward compatibility)**: Existing
  configurations with no `auth_method` continue to behave exactly as
  before (`password` provider). No migration step is required for
  existing deployments.
- **ARQ-SIGNALS-AUTH-NFR004 (dependency hygiene)**: Cloud SDK
  dependencies introduced by token providers MUST be pinned and pass the
  repository's security gates (Trivy/govulncheck). Where practical,
  providers SHOULD be build-tag or interface isolated so the core
  collector does not link a cloud SDK it does not use.

## Acceptance Rules

- **ARQ-SIGNALS-AUTH-RULE001**: A target with no `auth_method` connects
  using the existing password resolution, unchanged. *(normal)*
- **ARQ-SIGNALS-AUTH-RULE002**: A target with `auth_method:
  aws_rds_iam` (and `verify-full` TLS) connects to a database whose role
  is granted `rds_iam`, using a token minted from ambient identity, with
  **no password in config**, and the token is re-minted on reconnect.
  *(normal — verified live in #94)*
- **ARQ-SIGNALS-AUTH-RULE003**: A token method configured alongside a
  password source is rejected at startup with an actionable error
  (FC005). *(invalid)*
- **ARQ-SIGNALS-AUTH-RULE004**: A token method configured on a target
  with weak/unverified TLS is rejected at startup in every environment
  (FC006). *(boundary — stricter than the prod-only TLS rule)*
- **ARQ-SIGNALS-AUTH-RULE005**: When the identity endpoint is
  unreachable or denies the mint, the target's connection fails with a
  redacted, actionable error and the credential value never appears in
  any output surface (FC003 + INV002). *(failure)*
- **ARQ-SIGNALS-AUTH-RULE006**: `auth_method` set to a value outside the
  enum aborts startup naming the allowed values (FC001). *(invalid)*

## Safety Impact

This feature affects the safety model. Boxes checked:

- [x] Read-only enforcement — *unchanged and explicitly preserved
  (INV005); documented here to assert no regression.*
- [x] Role validation — *unchanged; `ValidateRoleSafety` still runs
  regardless of `auth_method` (INV005).*
- [x] Credential handling — *primary subject: new credential kinds
  (tokens, certs) brought under the existing never-store/never-log
  principle (INV001, INV002).*
- [ ] Export metadata
- [x] Network behavior — *new, explicit, operator-selected outbound
  calls to cloud identity/vault endpoints (NFR002).*

Per the repository safety rule, the safety specification and tests are
updated **before** implementation: this spec is the safety-model update,
and it must reach `ACTIVE` with derived acceptance tests before #94–#98
implement any provider.

## Traceability

```
credential-providers.md (this spec, DRAFT)
  → derived behavioral specs:
      #94 aws_rds_iam   → tests → implementation
      #95 azure_entra   → tests → implementation
      #96 gcp_cloudsql_iam → tests → implementation
      #97 secret_store  → tests → implementation
      #98 mtls          → tests → implementation
  → consumers (no new behavior defined here):
      #99 arqctl connect, #100 IaC templates, #101 docs
```

Acceptance cases for RULE001–RULE006 are added to
`acceptance-tests.md` and mapped in `traceability.md` when this spec is
promoted from `DRAFT` to `ACTIVE`.

## Open questions (resolve before ACTIVE)

1. **GCP TLS path** — does INV003 require `verify-full` directly, or is
   the Cloud SQL Go connector (which provides equivalent server-identity
   verification) an accepted satisfier? Decide in #96's derived spec and
   reflect the wording here.
2. **Token cache scope** — per-target in-memory cache vs. shared; and the
   refresh-before-expiry skew value (NFR001). Propose a default in #94
   and adopt it as the cross-provider default.
3. **`secret_store` rotation signal** — rely solely on
   reconnect-driven re-fetch (INV004), or also honour a vault-provided
   TTL to force earlier reconnect? Default to reconnect-driven for the
   first release unless #97 surfaces a concrete need.
