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
| `mysql_columns_v1` | schema | `information_schema.COLUMNS` (excl. system schemas, `ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION`) | #538 Replay Tier-1 evidence; per-column name, `COLUMN_TYPE` (DDL-faithful, engine-native), `DATA_TYPE`, `IS_NULLABLE`, ordinal, `COLUMN_KEY`, default, extra. Schema metadata only — not HighSensitivity. |
| `mysql_index_inventory_v1` | schema | `information_schema.STATISTICS` (excl. system schemas, `ORDER BY TABLE_SCHEMA, TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX`) | #538 Replay Tier-1 evidence; per-index name, ordered columns (`SEQ_IN_INDEX`), uniqueness (`NON_UNIQUE`), type. `INDEX_NAME='PRIMARY'` carries PK identity losslessly. Per-index `CARDINALITY` is Tier-2 and NOT collected. Not HighSensitivity. |
| `mysql_replication_status_v1` | topology | `performance_schema.replication_connection_status` | legitimate zero-row on a standalone server |
| `mysql_statement_digests_v1` | workload | `performance_schema.events_statements_summary_by_digest` (top-200 by latency) | #540; **HighSensitivity**, `DIGEST_TEXT` redacted; empty when Performance Schema is off; NULL-digest overflow row included naturally. The MySQL query-workload signal (analogue of pg_stat_statements). A digest id is never equated with a PostgreSQL queryid. |
| `mysql_innodb_transactions_v1` | activity | `information_schema.INNODB_TRX` (oldest-first, LIMIT 200) | #543; active/session point-in-time evidence. **HighSensitivity**, `trx_query` redacted (carries live SQL text; result key is lowercase `trx_query`). `trx_state` distinguishes `RUNNING` vs `LOCK WAIT`. Legitimate zero-row on an idle server. |
| `mysql_data_locks_v1` | activity | `performance_schema.data_locks` (WAITING-first, LIMIT 500) | #543; active lock inventory. **HighSensitivity**, `LOCK_DATA` redacted (the locked index-key value — row data, not SQL; table/index/mode/status preserved). Legitimate zero-row when no locks are held. |
| `mysql_data_lock_waits_v1` | activity | `performance_schema.data_lock_waits` (LIMIT 500) | #543; the blocked→blocking wait-for edges (transaction + thread ids only; no SQL or row data). Legitimate zero-row when nothing is blocked. |
| `mysql_metadata_locks_v1` | activity | `performance_schema.metadata_locks` (PENDING-first, LIMIT 500) | #543; metadata-lock (MDL) inventory incl. PENDING waits. Object schema/name only, no SQL or row data. Rarely empty — the reading session's own MDL is visible; empty is still a legitimate success. |
| `mysql_replica_applier_status_v1` | topology | `performance_schema.replication_applier_status` | #543; replica applier state (per channel). Legitimate zero-row on a standalone (non-replica) server — a distinct state, not a failure. Native state fields preserved. |
| `mysql_group_replication_members_v1` | topology | `performance_schema.replication_group_members` | #543; Group Replication membership + role/state. Legitimate zero-row when Group Replication is not enabled — a distinct state, not a failure. Native topology fields (host/port/role/state) preserved. |

## Behaviors

- **Given** a MySQL target collected over the wire, **when** the catalog runs
  under a `SELECT, PROCESS` monitoring role, **then** every collector returns
  rows or a legitimate empty result, and `mysql_processlist_v1`'s `INFO` is
  redacted unless high-sensitivity collection is enabled.
- **Given** a standalone (non-replica) MySQL, **when** `mysql_replication_status_v1`
  runs, **then** it returns zero rows and is recorded a successful empty result
  (not a failure).
- **Given** concurrent transactions contending for a row, **when** the activity
  collectors run, **then** `mysql_innodb_transactions_v1` shows the `RUNNING`
  holder and the `LOCK WAIT` waiter (session/active evidence, distinct from the
  aggregate `workload`/`resources` counters), `mysql_data_locks_v1` shows the
  granted and waiting record locks, and `mysql_data_lock_waits_v1` shows the
  blocked→blocking edge — with `trx_query` and `LOCK_DATA` redacted under default
  (non-high-sensitivity) collection.
- **Given** a standalone server or one without Group Replication, **when**
  `mysql_replica_applier_status_v1` / `mysql_group_replication_members_v1` run,
  **then** each returns zero rows recorded as a successful empty result — the
  replication-disabled / GR-not-enabled state is distinct from unavailable or
  permission-denied.

## Rules

- **MYC-R001** | Every catalog statement is a read-only `SELECT`; no writes
  (Signals safety, mirrors SSC-R001).
- **MYC-R002** | A collector whose statement errors records `Err` and does not
  abort the others (partial collection captured, not fatal).
- **MYC-R003** | With `redactSensitive=true`, each `SensitiveColumns` value is
  nil in every row of that collector's result.
- **MYC-R004** | Any collector selecting a query-text column (`INFO`,
  `DIGEST_TEXT`, `trx_query`) MUST be `HighSensitivity` with that column —
  spelled with the exact case the result row uses (`INFO`/`DIGEST_TEXT`
  uppercase, `trx_query` lowercase) — in `SensitiveColumns`.
- **MYC-R005** | `CatalogFor(mysql)` returns exactly `mysqlCatalog()`;
  `CatalogFor(singlestore)` returns `Catalog()`; `CatalogFor` of any other
  engine returns nil (no cross-engine leak, no PostgreSQL fallback).
- **MYC-R006** | The activity collectors (`mysql_innodb_transactions_v1`,
  `mysql_data_locks_v1`, `mysql_data_lock_waits_v1`, `mysql_metadata_locks_v1`)
  are point-in-time session evidence and are bounded by an explicit `LIMIT` so a
  busy server cannot produce an unbounded payload; truncation prioritises the
  operationally interesting rows (oldest transactions, WAITING/PENDING locks).
  An idle server yields a legitimate zero-row success, never a failure.
- **MYC-R007** | The replication collectors (`mysql_replication_status_v1`,
  `mysql_replica_applier_status_v1`, `mysql_group_replication_members_v1`) return
  a legitimate zero-row success when the server is standalone, replication is
  disabled, or Group Replication is not enabled. This state is distinct from a
  collector error (unavailable) or a permission denial; native topology/state
  fields are preserved verbatim when present.
- **MYC-R008** | Sensitive payload beyond query text is redacted by the same
  mechanism: `mysql_data_locks_v1` is `HighSensitivity` with `LOCK_DATA` (the
  locked index-key value — row data) in `SensitiveColumns`. Native lock
  descriptors (engine, object, index, mode, status) are preserved; only the key
  value is nilled under default collection.
- **MYC-R009** | `mysql_columns_v1` + `mysql_index_inventory_v1` provide the
  Analyzer-Replay Tier-1 evidence (`replay-evidence-contract.md`): per-column
  name/type/nullability/ordinal and per-index name/ordered-columns/uniqueness,
  with `INDEX_NAME='PRIMARY'` carrying primary-key identity (no separate PK
  collector). Ordinal position and composite-index column ordering are preserved
  (never flattened to an unordered set). `COLUMN_TYPE` is emitted engine-native —
  MySQL's own type spelling (`bigint`, `int`), never normalized — so the DDL
  reconstructs correctly on MySQL. Per-column / per-index selectivity and
  cardinality are Tier-2 and out of scope. These two collectors are schema
  metadata (no query text, no row data) and are not `HighSensitivity`.

## Invariants

- **INV-MYC-01** | Collection never issues a non-`SELECT` statement
  (`HasWrite` is false for every catalog SQL).
- **INV-MYC-02** | `mysql_processlist_v1` is `HighSensitivity` with `INFO` in
  `SensitiveColumns` (it carries application SQL text).
- **INV-MYC-03** | No MySQL collector selecting a query-text column (`INFO`,
  `DIGEST_TEXT`, `trx_query`) or locked-key data (`LOCK_DATA`) escapes the
  HighSensitivity + redaction contract (MYC-R004 / MYC-R008). The catalog-wide
  scan matches column names case-insensitively but each `SensitiveColumns` entry
  uses the exact result-row case so runtime redaction (an exact map-key nil)
  actually fires.

## Failure conditions

| Trigger | Response |
|---------|----------|
| A collector's SQL errors (missing view / permission) | Captured in `Result.Err`; other collectors still run (MYC-R002). |
| A standalone server with no replication channel | `mysql_replication_status_v1` / `mysql_replica_applier_status_v1` are successful zero-row results, never failures. |
| Group Replication not enabled | `mysql_group_replication_members_v1` is a successful zero-row result, never a failure. |
| No active transactions / locks (idle server) | The activity collectors return successful zero-row results (0 == 0 reconciles), never failures. |
| A very busy server (many transactions/locks) | The activity collectors are bounded by `LIMIT`; the payload stays bounded, prioritising oldest transactions and WAITING/PENDING locks (MYC-R006). |

## Constraints / NFR

- Read-only; no writes; no superuser. Monitoring role = `GRANT SELECT, PROCESS
  ON *.*` (see `specifications/mysql-grants.md`). `PROCESS` is what lets
  `information_schema.INNODB_TRX` and the `performance_schema` lock/replication
  views surface all sessions; no additional grant is required (confirmed live as
  `signals_ro` against MySQL 8.4 — every #543 collector ran clean under
  `SELECT, PROCESS`). `REPLICATION CLIENT` is optional and not required.
- The activity collectors are bounded: `INNODB_TRX` LIMIT 200 (oldest first),
  `data_locks` / `data_lock_waits` / `metadata_locks` LIMIT 500.
- Columns were confirmed live (incl. the INNODB_TRX / data_locks /
  data_lock_waits / metadata_locks / replication_* shapes on MySQL 8.4.13);
  regenerate against the pinned MySQL image if the server version changes shape.

## Traceability

Acceptance: `specifications/mysql-collectors.acceptance.md`.
Matrix: `features/signals/traceability.md` (MYC-R001..R009, INV-MYC-01..03).
The Tier-1 evidence contract this serves is Analyzer's
`replay-evidence-contract.md` (#3891 follow-on); the live semantic qualification
that the evidence survives collection → persistence → export is
`TestMySQLLiveReplayTier1Evidence` + `VerifyReplayTier1Evidence`.
