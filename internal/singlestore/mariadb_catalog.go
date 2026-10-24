package singlestore

// MariaDB collector catalog (specifications/mariadb-collectors.md). Collected over
// the SAME shared MySQL-wire runtime (Collect / NewRunner / redaction) as MySQL
// and SingleStore — only the catalog differs. Every statement is a read-only
// SELECT, confirmed live against a MariaDB 11.4 server (not authored blind).
//
// MariaDB's information_schema differs from MySQL 8: GLOBAL_VARIABLES and
// GLOBAL_STATUS live in information_schema (MySQL 8 moved them to
// performance_schema, which MariaDB leaves OFF by default). INNODB_METRICS and
// the perf_schema replication views are not available under the baseline role /
// default config, so the MariaDB catalog omits them — every collector here
// succeeds under GRANT SELECT, PROCESS.

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
	}
}
