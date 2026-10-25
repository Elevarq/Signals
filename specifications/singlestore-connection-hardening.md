# SingleStore Connection Hardening

## Status

ACTIVE

## Purpose

Bring the SingleStore (MySQL-wire) connection up to the same security + control
contract Signals already enforces for PostgreSQL, in three areas that the POC
left as refinements:

- **TLS CA verification (#585)** — honor the configured private CA
  (`sslrootcert_file`) for `verify-ca` / `verify-full`, instead of dropping it
  and falling back to the system roots (which rejects a private CA) or, worse,
  accepting an unverified connection. A requested verified connection must
  verify against the configured CA and must never fall back to plaintext.
- **Query timeout (#586)** — apply the configured Signals per-query timeout to
  each SingleStore collector operation, not an unrelated driver default, while
  the per-target timeout remains the outer bound of the whole cycle.
- **Environment engine selection (#587)** — the environment-variable config path
  can express a non-PostgreSQL engine via `SIGNALS_TARGET_ENGINE`; omitting it
  keeps the backward-compatible PostgreSQL default. Example/operator guidance
  must match this behavior.

PostgreSQL TLS and timeout behavior are unchanged (both non-goals of #585/#586).

## Scope

- `internal/mysqlwire`: `ConnConfig` carries the CA material + verification mode;
  a pure `BuildTLSConfig` builder; CA registration + fail-closed resolution in
  `Open`.
- `internal/collector/singlestore_session.go`: map `sslmode` + `sslrootcert_file`
  onto the wire TLS fields; propagate the per-query timeout; bound each collector
  by it; classify a failed collector's reason canonically.
- `internal/config/config.go`: read `SIGNALS_TARGET_ENGINE` when building the
  environment target; engine-aware default port.
- `examples/singlestore-daemon.yaml`: corrected env-config guidance.

## Interfaces

- **`mysqlwire.BuildTLSConfig(caFile, serverName string, verifyHostname bool)
  (*tls.Config, error)`** — loads a PEM CA bundle into a `RootCAs` pool and
  returns a `*tls.Config`. `verifyHostname == true` sets `ServerName` and does
  full verification; `verifyHostname == false` verifies the chain against the CA
  but NOT the hostname (PostgreSQL `verify-ca` semantics). Errors (fail closed)
  when the CA file is unreadable or contains no parseable certificate.
- **`mysqlwire.ConnConfig`** gains `TLSRootCAFile string` and
  `TLSVerifyHostname bool`. When `TLSRootCAFile` is set, `Open` builds +
  registers the verifying `*tls.Config` and uses it; otherwise the existing
  `TLS` key (`false`/`true`/`skip-verify`/`preferred`) is used unchanged.
- **`config.TargetConfig.Engine`** is populated from `SIGNALS_TARGET_ENGINE` on
  the environment target (empty → PostgreSQL via `EffectiveEngine`).

## Behaviors

- **Given** a SingleStore server whose certificate is signed by the configured
  private CA, **when** `sslmode=verify-full` (or `verify-ca`) with
  `sslrootcert_file` is used, **then** the TLS handshake succeeds.
- **Given** the same server but an untrusted CA, or (under `verify-full`) a
  hostname mismatch, **when** connecting, **then** the handshake fails and the
  connection is never downgraded to plaintext.
- **Given** a collector whose query runs longer than the configured
  `query_timeout`, **when** the cycle runs, **then** that collector is canceled
  at `query_timeout`, persisted `failed` with reason `timeout`, and the
  remaining collectors still run while target time remains.
- **Given** `SIGNALS_TARGET_HOST` set and `SIGNALS_TARGET_ENGINE=singlestore`,
  **when** the env config is loaded, **then** the environment target's engine is
  `singlestore`; omitting `SIGNALS_TARGET_ENGINE` yields PostgreSQL.

## Rules

- **SCH-R001** — For `sslmode=verify-full` with `sslrootcert_file`, the
  SingleStore TLS config verifies the server certificate chain against that CA
  **and** checks the hostname (`ServerName`). A certificate not chaining to the
  CA, or a hostname mismatch, fails the handshake. (#585)
- **SCH-R002** — For `sslmode=verify-ca` with `sslrootcert_file`, the TLS config
  verifies the chain against that CA but does **not** check the hostname; a
  certificate not chaining to the CA fails, a hostname mismatch does not. (#585)
- **SCH-R003** — TLS fails closed: when a verified mode requests a CA that is
  unreadable or has no parseable certificate, `Open` returns an error and no
  usable handle; a requested verified connection never falls back to plaintext
  or to unverified TLS. (#585)
- **SCH-R004** — Each SingleStore collector operation is bounded by
  `min(query_timeout, remaining target budget)`; a collector that exceeds it is
  canceled and persisted `failed` with reason `timeout` (via the canonical
  `classifyRunError`), and later collectors still run while the target context is
  live. (#586)
- **SCH-R005** — The per-target timeout remains the outer bound of the whole
  cycle; the query timeout never silently replaces it, and cancellation releases
  the connection without leaking it or blocking shutdown. (#586)
- **SCH-R006** — The environment config path reads `SIGNALS_TARGET_ENGINE` and
  sets the environment target's engine; an omitted value defaults to PostgreSQL
  (backward compatible), never an implicit SingleStore detection. The engine-aware
  default port and database behavior is owned by `engine-connection-defaults.md`
  (ECONN-R001..R004, #623), which generalizes it to all five engines (MySQL /
  MariaDB / SingleStore → 3306, ClickHouse → 9000, PostgreSQL → 5432) and stops
  the non-PostgreSQL `postgres`-database leak. (#587, generalized by #623)
- **SCH-R007** — A failed SingleStore collector's persisted `reason` is the
  canonical classification of its error (`classifyRunError`): `timeout`,
  `permission_denied`, `object_missing`, or `execution_error` — never empty. The
  elevated-privilege permission-denied degrade remains `skipped`/`permission_denied`
  (engine-keyed-reporting MKR-R007). (#586)

## Invariants

- **INV-SCH-01** — No credential, private key, or certificate private material is
  logged or persisted (the redacted DSN already blanks the password; the CA file
  path is non-secret).
- **INV-SCH-02** — PostgreSQL TLS and query-timeout behavior are unchanged; these
  rules only touch the MySQL-wire path and the (previously PostgreSQL-only)
  environment-target engine field.
- **INV-SCH-03** — Collection stays read-only; none of these changes issues a
  write to any engine.

## Failure conditions

- Unreadable / empty / unparseable CA under a verified mode → `Open` error
  (SCH-R003), collection recorded as a target failure, nothing mislabeled.
- A collector exceeding its bound → `failed` + reason `timeout` (SCH-R004), not a
  silent drop and not a cycle-wide abort.

## Constraints

- No broad multi-engine TLS abstraction (explicit #585 non-goal); the CA path is
  the only addition, scoped to the MySQL-wire connection.
- TLS tests use locally-generated temporary CA material + a local TLS listener;
  never public CA infrastructure (#585 testing requirement).

## NFR overrides

None beyond the Signals safety baseline (read-only, fail-closed TLS, no secrets
in logs/output).

## Traceability

Acceptance: `specifications/singlestore-connection-hardening.acceptance.md`.
Matrix: `features/signals/traceability.md` (SCH-R001..R007, INV-SCH-01..03).
