# fdw_wrappers_v1 — Collector Specification

## Purpose

Foreign Data Wrapper inventory. One row per installed FDW (e.g.
`postgres_fdw`, `file_fdw`, `oracle_fdw`, `mysql_fdw`,
`tds_fdw`). Used for (a) operational visibility — does this database
talk to anything via FDW? — and (b) downstream evidence for future
analyzer rules that may reason about wrapper handlers, validators,
or FDW-level options.

## Catalog source

- `pg_foreign_data_wrapper` — one row per `CREATE FOREIGN DATA WRAPPER`.
- `pg_proc` (joined twice) — resolves the handler / validator OIDs to
  function names. `LEFT JOIN` so a wrapper missing one of these
  surfaces an empty string rather than failing the row.

## Output columns

| Column | Type | Description |
|---|---|---|
| fdw_oid | bigint | `pg_foreign_data_wrapper.oid` |
| fdw_name | text | `fdwname` (e.g. `postgres_fdw`) |
| fdw_owner | text | role name from `pg_get_userbyid(fdwowner)` |
| fdw_handler | text | function name (`pg_proc.proname`); `''` when no handler |
| fdw_validator | text | function name; `''` when no validator |
| fdw_options | text[] | raw libpq-style `key=value` array (`fdwoptions`); redacted by the collector pipeline before persistence (REDACT-R001) |

## Scope filter

- All installed FDWs are included. No filter.

## Invariants

- Read-only: a single SELECT against `pg_catalog`. No FROM-clause writes.
- `relkind` is irrelevant here — `pg_foreign_data_wrapper` is the source.
- Deterministic ordering: `ORDER BY fdwname` ascending.
- Stable output column order matches the table above.
- No remote network connection: we never invoke the wrapper handler;
  we only read the local catalog.

## Failure conditions

- FC-01: No FDW installed — query returns **zero rows**, not an error.
  Operationally common; the collector_status row records `success`
  with `row_count=0`.
- FC-02: Permission denied — fails through the standard error path
  (`pg_monitor` is sufficient; superuser is not required).

## Redaction

- **REDACT-R001**: `fdw_options` MUST be passed through the FDW
  option redactor (`internal/pgqueries/fdw_redact.go`)
  before the rows reach the snapshot writer. Sensitive option keys
  (password, token, key, credential, connstr, etc.) emit
  `<redacted>` for their value; other keys round-trip unchanged.
  The redactor is exercised independently in
  `internal/pgqueries/fdw_redact_test.go`.

## Configuration

- Category: `schema`
- Cadence: `CadenceDaily`
- Retention: `RetentionLong` — wrapper inventory changes rarely;
  long retention supports historical comparison.
- Timeout: 15 s. The catalog is small (one row per wrapper).
- HighSensitivity: false. Option-key names alone are safe to
  collect at default sensitivity; secret values are redacted.

## PostgreSQL version compatibility

- PG 14, 15, 16, 17, 18 — all share the same
  `pg_foreign_data_wrapper` shape; the collector uses the default
  SQL with no per-major override.
- PG 19 — inherits the default until a real divergence is observed,
  in which case a per-major override lands via the standard
  `overrideRegistry` path (see `internal/pgqueries/registry.go`).

## Acceptance criteria

| AC | What | Test |
|----|------|------|
| FDW-W-AC1 | Registry has `fdw_wrappers_v1` for every supported PG major. | `TestFDWWrappers_Registered` |
| FDW-W-AC2 | SQL parses cleanly. | Compile-time via build (Go vet); also exercised in any pgtest harness if/when it lands. |
| FDW-W-AC3 | No-FDW database → zero rows, not error. | Live pgtest (when available) or smoke. |
| FDW-W-AC4 | A database with `postgres_fdw` installed produces ≥ 1 row whose `fdw_name = 'postgres_fdw'`. | Live pgtest / smoke. |

Spec status: ACTIVE.
