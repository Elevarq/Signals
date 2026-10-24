# SingleStore / MySQL-wire Connection and Engine Detection

## Status

ACTIVE

## Purpose

Provide a shared **MySQL-wire connector** that opens a bounded, read-only-by
-use connection to a MySQL-protocol server — MySQL **or** SingleStore — reads
the identity probe variables, and resolves the engine via
`internal/engineid`. This is the shared foundation for both MySQL
(Elevarq/Signals#525) and SingleStore (#526): #535 (shared MySQL-wire
connectivity) + #548 (connect to and identify SingleStore over the MySQL
wire).

Identity decisions are **delegated** to `engineid.Resolve` — this package
only connects and reads the probe; it never classifies an engine itself.

PostgreSQL is out of scope: it uses its own connector (`pgx`) and is never
reached through this package.

## Scope

```
BuildDSN(ConnConfig) -> dsn string                      (pure)
Redacted(ConnConfig) -> dsn string (password blanked)   (pure, for logs)
Open(ctx, ConnConfig) -> *sql.DB                         (bounded pool + ping)
NewScalar(*sql.DB) -> ScalarQuery
Identify(ctx, ScalarQuery) -> (engine, version, error)
```

## Interfaces

**`ConnConfig`** (self-contained for the POC; mapping from
`config.TargetConfig` is a later increment):

| Field | Meaning | Default |
|-------|---------|---------|
| `Host` | server host | — |
| `Port` | server port | `3306` when zero |
| `User` | username | — |
| `Password` | password (never logged) | — |
| `DBName` | optional database | empty |
| `TLS` | go-sql-driver TLS key (`true`/`false`/`skip-verify`/`preferred`) | `preferred` when empty |
| `ConnectTimeout` | dial timeout | `5s` when zero |
| `QueryTimeout` | per-read timeout | `10s` when zero |

**`ScalarQuery`** — `func(ctx, sql string) (string, error)`; returns a single
string cell. A seam so `Identify` is unit-testable without a live server.

## Behaviors

- **Given** a `ConnConfig`, **when** `BuildDSN` runs, **then** it returns a
  go-sql-driver DSN addressing `tcp(host:port)` with `interpolateParams=false`,
  the TLS key, and the bounded timeouts.
- **Given** a live connection, **when** `Identify` runs, **then** it reads
  `@@memsql_version`, `@@version_comment`, and `VERSION()` into an
  `engineid.Probe` and returns `engineid.Resolve(probe)`.
- **Given** a server that rejects `@@memsql_version` (any non-SingleStore
  server), **when** `Identify` runs, **then** that error is tolerated and the
  field is treated as absent — detection still proceeds.

## Rules

| ID | Rule |
|----|------|
| **WIRE-R001** | `Port` defaults to `3306` when zero. |
| **WIRE-R002** | `interpolateParams=false` is always set — no client-side parameter interpolation (injection-safe default). |
| **WIRE-R003** | A `@@memsql_version` query error is tolerated and mapped to an absent field; it must never fail detection (non-SingleStore servers reject the unknown variable). |
| **WIRE-R004** | Detection issues **only `SELECT`** statements — read-only by construction. |
| **WIRE-R005** | `TLS` defaults to `preferred` when empty; the value is passed through to the driver, never silently forced to plaintext. |

## Invariants

| ID | Invariant |
|----|-----------|
| **INV-WIRE-01** | No write statement is ever issued (Signals read-only safety). |
| **INV-WIRE-02** | The password never appears in logs; `Redacted` is the only DSN form intended for logging. |
| **INV-WIRE-03** | Engine identity comes **only** from `engineid.Resolve`; this package never classifies an engine itself (INV-SSID-* hold transitively). |

## Failure conditions

| Trigger | Response |
|---------|----------|
| Dial / ping fails within `ConnectTimeout` | `Open` returns an error; upstream records a collection failure — no snapshot is emitted and nothing is mislabeled. |
| `@@version_comment` and `VERSION()` both unreadable and no `@@memsql_version` | `Identify` returns `engineid.ErrIndeterminate` (fail closed). |

## Constraints

- Bounded connection pool (small `MaxOpenConns`); all queries honor `ctx`
  and `QueryTimeout`.
- No global driver state beyond the standard `database/sql` registration.

## NFR overrides

- **Security:** injection-safe params (WIRE-R002), configurable TLS
  (WIRE-R005), no secret logging (INV-WIRE-02).
- **Safety:** read-only by construction (WIRE-R004, INV-WIRE-01).
- **PostgreSQL untouched:** additive package; the `pgx` path is unchanged.

## Traceability

- Parent: Elevarq/Signals#535 (shared MySQL-wire connectivity), #548
  (connect + identify SingleStore). Depends on
  `specifications/singlestore-engine-identity.md`.
- Acceptance: `singlestore-wire-connect.acceptance.md`.
- Live qualification against the SingleStore Dev Image is #555 (integration,
  build-tag guarded; skipped without an instance).
