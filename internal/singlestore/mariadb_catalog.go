package singlestore

// MariaDB collector catalog (specifications/mariadb-collectors.md). Collected over
// the SAME shared MySQL-wire runtime (Collect / NewRunner / redaction) as MySQL
// and SingleStore — only the catalog differs. Every statement is a read-only
// SELECT, confirmed live against a MariaDB 11.4 server (not authored blind).
//
// MariaDB's system views differ from MySQL 8 (#616 brings the catalog to parity
// by capability, with MariaDB-native SQL — not a copy of MySQL's):
//   - GLOBAL_VARIABLES / GLOBAL_STATUS (and wsrep/Galera status) live in
//     information_schema (MySQL 8 moved them to performance_schema).
//   - InnoDB transactions + locks live in information_schema: INNODB_TRX,
//     INNODB_LOCKS, INNODB_LOCK_WAITS (MySQL 8.0 removed the latter two and uses
//     performance_schema.data_locks / data_lock_waits, which MariaDB lacks).
//     INNODB_METRICS IS available under SELECT, PROCESS (an earlier claim that it
//     was not was wrong — disproven live against MariaDB 11.4.13).
//   - performance_schema is OFF by default, so metadata_locks and
//     events_statements_summary_by_digest return a legitimate empty result until
//     an operator enables it (perf_schema-gated, not absent).
//   - MariaDB has Galera (wsrep), not Group Replication, and exposes replica
//     state via GTID server variables, not a SHOW SLAVE STATUS table.
// Every collector here succeeds under GRANT SELECT, PROCESS.

// mariadbCatalog returns the validated MariaDB collector set. Columns were
// confirmed live against MariaDB 11.4; all statements are read-only SELECTs
// (INV-MDC-01).
func mariadbCatalog() []Collector {
	return []Collector{
		{
			ID:       "mariadb_identity_v1",
			Category: "server",
			SQL:      "SELECT VERSION() AS version, @@version_comment AS version_comment",
		},
		{
			// #699: the neutral server_identity_v1 row carries the connected
			// database_name (DATABASE(), resolved from the target DSN's dbname —
			// internal/mysqlwire sets it). The Analyzer keys a database's
			// identity on server_identity_v1.database_name; without it, two
			// targets on ONE MariaDB server (tpcc OLTP + tpch OLAP) both arrive
			// with an empty name and collapse into a single Workbench card.
			// MariaDB performance_schema is server-wide, so the statement-digest
			// population is shared across those cards, but the per-database
			// schema + identity is distinct — matching the PostgreSQL path which
			// emits server_identity_v1 with current_database(). DATABASE() is NULL
			// when the target carries no dbname; carried through as absent, never
			// an error.
			ID:       "server_identity_v1",
			Category: "server",
			SQL:      "SELECT DATABASE() AS database_name, VERSION() AS version",
		},
		{
			ID:       "mariadb_global_variables_v1",
			Category: "config",
			SQL:      "SELECT VARIABLE_NAME, VARIABLE_VALUE FROM information_schema.GLOBAL_VARIABLES WHERE VARIABLE_NAME IN ('max_connections', 'innodb_buffer_pool_size', 'max_allowed_packet', 'wait_timeout', 'version', 'log_bin', 'read_only', 'innodb_flush_log_at_trx_commit', 'tmp_table_size', 'max_heap_table_size', 'innodb_log_file_size', 'innodb_log_files_in_group', 'innodb_redo_log_capacity', 'sort_buffer_size', 'join_buffer_size', 'table_open_cache', 'table_definition_cache', 'thread_cache_size', 'innodb_io_capacity', 'innodb_io_capacity_max', 'innodb_flush_method', 'innodb_buffer_pool_instances', 'binlog_format', 'sync_binlog', 'skip_name_resolve', 'slow_query_log', 'long_query_time', 'log_output', 'performance_schema', 'gtid_strict_mode', 'gtid_domain_id', 'expire_logs_days', 'rpl_semi_sync_master_enabled', 'rpl_semi_sync_slave_enabled', 'log_slave_updates', 'innodb_adaptive_hash_index', 'innodb_flush_neighbors', 'innodb_change_buffering', 'innodb_read_io_threads', 'innodb_write_io_threads', 'character_set_server', 'collation_server', 'sql_mode')",
		},
		{
			// #656: Innodb_buffer_pool_reads + Innodb_buffer_pool_read_requests
			// added for Analyzer's buffer-pool-read-pressure rate rule (#3918).
			// Cumulative counters — no producer-side delta; the Analyzer owns the
			// interval (MDC-R012). Confirmed present live across MariaDB
			// 10.11/11.4/12.3. MariaDB returns GLOBAL_STATUS names uppercased; the
			// IN-list matches case-insensitively.
			//
			// #678 (MDC-R017): the GLOBAL_STATUS analog of the #679/MDC-R016
			// GLOBAL_VARIABLES batch-2 expansion — workload-counter and
			// InnoDB-internals counters for the Analyzer workload-counter and
			// InnoDB-internals rule tranches (umbrella Elevarq/Analyzer#4004). Same
			// counter set as MySQL (MYC-R015); these counters are present on both
			// engines. Workload/query: Created_tmp_disk_tables/Created_tmp_tables
			// (on-disk temp-table ratio), Select_scan/Select_full_join (scan/join
			// rate), Sort_merge_passes (sort-buffer pressure), Handler_read_rnd_next
			// (scan-heavy), Threads_created/Table_open_cache_hits/
			// Table_open_cache_misses (thread + table-cache miss rate). InnoDB:
			// Innodb_buffer_pool_pages_dirty/_total (dirty/flush pressure),
			// Innodb_buffer_pool_wait_free/Innodb_log_waits (redo/flush stalls),
			// Innodb_row_lock_waits/Innodb_row_lock_time (lock pressure), and
			// Innodb_deadlocks where present (enables the deadlock-RATE rule; cf.
			// the deadlock-observability rule Elevarq/Analyzer#4009). All are
			// cumulative counters. The IN-list stays a curated allowlist (no SHOW
			// ALL STATUS, no credentials), is version-tolerant (an absent counter
			// returns no row, never an error), and matches the uppercased MariaDB
			// GLOBAL_STATUS names case-insensitively.
			ID:       "mariadb_global_status_v1",
			Category: "resources",
			SQL:      "SELECT VARIABLE_NAME, VARIABLE_VALUE FROM information_schema.GLOBAL_STATUS WHERE VARIABLE_NAME IN ('Threads_connected', 'Threads_running', 'Uptime', 'Questions', 'Slow_queries', 'Aborted_connects', 'Max_used_connections', 'Open_tables', 'Innodb_buffer_pool_reads', 'Innodb_buffer_pool_read_requests', 'Created_tmp_disk_tables', 'Created_tmp_tables', 'Select_scan', 'Select_full_join', 'Sort_merge_passes', 'Handler_read_rnd_next', 'Threads_created', 'Table_open_cache_hits', 'Table_open_cache_misses', 'Innodb_buffer_pool_pages_dirty', 'Innodb_buffer_pool_pages_total', 'Innodb_buffer_pool_wait_free', 'Innodb_log_waits', 'Innodb_row_lock_waits', 'Innodb_row_lock_time', 'Innodb_deadlocks')",
		},
		{
			ID:               "mariadb_processlist_v1",
			Category:         "activity",
			SQL:              "SELECT ID, USER, HOST, DB, COMMAND, TIME, STATE, INFO FROM information_schema.PROCESSLIST",
			HighSensitivity:  true,
			SensitiveColumns: []string{"INFO"},
		},
		{
			// #677 (MDC-R014): AUTO_INCREMENT is the current AUTO_INCREMENT counter
			// value — the evidence for Analyzer's AUTO_INCREMENT-exhaustion-risk
			// rule (Elevarq/Analyzer#4004), which compares it against the AI
			// column's type max (derived from the COLUMN_TYPE/DATA_TYPE already
			// captured by mariadb_columns_v1) to compute headroom. It is NULL for a
			// table with no AUTO_INCREMENT column and is carried through as an
			// absent (nil) value exactly like the other nullable sizing columns —
			// never an error, never a fabricated zero. Still information_schema.
			// TABLES metadata only (no row data); read-only; not HighSensitivity.
			//
			// #684 (MDC-R015): TABLE_COLLATION is the per-table collation —
			// evidence for Analyzer's utf8mb4-readiness / charset-collation-mismatch
			// rule (Elevarq/Analyzer#4001), which flags legacy utf8(=utf8mb3)/latin1
			// and collations inconsistent with the database default (from
			// mariadb_schemata_v1). The charset family is derivable from the
			// collation; information_schema.TABLES has no direct charset column.
			// Still metadata only; no new privilege; not HighSensitivity.
			ID:       "mariadb_table_statistics_v1",
			Category: "schema",
			SQL:      "SELECT TABLE_SCHEMA, TABLE_NAME, ENGINE, TABLE_ROWS, DATA_LENGTH, INDEX_LENGTH, AUTO_INCREMENT, TABLE_COLLATION FROM information_schema.TABLES WHERE TABLE_SCHEMA NOT IN ('mysql', 'performance_schema', 'information_schema', 'sys')",
		},
		{
			ID:       "mariadb_engines_v1",
			Category: "server",
			SQL:      "SELECT ENGINE, SUPPORT, TRANSACTIONS, XA, SAVEPOINTS FROM information_schema.ENGINES",
		},
		{
			ID:       "mariadb_schemata_v1",
			Category: "schema",
			SQL:      "SELECT SCHEMA_NAME, DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA",
		},
		{
			// #538 (Replay Tier-1 evidence): per-column inventory so Analyzer
			// Replay reconstructs valid DDL without inventing columns, types, or
			// nullability. Identical acquisition SQL to MySQL (empirically verified
			// equivalent across MySQL 8.0/8.4/9.7 + MariaDB 10.11/11.4/11.8/12.3),
			// but a DISTINCT MariaDB collector identity. COLUMN_TYPE is emitted
			// engine-native: MariaDB retains the integer display width
			// (`bigint(20)`/`int(11)`) MySQL 8.0 dropped — correct DDL for MariaDB,
			// never normalized to MySQL. ORDINAL_POSITION preserves column order.
			// Schema metadata only -> not HighSensitivity.
			//
			// #684 (MDC-R015): CHARACTER_SET_NAME / COLLATION_NAME are the per-column
			// charset/collation — evidence for Analyzer's utf8mb4-readiness /
			// charset-collation-mismatch rule (Elevarq/Analyzer#4001). Both are NULL
			// for non-string columns (int, date, etc.) and are carried through as an
			// absent (nil) value exactly like the other nullable columns — never an
			// error. Still schema metadata only; no new privilege.
			ID:       "mariadb_columns_v1",
			Category: "schema",
			SQL:      "SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, ORDINAL_POSITION, COLUMN_TYPE, DATA_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_DEFAULT, EXTRA, CHARACTER_SET_NAME, COLLATION_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA NOT IN ('mysql', 'performance_schema', 'information_schema', 'sys') ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION",
		},
		{
			// #538 (Replay Tier-1 evidence): per-index inventory. INDEX_NAME='PRIMARY'
			// carries primary-key identity losslessly (no separate PK collector);
			// NON_UNIQUE=0 marks unique/PK vs ordinary; SEQ_IN_INDEX preserves
			// composite-index ordering exactly. #640 Replay Tier-2: CARDINALITY is
			// the engine's precomputed distinct-value estimate for the index prefix
			// — the per-index selectivity the Analyzer replay-evidence-contract
			// Tier-2 leads with (consumed by Elevarq/Analyzer#4142). Read-only
			// metadata from the SAME information_schema.STATISTICS row; MariaDB
			// populates it from its engine-independent statistics / InnoDB stats,
			// NULL until ANALYZE (carried as absent). Identical acquisition SQL to
			// MySQL, verified equivalent live across the MariaDB LTS lines; distinct
			// MariaDB identity. Schema metadata only -> not HighSensitivity.
			ID:       "mariadb_index_inventory_v1",
			Category: "schema",
			SQL:      "SELECT TABLE_SCHEMA, TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX, COLUMN_NAME, NON_UNIQUE, INDEX_TYPE, NULLABLE, SUB_PART, COLLATION, CARDINALITY FROM information_schema.STATISTICS WHERE TABLE_SCHEMA NOT IN ('mysql', 'performance_schema', 'information_schema', 'sys') ORDER BY TABLE_SCHEMA, TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX",
		},
		{
			// #640 Replay Tier-2: per-index-PREFIX cardinality from InnoDB's
			// persistent statistics (mysql.innodb_index_stats). The `n_diff_pfxNN`
			// rows carry MariaDB's estimated distinct-value count per index prefix —
			// finer than information_schema.STATISTICS.CARDINALITY and the direct
			// input to the Analyzer row-estimate injection (Analyzer#4142). MariaDB
			// ships InnoDB persistent stats in the same `mysql.innodb_index_stats`
			// table; distinct MariaDB identity. Read-only metadata (baseline global
			// SELECT grant; privilege denial is a legitimate skip). Schema-bounded.
			// Signals reads the engine's precomputed stat; never computes it. Not
			// HighSensitivity.
			ID:       "mariadb_innodb_index_stats_v1",
			Category: "schema",
			SQL:      "SELECT database_name, table_name, index_name, stat_name, stat_value, sample_size FROM mysql.innodb_index_stats WHERE database_name NOT IN ('mysql', 'performance_schema', 'information_schema', 'sys') AND stat_name LIKE 'n_diff_pfx%' ORDER BY database_name, table_name, index_name, stat_name",
		},
		{
			// #640 Replay Tier-2: per-COLUMN selectivity from MariaDB's
			// Engine-Independent Table Statistics (mysql.column_stats, populated
			// by ANALYZE TABLE ... PERSISTENT). nulls_ratio / avg_length /
			// avg_frequency are the per-column distribution inputs the Analyzer
			// row-estimate injection (Analyzer#4142) uses to refine single- and
			// multi-column selectivity. MariaDB-ONLY: MySQL has no column_stats
			// equivalent (MySQL ships histograms via information_schema.
			// COLUMN_STATISTICS — a later increment).
			//
			// SAFETY: mysql.column_stats also carries min_value / max_value —
			// ACTUAL column data values (potential PII) — and a histogram blob.
			// This SELECT deliberately projects ONLY the three distribution
			// scalars and NEVER a data value; the histogram is deferred to a
			// later increment. Read-only mysql-schema metadata (baseline global
			// SELECT grant; a privilege denial is a legitimate skip). Schema-
			// bounded. Signals reads the engine's precomputed stat; never
			// computes it. Not HighSensitivity.
			ID:       "mariadb_column_stats_v1",
			Category: "schema",
			SQL:      "SELECT db_name, table_name, column_name, nulls_ratio, avg_length, avg_frequency FROM mysql.column_stats WHERE db_name NOT IN ('mysql', 'performance_schema', 'information_schema', 'sys') ORDER BY db_name, table_name, column_name",
		},
		{
			// #640 Replay Tier-3: per-partition inventory from information_schema.
			// PARTITIONS — partitioning method, the partitioning expression, the
			// range/list boundaries (PARTITION_DESCRIPTION), the ordinal, and the
			// per-partition TABLE_ROWS estimate. This lets the Analyzer model
			// partition pruning and per-partition row estimates (Analyzer#4142).
			// MariaDB exposes the same information_schema.PARTITIONS shape as MySQL;
			// distinct MariaDB identity. Bounded to actually-partitioned tables via
			// PARTITION_NAME IS NOT NULL (a non-partitioned table surfaces a single
			// NULL-partition row we skip), so output is one row per real partition,
			// never one per table.
			//
			// SAFETY: the boundaries (PARTITION_DESCRIPTION) and the partitioning
			// expression are DBA-authored DDL — they appear verbatim in SHOW CREATE
			// TABLE and are part of the schema definition, NOT sampled column data
			// (contrast the i3 min_value/max_value exclusion, which ARE observed data
			// values). Read-only; schema-bounded; engine-precomputed, never computed.
			// Not HighSensitivity.
			ID:       "mariadb_partitions_v1",
			Category: "schema",
			SQL:      "SELECT TABLE_SCHEMA, TABLE_NAME, PARTITION_NAME, SUBPARTITION_NAME, PARTITION_ORDINAL_POSITION, PARTITION_METHOD, SUBPARTITION_METHOD, PARTITION_EXPRESSION, SUBPARTITION_EXPRESSION, PARTITION_DESCRIPTION, TABLE_ROWS FROM information_schema.PARTITIONS WHERE TABLE_SCHEMA NOT IN ('mysql', 'performance_schema', 'information_schema', 'sys') AND PARTITION_NAME IS NOT NULL ORDER BY TABLE_SCHEMA, TABLE_NAME, PARTITION_ORDINAL_POSITION",
		},
		{
			// #616: InnoDB engine counters (MariaDB keeps INNODB_METRICS in
			// information_schema; MariaDB uses the ENABLED flag, not MySQL's
			// STATUS='enabled').
			ID:       "mariadb_innodb_metrics_v1",
			Category: "resources",
			SQL:      "SELECT NAME, SUBSYSTEM, COUNT, TYPE, COMMENT FROM information_schema.INNODB_METRICS WHERE ENABLED=1",
		},
		{
			// #656 (MDC-R013): the cumulative InnoDB deadlock count for Analyzer's
			// innodb.deadlock_rate rate rule (#3918). Selected unconditionally by
			// NAME — deliberately WITHOUT the ENABLED=1 filter the general
			// mariadb_innodb_metrics_v1 uses — so the single lock_deadlocks row is
			// always present with its ENABLED flag. This makes the signal module-
			// independent: ENABLED=1 (the default on MariaDB 10.11/11.4/12.3) ->
			// COUNT is the live cumulative counter; ENABLED=0 -> the consumer treats
			// it as NOT_ASSESSED rather than a silent zero. COUNT is a cumulative
			// counter (no producer-side delta; the Analyzer owns the interval).
			// Bounded to one row; read-only; no sensitive payload.
			ID:       "mariadb_innodb_deadlocks_v1",
			Category: "resources",
			SQL:      "SELECT NAME, COUNT, ENABLED FROM information_schema.INNODB_METRICS WHERE NAME = 'lock_deadlocks'",
		},
		{
			// #616: active InnoDB transactions (point-in-time session evidence).
			// trx_query carries the transaction's live SQL text -> HighSensitivity +
			// redacted; the result key is lowercase trx_query. Oldest-first +
			// bounded. trx_state distinguishes RUNNING vs LOCK WAIT. Empty on an idle
			// server is a legitimate success. Needs the PROCESS grant (held).
			ID:               "mariadb_innodb_transactions_v1",
			Category:         "activity",
			SQL:              "SELECT trx_id, trx_state, trx_started, trx_requested_lock_id, trx_wait_started, trx_weight, trx_mysql_thread_id, trx_query, trx_operation_state, trx_tables_in_use, trx_tables_locked, trx_rows_locked, trx_rows_modified, trx_isolation_level FROM information_schema.INNODB_TRX ORDER BY trx_started LIMIT 200",
			HighSensitivity:  true,
			SensitiveColumns: []string{"trx_query"},
		},
		{
			// #616: the active lock inventory (MariaDB's information_schema.INNODB_LOCKS,
			// which MySQL 8.0 removed). lock_data is the locked index-key value (row
			// data, not SQL) -> HighSensitivity + redacted; native lock descriptors
			// (mode/type/table/index) are preserved. Bounded. Empty when no locks are
			// held is a legitimate success.
			ID:               "mariadb_innodb_locks_v1",
			Category:         "activity",
			SQL:              "SELECT lock_id, lock_trx_id, lock_mode, lock_type, lock_table, lock_index, lock_space, lock_page, lock_rec, lock_data FROM information_schema.INNODB_LOCKS LIMIT 500",
			HighSensitivity:  true,
			SensitiveColumns: []string{"lock_data"},
		},
		{
			// #616: the blocked -> blocking wait-for edges (trx + lock ids only; no
			// SQL, no row data). Empty when nothing is blocked is a legitimate
			// success.
			ID:       "mariadb_innodb_lock_waits_v1",
			Category: "activity",
			SQL:      "SELECT requesting_trx_id, requested_lock_id, blocking_trx_id, blocking_lock_id FROM information_schema.INNODB_LOCK_WAITS LIMIT 500",
		},
		{
			// #616: metadata-lock (MDL) inventory. performance_schema is OFF by
			// default on MariaDB, so this is a legitimate empty result until an
			// operator enables it (perf_schema-gated, not a failure). PENDING-first +
			// bounded. Object schema/name only, no SQL or row data.
			ID:       "mariadb_metadata_locks_v1",
			Category: "activity",
			SQL:      "SELECT OBJECT_TYPE, OBJECT_SCHEMA, OBJECT_NAME, LOCK_TYPE, LOCK_DURATION, LOCK_STATUS, SOURCE, OWNER_THREAD_ID FROM performance_schema.metadata_locks ORDER BY LOCK_STATUS DESC LIMIT 500",
		},
		{
			// #616: the query-workload signal (MariaDB's perf_schema digest summary).
			// DIGEST_TEXT is the normalized sample SQL -> HighSensitivity + redacted.
			// perf_schema is OFF by default on MariaDB -> the digest table is empty
			// until an operator enables it. #665: a 0-row result is probed
			// (@@performance_schema) and, when OFF, recorded as a DEGRADED /
			// NOT_ASSESSED skip (perf_schema_disabled) rather than a silent empty
			// success — so a consumer can tell "unavailable" from "no slow queries".
			// Top statements by accumulated latency, bounded. A digest id is never
			// equated with a PostgreSQL queryid.
			ID:               "mariadb_statement_digests_v1",
			Category:         "workload",
			SQL:              "SELECT SCHEMA_NAME, DIGEST, DIGEST_TEXT, COUNT_STAR, SUM_TIMER_WAIT, SUM_LOCK_TIME, SUM_ROWS_EXAMINED, SUM_ROWS_SENT, SUM_ERRORS, SUM_SELECT_SCAN, SUM_NO_INDEX_USED, FIRST_SEEN, LAST_SEEN FROM performance_schema.events_statements_summary_by_digest ORDER BY SUM_TIMER_WAIT DESC LIMIT 200",
			HighSensitivity:  true,
			SensitiveColumns: []string{"DIGEST_TEXT"},
			StructuralColumn: "DIGEST_TEXT", // #647 structural-query opt-in column
			PerfSchemaGated:  true,          // #665 (MDC-R010): empty when perf_schema OFF
		},
		{
			// #616: MariaDB replica-state signal. MariaDB has no SHOW SLAVE STATUS
			// table nor the MySQL perf_schema replication_* views; the GTID + binlog
			// server variables are the SELECT-able source. Standalone (no replica) ->
			// empty GTID positions, a legitimate distinct state.
			ID:       "mariadb_replication_status_v1",
			Category: "topology",
			SQL:      "SELECT @@global.server_id AS server_id, @@global.log_bin AS log_bin, @@global.read_only AS read_only, @@global.gtid_current_pos AS gtid_current_pos, @@global.gtid_binlog_pos AS gtid_binlog_pos, @@global.gtid_slave_pos AS gtid_slave_pos",
		},
		{
			// #616: Galera (wsrep) cluster state via information_schema.GLOBAL_STATUS
			// — MariaDB's Group-Replication analogue. A standalone server reports
			// WSREP_READY=OFF / WSREP_CLUSTER_STATUS=Disconnected / size 0, a
			// legitimate "not clustered" state, never a failure.
			ID:       "mariadb_galera_status_v1",
			Category: "topology",
			SQL:      "SELECT VARIABLE_NAME, VARIABLE_VALUE FROM information_schema.GLOBAL_STATUS WHERE VARIABLE_NAME IN ('WSREP_CLUSTER_SIZE', 'WSREP_CLUSTER_STATUS', 'WSREP_CONNECTED', 'WSREP_READY', 'WSREP_LOCAL_STATE_COMMENT', 'WSREP_LOCAL_RECV_QUEUE', 'WSREP_FLOW_CONTROL_PAUSED', 'WSREP_CERT_DEPS_DISTANCE')",
		},
		{
			// #693 (MDC-R018): the user-account inventory for the Analyzer
			// security/grants rule tranche (umbrella Elevarq/Analyzer#4004):
			// remote-root / anonymous / passwordless accounts and weak/legacy auth
			// plugins. THE MOST SENSITIVE COLLECTOR — it reads the read-only
			// mysql.user VIEW, so the SELECT list is proven secret-free by
			// construction (INV-MDC-04): it carries ONLY User, Host, plugin, and a
			// COMPUTED boolean has_empty_password. The Password / authentication_string
			// hash columns (the MariaDB mysql.user view retains both) are NEVER named
			// in the select list, the normalized evidence, logs, or any error — the
			// empty-password signal is the derived boolean only: both hash columns
			// empty/NULL ⇒ no password.
			//
			// LOWEST-EXPOSURE SOURCE + OWNER-DECISION FLAG: MariaDB (unlike MySQL 8)
			// has NO reliable hash-free single source. The mysql.user VIEW is the
			// lowest-exposure SELECT-able source that works under the baseline grant;
			// we deliberately DO NOT read mysql.global_priv because its JSON Priv
			// column embeds the hash. The documented cost is plugin precision:
			// MariaDB states the mysql.user view "doesn't show alternative
			// authentication plugins", so plugin can be empty/mysql_native_password
			// for accounts whose real plugin (ed25519, unix_socket, multi-auth) lives
			// in the JSON. has_empty_password stays correct regardless. This residual
			// trade-off is flagged for owner review (issue #693), not silently
			// resolved by requiring the hash-exposing source. Reading the mysql schema
			// is covered by the baseline global SELECT grant (mariadb-grants.md
			// MDG-R001); a permission denial is a legitimate skip (MDC-R002/R019).
			// Metadata only (no row data); read-only; not HighSensitivity.
			ID:       "mariadb_user_accounts_v1",
			Category: "security",
			SQL:      "SELECT User, Host, plugin, ((Password = '' OR Password IS NULL) AND (authentication_string = '' OR authentication_string IS NULL)) AS has_empty_password FROM mysql.user",
		},
		{
			// #693 (MDC-R019): the global-privilege inventory so the Analyzer
			// security rules can see *.* privilege sprawl and WITH GRANT OPTION
			// (IS_GRANTABLE='YES'). information_schema.USER_PRIVILEGES has no
			// password/hash column (INV-MDC-04) — secret-free by construction. Under
			// the baseline global SELECT grant the account sees every grantee; a
			// narrower grant shows only its own rows (a legitimate reduced result).
			// Metadata only; read-only; not HighSensitivity.
			ID:       "mariadb_user_privileges_v1",
			Category: "security",
			SQL:      "SELECT GRANTEE, PRIVILEGE_TYPE, IS_GRANTABLE FROM information_schema.USER_PRIVILEGES",
		},
	}
}
