# Feature Specification: Guided onboarding — `signalsctl connect --auto`

- **Spec ID prefix:** `SIGNALS-CONNECT-`
- **Lifecycle status:** `ACTIVE`
- **Tracking issue:** [#99](https://github.com/Elevarq/Signals/issues/99)
- **Consumes:** `credential-providers.md` (ACTIVE, #93) and its derived
  provider specs (#94–#98). This is a **consumer** spec — it defines the
  guided-onboarding command's behavior over the existing `auth_method`
  abstraction; it does not define or change any provider's behavior.
- **Type:** Behavioral (CLI command + diagnostics orchestration)

## Purpose

Deliver the "click-click-click & done" onboarding experience: a single
`signalsctl connect --auto` command that takes an operator from *nothing
configured* to either a **verified, least-privilege, passwordless connection**
or an **actionable, copy-pasteable fix**. It auto-detects the cloud and ambient
identity, selects the right `auth_method`, resolves the credential, runs the
existing connection diagnostic, validates the role is read-only, and — when the
cloud-side login mapping is missing — prints the exact grant SQL / IAM binding
the operator must apply.

The command is an **orchestrator**: it reuses the existing providers
(#94–#98), the existing `doctor` connection diagnostic (C3/C4), and
`ValidateRoleSafety` (INV005). It introduces no new connection, credential, or
safety behavior — only the guided flow that sequences them and renders
guidance.

## Scope

### In scope
- A `connect --auto` subcommand of `signalsctl`.
- Auto-detection of the cloud platform + ambient identity and the resulting
  `auth_method` proposal.
- Orchestration of: select method → resolve credential → connection diagnostic
  → role-safety check → missing-grant guidance.
- Rendering a ready-to-use target config block (no secrets) on success.
- Redacted, actionable output on every failure path.

### Out of scope
- Any change to provider behavior, the credential abstraction, or the
  read-only model (those are #93–#98).
- Mutating the database or the cloud (the command never runs grants or creates
  identities — it *prints* what the operator must run).
- IaC provisioning (that is #100).

## Inputs

- **Connection target** — `--host`, `--port` (default 5432), `--dbname`,
  `--user` (required; the role to authenticate as).
- **`--auth-method`** (optional) — override the auto-detected method
  (`password` | `aws_rds_iam` | `azure_entra` | `gcp_cloudsql_iam` |
  `secret_store` | `mtls`). When omitted, the command proposes one from
  detection.
- **Method-specific optional flags** mirroring the per-target config:
  `--region`, `--azure-client-id`, `--gcp-impersonate-service-account`,
  `--secret-ref`, `--sslcert` / `--sslkey`, `--sslrootcert-file`.
- **`--write <path>`** (optional) — append the verified target block to a
  config file. Default is **dry-run**: print the block, write nothing.
- **Ambient cloud identity / environment** — read (never configured as a
  secret) to drive detection.

## Outputs

- **On success** — a confirmation that the connection authenticated and the
  role passed the read-only safety check, plus a **ready target config block**
  (YAML, with the correct `name:` identifier, the selected `auth_method`,
  `sslmode: verify-full`, and method fields — **never a credential**). Written
  to `--write` only when given.
- **On a fixable gap** — a redacted, actionable message naming exactly what is
  missing and the **copy-pasteable** remediation (grant SQL / IAM binding /
  `pg_hba` line) for the selected method.
- **Metadata only** — the command surfaces method, db user, detection result,
  and diagnostic outcomes; it **never** prints a token, fetched secret, private
  key, or passphrase (INV002/INV007).

## Interfaces

### Detection → method proposal

| Detected environment | Proposed `auth_method` |
|---|---|
| AWS identity (instance profile / ECS / IRSA) **and** an RDS-style host | `aws_rds_iam` |
| Azure managed identity **and** a `*.postgres.database.azure.com` host | `azure_entra` |
| GCP ADC / Workload Identity **and** a Cloud SQL target | `gcp_cloudsql_iam` |
| `--secret-ref` supplied | `secret_store` (backend inferred from the ref) |
| `--sslcert` / `--sslkey` supplied | `mtls` |
| none of the above | `password` (prompt for a local credential source) |

Detection is **advisory**: `--auth-method` always overrides, and an ambiguous
detection (e.g. multiple cloud identities) is reported, not guessed
(CONNECT-FC001).

### Guided flow (stages)

```
1. detect      -> propose auth_method (or honour --auth-method)
2. resolve     -> invoke the selected provider's Resolve() (mint/fetch/load)
3. diagnose    -> run the existing doctor connection test (C3/C4) over the
                  resolved credential, sslmode=verify-full
4. role-safety -> ValidateRoleSafety on the connected role (INV005)
5. guidance    -> if a step fails for a missing login mapping, print the exact
                  grant for the selected method (owned by the provider specs'
                  operator-guidance ACs: AC-AWS-009, AC-AZURE-009, AC-GCP-009,
                  AC-SECRET-012, AC-MTLS-010)
6. emit        -> success: render target config block; failure: redacted fix
```

Each stage short-circuits to stage 5/6 on failure with a stage-specific,
redacted message.

## Invariants

- **SIGNALS-CONNECT-INV001 (no secret printed)**: No token, fetched
  secret, private key, passphrase, or password is ever printed, logged, or
  written to the emitted config block. Only metadata and operator remediation
  text. (Instance of keystone INV002/INV007 — the strictest reading, since this
  command's whole job is to print things.)
- **SIGNALS-CONNECT-INV002 (read-only, non-mutating)**: The command never
  executes a grant, creates a cloud identity, or writes to the database. It
  *prints* remediation for the operator to run. The only DB interaction is the
  read-only diagnostic connection. (Aligns with keystone INV005 and the repo
  no-write principle.)
- **SIGNALS-CONNECT-INV003 (reuse, don't reimplement)**: Credential
  resolution reuses the providers (#94–#98), the connection test reuses
  `doctor` (C3/C4), and the role check reuses `ValidateRoleSafety`. The command
  adds orchestration only — no parallel connection or validation logic.
- **SIGNALS-CONNECT-INV004 (single method)**: Exactly one `auth_method` is
  selected per run; only that method's flags/config are read. (Instance of
  keystone INV006.)
- **SIGNALS-CONNECT-INV005 (verify-full)**: The diagnostic connection and
  the emitted config use `sslmode=verify-full` for every credential-bearing
  method, consistent with the providers' transport floor (INV003).

## Failure Conditions

- **CONNECT-FC001 (ambiguous / undiscoverable identity)**: detection finds no
  usable identity, or more than one and no `--auth-method` / disambiguating
  flag → report what was found and how to disambiguate; do **not** guess.
- **CONNECT-FC002 (resolve failure)**: the selected provider's `Resolve` fails
  (mint/fetch/load) → surface the provider's redacted error plus its
  remediation; never the credential. (Maps to the providers' FC003-class.)
- **CONNECT-FC003 (connection failure)**: the diagnostic cannot connect after a
  successful resolve → surface the `doctor` diagnosis (host/TLS/network),
  redacted.
- **CONNECT-FC004 (login mapping missing)**: connection is refused because the
  role lacks the cloud-side mapping (`rds_iam` grant, Entra principal, IAM DB
  user, vault permission, trusted client cert) → print the **exact**
  copy-pasteable grant for the selected method (CONNECT-AC005).
- **CONNECT-FC005 (role not least-privilege)**: the role connects but
  `ValidateRoleSafety` flags excess privilege → report exactly which check
  failed and the corrective `REVOKE` / role change; do not emit a "success"
  config block.
- **CONNECT-FC006 (no method possible)**: no cloud identity and no
  password/cert source supplied → guide the operator to provide one
  (`password` source or `--sslcert`/`--sslkey`).

## Non-Functional Requirements

- **SIGNALS-CONNECT-NFR001 (no hidden network calls)**: the only outbound
  calls are the selected provider's identity/vault endpoint and the PostgreSQL
  diagnostic connection — both explicit and operator-initiated. No telemetry.
- **SIGNALS-CONNECT-NFR002 (idempotent, side-effect-free by default)**:
  running the command changes nothing unless `--write` is given; re-running is
  safe.
- **SIGNALS-CONNECT-NFR003 (fast feedback)**: the guided flow completes
  within the existing per-target connection + mint budget; a single token mint
  / vault fetch, one diagnostic connection.

## Acceptance Rules

- **CONNECT-AC001 (normal — full success)**: in an environment with a
  detectable cloud identity and a correctly-mapped least-privilege role,
  `connect --auto` detects the method, connects passwordlessly with
  `verify-full`, passes the role-safety check, and emits a ready config block —
  **no secret in the output**.
- **CONNECT-AC002 (normal — detection)**: each row of the detection table
  proposes the documented method; `--auth-method` overrides detection.
- **CONNECT-AC003 (failure — ambiguous identity)**: ambiguous/undiscoverable
  identity is reported with disambiguation guidance, not guessed (CONNECT-FC001).
- **CONNECT-AC004 (failure — resolve/connection redacted)**: a mint/fetch/load
  or connection failure prints a redacted, actionable error; no token, secret,
  key, or passphrase appears in any output (CONNECT-FC002/FC003 + INV001).
- **CONNECT-AC005 (failure — missing grant guidance)**: when the login mapping
  is missing, the exact grant for the selected method is printed
  (`GRANT rds_iam` / `pgaadauth_create_principal` / `gcloud sql users create` /
  vault IAM permission / `pg_hba` clientcert line), copy-pasteable
  (CONNECT-FC004).
- **CONNECT-AC006 (failure — role not least-privilege)**: an over-privileged
  role is reported with the specific failed check and corrective action; no
  success block is emitted (CONNECT-FC005).
- **CONNECT-AC007 (invariant — non-mutating dry-run)**: without `--write` the
  command makes no config or database changes; with `--write` it appends only
  the (secret-free) target block (INV002, NFR002).
- **CONNECT-AC008 (coverage)**: the command covers all methods where detection
  or explicit selection is possible — `aws_rds_iam`, `azure_entra`,
  `gcp_cloudsql_iam` by detection; `secret_store` and `mtls` by explicit
  flags; `password` as the no-cloud fallback.

## Proposed Tests (derived; written failing before implementation)

| Test | Maps to | Kind |
|---|---|---|
| detection table → proposed method; `--auth-method` overrides | CONNECT-AC002 | unit (env fixtures) |
| full happy path emits secret-free config block | CONNECT-AC001 | unit (fake provider + fake doctor) |
| ambiguous identity reported, not guessed | CONNECT-AC003 | unit |
| resolve/connection failure redacted; no secret in output | CONNECT-AC004 | unit (fake provider error + output capture) |
| missing-grant guidance per method is exact + copy-pasteable | CONNECT-AC005 | unit (table per method) |
| over-privileged role → failed-check report, no success block | CONNECT-AC006 | unit (fake ValidateRoleSafety) |
| dry-run mutates nothing; `--write` appends only the block | CONNECT-AC007 | unit (fs fixture) |
| coverage across all six auth_methods | CONNECT-AC008 | unit |
| live guided connect on a real cloud target | CONNECT-AC001 | smoke (env-gated, build-tagged) |

## Safety Impact

- [x] Read-only enforcement — preserved; the command's only DB interaction is
  the read-only diagnostic, and it never executes grants (INV002).
- [x] Role validation — reuses `ValidateRoleSafety` (INV003); does not relax it.
- [x] Credential handling — the command's defining risk is *output*; INV001
  forbids printing any credential, on every path.
- [ ] Export metadata
- [x] Network behavior — only the provider endpoint + the diagnostic connection
  (NFR001).

Per the repo safety rule, this spec + its derived failing tests land before
implementation; the spec reaches `ACTIVE` (and acceptance cases are added to
`acceptance-tests.md` / `traceability.md`) as the tests are committed.

## Resolved design decisions

_(Confirmed at promotion to `ACTIVE`.)_

1. **Interactivity — non-interactive by default.** The command runs from flags
   + detection only (CI-friendly). It prompts **only** for the `password`
   fallback, and only when attached to a TTY and no credential source is given;
   otherwise it reports `CONNECT-FC006` and exits.
2. **`--write` shape — fragment by default, append on `--write`.** Without
   `--write` it prints a standalone target fragment. With `--write` it appends
   the (secret-free) block to the config's `targets:` list and **refuses to
   duplicate an existing `name`** (CONNECT-AC007).
3. **Detection threshold — identity AND host pattern.** A cloud method is
   proposed only when both an ambient cloud identity *and* a matching host
   pattern are present; otherwise the command proposes `password` and reports
   what was and wasn't detected (CONNECT-FC001 / CONNECT-AC003).
