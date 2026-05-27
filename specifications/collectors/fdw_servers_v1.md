# fdw_servers_v1 — Collector Specification

## Purpose

Foreign-server inventory linked to its FDW. One row per
`CREATE SERVER`. Operational visibility for which remote endpoints
this database is configured to talk to and downstream evidence for
future analyzer rules (e.g. orphan servers without user mappings,
servers whose FDW handler is missing, servers with risky options).

## Catalog source

- `pg_foreign_server` — one row per server.
- `pg_foreign_data_wrapper` — joined to surface `fdw_name` for each
  server (the wrapper this server uses).

## Output columns

| Column | Type | Description |
|---|---|---|
| server_oid | bigint | `pg_foreign_server.oid` |
| server_name | text | `srvname` |
| fdw_name | text | `pg_foreign_data_wrapper.fdwname` |
| server_type | text | `srvtype`; `''` when NULL |
| server_version | text | `srvversion`; `''` when NULL |
| server_owner | text | `pg_get_userbyid(srvowner)` |
| server_options | text[] | raw libpq-style `key=value` array (`srvoptions`); redacted by the collector pipeline (REDACT-R002) |

## Scope filter

- All servers are included. No filter.

## Invariants

- Read-only. Single SELECT against `pg_catalog`.
- Server options are NEVER followed (no remote connection).
- Deterministic ordering: `ORDER BY srvname`.
- INNER JOIN against `pg_foreign_data_wrapper` — a server cannot exist
  without its wrapper; the join is structural.

## Failure conditions

- FC-01: No FDW servers → zero rows.
- FC-02: Permission denied → standard error path. `pg_monitor` is
  sufficient.

## Redaction

- **REDACT-R002**: `server_options` is passed through the FDW
  option redactor before persistence. Server options for
  `postgres_fdw` are typically host/port/dbname (not secret), but
  some FDW drivers store API endpoints / connection strings here;
  the redactor catches the sensitive ones.
- The `server_owner` field is a local PostgreSQL role name and is
  NOT redacted.

## Configuration

- Category: `schema`
- Cadence: `CadenceDaily`
- Retention: `RetentionLong`
- Timeout: 15 s
- HighSensitivity: false

## PostgreSQL version compatibility

- PG 14–18: same shape; default SQL works.
- PG 19: default until divergence observed.

## Acceptance criteria

| AC | What | Test |
|----|------|------|
| FDW-S-AC1 | Registry contains `fdw_servers_v1`. | `TestFDWServers_Registered` |
| FDW-S-AC2 | SQL compiles cleanly. | build / vet |
| FDW-S-AC3 | No-FDW database → zero rows. | Live pgtest / smoke |
| FDW-S-AC4 | A `postgres_fdw` server with `OPTIONS (host 'remote', dbname 'foo')` produces a row with `fdw_name='postgres_fdw'`, and `server_options` carries the host/dbname unchanged. | Live pgtest / smoke |
| FDW-S-AC5 | A server option keyed `password=...` (defensive — operators sometimes mis-place secrets) is REDACTED in `server_options`. | Live pgtest / smoke |

Spec status: ACTIVE.
