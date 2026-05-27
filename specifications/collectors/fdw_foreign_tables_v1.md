# fdw_foreign_tables_v1 — Collector Specification

## Purpose

Foreign-table inventory linked to its server + FDW. Provides the
analyzer the upstream identity that `pg_columns_v1` lacks — without
this collector, foreign tables surface only as columns with
`relkind='f'` and no link back to their source.

## Catalog source

- `pg_foreign_table` — one row per foreign table (`ftrelid` →
  `pg_class`).
- `pg_class` — provides `relname` and `relkind`.
- `pg_namespace` — provides `schemaname`.
- `pg_foreign_server` → `pg_foreign_data_wrapper` — provides server
  + wrapper linkage.

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | `pg_namespace.nspname` |
| table_name | text | `pg_class.relname` |
| table_oid | bigint | `pg_class.oid` (lets the analyzer cross-reference rows in other collectors that emit OIDs) |
| relkind | char | always `'f'` for this collector — emitted explicitly so a future analyzer doing OID-cross-reference can sanity-check the row source |
| server_name | text | `pg_foreign_server.srvname` |
| fdw_name | text | `pg_foreign_data_wrapper.fdwname` |
| foreign_table_options | text[] | raw `key=value` option array (`ftoptions`); redacted before persistence (REDACT-R004) |

## Scope filter

- Excludes `pg_catalog`, `information_schema`, `pg_toast`,
  `pg_temp_*`, `pg_toast_temp_*` — same exclusion policy as the
  other schema collectors. A foreign table defined inside any of
  those schemas (rare but legal in some platforms) is deliberately
  out of scope; the analyzer's existing cross-collector model
  doesn't reach those namespaces.

## Invariants

- Read-only. Single SELECT.
- INNER JOINs throughout: a foreign table cannot exist without its
  server; a server cannot exist without its wrapper.
- Deterministic ordering: `ORDER BY schemaname, table_name`.
- Foreign-table COLUMN metadata stays in `pg_columns_v1` (which
  already includes `relkind='f'`); this collector does NOT
  duplicate it.
- No remote network connection: we never read remote rows.

## Failure conditions

- FC-01: No foreign tables → zero rows.
- FC-02: Permission denied on `pg_foreign_table` (rare; usually
  visible to all roles via `information_schema.foreign_tables`) →
  standard error path.

## Redaction

- **REDACT-R004**: `foreign_table_options` is redacted by the
  collector pipeline. `postgres_fdw` foreign-table options are
  typically `schema_name='remote_schema'` and
  `table_name='remote_table'` — non-sensitive — but other FDW
  drivers may store credentials per-table; the redactor catches
  them.

## Configuration

- Category: `schema`
- Cadence: `CadenceDaily`
- Retention: `RetentionMedium`
- Timeout: 30 s — the join chain is small but a database with
  thousands of foreign tables (uncommon but observed in CDC
  topologies) can take longer than the wrapper / server queries.
- HighSensitivity: false (option values redacted).

## Relationship to existing collectors

- `pg_columns_v1` already emits foreign-table columns (`relkind='f'`
  is in its WHERE clause). This collector adds the missing
  *table-level* identity (server + FDW + options).
- `pg_class_storage_v1` does NOT include `relkind='f'` — foreign
  tables are storage-less locally — so there is no overlap with
  storage metadata.
- `pg_stat_user_tables_v1` is view-based and excludes foreign
  tables; no overlap.

## PostgreSQL version compatibility

- PG 14–18: same `pg_foreign_table` shape; default SQL works.
- PG 19: default until divergence observed.

## Acceptance criteria

| AC | What | Test |
|----|------|------|
| FDW-T-AC1 | Registry contains `fdw_foreign_tables_v1`. | `TestFDWForeignTables_Registered` |
| FDW-T-AC2 | SQL compiles cleanly. | build / vet |
| FDW-T-AC3 | No foreign tables → zero rows. | Live pgtest / smoke |
| FDW-T-AC4 | A `CREATE FOREIGN TABLE public.t (...) SERVER s OPTIONS (schema_name 'rs', table_name 'rt')` produces a row with `schemaname='public'`, `table_name='t'`, `relkind='f'`, `server_name='s'`, and `foreign_table_options` containing `schema_name=rs`, `table_name=rt` unchanged. | Live pgtest / smoke |
| FDW-T-AC5 | A foreign table whose options include `password=...` (defensive) emits `<redacted>` for that value. | Redactor unit + live pgtest / smoke |

Spec status: ACTIVE.
