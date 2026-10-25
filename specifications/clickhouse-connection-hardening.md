# ClickHouse Connection Hardening (TLS)

## Status

ACTIVE

## Purpose

Apply and enforce the configured TLS settings on the ClickHouse native connection
(Elevarq/Signals #558, audit finding F1). The CH-1 foundation accepted a target's
TLS configuration but never passed it to the native driver, so a target
requesting `verify-ca` / `verify-full` could connect in plaintext without the
requested guarantee. This spec closes that gap: the configured `sslmode` + CA
reach the driver, verification is real, and a TLS-required mode fails closed
rather than silently downgrading to plaintext.

Mirrors the proven MySQL-wire TLS hardening (`singlestore-connection-hardening.md`
SCH-R001/R002/R003); the verify crypto is the shared `internal/dbtls` helper.
PostgreSQL TLS behavior is unchanged (separate pgx path).

## Scope

- `clickhouse.ConnConfig` gains `TLSMode` + `TLSRootCAFile`; `clickhouse.Open`
  resolves them to the native driver's `clickhouse.Options.TLS` (`*tls.Config`).
- `internal/collector/clickhouse_session.go` passes the target's `SSLMode` +
  `SSLRootCertFile` into the ConnConfig (the F1 bug: it previously did not).

Out of scope: HTTP transport, other engines, the collector catalog.

## sslmode mapping (PostgreSQL-style)

| sslmode | TLS applied | Verification |
|---------|-------------|--------------|
| `disable` / empty | none (plaintext) | — (explicit opt-out; loopback/dev) |
| `require` / `required` | yes | encryption only, certificate NOT verified |
| `verify-ca` | yes | chain verified against the configured CA; hostname NOT checked |
| `verify-full` | yes | chain AND hostname verified against the configured CA |
| anything else | — | hard error (unknown sslmode) |

## Rules

- **CCH-R001** | `verify-full` builds a `*tls.Config` that verifies the server
  certificate chain against `TLSRootCAFile` AND the hostname (`ServerName` set),
  via `dbtls.BuildVerifiedTLSConfig(..., true)`.
- **CCH-R002** | `verify-ca` verifies the chain against `TLSRootCAFile` but NOT
  the hostname (`dbtls.BuildVerifiedTLSConfig(..., false)`): the connection is
  verified, not unverified.
- **CCH-R003** | A TLS-required mode (`require`/`verify-ca`/`verify-full`) always
  produces a non-nil `*tls.Config` — the connection is never plaintext. A verify
  mode whose CA is missing/unreadable/unparseable **fails closed** (`Open`
  returns an error; no handle), never a plaintext or unverified fallback. An
  unknown sslmode is a hard error.
- **CCH-R004** | `disable` or an empty sslmode yields a nil `*tls.Config`
  (plaintext) — an explicit, documented opt-out for loopback/dev use, never the
  silent default for a verify request.
- **CCH-R005** | The collector session passes the target's `SSLMode` +
  `SSLRootCertFile` into `clickhouse.ConnConfig` so the configuration actually
  reaches the native driver (the F1 fix).

## Invariants

- **INV-CCH-01** | A configured TLS verification mode never silently downgrades
  to plaintext or to an unverified connection (CCH-R003).
- **INV-CCH-02** | PostgreSQL (and MySQL-wire) TLS behavior is unchanged.
- **INV-CCH-03** | No credential or CA private material is logged or persisted;
  the CA file path is non-secret, the password is used only to dial.

## Failure conditions

| Trigger | Response |
|---------|----------|
| `verify-ca`/`verify-full` with missing/unreadable/unparseable CA | `Open` returns an error (fail closed); no connection, never plaintext. |
| Unknown sslmode value | Hard error from `resolveTLS`. |
| Untrusted CA / hostname mismatch under the verify mode | TLS handshake fails at dial; collection records a failure. |

## Constraints / NFR

- Native protocol (default 9000 plaintext; TLS commonly 9440 — the operator sets
  the port + sslmode; Signals applies the TLS config, it does not auto-switch
  ports). `MinVersion` TLS 1.2.
- Read-only posture and bounded timeouts unchanged.

## Traceability

Acceptance: `specifications/clickhouse-connection-hardening.acceptance.md`.
Verify crypto: `internal/dbtls` (hermetic TLS tests). Mirrors SCH-R001/R002/R003.
