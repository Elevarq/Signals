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
		},
	}
}
