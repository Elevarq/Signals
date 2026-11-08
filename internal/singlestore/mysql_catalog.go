package singlestore

import "github.com/elevarq/signals/internal/engineid"

// MySQL collector catalog (specifications/mysql-collectors.md). Collected over
// the SAME shared MySQL-wire runtime (Collect / NewRunner / redaction) as
// SingleStore — only the catalog differs. Every statement is a read-only SELECT
// over information_schema / performance_schema, confirmed live against a MySQL
// 8.4 Community Server (not authored blind).

// CatalogFor returns the collector catalog for a MySQL-wire engine (MYC-R005):
// singlestore → the SingleStore catalog, mysql → the MySQL catalog, anything
// else → nil. The wire session uses this single registry to pick the catalog by
// the target's engine; it never runs a foreign engine's catalog, and there is
// no PostgreSQL fallback (PostgreSQL uses the separate pgx path).
func CatalogFor(engine string) []Collector {
	switch engine {
	case engineid.EngineSingleStore:
		return Catalog()
	case engineid.EngineMySQL:
		return mysqlCatalog()
	case engineid.EngineMariaDB:
		return mariadbCatalog()
	default:
		return nil
	}
}

// mysqlCatalog returns the validated MySQL collector set. Columns were confirmed
// live against MySQL 8.4; all statements are read-only SELECTs (INV-MYC-01).
func mysqlCatalog() []Collector {
	return []Collector{
		{
			ID:       "mysql_identity_v1",
			Category: "server",
			SQL:      "SELECT VERSION() AS version, @@version_comment AS version_comment, @@version_compile_os AS compile_os",
		},
		{
			// #699: the neutral server_identity_v1 row carries the connected
			// database_name (DATABASE(), which resolves from the target DSN's
			// dbname — internal/mysqlwire sets it). The Analyzer keys a
			// database's identity on server_identity_v1.database_name; without
			// it, two targets on ONE MySQL server (e.g. the tpcc OLTP + tpch OLAP
			// schemas) both arrive with an empty name and collapse into a single
			// Workbench card. MySQL performance_schema is server-wide, so the
			// statement-digest population is shared across those cards, but the
			// per-database schema + identity is distinct — matching the
			// PostgreSQL path, which already emits server_identity_v1 with
			// current_database(). DATABASE() is NULL when the target carries no
			// dbname; that is carried through as an absent value (no card split),
			// never an error.
			ID:       "server_identity_v1",
			Category: "server",
			SQL:      "SELECT DATABASE() AS database_name, VERSION() AS version",
		},
		{
			ID:       "mysql_global_variables_v1",
			Category: "config",
			SQL:      "SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_variables WHERE VARIABLE_NAME IN ('max_connections', 'innodb_buffer_pool_size', 'max_allowed_packet', 'wait_timeout', 'version', 'log_bin', 'read_only', 'innodb_flush_log_at_trx_commit', 'tmp_table_size', 'max_heap_table_size', 'innodb_log_file_size', 'innodb_log_files_in_group', 'innodb_redo_log_capacity', 'sort_buffer_size', 'join_buffer_size', 'table_open_cache', 'table_definition_cache', 'thread_cache_size', 'innodb_io_capacity', 'innodb_io_capacity_max', 'innodb_flush_method', 'innodb_buffer_pool_instances', 'binlog_format', 'sync_binlog', 'skip_name_resolve', 'slow_query_log', 'long_query_time', 'log_output', 'performance_schema', 'super_read_only', 'gtid_mode', 'enforce_gtid_consistency', 'binlog_expire_logs_seconds', 'expire_logs_days', 'rpl_semi_sync_source_enabled', 'rpl_semi_sync_replica_enabled', 'rpl_semi_sync_master_enabled', 'rpl_semi_sync_slave_enabled', 'log_replica_updates', 'log_slave_updates', 'innodb_adaptive_hash_index', 'innodb_flush_neighbors', 'innodb_change_buffering', 'innodb_read_io_threads', 'innodb_write_io_threads', 'character_set_server', 'collation_server', 'sql_mode')",
		},
		{
			// #656: Innodb_buffer_pool_reads + Innodb_buffer_pool_read_requests
			// added for Analyzer's buffer-pool-read-pressure rate rule (#3918).
			// Cumulative counters — no producer-side delta; the Analyzer owns the
			// interval (MYC-R010). Confirmed present live across MySQL 8.0/8.4/9.7.
			//
			// #678 (MYC-R015): the global_status analog of the #679/MYC-R014
			// global_variables batch-2 expansion — workload-counter and
			// InnoDB-internals counters for the Analyzer workload-counter and
			// InnoDB-internals rule tranches (umbrella Elevarq/Analyzer#4004).
			// Workload/query: Created_tmp_disk_tables/Created_tmp_tables (on-disk
			// temp-table ratio), Select_scan/Select_full_join (scan/join rate),
			// Sort_merge_passes (sort-buffer pressure), Handler_read_rnd_next
			// (scan-heavy), Threads_created/Table_open_cache_hits/
			// Table_open_cache_misses (thread + table-cache miss rate). InnoDB:
			// Innodb_buffer_pool_pages_dirty/_total (dirty/flush pressure),
			// Innodb_buffer_pool_wait_free/Innodb_log_waits (redo/flush stalls),
			// Innodb_row_lock_waits/Innodb_row_lock_time (lock pressure), and
			// Innodb_deadlocks where present (enables the deadlock-RATE rule; cf.
			// the deadlock-observability rule Elevarq/Analyzer#4009). All are
			// cumulative counters (no producer-side delta — the Analyzer owns the
			// interval). The IN-list stays a curated allowlist (no SHOW ALL STATUS,
			// no credentials) and is version-tolerant: a counter absent on a given
			// build simply returns no row, never an error.
			ID:       "mysql_global_status_v1",
			Category: "resources",
			SQL:      "SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME IN ('Threads_connected', 'Threads_running', 'Uptime', 'Questions', 'Slow_queries', 'Aborted_connects', 'Max_used_connections', 'Open_tables', 'Innodb_buffer_pool_reads', 'Innodb_buffer_pool_read_requests', 'Created_tmp_disk_tables', 'Created_tmp_tables', 'Select_scan', 'Select_full_join', 'Sort_merge_passes', 'Handler_read_rnd_next', 'Threads_created', 'Table_open_cache_hits', 'Table_open_cache_misses', 'Innodb_buffer_pool_pages_dirty', 'Innodb_buffer_pool_pages_total', 'Innodb_buffer_pool_wait_free', 'Innodb_log_waits', 'Innodb_row_lock_waits', 'Innodb_row_lock_time', 'Innodb_deadlocks')",
		},
		{
			ID:               "mysql_processlist_v1",
			Category:         "activity",
			SQL:              "SELECT ID, USER, HOST, DB, COMMAND, TIME, STATE, INFO FROM information_schema.PROCESSLIST",
			HighSensitivity:  true,
			SensitiveColumns: []string{"INFO"},
		},
		{
			ID:       "mysql_innodb_metrics_v1",
			Category: "resources",
			SQL:      "SELECT NAME, SUBSYSTEM, COUNT, STATUS FROM information_schema.INNODB_METRICS WHERE STATUS = 'enabled'",
		},
		{
			// #656 (MYC-R011): the cumulative InnoDB deadlock count for Analyzer's
			// innodb.deadlock_rate rate rule (#3918). Selected unconditionally by
			// NAME — deliberately WITHOUT the STATUS='enabled' filter the general
			// mysql_innodb_metrics_v1 uses — so the single lock_deadlocks row is
			// always present with its STATUS. This makes the signal module-
			// independent: STATUS='enabled' (the default on MySQL 8.0/8.4/9.7) ->
			// COUNT is the live cumulative counter; any other STATUS -> the consumer
			// treats it as NOT_ASSESSED rather than a silent zero. COUNT is a
			// cumulative counter (no producer-side delta; the Analyzer owns the
			// interval). Bounded to one row; read-only; no sensitive payload.
			ID:       "mysql_innodb_deadlocks_v1",
			Category: "resources",
			SQL:      "SELECT NAME, COUNT, STATUS FROM information_schema.INNODB_METRICS WHERE NAME = 'lock_deadlocks'",
		},
		{
			// #677 (MYC-R012): AUTO_INCREMENT is the current AUTO_INCREMENT counter
			// value — the evidence for Analyzer's AUTO_INCREMENT-exhaustion-risk
			// rule (Elevarq/Analyzer#4004), which compares it against the AI
			// column's type max (derived from the COLUMN_TYPE/DATA_TYPE already
			// captured by mysql_columns_v1) to compute headroom. It is NULL for a
			// table with no AUTO_INCREMENT column and is carried through as an
			// absent (nil) value exactly like the other nullable sizing columns —
			// never an error, never a fabricated zero. Still information_schema.
			// TABLES metadata only (no row data); read-only; not HighSensitivity.
			//
			// #684 (MYC-R013): TABLE_COLLATION is the per-table collation —
			// evidence for Analyzer's utf8mb4-readiness / charset-collation-mismatch
			// rule (Elevarq/Analyzer#4001), which flags legacy utf8(=utf8mb3)/latin1
			// and collations inconsistent with the database default (from
			// mysql_schemata_v1). The charset family is derivable from the collation;
			// information_schema.TABLES has no direct charset column. Still metadata
			// only; no new privilege; not HighSensitivity.
			ID:       "mysql_table_statistics_v1",
			Category: "schema",
			SQL:      "SELECT TABLE_SCHEMA, TABLE_NAME, ENGINE, TABLE_ROWS, DATA_LENGTH, INDEX_LENGTH, AUTO_INCREMENT, TABLE_COLLATION FROM information_schema.TABLES WHERE TABLE_SCHEMA NOT IN ('mysql', 'performance_schema', 'information_schema', 'sys')",
		},
		{
			ID:       "mysql_engines_v1",
			Category: "server",
			SQL:      "SELECT ENGINE, SUPPORT, TRANSACTIONS, XA, SAVEPOINTS FROM information_schema.ENGINES",
		},
		{
			// #640 Replay Tier-3: the foreign-key column graph from information_
			// schema.KEY_COLUMN_USAGE, restricted to FK rows via REFERENCED_TABLE_NAME
			// IS NOT NULL (PK/unique rows carry a NULL referenced table). Each row
			// maps an FK column to the schema/table/column it references, with the
			// constraint name and ordinal — the join-cardinality / FK-pruning signal
			// the Analyzer consumes (Analyzer#4142). All values are schema
			// identifiers (constraint, table, column names — verbatim in SHOW CREATE
			// TABLE), never row data. Read-only; schema-bounded (one row per FK
			// column); engine-precomputed, never computed. Not HighSensitivity.
			ID:       "mysql_foreign_keys_v1",
			Category: "schema",
			SQL:      "SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, CONSTRAINT_NAME, ORDINAL_POSITION, POSITION_IN_UNIQUE_CONSTRAINT, REFERENCED_TABLE_SCHEMA, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA NOT IN ('mysql', 'performance_schema', 'information_schema', 'sys') AND REFERENCED_TABLE_NAME IS NOT NULL ORDER BY TABLE_SCHEMA, TABLE_NAME, CONSTRAINT_NAME, ORDINAL_POSITION",
		},
		{
			ID:       "mysql_schemata_v1",
			Category: "schema",
			SQL:      "SELECT SCHEMA_NAME, DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA",
		},
		{
			// #538 (Replay Tier-1 evidence): per-column inventory so Analyzer
			// Replay can reconstruct valid DDL without inventing columns, types,
			// or nullability. COLUMN_TYPE is the DDL-faithful engine-native type
			// (MySQL emits `bigint`/`int`; MariaDB emits `bigint(20)`/`int(11)` —
			// each correct for its own engine, emitted verbatim). ORDINAL_POSITION
			// preserves declared column order. Schema metadata only (no SQL text,
			// no row data) -> not HighSensitivity. System schemas excluded.
			//
			// #684 (MYC-R013): CHARACTER_SET_NAME / COLLATION_NAME are the per-column
			// charset/collation — evidence for Analyzer's utf8mb4-readiness /
			// charset-collation-mismatch rule (Elevarq/Analyzer#4001). Both are NULL
			// for non-string columns (int, date, etc.) and are carried through as an
			// absent (nil) value exactly like the other nullable columns — never an
			// error. Still schema metadata only; no new privilege.
			ID:       "mysql_columns_v1",
			Category: "schema",
			SQL:      "SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, ORDINAL_POSITION, COLUMN_TYPE, DATA_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_DEFAULT, EXTRA, CHARACTER_SET_NAME, COLLATION_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA NOT IN ('mysql', 'performance_schema', 'information_schema', 'sys') ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION",
		},
		{
			// #538 (Replay Tier-1 evidence): per-index inventory so Replay sees the
			// real access paths. INDEX_NAME='PRIMARY' carries primary-key identity
			// losslessly (so no separate PK collector is needed); NON_UNIQUE=0
			// distinguishes unique/PK from ordinary indexes; SEQ_IN_INDEX preserves
			// composite-index column ordering exactly (e.g. idx_tenant_created ->
			// tenant_id seq 1, created_at seq 2). #640 Replay Tier-2: CARDINALITY
			// is the engine's precomputed estimate of distinct values in the index
			// prefix up to this column — the per-index selectivity the Analyzer
			// replay-evidence-contract Tier-2 leads with (consumed by
			// Elevarq/Analyzer#4142). Read-only metadata from the SAME
			// information_schema.STATISTICS row; Signals reads the engine's stat,
			// never computes it. NULL when ANALYZE has not populated it (carried as
			// absent). Schema metadata only -> not HighSensitivity. System schemas
			// excluded.
			ID:       "mysql_index_inventory_v1",
			Category: "schema",
			SQL:      "SELECT TABLE_SCHEMA, TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX, COLUMN_NAME, NON_UNIQUE, INDEX_TYPE, NULLABLE, SUB_PART, COLLATION, CARDINALITY FROM information_schema.STATISTICS WHERE TABLE_SCHEMA NOT IN ('mysql', 'performance_schema', 'information_schema', 'sys') ORDER BY TABLE_SCHEMA, TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX",
		},
		{
			// #640 Replay Tier-2: per-index-PREFIX cardinality from InnoDB's
			// persistent statistics (mysql.innodb_index_stats). The `n_diff_pfxNN`
			// rows carry the engine's estimated distinct-value count for each index
			// prefix length — finer-grained than information_schema.STATISTICS.
			// CARDINALITY and the direct input to the Analyzer row-estimate
			// injection (Analyzer#4142). Read-only metadata from the `mysql` system
			// schema (baseline global SELECT grant; a privilege denial is a
			// legitimate skip, not a failure). Bounded by schema size (one row per
			// index prefix), not data size. Signals reads the engine's precomputed
			// stat; it never computes it. Not HighSensitivity.
			ID:       "mysql_innodb_index_stats_v1",
			Category: "schema",
			SQL:      "SELECT database_name, table_name, index_name, stat_name, stat_value, sample_size FROM mysql.innodb_index_stats WHERE database_name NOT IN ('mysql', 'performance_schema', 'information_schema', 'sys') AND stat_name LIKE 'n_diff_pfx%' ORDER BY database_name, table_name, index_name, stat_name",
		},
		{
			// #640 Replay Tier-3: per-partition inventory from information_schema.
			// PARTITIONS — partitioning method, the partitioning expression, the
			// range/list boundaries (PARTITION_DESCRIPTION), the ordinal, and the
			// per-partition TABLE_ROWS estimate. This lets the Analyzer model
			// partition pruning and per-partition row estimates (Analyzer#4142).
			// Bounded to actually-partitioned tables via PARTITION_NAME IS NOT NULL
			// (a non-partitioned table surfaces a single NULL-partition row we skip),
			// so output is one row per real partition, never one per table.
			//
			// SAFETY: the boundaries (PARTITION_DESCRIPTION) and the partitioning
			// expression are DBA-authored DDL — they appear verbatim in SHOW CREATE
			// TABLE and are part of the schema definition, NOT sampled column data
			// (contrast the i3 min_value/max_value exclusion, which ARE observed data
			// values). Read-only; schema-bounded; engine-precomputed, never computed.
			// Not HighSensitivity.
			ID:       "mysql_partitions_v1",
			Category: "schema",
			SQL:      "SELECT TABLE_SCHEMA, TABLE_NAME, PARTITION_NAME, SUBPARTITION_NAME, PARTITION_ORDINAL_POSITION, PARTITION_METHOD, SUBPARTITION_METHOD, PARTITION_EXPRESSION, SUBPARTITION_EXPRESSION, PARTITION_DESCRIPTION, TABLE_ROWS FROM information_schema.PARTITIONS WHERE TABLE_SCHEMA NOT IN ('mysql', 'performance_schema', 'information_schema', 'sys') AND PARTITION_NAME IS NOT NULL ORDER BY TABLE_SCHEMA, TABLE_NAME, PARTITION_ORDINAL_POSITION",
		},
		{
			// #640 Replay Tier-3: per-column histogram SHAPE metadata from
			// information_schema.COLUMN_STATISTICS (MySQL 8.0+). The HISTOGRAM column
			// is a JSON document; we extract only its scalar shape fields
			// (histogram-type, number-of-buckets-specified, sampling-rate,
			// null-values, data-type, last-updated) plus the bucket COUNT
			// (JSON_LENGTH of the buckets array). This tells the Analyzer that a
			// column carries an N-bucket histogram at a given sampling rate so it can
			// weight selectivity-estimate confidence (Analyzer#4142).
			//
			// SAFETY: the buckets array ($.buckets[*]) holds ACTUAL sampled column
			// boundary VALUES (potential PII) — the same class of data as the i3
			// min_value/max_value we excluded. This SELECT deliberately extracts ONLY
			// the scalar shape fields and the bucket COUNT via JSON_LENGTH, and NEVER
			// an indexed bucket value ($.buckets[N]); the raw bucket contents are
			// deferred behind a separate redaction design. Read-only; schema-bounded;
			// engine-precomputed, never computed. Not HighSensitivity.
			ID:       "mysql_column_histograms_v1",
			Category: "schema",
			SQL:      "SELECT SCHEMA_NAME, TABLE_NAME, COLUMN_NAME, JSON_UNQUOTE(JSON_EXTRACT(HISTOGRAM, '$.\"histogram-type\"')) AS histogram_type, JSON_EXTRACT(HISTOGRAM, '$.\"number-of-buckets-specified\"') AS buckets_specified, JSON_LENGTH(JSON_EXTRACT(HISTOGRAM, '$.buckets')) AS bucket_count, JSON_EXTRACT(HISTOGRAM, '$.\"sampling-rate\"') AS sampling_rate, JSON_EXTRACT(HISTOGRAM, '$.\"null-values\"') AS null_fraction, JSON_UNQUOTE(JSON_EXTRACT(HISTOGRAM, '$.\"data-type\"')) AS data_type, JSON_UNQUOTE(JSON_EXTRACT(HISTOGRAM, '$.\"last-updated\"')) AS last_updated FROM information_schema.COLUMN_STATISTICS WHERE SCHEMA_NAME NOT IN ('mysql', 'performance_schema', 'information_schema', 'sys') ORDER BY SCHEMA_NAME, TABLE_NAME, COLUMN_NAME",
		},
		{
			ID:       "mysql_replication_status_v1",
			Category: "topology",
			SQL:      "SELECT CHANNEL_NAME, SERVICE_STATE, SOURCE_UUID FROM performance_schema.replication_connection_status",
		},
		{
			// #540: MySQL's query-workload signal — the Performance Schema
			// statement-digest summary (the MySQL analogue of pg_stat_statements
			// / SingleStore MV_PLANCACHE / ClickHouse query_log). DIGEST_TEXT is
			// the normalized sample SQL → HighSensitivity + redacted. Bounded to
			// the top statements by accumulated latency so a busy server does not
			// produce an unbounded payload; the NULL-digest overflow row is
			// included naturally. Empty when Performance Schema instrumentation
			// is off. #665: a 0-row result is probed (@@performance_schema) and,
			// when OFF, recorded as a DEGRADED / NOT_ASSESSED skip
			// (perf_schema_disabled) rather than a silent empty success, so a
			// consumer can tell "unavailable" from "no slow queries". (MySQL 8
			// ships perf_schema ON by default, but it can be disabled, and the
			// MariaDB sibling collector defaults OFF — the gate is engine-shared.)
			ID:               "mysql_statement_digests_v1",
			Category:         "workload",
			SQL:              "SELECT SCHEMA_NAME, DIGEST, DIGEST_TEXT, COUNT_STAR, SUM_TIMER_WAIT, SUM_ROWS_EXAMINED, SUM_ROWS_SENT, SUM_ERRORS, SUM_CREATED_TMP_TABLES, SUM_SELECT_SCAN, SUM_NO_INDEX_USED, FIRST_SEEN, LAST_SEEN FROM performance_schema.events_statements_summary_by_digest ORDER BY SUM_TIMER_WAIT DESC LIMIT 200",
			HighSensitivity:  true,
			SensitiveColumns: []string{"DIGEST_TEXT"},
			StructuralColumn: "DIGEST_TEXT", // #647 structural-query opt-in column
			PerfSchemaGated:  true,          // #665 (MDC-R010): empty when perf_schema OFF
		},
		{
			// #543: active InnoDB transactions (point-in-time session evidence,
			// distinct from the aggregate workload/resources counters). trx_query
			// carries the transaction's live SQL text -> HighSensitivity + redacted;
			// the result key is lowercase `trx_query` (information_schema.INNODB_TRX
			// returns lowercase columns). Oldest-first + bounded so a busy server
			// surfaces the long-running transactions without an unbounded payload.
			// trx_state distinguishes RUNNING from LOCK WAIT. Empty on an idle
			// server is a legitimate success. Needs the PROCESS grant (held).
			ID:               "mysql_innodb_transactions_v1",
			Category:         "activity",
			SQL:              "SELECT trx_id, trx_state, trx_started, trx_requested_lock_id, trx_wait_started, trx_weight, trx_mysql_thread_id, trx_query, trx_operation_state, trx_tables_in_use, trx_tables_locked, trx_rows_locked, trx_rows_modified, trx_isolation_level FROM information_schema.INNODB_TRX ORDER BY trx_started LIMIT 200",
			HighSensitivity:  true,
			SensitiveColumns: []string{"trx_query"},
		},
		{
			// #543: the active lock inventory. LOCK_DATA is the locked index-key
			// value (row data, not SQL) -> HighSensitivity + redacted; the native
			// lock descriptors (engine/object/index/mode/status) are preserved so
			// contention is still analyzable. WAITING-first + bounded. Empty when no
			// locks are held is a legitimate success.
			ID:               "mysql_data_locks_v1",
			Category:         "activity",
			SQL:              "SELECT ENGINE, ENGINE_TRANSACTION_ID, THREAD_ID, OBJECT_SCHEMA, OBJECT_NAME, PARTITION_NAME, INDEX_NAME, LOCK_TYPE, LOCK_MODE, LOCK_STATUS, LOCK_DATA FROM performance_schema.data_locks ORDER BY LOCK_STATUS DESC LIMIT 500",
			HighSensitivity:  true,
			SensitiveColumns: []string{"LOCK_DATA"},
		},
		{
			// #543: the blocked -> blocking wait-for edges. Transaction + thread ids
			// only (no SQL, no row data). Empty when nothing is blocked is a
			// legitimate success.
			ID:       "mysql_data_lock_waits_v1",
			Category: "activity",
			SQL:      "SELECT ENGINE, REQUESTING_ENGINE_TRANSACTION_ID, REQUESTING_THREAD_ID, BLOCKING_ENGINE_TRANSACTION_ID, BLOCKING_THREAD_ID FROM performance_schema.data_lock_waits LIMIT 500",
		},
		{
			// #543: the metadata-lock (MDL) inventory, PENDING waits first. Object
			// schema/name only, no SQL or row data. The reading session's own MDL is
			// typically visible; a zero-row result is still a legitimate success.
			ID:       "mysql_metadata_locks_v1",
			Category: "activity",
			SQL:      "SELECT OBJECT_TYPE, OBJECT_SCHEMA, OBJECT_NAME, COLUMN_NAME, LOCK_TYPE, LOCK_DURATION, LOCK_STATUS, SOURCE, OWNER_THREAD_ID FROM performance_schema.metadata_locks ORDER BY LOCK_STATUS DESC LIMIT 500",
		},
		{
			// #543: replica applier state per channel. Zero rows on a standalone
			// (non-replica) server is a legitimate, DISTINCT state (not a failure);
			// native state fields are preserved. Complements the existing
			// mysql_replication_status_v1 (connection/IO side).
			ID:       "mysql_replica_applier_status_v1",
			Category: "topology",
			SQL:      "SELECT CHANNEL_NAME, SERVICE_STATE, REMAINING_DELAY, COUNT_TRANSACTIONS_RETRIES FROM performance_schema.replication_applier_status",
		},
		{
			// #543: Group Replication membership + role/state. Zero rows when Group
			// Replication is not enabled is a legitimate, DISTINCT state (not a
			// failure); native topology fields (host/port/role/state) are preserved.
			ID:       "mysql_group_replication_members_v1",
			Category: "topology",
			SQL:      "SELECT CHANNEL_NAME, MEMBER_ID, MEMBER_HOST, MEMBER_PORT, MEMBER_STATE, MEMBER_ROLE, MEMBER_VERSION FROM performance_schema.replication_group_members",
		},
		{
			// #676 (MYC-R019): the replica-lag-seconds metric for Analyzer's
			// replica-lag rule (Elevarq/Analyzer#4007, replication band #4004).
			// Source = performance_schema.replication_applier_status_by_worker,
			// populated for ANY replica (single- or multi-threaded) and EMPTY on a
			// source / standalone. replica_lag_seconds is derived as the seconds
			// between when the currently-applying (else last-applied) transaction
			// was committed on the ORIGINAL source and now — the perf_schema
			// analogue of SHOW REPLICA STATUS's Seconds_Behind_Source (8.0.22+) /
			// Seconds_Behind_Master (pre-8.0.22). The perf_schema derivation is
			// chosen over SHOW REPLICA STATUS / SHOW SLAVE STATUS deliberately:
			// those are NOT SELECT statements (they would break INV-MYC-01 /
			// MYC-R001) and require the REPLICATION CLIENT privilege (they would
			// break the baseline SELECT, PROCESS role, MYG-R001), whereas the
			// worker table is readable under the baseline role and this stays a
			// read-only SELECT. On a non-replica the empty worker table yields a
			// zero-row success with NO replica_lag_seconds value — absent, never a
			// fabricated zero and never an error / failed cycle (MYC-R007).
			ID:       "mysql_replica_lag_v1",
			Category: "topology",
			SQL:      "SELECT CHANNEL_NAME, SERVICE_STATE, TIMESTAMPDIFF(SECOND, COALESCE(APPLYING_TRANSACTION_ORIGINAL_COMMIT_TIMESTAMP, LAST_APPLIED_TRANSACTION_ORIGINAL_COMMIT_TIMESTAMP), NOW(6)) AS replica_lag_seconds FROM performance_schema.replication_applier_status_by_worker",
		},
		{
			// #693 (MYC-R016): the user-account inventory for the Analyzer
			// security/grants rule tranche (umbrella Elevarq/Analyzer#4004):
			// remote-root (root@'%') / anonymous / passwordless accounts and
			// weak/legacy auth plugins. THE MOST SENSITIVE COLLECTOR — it reads
			// mysql.user, so the SELECT list is proven secret-free by construction
			// (INV-MYC-04): it carries ONLY User, Host, plugin, and a COMPUTED
			// boolean has_empty_password. The authentication_string hash column (MySQL
			// 8 has no Password column) is NEVER named in the select list, the
			// normalized evidence, logs, or any error — the empty-password signal is
			// the *fact* of an empty/absent password, carried solely as the derived
			// boolean (authentication_string = '' OR authentication_string IS NULL).
			// plugin is always non-empty for a usable MySQL 8 account (the server
			// ignores empty-plugin rows), so it reliably reports the real plugin; a
			// consumer pairs it with has_empty_password to tell a true blank password
			// from an external-auth account (e.g. auth_socket). Reading the mysql
			// schema is covered by the baseline global SELECT grant (mysql-grants.md
			// MYG-R001); a permission denial is a legitimate skip recorded in Err
			// (MYC-R002/R017), never a cycle failure. Metadata only (no row data);
			// read-only; not HighSensitivity.
			ID:       "mysql_user_accounts_v1",
			Category: "security",
			SQL:      "SELECT User, Host, plugin, (authentication_string = '' OR authentication_string IS NULL) AS has_empty_password FROM mysql.user",
		},
		{
			// #693 (MYC-R017): the global-privilege inventory so the Analyzer
			// security rules can see *.* privilege sprawl and WITH GRANT OPTION
			// (IS_GRANTABLE='YES'). information_schema.USER_PRIVILEGES has no
			// password/hash column at all (INV-MYC-04) — secret-free by construction.
			// Under the baseline global SELECT grant the account sees every grantee's
			// global privileges; a narrower grant shows only its own rows (a
			// legitimate reduced result, not a failure). Metadata only; read-only;
			// not HighSensitivity.
			ID:       "mysql_user_privileges_v1",
			Category: "security",
			SQL:      "SELECT GRANTEE, PRIVILEGE_TYPE, IS_GRANTABLE FROM information_schema.USER_PRIVILEGES",
		},
	}
}
