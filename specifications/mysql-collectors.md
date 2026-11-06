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
| `mysql_global_variables_v1` | config | `performance_schema.global_variables` (IN-list) | connection, durability + config-sizing tunables (buffer pool, redo log, temp/sort/join buffers, table/thread caches, io_capacity; #660); + observability (slow-query log, `performance_schema`), replication/HA config (`super_read_only`, GTID, semi-sync, binlog retention), InnoDB internals (adaptive hash, flush neighbors, change buffering, IO threads), and server charset/collation (#679 / Analyzer #4004); + `sql_mode` (the comma-separated server SQL-mode flag string, #686 / Analyzer #4010); version-tolerant IN-list (absent vars simply return no row) |
| `mysql_global_status_v1` | resources | `performance_schema.global_status` (IN-list) | live counters; carries the InnoDB buffer-pool read counters `Innodb_buffer_pool_reads` + `Innodb_buffer_pool_read_requests` for Analyzer's buffer-pool-read-pressure rate rule (#656 / Analyzer #3918) |
| `mysql_processlist_v1` | activity | `information_schema.PROCESSLIST` | **HighSensitivity**, `INFO` redacted; `PROCESS` grant surfaces all sessions |
| `mysql_innodb_metrics_v1` | resources | `information_schema.INNODB_METRICS WHERE STATUS='enabled'` | engine counters |
| `mysql_innodb_deadlocks_v1` | resources | `information_schema.INNODB_METRICS WHERE NAME='lock_deadlocks'` (no `STATUS` filter) | #656; the cumulative InnoDB deadlock count for Analyzer's `innodb.deadlock_rate` rate rule (#3918). Selected **unconditionally** (not gated on `STATUS='enabled'`) so the single `lock_deadlocks` row is always present with its `STATUS`, making the signal module-independent: when the row's `STATUS='enabled'` (the default across MySQL 8.0/8.4/9.7) `COUNT` is the live cumulative counter; when an operator has disabled the metric the row still returns with `STATUS='disabled'` so the consumer emits NOT_ASSESSED rather than a silent zero. Bounded to one row. No sensitive payload. |
| `mysql_table_statistics_v1` | schema | `information_schema.TABLES` (excl. system schemas) | row/byte sizing + `AUTO_INCREMENT` (the current counter value; NULL for tables with no AUTO_INCREMENT column — carried as absent, #677 / Analyzer #4004) + `TABLE_COLLATION` (per-table collation; charset family derivable from it; #684 / Analyzer #4001). |
| `mysql_engines_v1` | server | `information_schema.ENGINES` | storage engine support |
| `mysql_schemata_v1` | schema | `information_schema.SCHEMATA` | databases + charset/collation |
| `mysql_columns_v1` | schema | `information_schema.COLUMNS` (excl. system schemas, `ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION`) | #538 Replay Tier-1 evidence; per-column name, `COLUMN_TYPE` (DDL-faithful, engine-native), `DATA_TYPE`, `IS_NULLABLE`, ordinal, `COLUMN_KEY`, default, extra; + `CHARACTER_SET_NAME` / `COLLATION_NAME` (per-column charset/collation; NULL for non-string columns — carried as absent; #684 / Analyzer #4001). Schema metadata only — not HighSensitivity. |
| `mysql_index_inventory_v1` | schema | `information_schema.STATISTICS` (excl. system schemas, `ORDER BY TABLE_SCHEMA, TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX`) | #538 Replay Tier-1 evidence; per-index name, ordered columns (`SEQ_IN_INDEX`), uniqueness (`NON_UNIQUE`), type. `INDEX_NAME='PRIMARY'` carries PK identity losslessly. Per-index `CARDINALITY` is Tier-2 and NOT collected. Not HighSensitivity. |
| `mysql_replication_status_v1` | topology | `performance_schema.replication_connection_status` | legitimate zero-row on a standalone server |
| `mysql_statement_digests_v1` | workload | `performance_schema.events_statements_summary_by_digest` (top-200 by latency) | #540; **HighSensitivity**, `DIGEST_TEXT` redacted; empty when Performance Schema is off; NULL-digest overflow row included naturally. The MySQL query-workload signal (analogue of pg_stat_statements). A digest id is never equated with a PostgreSQL queryid. |
| `mysql_innodb_transactions_v1` | activity | `information_schema.INNODB_TRX` (oldest-first, LIMIT 200) | #543; active/session point-in-time evidence. **HighSensitivity**, `trx_query` redacted (carries live SQL text; result key is lowercase `trx_query`). `trx_state` distinguishes `RUNNING` vs `LOCK WAIT`. Legitimate zero-row on an idle server. |
| `mysql_data_locks_v1` | activity | `performance_schema.data_locks` (WAITING-first, LIMIT 500) | #543; active lock inventory. **HighSensitivity**, `LOCK_DATA` redacted (the locked index-key value — row data, not SQL; table/index/mode/status preserved). Legitimate zero-row when no locks are held. |
| `mysql_data_lock_waits_v1` | activity | `performance_schema.data_lock_waits` (LIMIT 500) | #543; the blocked→blocking wait-for edges (transaction + thread ids only; no SQL or row data). Legitimate zero-row when nothing is blocked. |
| `mysql_metadata_locks_v1` | activity | `performance_schema.metadata_locks` (PENDING-first, LIMIT 500) | #543; metadata-lock (MDL) inventory incl. PENDING waits. Object schema/name only, no SQL or row data. Rarely empty — the reading session's own MDL is visible; empty is still a legitimate success. |
| `mysql_replica_applier_status_v1` | topology | `performance_schema.replication_applier_status` | #543; replica applier state (per channel). Legitimate zero-row on a standalone (non-replica) server — a distinct state, not a failure. Native state fields preserved. |
| `mysql_group_replication_members_v1` | topology | `performance_schema.replication_group_members` | #543; Group Replication membership + role/state. Legitimate zero-row when Group Replication is not enabled — a distinct state, not a failure. Native topology fields (host/port/role/state) preserved. |
| `mysql_user_accounts_v1` | security | `mysql.user` (`User`, `Host`, `plugin`, computed `has_empty_password`) | #693 / Analyzer #4004; the user-account inventory for the security/grants rule tranche (remote-root / anonymous / passwordless accounts, weak/legacy auth plugin). **Secret-free by construction**: the `SELECT` list carries `User`, `Host`, `plugin`, and a *computed BOOLEAN* `(authentication_string = '' OR authentication_string IS NULL) AS has_empty_password` — the `authentication_string` hash column is NEVER in the select list, the evidence, logs, or any error (INV-MYC-04). `plugin` is always non-empty for a usable MySQL 8 account (the server ignores empty-plugin rows), so it reliably carries the real plugin; pairing it with `has_empty_password` lets a consumer distinguish a true blank password from an external-auth account (e.g. `auth_socket`). Reads the `mysql` schema, covered by the baseline global `SELECT` grant (MYG-R005); a privilege denial is a legitimate skip, never a cycle failure (MYC-R017). Metadata only (no row data); not `HighSensitivity`. |
| `mysql_user_privileges_v1` | security | `information_schema.USER_PRIVILEGES` (`GRANTEE`, `PRIVILEGE_TYPE`, `IS_GRANTABLE`) | #693 / Analyzer #4004; the global-privilege inventory so the rules see `*.*` sprawl and `WITH GRANT OPTION`. `USER_PRIVILEGES` has no password/hash column at all (INV-MYC-04). Under the baseline global `SELECT` grant the account sees every grantee's global privileges; a narrower grant shows only its own rows (a legitimate reduced result, not a failure). Metadata only; not `HighSensitivity`. |

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
- **Given** a MySQL target with a passwordless account and a `root@'%'` account
  with `*.*` privileges, **when** `mysql_user_accounts_v1` and
  `mysql_user_privileges_v1` run under the baseline grant, **then** the account
  inventory carries each account's `User` / `Host` / `plugin` and a
  `has_empty_password` boolean (true for the passwordless account), the privilege
  inventory carries each `GRANTEE` / `PRIVILEGE_TYPE` / `IS_GRANTABLE`, and no
  password hash or `authentication_string` value appears anywhere in the
  normalized evidence.
- **Given** a monitoring role without read access to the `mysql` schema, **when**
  `mysql_user_accounts_v1` runs, **then** it records a permission-denied `Err`
  and the other collectors still run — a degraded skip, never a cycle failure.

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

- **MYC-R010** | `mysql_global_status_v1` carries the InnoDB buffer-pool read
  counters `Innodb_buffer_pool_reads` and `Innodb_buffer_pool_read_requests` in
  its IN-list (cumulative counters; no producer-side delta — the Analyzer owns
  the interval). These are the evidence for Analyzer's buffer-pool-read-pressure
  rate rule (#3918). Present across MySQL 8.0/8.4/9.7 (confirmed live).
- **MYC-R011** | `mysql_innodb_deadlocks_v1` selects the single `lock_deadlocks`
  row from `information_schema.INNODB_METRICS` by `NAME` **without** the
  `STATUS='enabled'` filter, returning `NAME`, `COUNT`, and `STATUS`. The row is
  therefore always present with its enabled-state, so the deadlock signal is
  reliable regardless of the InnoDB metrics lock-module default: `STATUS='enabled'`
  (the default) → `COUNT` is the live cumulative deadlock counter; `STATUS` any
  other value → the consumer treats the count as NOT_ASSESSED rather than a
  silent zero. `COUNT` is a cumulative counter (no producer-side delta — the
  Analyzer owns the interval, #3918). Bounded to one row; read-only; not
  `HighSensitivity`.
- **MYC-R012** | `mysql_table_statistics_v1` carries `AUTO_INCREMENT` — the
  current AUTO_INCREMENT counter value from `information_schema.TABLES` — in its
  `SELECT`, alongside the existing `TABLE_ROWS` / `DATA_LENGTH` / `INDEX_LENGTH`
  sizing columns. This is the evidence for Analyzer's AUTO_INCREMENT-exhaustion
  -risk rule (Elevarq/Analyzer#4004): the consumer compares the current counter
  against the AI column's type maximum (derived from the `COLUMN_TYPE` /
  `DATA_TYPE` already captured by `mysql_columns_v1`, MYC-R009) to compute
  headroom. `AUTO_INCREMENT` is NULL for a table with no AUTO_INCREMENT column;
  that NULL is carried through as an absent (nil) value in the normalized
  evidence exactly like the other nullable sizing columns — never an error and
  never a fabricated zero. Still `information_schema.TABLES` metadata only (no
  row data); read-only; not `HighSensitivity`.
- **MYC-R013** | Per-table and per-column charset/collation for Analyzer's
  utf8mb4-readiness / charset-collation-mismatch rule (Elevarq/Analyzer#4001).
  `mysql_table_statistics_v1` carries `TABLE_COLLATION` in its `SELECT`
  (the charset family is derivable from the collation — `information_schema.TABLES`
  has no direct charset column); `mysql_columns_v1` carries `CHARACTER_SET_NAME`
  and `COLLATION_NAME` in its `SELECT`. The Analyzer rule flags legacy
  `utf8`(=utf8mb3) / `latin1` and collations inconsistent with the database
  default (already captured by `mysql_schemata_v1`'s
  `DEFAULT_CHARACTER_SET_NAME` / `DEFAULT_COLLATION_NAME`). `CHARACTER_SET_NAME`
  and `COLLATION_NAME` are NULL for non-string columns; those NULLs are carried
  through as absent (nil) values in the normalized evidence exactly like the
  other nullable columns — never an error. Read-only `information_schema`
  metadata only (no row data); no new privilege; not `HighSensitivity`.
- **MYC-R014** | `mysql_global_variables_v1` extends its `global_variables`
  IN-list with the "batch 2" variables for the Analyzer observability,
  replication-config, and InnoDB-internals rule tranches (Elevarq/Analyzer#4004):
  observability — `slow_query_log`, `long_query_time`, `log_output`,
  `performance_schema`; replication/HA config — `super_read_only`, `gtid_mode`,
  `enforce_gtid_consistency`, `binlog_expire_logs_seconds`, `expire_logs_days`,
  `rpl_semi_sync_source_enabled`, `rpl_semi_sync_replica_enabled`,
  `rpl_semi_sync_master_enabled`, `rpl_semi_sync_slave_enabled`,
  `log_replica_updates`, `log_slave_updates`; InnoDB internals —
  `innodb_adaptive_hash_index`, `innodb_flush_neighbors`,
  `innodb_change_buffering`, `innodb_read_io_threads`,
  `innodb_write_io_threads`; and server charset/collation —
  `character_set_server`, `collation_server` (the global complement of the
  per-table/per-column charset evidence of MYC-R013). The IN-list stays a
  curated allowlist (no `SHOW ALL VARIABLES`, no credential variables) and is
  version-tolerant: a variable absent on a given MySQL version (the semi-sync
  `source`/`replica` names on <8.0.26, the `master`/`slave` names on newer
  releases, `binlog_expire_logs_seconds` vs `expire_logs_days`) simply returns
  no row — never an error. Read-only; not `HighSensitivity`.
- **MYC-R015** | `mysql_global_status_v1` extends its `global_status` IN-list
  with the workload-counter and InnoDB-internals counters for the Analyzer
  workload-counter and InnoDB-internals rule tranches (umbrella
  Elevarq/Analyzer#4004) — the `global_status` analog of the MYC-R014
  `global_variables` batch-2 expansion. Workload / query counters —
  `Created_tmp_disk_tables`, `Created_tmp_tables` (on-disk temp-table ratio),
  `Select_scan`, `Select_full_join` (server-wide scan/join rate),
  `Sort_merge_passes` (sort-buffer pressure), `Handler_read_rnd_next`
  (scan-heavy workload), `Threads_created`, `Table_open_cache_hits`,
  `Table_open_cache_misses` (thread/table-cache miss rate). InnoDB internals —
  `Innodb_buffer_pool_pages_dirty`, `Innodb_buffer_pool_pages_total`
  (dirty/flush pressure), `Innodb_buffer_pool_wait_free`, `Innodb_log_waits`
  (redo/flush stalls), `Innodb_row_lock_waits`, `Innodb_row_lock_time` (lock
  pressure), and `Innodb_deadlocks` where present (enables the deadlock-*rate*
  rule; cf. the deadlock-*observability* rule Elevarq/Analyzer#4009 that flags
  its absence). All are cumulative counters — no producer-side delta; the
  Analyzer owns the interval (as with MYC-R010). The IN-list stays a curated
  allowlist (no `SHOW ALL STATUS`, no credentials) and is version-tolerant: a
  counter absent on a given MySQL version (e.g. `Innodb_deadlocks` where the
  build does not expose it) simply returns no row — never an error, never a
  fabricated zero. Read-only; not `HighSensitivity`.
- **MYC-R016** | `mysql_user_accounts_v1` is the user-account inventory for the
  Analyzer security/grants rule tranche (Elevarq/Analyzer#4004): remote-root
  (`root@'%'`) / anonymous / passwordless accounts and weak/legacy auth plugins.
  It reads `mysql.user` and its `SELECT` list is **exactly** `User`, `Host`,
  `plugin`, and a *computed boolean* `(authentication_string = '' OR
  authentication_string IS NULL) AS has_empty_password`. The
  `authentication_string` hash column (and any other credential/hash column —
  there is no `Password` column in MySQL 8) MUST NEVER appear in the select list,
  the normalized evidence, logs, or any error (INV-MYC-04): the empty-password
  signal is carried solely as the derived boolean `has_empty_password` (the
  *fact* of an empty/absent password), never the hash value. `plugin` is always
  non-empty for a usable MySQL 8 account (the server ignores empty-plugin rows),
  so it reliably reports the real authentication plugin; a consumer pairs it with
  `has_empty_password` to tell a true blank password from an external-auth
  account. Metadata only (no row data); read-only; not `HighSensitivity`.
- **MYC-R017** | `mysql_user_privileges_v1` is the global-privilege inventory
  (`SELECT GRANTEE, PRIVILEGE_TYPE, IS_GRANTABLE FROM
  information_schema.USER_PRIVILEGES`) so the Analyzer security rules can see
  `*.*` privilege sprawl and `WITH GRANT OPTION` (`IS_GRANTABLE='YES'`).
  `USER_PRIVILEGES` carries no password/hash column, so it is secret-free by
  construction (INV-MYC-04). Both security collectors read the `mysql`
  schema / its view, covered by the baseline global `SELECT` grant
  (`mysql-grants.md` MYG-R005); where the monitoring role lacks that read, the
  collector records a permission-denied `Err` and the rest of the catalog still
  runs (MYC-R002) — a legitimate degraded skip, never a cycle failure, and no
  grant beyond the baseline `SELECT, PROCESS` is ever required. Metadata only;
  read-only; not `HighSensitivity`.
- **MYC-R018** | `mysql_global_variables_v1` additionally carries `sql_mode` —
  the comma-separated server SQL-mode flag string (e.g.
  `ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,NO_ZERO_DATE,...`) — for the Analyzer
  `sql_mode` advisory rule (Elevarq/Analyzer#4010), which flags unsafe or
  non-strict mode combinations. It is added to the same curated, version-tolerant
  IN-list (no `SHOW ALL VARIABLES`, no credential variables): `sql_mode` absent on
  a given build simply returns no row — never an error. No data content (only the
  mode-flag string), same posture as MYC-R014 / MYC-R016. Read-only; not
  `HighSensitivity`.

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
- **INV-MYC-04** | No credential/secret material ever appears in the security
  collectors. No MySQL catalog statement names a password/hash column
  (`authentication_string`, `Password`, or any `*password*`/hash column) in its
  `SELECT` list, and the normalized evidence of `mysql_user_accounts_v1` /
  `mysql_user_privileges_v1` contains no such column key and no hash-shaped
  value — the empty-password signal is carried only as the derived boolean
  `has_empty_password`. This is a structural guard independent of redaction:
  the hash is never fetched in the first place.

## Failure conditions

| Trigger | Response |
|---------|----------|
| A collector's SQL errors (missing view / permission) | Captured in `Result.Err`; other collectors still run (MYC-R002). |
| A standalone server with no replication channel | `mysql_replication_status_v1` / `mysql_replica_applier_status_v1` are successful zero-row results, never failures. |
| Group Replication not enabled | `mysql_group_replication_members_v1` is a successful zero-row result, never a failure. |
| No active transactions / locks (idle server) | The activity collectors return successful zero-row results (0 == 0 reconciles), never failures. |
| A very busy server (many transactions/locks) | The activity collectors are bounded by `LIMIT`; the payload stays bounded, prioritising oldest transactions and WAITING/PENDING locks (MYC-R006). |
| The monitoring role cannot read the `mysql` schema | `mysql_user_accounts_v1` records a permission-denied `Err`; the other collectors still run (MYC-R002 / MYC-R017) — a degraded skip, never a cycle failure. |
| A narrow grant limits `USER_PRIVILEGES` to the own account | `mysql_user_privileges_v1` returns only the visible rows — a legitimate reduced result, never a failure. |

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
Matrix: `features/signals/traceability.md` (MYC-R001..R018, INV-MYC-01..04).
The Tier-1 evidence contract this serves is Analyzer's
`replay-evidence-contract.md` (#3891 follow-on); the live semantic qualification
that the evidence survives collection → persistence → export is
`TestMySQLLiveReplayTier1Evidence` + `VerifyReplayTier1Evidence`.
