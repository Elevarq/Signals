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
			ID:       "mariadb_global_variables_v1",
			Category: "config",
			SQL:      "SELECT VARIABLE_NAME, VARIABLE_VALUE FROM information_schema.GLOBAL_VARIABLES WHERE VARIABLE_NAME IN ('max_connections', 'innodb_buffer_pool_size', 'max_allowed_packet', 'wait_timeout', 'version', 'log_bin', 'read_only', 'innodb_flush_log_at_trx_commit')",
		},
		{
			ID:       "mariadb_global_status_v1",
			Category: "resources",
			SQL:      "SELECT VARIABLE_NAME, VARIABLE_VALUE FROM information_schema.GLOBAL_STATUS WHERE VARIABLE_NAME IN ('Threads_connected', 'Threads_running', 'Uptime', 'Questions', 'Slow_queries', 'Aborted_connects', 'Max_used_connections', 'Open_tables')",
		},
		{
			ID:               "mariadb_processlist_v1",
			Category:         "activity",
			SQL:              "SELECT ID, USER, HOST, DB, COMMAND, TIME, STATE, INFO FROM information_schema.PROCESSLIST",
			HighSensitivity:  true,
			SensitiveColumns: []string{"INFO"},
		},
		{
			ID:       "mariadb_table_statistics_v1",
			Category: "schema",
			SQL:      "SELECT TABLE_SCHEMA, TABLE_NAME, ENGINE, TABLE_ROWS, DATA_LENGTH, INDEX_LENGTH FROM information_schema.TABLES WHERE TABLE_SCHEMA NOT IN ('mysql', 'performance_schema', 'information_schema', 'sys')",
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
			ID:       "mariadb_columns_v1",
			Category: "schema",
			SQL:      "SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, ORDINAL_POSITION, COLUMN_TYPE, DATA_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_DEFAULT, EXTRA FROM information_schema.COLUMNS WHERE TABLE_SCHEMA NOT IN ('mysql', 'performance_schema', 'information_schema', 'sys') ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION",
		},
		{
			// #538 (Replay Tier-1 evidence): per-index inventory. INDEX_NAME='PRIMARY'
			// carries primary-key identity losslessly (no separate PK collector);
			// NON_UNIQUE=0 marks unique/PK vs ordinary; SEQ_IN_INDEX preserves
			// composite-index ordering exactly. Per-index CARDINALITY is Tier-2 and
			// NOT collected. Identical acquisition SQL to MySQL, verified equivalent
			// live across all four MariaDB LTS lines; distinct MariaDB identity.
			// Schema metadata only -> not HighSensitivity.
			ID:       "mariadb_index_inventory_v1",
			Category: "schema",
			SQL:      "SELECT TABLE_SCHEMA, TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX, COLUMN_NAME, NON_UNIQUE, INDEX_TYPE, NULLABLE, SUB_PART, COLLATION FROM information_schema.STATISTICS WHERE TABLE_SCHEMA NOT IN ('mysql', 'performance_schema', 'information_schema', 'sys') ORDER BY TABLE_SCHEMA, TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX",
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
			// perf_schema is OFF by default -> legitimate empty until enabled. Top
			// statements by accumulated latency, bounded. A digest id is never
			// equated with a PostgreSQL queryid.
			ID:               "mariadb_statement_digests_v1",
			Category:         "workload",
			SQL:              "SELECT SCHEMA_NAME, DIGEST, DIGEST_TEXT, COUNT_STAR, SUM_TIMER_WAIT, SUM_LOCK_TIME, SUM_ROWS_EXAMINED, SUM_ROWS_SENT, SUM_ERRORS, SUM_SELECT_SCAN, SUM_NO_INDEX_USED, FIRST_SEEN, LAST_SEEN FROM performance_schema.events_statements_summary_by_digest ORDER BY SUM_TIMER_WAIT DESC LIMIT 200",
			HighSensitivity:  true,
			SensitiveColumns: []string{"DIGEST_TEXT"},
			StructuralColumn: "DIGEST_TEXT", // #647 structural-query opt-in column
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
	}
}
