# MariaDB Collectors

## Status

ACTIVE

## Purpose

Define the read-only diagnostic collector set for a **MariaDB** target, collected
over the shared MySQL-wire backend. MariaDB is added as a **catalog + registry
entry** only — dispatch, the wire session, identity resolution, and reporting are
engine-generic and unchanged (engine-collection-dispatch ECD-R008). Identity was
already made distinct from MySQL in M5 (`engineid` SS-ID-R003: a MariaDB server
resolves to `mariadb`, never `mysql`). This spec owns only the MariaDB catalog
SQL + handling metadata.

Every statement is a read-only `SELECT`, confirmed live against a MariaDB 11.4
server — not authored blind.

## Scope

- `mariadbCatalog() []Collector` (in `internal/singlestore`) and its registration
  in `CatalogFor(engine)` for `engine == mariadb`.
- `SelectBackend`: `mariadb → BackendMySQLWire`; `config.SupportedEngines`
  includes `mariadb`.

## MariaDB vs MySQL — catalog differences (confirmed live, #616)

Parity with the MySQL catalog is by **capability**, not by reusing MySQL's SQL —
MariaDB's system views genuinely differ (this is why the catalogs are separate):

- `GLOBAL_VARIABLES` / `GLOBAL_STATUS` live in **`information_schema`** on MariaDB
  (MySQL 8 moved them to `performance_schema`). The MariaDB catalog reads
  `information_schema.GLOBAL_*`. Galera/wsrep state is read the same way, from
  `information_schema.GLOBAL_STATUS`.
- InnoDB transactions + locks are in **`information_schema`**: `INNODB_TRX`,
  `INNODB_LOCKS`, `INNODB_LOCK_WAITS` (MySQL 8.0 removed `INNODB_LOCKS` /
  `INNODB_LOCK_WAITS` and uses `performance_schema.data_locks` /
  `data_lock_waits` instead — those do **not** exist on MariaDB).
  `information_schema.INNODB_METRICS` IS available under the baseline `SELECT,
  PROCESS` role (an earlier spec claim that it was unavailable was wrong —
  disproven live as `signals_ro` against MariaDB 11.4.13).
- `performance_schema` is **OFF by default** on MariaDB, so
  `performance_schema.metadata_locks` and
  `performance_schema.events_statements_summary_by_digest` return an **empty
  result** (exit 0, no error) unless an operator has enabled perf_schema — they
  are perf_schema-gated, not absent. For the statement-digest workload signal
  (`mariadb_statement_digests_v1`) an empty table is ambiguous (perf_schema OFF
  vs perf_schema ON with no recorded statements), so it is disambiguated into a
  DEGRADED / NOT_ASSESSED signal (MDC-R010) rather than a silent success.
  **Prerequisite:** to collect the statement-digest workload signal the server
  must have `performance_schema=ON` (MariaDB: set `performance_schema=ON` in the
  server config / `my.cnf` and restart; it cannot be toggled at runtime). Signals
  never enables it — that is a server/deploy concern (for the demo, tracked in
  Elevarq/timeseries-demo).
- MariaDB has **Galera (wsrep)**, not Group Replication, and exposes replica
  state through **GTID server variables** (`gtid_current_pos`, `gtid_binlog_pos`,
  `gtid_slave_pos`), not a `SHOW SLAVE STATUS` table or the MySQL
  `performance_schema.replication_*` views.

## Validated catalog (columns confirmed live against MariaDB 11.4.13)

| ID | Category | Source |
|----|----------|--------|
| `mariadb_identity_v1` | server | `SELECT VERSION(), @@version_comment` |
| `server_identity_v1` | server | `SELECT DATABASE() AS database_name, VERSION()` — neutral (engine-agnostic) identity row carrying the connected `database_name` (`DATABASE()`, resolved from the target DSN's dbname). The Analyzer keys a database's identity on `server_identity_v1.database_name`, so each per-dbname target on ONE server is a distinct card (tpcc OLTP vs tpch OLAP); without it they collapse. A NULL `DATABASE()` is carried through as absent (#699, MDC-R021). |
| `mariadb_global_variables_v1` | config | `information_schema.GLOBAL_VARIABLES` (IN-list) | connection, durability + config-sizing tunables (buffer pool, log, temp/sort/join buffers, table/thread caches, io_capacity; #660); + observability (slow-query log, `performance_schema`), replication/HA config (`gtid_strict_mode`/`gtid_domain_id`, semi-sync, binlog retention), InnoDB internals (adaptive hash, flush neighbors, change buffering, IO threads), and server charset/collation (#679 / Analyzer #4004); + `sql_mode` (the comma-separated server SQL-mode flag string, #686 / Analyzer #4010); version-tolerant IN-list (absent vars simply return no row) |
| `mariadb_global_status_v1` | resources | `information_schema.GLOBAL_STATUS` (IN-list) — carries the InnoDB buffer-pool read counters `Innodb_buffer_pool_reads` + `Innodb_buffer_pool_read_requests` for Analyzer's buffer-pool-read-pressure rate rule (#656 / Analyzer #3918). MariaDB returns `GLOBAL_STATUS` names uppercased; the IN-list matches case-insensitively. |
| `mariadb_processlist_v1` | activity | `information_schema.PROCESSLIST` — **HighSensitivity**, `INFO` redacted |
| `mariadb_table_statistics_v1` | schema | `information_schema.TABLES` (excl. system schemas) — row/byte sizing + `AUTO_INCREMENT` (the current counter value; NULL for tables with no AUTO_INCREMENT column — carried as absent, #677 / Analyzer #4004) + `TABLE_COLLATION` (per-table collation; charset family derivable from it; #684 / Analyzer #4001). |
| `mariadb_engines_v1` | server | `information_schema.ENGINES` |
| `mariadb_schemata_v1` | schema | `information_schema.SCHEMATA` |
| `mariadb_columns_v1` | schema | `information_schema.COLUMNS` (excl. system schemas, `ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION`) — #538 Replay Tier-1 evidence; per-column name, `COLUMN_TYPE` (DDL-faithful, engine-native: MariaDB retains `bigint(20)`/`int(11)`), `IS_NULLABLE`, ordinal, key, default, extra; + `CHARACTER_SET_NAME` / `COLLATION_NAME` (per-column charset/collation; NULL for non-string columns — carried as absent; #684 / Analyzer #4001). Not HighSensitivity. |
| `mariadb_index_inventory_v1` | schema | `information_schema.STATISTICS` (excl. system schemas, `ORDER BY TABLE_SCHEMA, TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX`) — #538 Replay Tier-1 evidence; per-index name, ordered columns (`SEQ_IN_INDEX`), uniqueness (`NON_UNIQUE`). `INDEX_NAME='PRIMARY'` carries PK identity. **#640 Replay Tier-2:** `CARDINALITY` — MariaDB's precomputed distinct-value estimate for the index prefix (per-index selectivity consumed by Analyzer#4142); read from the SAME row, never computed; NULL until `ANALYZE` (carried as absent); distinct MariaDB identity. Not HighSensitivity. |
| `mariadb_innodb_index_stats_v1` | schema | `mysql.innodb_index_stats` (`n_diff_pfx%` rows, excl. system schemas) | **#640 Replay Tier-2:** per-index-PREFIX cardinality from InnoDB persistent stats (`n_diff_pfxNN`); MariaDB ships the same table; distinct identity; finer than `STATISTICS.CARDINALITY`, the direct row-estimate injection input (Analyzer#4142). Read-only `mysql`-schema metadata; schema-bounded; engine-precomputed, never computed. Not HighSensitivity. |
| `mariadb_column_stats_v1` | schema | `mysql.column_stats` (`nulls_ratio`, `avg_length`, `avg_frequency` only; excl. system schemas) | **#640 Replay Tier-2:** per-COLUMN selectivity from MariaDB Engine-Independent Table Statistics (`mysql.column_stats`, populated by `ANALYZE TABLE … PERSISTENT`); the distribution inputs the Analyzer row-estimate injection (Analyzer#4142) uses to refine single-/multi-column selectivity. **MariaDB-only** — MySQL has no `column_stats` equivalent. **Safety:** `min_value`/`max_value` are ACTUAL column data values (potential PII) and the histogram blob is deferred — the `SELECT` deliberately projects ONLY the three distribution scalars, never a data value. Read-only `mysql`-schema metadata; schema-bounded; engine-precomputed, never computed. Not HighSensitivity. |
| `mariadb_partitions_v1` | schema | `information_schema.PARTITIONS` (actually-partitioned tables only — `PARTITION_NAME IS NOT NULL`; excl. system schemas) | **#640 Replay Tier-3:** per-partition inventory — `PARTITION_METHOD`/`SUBPARTITION_METHOD`, `PARTITION_EXPRESSION`/`SUBPARTITION_EXPRESSION`, `PARTITION_DESCRIPTION` (range/list boundaries), ordinal, and the per-partition `TABLE_ROWS` estimate — so the Analyzer can model partition pruning and per-partition row estimates (Analyzer#4142). Boundaries/expressions are DBA-authored DDL schema (appear verbatim in `SHOW CREATE TABLE`), NOT sampled column data — distinct from the i3 min/max exclusion. Read-only; schema-bounded (one row per partition); engine-precomputed, never computed. Not HighSensitivity. |
| `mariadb_innodb_metrics_v1` | resources | `information_schema.INNODB_METRICS WHERE ENABLED=1` |
| `mariadb_innodb_deadlocks_v1` | resources | `information_schema.INNODB_METRICS WHERE NAME='lock_deadlocks'` (no `ENABLED` filter) | #656; the cumulative InnoDB deadlock count for Analyzer's `innodb.deadlock_rate` rate rule (#3918). Selected **unconditionally** (not gated on `ENABLED=1`) so the single `lock_deadlocks` row is always present with its `ENABLED` flag, making the signal module-independent: `ENABLED=1` (the default across MariaDB 10.11/11.4/12.3) → `COUNT` is the live cumulative counter; `ENABLED=0` → the row still returns so the consumer emits NOT_ASSESSED rather than a silent zero. Bounded to one row. No sensitive payload. |
| `mariadb_innodb_transactions_v1` | activity | `information_schema.INNODB_TRX` (oldest-first, LIMIT 200) — **HighSensitivity**, `trx_query` redacted (lowercase result key) |
| `mariadb_innodb_locks_v1` | activity | `information_schema.INNODB_LOCKS` (LIMIT 500) — **HighSensitivity**, `lock_data` redacted (locked key value, lowercase result key) |
| `mariadb_innodb_lock_waits_v1` | activity | `information_schema.INNODB_LOCK_WAITS` (LIMIT 500) — trx/lock ids only |
| `mariadb_metadata_locks_v1` | activity | `performance_schema.metadata_locks` (PENDING-first, LIMIT 500) — perf_schema-gated (empty when off) |
| `mariadb_statement_digests_v1` | workload | `performance_schema.events_statements_summary_by_digest` (top-200 by latency) — **HighSensitivity**, `DIGEST_TEXT` redacted; **perf_schema-gated (`PerfSchemaGated`)**: a 0-row result with perf_schema OFF is recorded `skipped`/`perf_schema_disabled` (DEGRADED / NOT_ASSESSED, MDC-R010), not a silent success |
| `mariadb_replication_status_v1` | topology | GTID/binlog/read_only server variables (`@@global.server_id`, `log_bin`, `read_only`, `gtid_current_pos`, `gtid_binlog_pos`, `gtid_slave_pos`) |
| `mariadb_galera_status_v1` | topology | `information_schema.GLOBAL_STATUS` (`WSREP_*` IN-list) — Galera cluster state; standalone = `Disconnected`/`OFF`, a distinct legitimate state |
| `mariadb_user_accounts_v1` | security | `mysql.user` view (`User`, `Host`, `plugin`, computed `has_empty_password`) | #693 / Analyzer #4004; the user-account inventory for the security/grants rule tranche. **Secret-free by construction** (INV-MDC-04): the `SELECT` list carries `User`, `Host`, `plugin`, and a *computed BOOLEAN* `((Password = '' OR Password IS NULL) AND (authentication_string = '' OR authentication_string IS NULL)) AS has_empty_password` — the `Password` / `authentication_string` hash columns are NEVER in the select list, the evidence, logs, or any error. **Lowest-exposure source chosen (MDC-R018) + documented owner-decision trade-off**: MariaDB has no reliable hash-free single source — the `mysql.user` view is read-only and hash-free *in the projected columns*, but MariaDB documents that the view "doesn't show alternative authentication plugins", so `plugin` can be empty/`mysql_native_password` for accounts whose real plugin lives in `mysql.global_priv` JSON. We read the view (lowest exposure, baseline grant) rather than the JSON (which embeds the hash) and FLAG the residual plugin-reliability limitation for the owner. Reads the `mysql` schema, covered by baseline global `SELECT`; a privilege denial is a legitimate skip (MDC-R019). Metadata only; not `HighSensitivity`. |
| `mariadb_user_privileges_v1` | security | `information_schema.USER_PRIVILEGES` (`GRANTEE`, `PRIVILEGE_TYPE`, `IS_GRANTABLE`) | #693 / Analyzer #4004; the global-privilege inventory so the rules see `*.*` sprawl and `WITH GRANT OPTION`. `USER_PRIVILEGES` has no password/hash column (INV-MDC-04). Under baseline global `SELECT` the account sees every grantee; a narrower grant shows only its own rows (a legitimate reduced result). Metadata only; not `HighSensitivity`. |

## Rules

- **MDC-R001** | Every catalog statement is a read-only `SELECT`; no writes.
- **MDC-R002** | A collector whose statement errors records `Err` and does not
  abort the others (partial collection captured, not fatal).
- **MDC-R003** | With `redactSensitive=true`, each `SensitiveColumns` value is nil
  in every row of that collector's result.
- **MDC-R004** | Any collector selecting a query-text column (`INFO`,
  `trx_query`, `DIGEST_TEXT`) MUST be `HighSensitivity` with that column — in its
  exact result-row case (`INFO`/`DIGEST_TEXT` uppercase, `trx_query` lowercase) —
  in `SensitiveColumns`.
- **MDC-R005** | `CatalogFor(mariadb)` returns exactly `mariadbCatalog()`; no
  cross-engine leak, no PostgreSQL fallback.
- **MDC-R006** | The activity collectors (`mariadb_innodb_transactions_v1`,
  `mariadb_innodb_locks_v1`, `mariadb_innodb_lock_waits_v1`,
  `mariadb_metadata_locks_v1`) are point-in-time session evidence, bounded by an
  explicit `LIMIT`; an idle server yields a legitimate zero-row success.
- **MDC-R007** | The topology / perf_schema-gated collectors return a legitimate
  zero-row success in their disabled states — standalone (no replica GTID),
  Galera not connected (`WSREP_READY=OFF`), or `performance_schema` off
  (`mariadb_metadata_locks_v1` empty). Each is distinct from a collector error or
  a permission denial; native fields are preserved verbatim when present.
  **Exception:** `mariadb_statement_digests_v1` no longer treats the
  perf_schema-off empty result as a bare success — it emits a DEGRADED /
  NOT_ASSESSED signal per MDC-R010, so a consumer can tell "the workload signal
  is unavailable because perf_schema is OFF" from "perf_schema is ON and there
  are no slow queries".
- **MDC-R008** | Sensitive payload beyond query text is redacted by the same
  mechanism: `mariadb_innodb_locks_v1` is `HighSensitivity` with `lock_data` (the
  locked index-key value — row data, lowercase result key) in `SensitiveColumns`.
  Native lock descriptors (mode, type, table, index) are preserved.
- **MDC-R010** | **performance_schema prerequisite (digest workload signal).**
  `mariadb_statement_digests_v1` reads
  `performance_schema.events_statements_summary_by_digest`, which is **empty
  whenever `performance_schema` is OFF** (the MariaDB default). A bare 0-row
  result is therefore ambiguous: it can mean "perf_schema ON, no statements
  recorded yet" (a legitimate empty success) OR "perf_schema OFF, the workload
  signal is unavailable" (NOT a success — the signal was never assessed). The
  collector MUST disambiguate these. It is flagged `PerfSchemaGated`, and when it
  returns **0 rows with no error** the runtime fires one cheap read-only probe
  `SELECT @@performance_schema AS performance_schema`. If the probe shows
  perf_schema **OFF** (value `0` / `OFF`), the collector run is recorded
  **`status=skipped`, `reason=perf_schema_disabled`** — a DEGRADED / NOT_ASSESSED
  signal, distinct from both a genuine empty success and a collector error. If
  the probe shows perf_schema **ON** (and the collector still returned 0 rows),
  the run stays a legitimate **`status=success`** ("no slow queries"). A
  collector that returned rows is never probed. The probe is read-only
  (`SELECT` only, INV-MDC-01 preserved) and runs at most once per
  digest-collector invocation (only on the 0-row path).
- **MDC-R011** | `perf_schema_disabled` is the canonical skip reason for the
  perf_schema-gated degrade (MDC-R010); it is pinned to
  `metrics.CollectorSkippedReasons`. The probe failing (e.g. a server that does
  not expose `@@performance_schema`, or a probe query error) does NOT turn the
  run into a failure — the run stays its pre-probe outcome (the 0-row success),
  because the probe is a diagnostic enrichment, not a correctness gate.
- **MDC-R009** | `mariadb_columns_v1` + `mariadb_index_inventory_v1` provide the
  Analyzer-Replay Tier-1 evidence (`replay-evidence-contract.md`): per-column
  name/type/nullability/ordinal and per-index name/ordered-columns/uniqueness,
  with `INDEX_NAME='PRIMARY'` carrying primary-key identity (no separate PK
  collector). Ordinal position and composite-index column ordering are preserved
  (never flattened to an unordered set). The acquisition SQL is identical to
  MySQL's (empirically verified equivalent across MySQL 8.0/8.4/9.7 and MariaDB
  10.11/11.4/11.8/12.3), but the collector identity is MariaDB's and `COLUMN_TYPE`
  is emitted engine-native — MariaDB keeps the integer display width
  (`bigint(20)`, `int(11)`) MySQL 8.0 dropped, never normalized to MySQL — so the
  DDL reconstructs correctly on MariaDB. Per-column / per-index selectivity and
  cardinality are Tier-2 and out of scope. Both collectors are schema metadata and
  are not `HighSensitivity`.
- **MDC-R012** | `mariadb_global_status_v1` carries the InnoDB buffer-pool read
  counters `Innodb_buffer_pool_reads` and `Innodb_buffer_pool_read_requests` in
  its IN-list (cumulative counters; no producer-side delta — the Analyzer owns
  the interval). These are the evidence for Analyzer's buffer-pool-read-pressure
  rate rule (#3918). Present across MariaDB 10.11/11.4/12.3 (confirmed live);
  MariaDB returns `GLOBAL_STATUS` names uppercased and the IN-list matches
  case-insensitively.
- **MDC-R013** | `mariadb_innodb_deadlocks_v1` selects the single `lock_deadlocks`
  row from `information_schema.INNODB_METRICS` by `NAME` **without** the
  `ENABLED=1` filter, returning `NAME`, `COUNT`, and `ENABLED`. The row is
  therefore always present with its enabled-state, so the deadlock signal is
  reliable regardless of the InnoDB metrics lock-module default: `ENABLED=1` (the
  default) → `COUNT` is the live cumulative deadlock counter; `ENABLED=0` → the
  consumer treats the count as NOT_ASSESSED rather than a silent zero. `COUNT` is
  a cumulative counter (no producer-side delta — the Analyzer owns the interval,
  #3918). Bounded to one row; read-only; not `HighSensitivity`.
- **MDC-R014** | `mariadb_table_statistics_v1` carries `AUTO_INCREMENT` — the
  current AUTO_INCREMENT counter value from `information_schema.TABLES` — in its
  `SELECT`, alongside the existing `TABLE_ROWS` / `DATA_LENGTH` / `INDEX_LENGTH`
  sizing columns. This is the evidence for Analyzer's AUTO_INCREMENT-exhaustion
  -risk rule (Elevarq/Analyzer#4004): the consumer compares the current counter
  against the AI column's type maximum (derived from the `COLUMN_TYPE` /
  `DATA_TYPE` already captured by `mariadb_columns_v1`, MDC-R009) to compute
  headroom. `AUTO_INCREMENT` is NULL for a table with no AUTO_INCREMENT column;
  that NULL is carried through as an absent (nil) value in the normalized
  evidence exactly like the other nullable sizing columns — never an error and
  never a fabricated zero. Still `information_schema.TABLES` metadata only (no
  row data); read-only; not `HighSensitivity`.
- **MDC-R015** | Per-table and per-column charset/collation for Analyzer's
  utf8mb4-readiness / charset-collation-mismatch rule (Elevarq/Analyzer#4001).
  `mariadb_table_statistics_v1` carries `TABLE_COLLATION` in its `SELECT`
  (the charset family is derivable from the collation — `information_schema.TABLES`
  has no direct charset column); `mariadb_columns_v1` carries
  `CHARACTER_SET_NAME` and `COLLATION_NAME` in its `SELECT`. The Analyzer rule
  flags legacy `utf8`(=utf8mb3) / `latin1` and collations inconsistent with the
  database default (already captured by `mariadb_schemata_v1`'s
  `DEFAULT_CHARACTER_SET_NAME` / `DEFAULT_COLLATION_NAME`). `CHARACTER_SET_NAME`
  and `COLLATION_NAME` are NULL for non-string columns; those NULLs are carried
  through as absent (nil) values in the normalized evidence exactly like the
  other nullable columns — never an error. Read-only `information_schema`
  metadata only (no row data); no new privilege; not `HighSensitivity`.
- **MDC-R016** | `mariadb_global_variables_v1` extends its `GLOBAL_VARIABLES`
  IN-list with the "batch 2" variables for the Analyzer observability,
  replication-config, and InnoDB-internals rule tranches (Elevarq/Analyzer#4004):
  observability — `slow_query_log`, `long_query_time`, `log_output`,
  `performance_schema`; replication/HA config — `gtid_strict_mode`,
  `gtid_domain_id` (the MariaDB GTID analogs), `expire_logs_days` (binlog
  retention), `rpl_semi_sync_master_enabled`, `rpl_semi_sync_slave_enabled`
  (MariaDB semi-sync naming), `log_slave_updates`; InnoDB internals —
  `innodb_adaptive_hash_index`, `innodb_flush_neighbors`,
  `innodb_change_buffering`, `innodb_read_io_threads`,
  `innodb_write_io_threads`; and server charset/collation —
  `character_set_server`, `collation_server` (the global complement of the
  per-table/per-column charset evidence of MDC-R015). The IN-list stays a
  curated allowlist (no `SHOW ALL VARIABLES`, no credential variables) and is
  version-tolerant: a variable absent on a given MariaDB version simply returns
  no row — never an error. MySQL-only names (`super_read_only`, `gtid_mode`,
  `enforce_gtid_consistency`, `binlog_expire_logs_seconds`, the
  `source`/`replica` semi-sync names, `log_replica_updates`) are not carried on
  the MariaDB collector, whose GTID/semi-sync model uses the names above.
  Read-only; not `HighSensitivity`.
- **MDC-R017** | `mariadb_global_status_v1` extends its `GLOBAL_STATUS` IN-list
  with the workload-counter and InnoDB-internals counters for the Analyzer
  workload-counter and InnoDB-internals rule tranches (umbrella
  Elevarq/Analyzer#4004) — the `GLOBAL_STATUS` analog of the MDC-R016
  `GLOBAL_VARIABLES` batch-2 expansion. Workload / query counters —
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
  Analyzer owns the interval (as with MDC-R012). The same counter set is carried
  as for MySQL (MYC-R015); these counters are present on both engines. The
  IN-list stays a curated allowlist (no `SHOW ALL STATUS`, no credentials) and
  is version-tolerant: a counter absent on a given MariaDB version simply
  returns no row — never an error, never a fabricated zero. MariaDB returns
  `GLOBAL_STATUS` names uppercased and the IN-list matches case-insensitively.
  Read-only; not `HighSensitivity`.
- **MDC-R018** | `mariadb_user_accounts_v1` is the user-account inventory for the
  Analyzer security/grants rule tranche (Elevarq/Analyzer#4004): remote-root /
  anonymous / passwordless accounts and weak/legacy auth plugins. It reads the
  read-only `mysql.user` view and its `SELECT` list is **exactly** `User`,
  `Host`, `plugin`, and a *computed boolean* `((Password = '' OR Password IS
  NULL) AND (authentication_string = '' OR authentication_string IS NULL)) AS
  has_empty_password` (the MariaDB `mysql.user` view retains the legacy `Password`
  column alongside `authentication_string`; both empty ⇒ no password). The hash
  columns (`Password`, `authentication_string`) MUST NEVER appear in the select
  list, the normalized evidence, logs, or any error (INV-MDC-04): the
  empty-password signal is carried solely as the derived boolean. **Lowest-
  exposure source + owner-decision flag**: MariaDB (unlike MySQL 8) has no
  reliable hash-free single source. The `mysql.user` view is the lowest-exposure
  SELECT-able source that works under the baseline grant, but MariaDB documents
  the view "doesn't show alternative authentication plugins" — so `plugin` is
  reliable for classic `mysql_native_password` accounts yet can be
  empty/`mysql_native_password` for accounts whose real plugin (e.g. `ed25519`,
  `unix_socket`, multi-auth) lives in the `mysql.global_priv` JSON. We
  deliberately DO NOT read `mysql.global_priv` because its JSON `Priv` column
  embeds the hash; reading the view keeps the collector hash-free at the cost of
  plugin precision for alternative-auth accounts. This residual trade-off is an
  **owner decision flagged for review** (see issue #693 report), not silently
  resolved by requiring the hash-exposing source. Metadata only (no row data);
  read-only; not `HighSensitivity`.
- **MDC-R019** | `mariadb_user_privileges_v1` is the global-privilege inventory
  (`SELECT GRANTEE, PRIVILEGE_TYPE, IS_GRANTABLE FROM
  information_schema.USER_PRIVILEGES`) so the Analyzer security rules can see
  `*.*` privilege sprawl and `WITH GRANT OPTION` (`IS_GRANTABLE='YES'`).
  `USER_PRIVILEGES` carries no password/hash column, so it is secret-free by
  construction (INV-MDC-04). Both security collectors read the `mysql` schema /
  its view, covered by the baseline global `SELECT` grant
  (`mariadb-grants.md` MDG-R001); where the monitoring role lacks that read, the
  collector records a permission-denied `Err` and the rest of the catalog still
  runs (MDC-R002) — a legitimate degraded skip, never a cycle failure, and no
  grant beyond the baseline `SELECT, PROCESS` is ever required. Metadata only;
  read-only; not `HighSensitivity`.
- **MDC-R020** | `mariadb_global_variables_v1` additionally carries `sql_mode` —
  the comma-separated server SQL-mode flag string (e.g.
  `STRICT_TRANS_TABLES,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION,...`) —
  for the Analyzer `sql_mode` advisory rule (Elevarq/Analyzer#4010), which flags
  unsafe or non-strict mode combinations. `sql_mode` is a shared MySQL/MariaDB
  server variable (same name on both engines), added to the same curated,
  version-tolerant IN-list (no `SHOW ALL VARIABLES`, no credential variables):
  `sql_mode` absent on a given build simply returns no row — never an error. No
  data content (only the mode-flag string), same posture as MDC-R016 / MDC-R018.
  Read-only; not `HighSensitivity`.
- **MDC-R021** | `server_identity_v1` (the neutral, engine-agnostic identity
  collector) supplies the connected `database_name` via `SELECT DATABASE() AS
  database_name, VERSION()`. `DATABASE()` resolves to the target DSN's dbname
  (`internal/mysqlwire` sets it on the connection). The Analyzer keys a
  database's identity on `server_identity_v1.database_name`; the engine-specific
  `mariadb_identity_v1` row is not read for identity, so without this neutral row
  two targets on ONE MariaDB server (the `tpcc` OLTP schema and the `tpch` OLAP
  schema) both arrive with an empty database name and collapse into a single
  Workbench card. MariaDB `performance_schema` is server-wide, so the
  statement-digest population is shared across those cards, but the per-database
  schema + identity is distinct — matching the PostgreSQL path, which emits
  `server_identity_v1` with `current_database()`. A NULL `DATABASE()` (a target
  with no dbname) is carried through as an absent value — no card split — never
  an error. Read-only; baseline `SELECT, PROCESS` only; not `HighSensitivity`.

## Invariants

- **INV-MDC-01** | Collection never issues a non-`SELECT` statement.
- **INV-MDC-02** | `mariadb_processlist_v1` is `HighSensitivity` with `INFO` in
  `SensitiveColumns`.
- **INV-MDC-03** | No MariaDB collector selecting a query-text column (`INFO`,
  `trx_query`, `DIGEST_TEXT`) or locked-key data (`lock_data`) escapes the
  HighSensitivity + redaction contract (MDC-R004 / MDC-R008). The catalog-wide
  scan matches column names case-insensitively but each `SensitiveColumns` entry
  uses the exact result-row case so runtime redaction actually fires.
- **INV-MDC-04** | No credential/secret material ever appears in the security
  collectors. No MariaDB catalog statement names a password/hash column
  (`Password`, `authentication_string`, or any `*password*`/hash column) in its
  `SELECT` list, and the normalized evidence of `mariadb_user_accounts_v1` /
  `mariadb_user_privileges_v1` contains no such column key and no hash-shaped
  value — the empty-password signal is carried only as the derived boolean
  `has_empty_password`. A structural guard independent of redaction: the hash is
  never fetched in the first place. (The `mysql.global_priv` JSON source, which
  embeds the hash, is deliberately not read — MDC-R018.)

## Failure conditions

| Trigger | Response |
|---------|----------|
| A collector's SQL errors (missing view / permission) | Captured in `Result.Err`; other collectors still run (MDC-R002). |
| A standalone server (no replica GTID) | `mariadb_replication_status_v1` reports the standalone state (empty GTID positions), never a failure. |
| Galera not configured / not connected | `mariadb_galera_status_v1` reports `WSREP_READY=OFF` / `Disconnected`, a legitimate state, never a failure. |
| `performance_schema` off (MariaDB default) — `mariadb_metadata_locks_v1` | A successful zero-row result, never a failure. |
| `performance_schema` off (MariaDB default) — `mariadb_statement_digests_v1` | DEGRADED / NOT_ASSESSED: the 0-row result is probed (`SELECT @@performance_schema`) and recorded `status=skipped`, `reason=perf_schema_disabled` (MDC-R010), never a silent success and never a failure. Enabling perf_schema on the server is a deploy concern (tracked in Elevarq/timeseries-demo), out of Signals' scope. |
| `performance_schema` on, no statements recorded — `mariadb_statement_digests_v1` | A legitimate zero-row `success` ("no slow queries"); the probe shows perf_schema ON (MDC-R010). |
| The `@@performance_schema` probe itself errors | The run keeps its pre-probe 0-row `success` outcome; the probe never turns a success into a failure (MDC-R011). |
| Idle server (no transactions/locks) | The activity collectors return successful zero-row results. |
| Busy server | The activity collectors are bounded by `LIMIT` (MDC-R006). |
| The monitoring role cannot read the `mysql` schema | `mariadb_user_accounts_v1` records a permission-denied `Err`; the other collectors still run (MDC-R002 / MDC-R019) — a degraded skip, never a cycle failure. |
| An account uses an alternative auth plugin not shown by the `mysql.user` view | `plugin` may be empty/`mysql_native_password`; `has_empty_password` is still correct. Documented MariaDB view limitation (MDC-R018), an owner-flagged trade-off, not a failure. |

## Constraints / NFR

- Read-only; no writes; no superuser. Monitoring role = `GRANT SELECT, PROCESS
  ON *.*` (see `specifications/mariadb-grants.md`). `PROCESS` surfaces all
  sessions in `INNODB_TRX` / `PROCESSLIST`; no additional grant is required
  (confirmed live as `signals_ro` against MariaDB 11.4.13 — every collector ran
  clean).
- The activity collectors are bounded: `INNODB_TRX` LIMIT 200 (oldest first),
  `INNODB_LOCKS` / `INNODB_LOCK_WAITS` / `metadata_locks` LIMIT 500.
- Columns confirmed live; regenerate against the pinned MariaDB image if the
  server version changes shape.

## Traceability

Acceptance: `specifications/mariadb-collectors.acceptance.md`.
Matrix: `features/signals/traceability.md` (MDC-R001..R021, INV-MDC-01..04).
The Tier-1 evidence contract this serves is Analyzer's
`replay-evidence-contract.md` (#3891 follow-on); the live semantic qualification
that the evidence survives collection → persistence → export is
`TestMariaDBLiveReplayTier1Evidence` + `VerifyReplayTier1Evidence`.
