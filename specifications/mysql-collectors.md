# MySQL Collectors

## Status

ACTIVE

## Purpose

Define the read-only diagnostic collector set for a **MySQL** target, collected
over the shared MySQL-wire backend (the same transport, identity, session, and
engine-neutral persistence/export used for SingleStore). MySQL is added as a
**catalog + registry entry**, not a new code path: dispatch, the wire session,
identity resolution, and reporting are engine-generic and unchanged per engine
(engine-collection-dispatch MKW seam). This spec owns only the MySQL catalog SQL
and its handling metadata.

Every statement is a read-only `SELECT` over `information_schema` /
`performance_schema`, confirmed live against a MySQL 8.4 Community Server — not
authored blind (same discipline as the SingleStore catalog).

## Scope

- `mysqlCatalog() []Collector` (in `internal/singlestore`, the shared MySQL-wire
  catalog+runtime package) and its registration in `CatalogFor(engine)` for
  `engine == mysql`.
- The MySQL monitoring role (grants) needed to run the catalog.

Out of scope: the shared `Collect`/`NewRunner`/redaction runtime (unchanged), the
wire session, identity resolution, and persistence/export.

## Interfaces

- **`CatalogFor(engine string) []Collector`** — the per-engine catalog registry:
  `singlestore` → `Catalog()`, `mysql` → `mysqlCatalog()`, anything else → nil.
- **`mysqlCatalog() []Collector`** — the validated MySQL catalog below. Each
  `Collector` carries `ID`, `Category`, `SQL`, and (for query-text collectors)
  `HighSensitivity` + `SensitiveColumns`.

## Validated catalog (columns confirmed live against MySQL 8.4)

| ID | Category | Source | Notes |
|----|----------|--------|-------|
| `mysql_identity_v1` | server | `SELECT VERSION(), @@version_comment, @@version_compile_os` | server identity |
| `mysql_global_variables_v1` | config | `performance_schema.global_variables` (IN-list) | key tunables |
| `mysql_global_status_v1` | resources | `performance_schema.global_status` (IN-list) | live counters |
| `mysql_processlist_v1` | activity | `information_schema.PROCESSLIST` | **HighSensitivity**, `INFO` redacted; `PROCESS` grant surfaces all sessions |
| `mysql_innodb_metrics_v1` | resources | `information_schema.INNODB_METRICS WHERE STATUS='enabled'` | engine counters |
| `mysql_table_statistics_v1` | schema | `information_schema.TABLES` (excl. system schemas) | row/byte sizing |
| `mysql_engines_v1` | server | `information_schema.ENGINES` | storage engine support |
| `mysql_schemata_v1` | schema | `information_schema.SCHEMATA` | databases + charset/collation |
| `mysql_replication_status_v1` | topology | `performance_schema.replication_connection_status` | legitimate zero-row on a standalone server |

## Behaviors

- **Given** a MySQL target collected over the wire, **when** the catalog runs
  under a `SELECT, PROCESS` monitoring role, **then** every collector returns
  rows or a legitimate empty result, and `mysql_processlist_v1`'s `INFO` is
  redacted unless high-sensitivity collection is enabled.
- **Given** a standalone (non-replica) MySQL, **when** `mysql_replication_status_v1`
  runs, **then** it returns zero rows and is recorded a successful empty result
  (not a failure).

## Rules

- **MYC-R001** | Every catalog statement is a read-only `SELECT`; no writes
  (Signals safety, mirrors SSC-R001).
- **MYC-R002** | A collector whose statement errors records `Err` and does not
  abort the others (partial collection captured, not fatal).
- **MYC-R003** | With `redactSensitive=true`, each `SensitiveColumns` value is
  nil in every row of that collector's result.
- **MYC-R004** | Any collector selecting a query-text column (`INFO`) MUST be
  `HighSensitivity` with that column in `SensitiveColumns`.
- **MYC-R005** | `CatalogFor(mysql)` returns exactly `mysqlCatalog()`;
  `CatalogFor(singlestore)` returns `Catalog()`; `CatalogFor` of any other
  engine returns nil (no cross-engine leak, no PostgreSQL fallback).

## Invariants

- **INV-MYC-01** | Collection never issues a non-`SELECT` statement
  (`HasWrite` is false for every catalog SQL).
- **INV-MYC-02** | `mysql_processlist_v1` is `HighSensitivity` with `INFO` in
  `SensitiveColumns` (it carries application SQL text).
- **INV-MYC-03** | No MySQL collector selecting a query-text column escapes the
  HighSensitivity + redaction contract (MYC-R004).

## Failure conditions

| Trigger | Response |
|---------|----------|
| A collector's SQL errors (missing view / permission) | Captured in `Result.Err`; other collectors still run (MYC-R002). |
| A standalone server with no replication channel | `mysql_replication_status_v1` is a successful zero-row result, never a failure. |

## Constraints / NFR

- Read-only; no writes; no superuser. Monitoring role = `GRANT SELECT, PROCESS
  ON *.*` (see `specifications/mysql-grants.md`). `REPLICATION CLIENT` is
  optional and not required for the perf_schema replication view.
- Columns were confirmed live; regenerate against the pinned MySQL image if the
  server version changes shape.

## Traceability

Acceptance: `specifications/mysql-collectors.acceptance.md`.
Matrix: `features/signals/traceability.md` (MYC-R001..R005, INV-MYC-01..03).
