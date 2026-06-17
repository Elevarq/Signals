# Feature Specification: Azure Entra ID Credential Provider

- **Spec ID prefix:** `ARQ-SIGNALS-AUTH-AZURE-`
- **Lifecycle status:** `ACTIVE`
- **Tracking issue:** [#95](https://github.com/Elevarq/Arq-Signals/issues/95)
- **Derives from:** `credential-providers.md` (ACTIVE, #93). This spec is a
  behavioral sub-spec; it MUST conform to that abstraction's interface,
  invariants (INV001–INV007), failure taxonomy, and resolved design
  decisions. Where this spec is silent, the keystone governs.
- **Type:** Behavioral + Integration-mapping (Azure identity + Entra OAuth2 token endpoint)

## Purpose

Implement the `azure_entra` credential provider: connect Elevarq Signals
to **Azure Database for PostgreSQL — Flexible Server** **passwordlessly**,
using a short-lived Microsoft Entra ID (formerly Azure AD) OAuth2 access
token obtained from the collector's ambient Azure identity (Managed
Identity on Azure VMs / VMSS, AKS workload identity, or the local Azure
credential chain in dev). The access token *is* the connection password;
no secret is stored in Signals' configuration.

This is the second provider to land. It reuses the shared scaffolding
introduced by #94 (the `auth_method` target field, the
`credentialResolver` dispatch wired into `BeforeConnect`, the per-target
token cache, and the config-validation rules) and adds only the
Azure-specific behavior: an Entra token minter behind a seam, the
`azure_entra` validation branch, and the operator-guidance text.

## Inputs

- **`auth_method: azure_entra`** (per target) — selects this provider.
- **`user`** (existing field) — the PostgreSQL role mapped to the Entra
  principal via `pgaadauth_create_principal` (the role name must match
  the Entra user/group/service-principal display name). Used verbatim as
  the token's DB user.
- **`azure_client_id`** (new, optional, under the method's config) — the
  client (application) ID of a **user-assigned** Managed Identity, used
  to disambiguate when the host carries more than one. Resolution order:
  1. explicit `azure_client_id` config value,
  2. `AZURE_CLIENT_ID` environment variable,
  3. unset → the credential chain selects the system-assigned identity
     (or the single available identity).
  When the host has multiple user-assigned identities and none is
  selected, the mint fails with an actionable error (FC-AZURE-005).
- **Ambient Azure identity** — discovered via the Azure default
  credential chain (see "Resolved design decision — credential chain").
  Never configured as a secret in Signals.
- **`host`, `port`, `dbname`, `sslmode`, `sslrootcert_file`** — existing
  fields, unchanged. `sslmode` must satisfy INV003 (see FC-AZURE-004).

## Outputs

- A resolved **password-kind** `Credential` whose `Password` is the Entra
  access token and whose `ExpiresAt` is the token's expiry (the
  `ExpiresOn` returned by the token endpoint; Entra access tokens are
  typically valid 60–90 minutes).
- Credential **metadata only** to logs / status surfaces (INV007):
  `auth_method=azure_entra`, resolved `db_user`, the scope, `resolved_at`,
  `expires_at`. The token value is never emitted.

## Interfaces

### Provider contract (conforms to keystone)

```
azureEntraProvider implements CredentialProvider:
  Resolve(ctx, target) -> (Credential{Kind: password, Password: <token>,
                                      ExpiresAt: <token ExpiresOn>}, error)
```

- Selected at startup when `auth_method == azure_entra`; invoked from the
  existing `BeforeConnect` hook (and from `BuildSafeDSN` for doctor/conntest).
- Token acquisition is behind a **seam interface** (an `entraTokenMinter`
  the provider depends on) so unit tests inject a fake and no test makes
  a real Azure call. The production implementation uses the Azure SDK for
  Go (`azidentity` for the credential chain, `azcore` `GetToken` for the
  fixed scope).

### Integration mapping — Azure

| Concern | Binding |
|---|---|
| Identity | Azure default credential chain (env / workload identity / managed identity / Azure CLI) |
| Token mint | Entra OAuth2 access token for scope `https://ossrdbms-aad.database.windows.net/.default` |
| Applied as | `ConnConfig.Password` (password kind) |
| Transport | TLS `verify-full` required (INV003); token never sent over weak/unverified TLS |

The token **scope is fixed** at
`https://ossrdbms-aad.database.windows.net/.default` (the OSS RDBMS
audience) and is not operator-configurable.

## Invariants (Azure-specific; keystone invariants also apply)

- **ARQ-SIGNALS-AUTH-AZURE-INV001**: A target with `auth_method:
  azure_entra` carries no password source (`password_file` /
  `password_env` / `pgpass_file`). (Instance of keystone INV001;
  enforced at startup by FC-AZURE-003.)
- **ARQ-SIGNALS-AUTH-AZURE-INV002**: The acquired token is never written
  to logs, errors, audit, metrics, the local DB, or exports — only its
  metadata (db_user, scope, resolved_at, expires_at). (Instance of
  keystone INV002/INV007.)
- **ARQ-SIGNALS-AUTH-AZURE-INV003**: The token is cached per target and
  re-acquired before expiry at `max(60s, min(5m, ttl*0.20))` (the
  keystone cross-provider default; for a ~75-minute token this caps at
  the **5-minute** ceiling). Cache key = `target_id` + `auth_method` +
  `db_user` + host/instance identity (reuses `TargetConfig.ConnIdentity()`
  plus the method). Never shared across targets. (Instance of keystone
  NFR001.)
- **ARQ-SIGNALS-AUTH-AZURE-INV004**: The token scope is fixed at
  `https://ossrdbms-aad.database.windows.net/.default`; no code path or
  config may widen or change the audience.

## Failure Conditions

- **FC-AZURE-001 (mint failure)**: the Entra token endpoint returns an
  error or times out → the target's connection attempt fails with a
  **redacted**, actionable error (operation + status class, never the
  token or credentials). No partial token cached. Other targets
  unaffected. (Keystone FC003.)
- **FC-AZURE-002 (expired at connect)**: a cached token is within the
  refresh skew or already expired at use → re-acquire before handing to
  pgx; if re-acquire fails, treat as FC-AZURE-001. A token is never
  knowingly presented expired. (Keystone FC004.)
- **FC-AZURE-003 (stored secret on token method)**: `azure_entra`
  combined with any password source → **hard config error at startup**
  naming the target and stating the method is passwordless. (Keystone
  FC005.)
- **FC-AZURE-004 (TLS too weak)**: `azure_entra` on a target whose
  effective `sslmode` is not `verify-full` → **hard config error at
  startup**, regardless of `env`, naming the target and the required
  mode. (Keystone FC006.)
- **FC-AZURE-005 (identity undiscoverable / ambiguous)**: the Azure
  credential chain yields no usable identity, **or** the host carries
  multiple user-assigned identities and none is selected (no
  `azure_client_id` / `AZURE_CLIENT_ID`) → **connect-time, target-scoped**
  failure with an actionable error naming the chain tried and how to
  disambiguate. Other targets keep collecting. (Keystone FC002.)

## Non-Functional Requirements

- **ARQ-SIGNALS-AUTH-AZURE-NFR001 (dependency hygiene)**: Use the Azure
  SDK for Go (`azidentity` + `azcore`) at pinned versions; the additions
  MUST pass the repo's Trivy / govulncheck gates. The Azure SDK is linked
  only on the Azure provider's path — core collection paths that don't
  use `azure_entra` must not require Azure credentials at runtime.
- **ARQ-SIGNALS-AUTH-AZURE-NFR002 (latency)**: steady-state reconnects
  reuse the cached token (no acquisition); a cold acquisition completes
  within the existing per-target connection budget.
- **ARQ-SIGNALS-AUTH-AZURE-NFR003 (no test-time Azure calls)**: unit
  tests use the injected `entraTokenMinter` fake and make no real Azure
  or network calls, consistent with the repo's "no hidden external
  network calls" safety principle. The live path is exercised only by the
  env-gated smoke.

## Acceptance Rules

- **AC-AZURE-001 (normal)**: target with `azure_entra`, `verify-full`,
  and a PG role mapped to the Entra principal connects with **no password
  in config**; a token is acquired and applied; `ExpiresAt` is the
  token's expiry. The minter is called for the fixed scope and the
  configured/ambient client id.
- **AC-AZURE-002 (boundary — cache & refresh)**: within the refresh skew
  the cached token is reused; once a cached token reaches its skew window
  it is re-acquired before the next connection; a token cached for one
  target is never presented for another (distinct cache key).
- **AC-AZURE-003 (invalid)**: `azure_entra` + `password_file` (or
  `_env`/`pgpass_file`) aborts startup with an actionable error
  (FC-AZURE-003).
- **AC-AZURE-004 (boundary — TLS floor)**: `azure_entra` +
  `sslmode=require` (or weaker) aborts startup in every environment
  (FC-AZURE-004).
- **AC-AZURE-005 (failure — mint denied)**: token acquisition
  denial/timeout fails the target's connection with a redacted,
  actionable error; the token / Azure credentials never appear in any
  output surface (FC-AZURE-001 + INV002).
- **AC-AZURE-006 (failure — identity)**: with no resolvable identity (or
  ambiguous user-assigned identities and no client id) the target fails
  with an actionable error naming the chain tried and the disambiguation
  step (FC-AZURE-005); other targets keep collecting.
- **AC-AZURE-007 (normal — metadata only)**: a successful resolution logs
  `auth_method`, db_user, scope, resolved_at, expires_at — and never the
  token (INV002/INV007).
- **AC-AZURE-008 (live smoke, env-gated)**: with
  `ARQ_SIGNALS_INTEGRATION_LIVE=1` against a real Azure Database for
  PostgreSQL Flexible Server whose role is mapped to the test principal,
  the collector connects passwordlessly and collects at least one
  snapshot; the token is re-acquired across a reconnect that crosses the
  refresh skew. Not run in default CI.
- **AC-AZURE-009 (operator guidance)**: when the connection fails because
  the DB role is not mapped to an Entra principal, `arqctl` surfaces the
  exact `SELECT * FROM pgaadauth_create_principal('<user>', false, false);`
  snippet (and the note that the role name must match the Entra principal
  display name) for the target. (UX detail may be refined alongside #99;
  the snippet text is owned here.)

## Proposed Tests (derived; written failing before implementation)

| Test | Maps to | Kind |
|---|---|---|
| token acquired, `ExpiresAt` set, applied as password; fixed scope used | AC-AZURE-001 | unit (fake minter) |
| reuse within skew; re-acquire after skew; per-target isolation | AC-AZURE-002 | unit (fake minter + clock) |
| startup rejects `azure_entra` + password source | AC-AZURE-003 | unit (config validation) |
| startup rejects `azure_entra` + non-`verify-full` sslmode | AC-AZURE-004 | unit (config validation) |
| mint error → redacted actionable error; no token in log/err | AC-AZURE-005 | unit (fake minter returns error + log capture) |
| identity unresolved/ambiguous → actionable error; minter not called | AC-AZURE-006 | unit |
| success logs metadata, never the token | AC-AZURE-007 | unit (log capture) |
| live passwordless connect + re-acquire across reconnect | AC-AZURE-008 | smoke (env-gated, build-tagged) |
| `pgaadauth_create_principal` snippet emitted on mapping-missing failure | AC-AZURE-009 | unit |

## Safety Impact

- [x] Read-only enforcement — preserved (INV005 of keystone); no change.
- [x] Credential handling — new token credential under never-store /
  never-log (INV002/INV007).
- [x] Network behavior — new, explicit, operator-selected outbound calls
  to the Azure identity / Entra token endpoint (NFR001/NFR003).

Per the repo safety rule, this spec + its derived failing tests land
before implementation; the spec reaches `ACTIVE` (and acceptance cases
are added to `traceability.md`) as the tests are committed.

## Resolved design decision — credential chain

_(Confirmed 2026-06-16: `DefaultAzureCredential` + optional
`azure_client_id`.)_

- **Credential chain**: use the Azure SDK `DefaultAzureCredential`, which
  consults, in order: environment variables → AKS workload identity →
  Managed Identity (system- or user-assigned) → Azure CLI (`az login`,
  dev only). This mirrors the AWS provider's "default credential chain"
  posture and supports both production (Managed Identity / workload
  identity) and local development without configuration.
- **User-assigned Managed Identity**: disambiguated by the optional
  `azure_client_id` (config) → `AZURE_CLIENT_ID` (env). When the host has
  exactly one identity, neither is required.
- **Sovereign clouds / tenant**: the authority host and tenant are read
  from the ambient environment (`AZURE_AUTHORITY_HOST`, `AZURE_TENANT_ID`)
  by the credential chain; not Signals config fields.
- **At connect time**: if no usable identity can be resolved (or the
  user-assigned identity is ambiguous), the target fails with
  FC-AZURE-005. The failure is **target-scoped** and MUST NOT stop
  collection for unrelated targets.
