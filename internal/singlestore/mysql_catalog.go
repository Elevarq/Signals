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
			ID:       "mysql_global_variables_v1",
			Category: "config",
			SQL:      "SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_variables WHERE VARIABLE_NAME IN ('max_connections', 'innodb_buffer_pool_size', 'max_allowed_packet', 'wait_timeout', 'version', 'log_bin', 'read_only', 'innodb_flush_log_at_trx_commit')",
		},
		{
			ID:       "mysql_global_status_v1",
			Category: "resources",
			SQL:      "SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME IN ('Threads_connected', 'Threads_running', 'Uptime', 'Questions', 'Slow_queries', 'Aborted_connects', 'Max_used_connections', 'Open_tables')",
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
			ID:       "mysql_table_statistics_v1",
			Category: "schema",
			SQL:      "SELECT TABLE_SCHEMA, TABLE_NAME, ENGINE, TABLE_ROWS, DATA_LENGTH, INDEX_LENGTH FROM information_schema.TABLES WHERE TABLE_SCHEMA NOT IN ('mysql', 'performance_schema', 'information_schema', 'sys')",
		},
		{
			ID:       "mysql_engines_v1",
			Category: "server",
			SQL:      "SELECT ENGINE, SUPPORT, TRANSACTIONS, XA, SAVEPOINTS FROM information_schema.ENGINES",
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
			ID:       "mysql_columns_v1",
			Category: "schema",
			SQL:      "SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, ORDINAL_POSITION, COLUMN_TYPE, DATA_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_DEFAULT, EXTRA FROM information_schema.COLUMNS WHERE TABLE_SCHEMA NOT IN ('mysql', 'performance_schema', 'information_schema', 'sys') ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION",
		},
		{
			// #538 (Replay Tier-1 evidence): per-index inventory so Replay sees the
			// real access paths. INDEX_NAME='PRIMARY' carries primary-key identity
			// losslessly (so no separate PK collector is needed); NON_UNIQUE=0
			// distinguishes unique/PK from ordinary indexes; SEQ_IN_INDEX preserves
			// composite-index column ordering exactly (e.g. idx_tenant_created ->
			// tenant_id seq 1, created_at seq 2). Per-index CARDINALITY is Tier-2
			// and deliberately NOT collected here. Schema metadata only -> not
			// HighSensitivity. System schemas excluded.
			ID:       "mysql_index_inventory_v1",
			Category: "schema",
			SQL:      "SELECT TABLE_SCHEMA, TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX, COLUMN_NAME, NON_UNIQUE, INDEX_TYPE, NULLABLE, SUB_PART, COLLATION FROM information_schema.STATISTICS WHERE TABLE_SCHEMA NOT IN ('mysql', 'performance_schema', 'information_schema', 'sys') ORDER BY TABLE_SCHEMA, TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX",
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
			// is off (a legitimate empty result, not a failure).
			ID:               "mysql_statement_digests_v1",
			Category:         "workload",
			SQL:              "SELECT SCHEMA_NAME, DIGEST, DIGEST_TEXT, COUNT_STAR, SUM_TIMER_WAIT, SUM_ROWS_EXAMINED, SUM_ROWS_SENT, SUM_ERRORS, SUM_CREATED_TMP_TABLES, SUM_SELECT_SCAN, SUM_NO_INDEX_USED, FIRST_SEEN, LAST_SEEN FROM performance_schema.events_statements_summary_by_digest ORDER BY SUM_TIMER_WAIT DESC LIMIT 200",
			HighSensitivity:  true,
			SensitiveColumns: []string{"DIGEST_TEXT"},
			StructuralColumn: "DIGEST_TEXT", // #647 structural-query opt-in column
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
	}
}
