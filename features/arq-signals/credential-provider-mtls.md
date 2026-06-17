# Feature Specification: mTLS Client-Certificate Credential Provider

- **Spec ID prefix:** `ARQ-SIGNALS-AUTH-MTLS-`
- **Lifecycle status:** `ACTIVE`
- **Tracking issue:** [#98](https://github.com/Elevarq/Arq-Signals/issues/98)
- **Derives from:** `credential-providers.md` (ACTIVE, #93). This spec is a
  behavioral sub-spec; it MUST conform to that abstraction's interface,
  invariants (INV001–INV007), failure taxonomy, and resolved design
  decisions. Where this spec is silent, the keystone governs.
- **Type:** Behavioral (local certificate material + pgx TLS wiring)

## Purpose

Implement the `mtls` credential provider: authenticate Elevarq Signals to a
PostgreSQL target with a **client certificate** (mutual TLS) instead of a
password or a minted token. This is the credential model mandated by many
on-prem / self-managed clusters and regulated environments where the database
trusts a client X.509 certificate signed by an internal CA.

`mtls` is the only provider that returns a **certificate-kind** credential:
the resolved cert + key material is applied to the connection's TLS config
(not to `ConnConfig.Password`). It is also the only provider with **no
minting and no cloud identity** — the credential is local file material that
changes only when the operator rotates it, picked up via the keystone's
re-resolve-on-reconnect seam (INV004).

This is the fifth and final provider on the credential-provider keystone
(#93), after `aws_rds_iam` (#94), `azure_entra` (#95), `gcp_cloudsql_iam`
(#96), and `secret_store` (#97). It reuses the shared scaffolding (the
`auth_method` target field, the `credentialResolver` dispatch wired into
`BeforeConnect`, and the config-validation rules) and adds only the
mTLS-specific behavior: the `sslcert` / `sslkey` fields, the certificate-kind
resolution, and the operator-guidance text.

## Inputs

- **`auth_method: mtls`** (per target) — selects this provider.
- **`sslcert`** (new, required) — filesystem path to the PEM client
  certificate presented to the server.
- **`sslkey`** (new, required) — filesystem path to the PEM private key for
  `sslcert`. Absent `sslcert`/`sslkey` → FC-MTLS-001.
- **`sslkey_passphrase_file`** (new, optional) — path to a file containing the
  passphrase for an encrypted `sslkey`. When the key is encrypted and no
  passphrase source is given, resolution fails (FC-MTLS-003). The passphrase
  is read from a file only — never an inline config value (consistent with the
  never-store-secrets posture).
- **`user`** (existing field) — the PostgreSQL role the certificate maps to
  (the cert Common Name / mapping is server-side, via `pg_ident.conf`).
- **`host`, `port`, `dbname`, `sslmode`, `sslrootcert_file`** — existing
  fields. `sslmode` MUST satisfy INV-MTLS-004 (`verify-full`). `sslrootcert_file`
  provides the CA bundle that verifies the **server** certificate.
- **No cloud identity, no token endpoint, no vault** — `mtls` makes no new
  outbound calls beyond the PostgreSQL connection itself.

## Outputs

- A resolved **certificate-kind** `Credential`: the parsed client
  certificate + private key, applied to the pgx connection's
  `tls.Config.Certificates`. `ExpiresAt` MAY be set to the client
  certificate's `NotAfter` (advisory; used only for observability, not for
  re-minting — there is nothing to re-mint).
- Credential **metadata only** to logs / status surfaces (INV007):
  `auth_method=mtls`, resolved `db_user`, the cert subject / SHA-256
  fingerprint, `NotAfter`, `resolved_at`. The private key material is **never**
  emitted.

## Interfaces

### Provider contract (conforms to keystone)

```
mtlsProvider implements CredentialProvider:
  Resolve(ctx, target) -> (Credential{Kind: certificate,
                                      Certificate: <cert+key bytes>,
                                      ExpiresAt: <cert NotAfter | nil>}, error)
```

- Selected at startup when `auth_method == mtls`; invoked from the existing
  `BeforeConnect` hook (and `BuildSafeDSN` for doctor/conntest).
- Loading the cert + key from disk is behind a **seam interface** (a
  `certLoader` the provider depends on) so unit tests inject fixtures and no
  test reads operator key material. The production implementation reads the
  PEM files, decrypts the key with the passphrase when present, and validates
  the cert/key pair.

### Integration mapping — TLS

| Concern | Binding |
|---|---|
| Identity | Local PEM client certificate + private key (`sslcert` / `sslkey`) |
| Credential | n/a (no token minted) — the cert is presented during the TLS handshake |
| Applied as | `tls.Config.Certificates` (certificate kind) |
| Transport | TLS `verify-full` required (INV-MTLS-004); the server cert is verified via `sslrootcert_file` |

## Invariants (mTLS-specific; keystone invariants also apply)

- **ARQ-SIGNALS-AUTH-MTLS-INV001 (private key never disclosed)**: The private
  key material (and any passphrase) is never written to logs, errors, audit,
  metrics, the local DB, or exports — only non-secret metadata (cert subject,
  fingerprint, `NotAfter`). Instance of keystone INV002/INV007 extended to
  private-key material.
- **ARQ-SIGNALS-AUTH-MTLS-INV002 (no inline password source)**: A target with
  `auth_method: mtls` carries no `password_file` / `password_env` /
  `pgpass_file` — authentication is by certificate. (Enforced at startup by
  FC-MTLS-005.) Note: unlike the token methods, the credential here is a local
  key file, so keystone INV001 (which forbids *stored secrets* for the
  *token* methods) does not forbid the key file; it forbids a *password*.
- **ARQ-SIGNALS-AUTH-MTLS-INV003 (rotation on reconnect)**: The cert + key are
  re-read on every new physical connection, so a rotated certificate or key is
  picked up on the next reconnect without a daemon restart. There is no token
  cache and no refresh timer — the file content at connect time is
  authoritative. (Instance of keystone INV004.)
- **ARQ-SIGNALS-AUTH-MTLS-INV004 (verify-full floor)**: A `mtls` target's
  effective `sslmode` MUST be `verify-full`, in every environment. Client-cert
  auth is meaningless without verifying the server it is presented to;
  presenting a client certificate to an unverified server risks disclosing it
  to an impostor. This applies the keystone INV003 transport posture to `mtls`
  — a confirmed local strengthening, not a weakening of any keystone rule.
  Enforced by FC-MTLS-004.
- **ARQ-SIGNALS-AUTH-MTLS-INV005 (read-only model untouched)**: `mtls` changes
  only how the connection authenticates; `ValidateRoleSafety` still runs and no
  write capability is added. (Instance of keystone INV005.)

## Failure Conditions

- **FC-MTLS-001 (missing cert/key)**: `auth_method: mtls` without `sslcert`
  and/or `sslkey` → **hard config error at startup** naming the missing
  field(s). (Keystone FC007.)
- **FC-MTLS-002 (unreadable / invalid material)**: `sslcert` or `sslkey` is
  missing on disk, unreadable, not valid PEM, or the cert and key do not form a
  matching pair → **connect-time, target-scoped** failure with a **redacted**,
  actionable error (which file + the failure class, never the key bytes). Other
  targets keep collecting. (Keystone FC003.)
- **FC-MTLS-003 (key passphrase failure)**: `sslkey` is encrypted and no
  `sslkey_passphrase_file` is given, or the passphrase is wrong → target-scoped
  connect-time failure; the passphrase and key are never echoed. (Keystone
  FC003.)
- **FC-MTLS-004 (TLS too weak)**: `mtls` on a target whose effective `sslmode`
  is not `verify-full` → **hard config error at startup**, regardless of `env`,
  naming the target and the required mode. (Keystone FC006 / INV-MTLS-004.)
- **FC-MTLS-005 (inline password source)**: `mtls` combined with any inline
  password source → **hard config error at startup** naming the target and
  stating that mTLS authenticates by certificate. (Keystone FC005.)

## Non-Functional Requirements

- **ARQ-SIGNALS-AUTH-MTLS-NFR001 (no new dependencies)**: `mtls` uses only the
  Go standard library (`crypto/tls`, `crypto/x509`, `encoding/pem`) and pgx —
  no cloud SDK. It links cleanly on the core path with no build-tag isolation
  required.
- **ARQ-SIGNALS-AUTH-MTLS-NFR002 (latency)**: cert + key parsing at
  `BeforeConnect` completes within the existing per-target connection budget;
  the material MAY be re-parsed per reconnect (no caching is required since
  there is no network mint).
- **ARQ-SIGNALS-AUTH-MTLS-NFR003 (no test-time key material)**: unit tests use
  injected fixtures (ephemeral self-signed test certs generated in-test) and
  read no operator key material, consistent with the repo's safety principles.

## Acceptance Rules

- **AC-MTLS-001 (normal)**: a target with `mtls`, `verify-full`, and a valid
  `sslcert`/`sslkey` pair connects with **no password in config**; the cert is
  applied to the TLS config and the connection authenticates.
- **AC-MTLS-002 (boundary — encrypted key)**: an encrypted `sslkey` with a
  correct `sslkey_passphrase_file` loads; a wrong/missing passphrase fails per
  FC-MTLS-003 without echoing the passphrase or key.
- **AC-MTLS-003 (boundary — rotation)**: replacing the cert/key files and
  forcing a reconnect picks up the new material without a daemon restart
  (INV-MTLS-003).
- **AC-MTLS-004 (invalid — missing fields)**: `mtls` without `sslcert`/`sslkey`
  aborts startup with an actionable error (FC-MTLS-001).
- **AC-MTLS-005 (invalid — TLS floor)**: `mtls` + `sslmode=require` (or weaker)
  aborts startup in every environment (FC-MTLS-004).
- **AC-MTLS-006 (invalid — inline password)**: `mtls` + a password source
  aborts startup with an actionable error (FC-MTLS-005).
- **AC-MTLS-007 (failure — bad material)**: an unreadable / non-PEM / mismatched
  cert-key pair fails the target's connection with a redacted, actionable error;
  the key never appears in any output surface; other targets keep collecting
  (FC-MTLS-002 + INV-MTLS-001).
- **AC-MTLS-008 (normal — metadata only)**: a successful resolution logs
  `auth_method`, db_user, cert subject/fingerprint, NotAfter, resolved_at —
  never the key (INV-MTLS-001).
- **AC-MTLS-009 (live smoke, env-gated)**: with `ARQ_SIGNALS_INTEGRATION_LIVE=1`
  against a PostgreSQL configured for client-cert auth (`clientcert=verify-full`
  in `pg_hba.conf`, a `pg_ident` map to the role), the collector connects with a
  client certificate and collects at least one snapshot. Not run in default CI.
- **AC-MTLS-010 (operator guidance)**: when the connection fails because the
  server does not trust / map the client cert, `signalsctl` surfaces the expected
  `pg_hba.conf` line (`hostssl ... clientcert=verify-full`) and the
  `pg_ident.conf` mapping note for the target. (UX may be refined alongside #99;
  the snippet text is owned here.)

## Proposed Tests (derived; written failing before implementation)

| Test | Maps to | Kind |
|---|---|---|
| cert+key loaded, applied to tls.Config, ExpiresAt = NotAfter | AC-MTLS-001 | unit (fixture loader) |
| encrypted key + passphrase loads; wrong/missing passphrase fails redacted | AC-MTLS-002 | unit |
| rotation: new files on reconnect are picked up | AC-MTLS-003 | unit (loader + clock) |
| startup rejects mtls without sslcert/sslkey | AC-MTLS-004 | unit (config validation) |
| startup rejects mtls + non-verify-full sslmode | AC-MTLS-005 | unit (config validation) |
| startup rejects mtls + inline password source | AC-MTLS-006 | unit (config validation) |
| unreadable/non-PEM/mismatched pair → redacted error; no key in log/err | AC-MTLS-007 | unit (loader error + log capture) |
| success logs metadata, never the key | AC-MTLS-008 | unit (log capture) |
| live client-cert connect + one snapshot | AC-MTLS-009 | smoke (env-gated, build-tagged) |
| pg_hba/pg_ident guidance emitted on cert-untrusted failure | AC-MTLS-010 | unit |

## Safety Impact

- [x] Read-only enforcement — preserved (keystone INV005); no change.
- [x] Credential handling — new certificate-kind credential (cert + private
  key) brought under never-store / never-log (INV-MTLS-001).
- [ ] Network behavior — no new outbound calls; `mtls` adds no identity/vault
  endpoint (NFR001).

Per the repo safety rule, this spec + its derived failing tests land before
implementation; the spec reaches `ACTIVE` (and acceptance cases are added to
`acceptance-tests.md` / `traceability.md`) as the tests are committed.

## Resolved design decisions

_(Confirmed at promotion to `ACTIVE`.)_

1. **Passphrase source — file-only.** `sslkey_passphrase_file` only; no inline
   or env variant, matching the never-store-inline posture. An encrypted
   `sslkey` with no passphrase file fails per FC-MTLS-003.
2. **`ExpiresAt` semantics — surface, advisory only.** The client cert's
   `NotAfter` is surfaced as `ExpiresAt` for observability (a near-expiry cert
   is worth logging) but drives no re-minting — there is nothing to re-mint;
   rotation is file-content-on-reconnect (INV-MTLS-003).
3. **Field names — `sslcert` / `sslkey`.** Mirror libpq's names for operator
   familiarity (over `client_cert_file` / `client_key_file`). The optional
   passphrase source is `sslkey_passphrase_file`.
