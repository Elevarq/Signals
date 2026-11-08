package singlestore

import (
	"context"
	"sort"
	"strings"
	"testing"
)

// Tests derive from specifications/mysql-collectors.acceptance.md.

// TC-MYC-01 (MYC-R001, INV-MYC-01): every MySQL catalog statement is read-only.
func TestMySQLCatalogReadOnly(t *testing.T) {
	for _, c := range mysqlCatalog() {
		if HasWrite(c.SQL) {
			t.Errorf("collector %q is not a read-only SELECT: %q", c.ID, c.SQL)
		}
		if c.ID == "" || c.Category == "" || strings.TrimSpace(c.SQL) == "" {
			t.Errorf("collector has empty id/category/sql: %+v", c)
		}
	}
}

// TC-MYC-02 (MYC-R005): expected MySQL collector set.
func TestMySQLCatalogExpectedSet(t *testing.T) {
	want := []string{
		"mysql_columns_v1",
		"mysql_data_lock_waits_v1",
		"mysql_data_locks_v1",
		"mysql_engines_v1",
		"mysql_global_status_v1",
		"mysql_global_variables_v1",
		"mysql_group_replication_members_v1",
		"mysql_identity_v1",
		"mysql_index_inventory_v1",
		"mysql_innodb_deadlocks_v1",
		"mysql_innodb_index_stats_v1",
		"mysql_innodb_metrics_v1",
		"mysql_innodb_transactions_v1",
		"mysql_metadata_locks_v1",
		"mysql_processlist_v1",
		"mysql_replica_applier_status_v1",
		"mysql_replica_lag_v1",
		"mysql_replication_status_v1",
		"mysql_schemata_v1",
		"mysql_statement_digests_v1",
		"mysql_table_statistics_v1",
		"mysql_user_accounts_v1",
		"mysql_user_privileges_v1",
		"server_identity_v1",
	}
	var got []string
	for _, c := range mysqlCatalog() {
		got = append(got, c.ID)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("mysql catalog set = %v, want %v", got, want)
	}
}

// TC-MYC-25 (MYC-R020, #699): the neutral server_identity_v1 collector carries
// the connected database_name (DATABASE()), so the Analyzer keys each per-dbname
// target on one MySQL server as a DISTINCT database (tpcc OLTP vs tpch OLAP) —
// without it both collapse into a single card. It is a read-only SELECT, needs
// no elevated privilege, and carries no sensitive payload. DATABASE() returns
// the connected schema (the target DSN sets it) and is carried through as a
// plain value; a NULL (target with no dbname) is absent, never an error.
func TestMySQLServerIdentityDatabaseName(t *testing.T) {
	c := collectorByID(mysqlCatalog(), "server_identity_v1")
	if c == nil {
		t.Fatal("server_identity_v1 missing from the MySQL catalog")
	}
	if HasWrite(c.SQL) {
		t.Errorf("server_identity_v1 must be a read-only SELECT: %q", c.SQL)
	}
	if c.HighSensitivity || len(c.SensitiveColumns) != 0 {
		t.Errorf("server_identity_v1 carries no sensitive payload; got HighSensitivity=%v cols=%v", c.HighSensitivity, c.SensitiveColumns)
	}
	up := strings.ToUpper(c.SQL)
	if !strings.Contains(up, "DATABASE()") || !strings.Contains(up, "DATABASE_NAME") {
		t.Errorf("server_identity_v1 must select DATABASE() AS database_name; SQL=%q", c.SQL)
	}
	// The connected database name is carried through as the row's database_name.
	runner := func(_ context.Context, sql string) ([]map[string]any, error) {
		if strings.Contains(strings.ToUpper(sql), "DATABASE()") {
			return []map[string]any{{"database_name": "tpch", "version": "8.4.0"}}, nil
		}
		return []map[string]any{}, nil
	}
	rs := Collect(context.Background(), runner, []Collector{*c}, true, false)
	if len(rs) != 1 || len(rs[0].Rows) != 1 {
		t.Fatalf("unexpected result: %+v", rs)
	}
	if v := rs[0].Rows[0]["database_name"]; v != "tpch" {
		t.Errorf("server_identity_v1 database_name = %v, want tpch", v)
	}
}

// TC-MYC-03 (MYC-R003/R004, INV-MYC-02): processlist redacts INFO.
func TestMySQLProcesslistRedaction(t *testing.T) {
	var pl *Collector
	for i := range mysqlCatalog() {
		if mysqlCatalog()[i].ID == "mysql_processlist_v1" {
			pl = &mysqlCatalog()[i]
		}
	}
	if pl == nil {
		t.Fatal("mysql_processlist_v1 missing")
	}
	if !pl.HighSensitivity {
		t.Error("mysql_processlist_v1 must be HighSensitivity")
	}
	found := false
	for _, col := range pl.SensitiveColumns {
		if col == "INFO" {
			found = true
		}
	}
	if !found {
		t.Error("mysql_processlist_v1 must list INFO in SensitiveColumns")
	}

	// Collect with redaction nils the INFO value.
	runner := func(_ context.Context, sql string) ([]map[string]any, error) {
		if strings.Contains(sql, "PROCESSLIST") {
			return []map[string]any{{"ID": "1", "INFO": "SELECT secret FROM t"}}, nil
		}
		return []map[string]any{}, nil
	}
	rs := Collect(context.Background(), runner, []Collector{*pl}, true, false)
	if len(rs) != 1 || len(rs[0].Rows) != 1 {
		t.Fatalf("unexpected result: %+v", rs)
	}
	if v := rs[0].Rows[0]["INFO"]; v != nil {
		t.Errorf("INFO must be redacted to nil, got %v", v)
	}
}

// TC-MYC-04 (MYC-R005): CatalogFor routes by engine, no cross-engine leak.
func TestCatalogForRegistry(t *testing.T) {
	ids := func(cs []Collector) map[string]bool {
		m := map[string]bool{}
		for _, c := range cs {
			m[c.ID] = true
		}
		return m
	}
	mysqlIDs := ids(CatalogFor("mysql"))
	if len(mysqlIDs) != len(mysqlCatalog()) || !mysqlIDs["mysql_identity_v1"] {
		t.Errorf("CatalogFor(mysql) wrong: %v", mysqlIDs)
	}
	ssIDs := ids(CatalogFor("singlestore"))
	if !ssIDs["singlestore_identity_v1"] {
		t.Errorf("CatalogFor(singlestore) missing singlestore catalog")
	}
	// No cross-engine leak.
	if mysqlIDs["singlestore_identity_v1"] || ssIDs["mysql_identity_v1"] {
		t.Error("CatalogFor leaked a collector across engines")
	}
	for _, e := range []string{"postgresql", "nonsense"} {
		if got := CatalogFor(e); got != nil {
			t.Errorf("CatalogFor(%q) = %v, want nil (no fallback)", e, got)
		}
	}
}

// #540: the statement-digest collector carries normalized sample SQL in
// DIGEST_TEXT and must be HighSensitivity with it redacted.
func TestMySQLStatementDigestsRedaction(t *testing.T) {
	var sd *Collector
	for i := range mysqlCatalog() {
		if mysqlCatalog()[i].ID == "mysql_statement_digests_v1" {
			sd = &mysqlCatalog()[i]
		}
	}
	if sd == nil {
		t.Fatal("mysql_statement_digests_v1 missing")
	}
	if !sd.HighSensitivity {
		t.Error("mysql_statement_digests_v1 must be HighSensitivity (carries DIGEST_TEXT)")
	}
	found := false
	for _, col := range sd.SensitiveColumns {
		if col == "DIGEST_TEXT" {
			found = true
		}
	}
	if !found {
		t.Error("mysql_statement_digests_v1 must list DIGEST_TEXT in SensitiveColumns")
	}
	runner := func(_ context.Context, sql string) ([]map[string]any, error) {
		if strings.Contains(sql, "events_statements_summary_by_digest") {
			return []map[string]any{{"DIGEST": "abc", "DIGEST_TEXT": "SELECT ? FROM secret_t"}}, nil
		}
		return []map[string]any{}, nil
	}
	rs := Collect(context.Background(), runner, []Collector{*sd}, true, false)
	if len(rs) != 1 || len(rs[0].Rows) != 1 {
		t.Fatalf("unexpected result: %+v", rs)
	}
	if v := rs[0].Rows[0]["DIGEST_TEXT"]; v != nil {
		t.Errorf("DIGEST_TEXT must be redacted to nil, got %v", v)
	}
}

// TC-MYC-10 (MYC-R010): global_status carries the InnoDB buffer-pool read
// counters for Analyzer's buffer-pool-read-pressure rate rule (#656 / #3918).
func TestMySQLGlobalStatusBufferPoolReads(t *testing.T) {
	gs := collectorByID(mysqlCatalog(), "mysql_global_status_v1")
	if gs == nil {
		t.Fatal("mysql_global_status_v1 missing")
	}
	for _, v := range []string{"Innodb_buffer_pool_reads", "Innodb_buffer_pool_read_requests"} {
		if !strings.Contains(gs.SQL, "'"+v+"'") {
			t.Errorf("mysql_global_status_v1 IN-list must contain %q; SQL=%q", v, gs.SQL)
		}
	}
}

// TC-MYC-11 (MYC-R011): the deadlock counter is collected unconditionally (no
// STATUS='enabled' filter) with its STATUS surfaced, so the signal is reliable
// regardless of the InnoDB metrics lock-module default (#656 / #3918).
func TestMySQLInnodbDeadlocksCollector(t *testing.T) {
	c := collectorByID(mysqlCatalog(), "mysql_innodb_deadlocks_v1")
	if c == nil {
		t.Fatal("mysql_innodb_deadlocks_v1 missing")
	}
	if HasWrite(c.SQL) {
		t.Errorf("mysql_innodb_deadlocks_v1 must be read-only: %q", c.SQL)
	}
	if c.HighSensitivity || len(c.SensitiveColumns) != 0 {
		t.Errorf("mysql_innodb_deadlocks_v1 carries no sensitive payload; got HighSensitivity=%v cols=%v", c.HighSensitivity, c.SensitiveColumns)
	}
	up := strings.ToUpper(c.SQL)
	if !strings.Contains(up, "INNODB_METRICS") || !strings.Contains(c.SQL, "'lock_deadlocks'") {
		t.Errorf("mysql_innodb_deadlocks_v1 must select lock_deadlocks from INNODB_METRICS; SQL=%q", c.SQL)
	}
	if strings.Contains(up, "STATUS = 'ENABLED'") || strings.Contains(up, "STATUS='ENABLED'") {
		t.Errorf("mysql_innodb_deadlocks_v1 must NOT filter on STATUS='enabled' (row must always return with its STATUS); SQL=%q", c.SQL)
	}
	if !strings.Contains(up, "STATUS") {
		t.Errorf("mysql_innodb_deadlocks_v1 must surface STATUS so the consumer can detect a disabled metric; SQL=%q", c.SQL)
	}
}

// TC-MYC-12 (MYC-R012): the table-statistics collector carries AUTO_INCREMENT
// (the current counter value) for Analyzer's AUTO_INCREMENT-exhaustion-risk rule
// (#677 / Analyzer #4004). It is NULL for a table with no AUTO_INCREMENT column
// and must be carried through as an absent (nil) value, never an error.
func TestMySQLTableStatisticsAutoIncrement(t *testing.T) {
	c := collectorByID(mysqlCatalog(), "mysql_table_statistics_v1")
	if c == nil {
		t.Fatal("mysql_table_statistics_v1 missing")
	}
	if HasWrite(c.SQL) {
		t.Errorf("mysql_table_statistics_v1 must be read-only: %q", c.SQL)
	}
	if c.HighSensitivity || len(c.SensitiveColumns) != 0 {
		t.Errorf("mysql_table_statistics_v1 carries no sensitive payload; got HighSensitivity=%v cols=%v", c.HighSensitivity, c.SensitiveColumns)
	}
	up := strings.ToUpper(c.SQL)
	if !strings.Contains(up, "INFORMATION_SCHEMA.TABLES") {
		t.Errorf("mysql_table_statistics_v1 must read information_schema.TABLES; SQL=%q", c.SQL)
	}
	if !strings.Contains(up, "AUTO_INCREMENT") {
		t.Errorf("mysql_table_statistics_v1 must select AUTO_INCREMENT; SQL=%q", c.SQL)
	}

	// A NULL AUTO_INCREMENT (table with no AI column) is carried through as nil,
	// exactly like the other nullable sizing columns — never an error.
	runner := func(_ context.Context, sql string) ([]map[string]any, error) {
		if strings.Contains(sql, "information_schema.TABLES") {
			return []map[string]any{
				{"TABLE_SCHEMA": "app", "TABLE_NAME": "orders", "AUTO_INCREMENT": "4096"},
				{"TABLE_SCHEMA": "app", "TABLE_NAME": "audit_log", "AUTO_INCREMENT": nil},
			}, nil
		}
		return []map[string]any{}, nil
	}
	rs := Collect(context.Background(), runner, []Collector{*c}, true, false)
	if len(rs) != 1 || len(rs[0].Rows) != 2 {
		t.Fatalf("unexpected result: %+v", rs)
	}
	if rs[0].Err != "" {
		t.Errorf("collector errored on a NULL AUTO_INCREMENT: %q", rs[0].Err)
	}
	if v := rs[0].Rows[0]["AUTO_INCREMENT"]; v != "4096" {
		t.Errorf("AI table AUTO_INCREMENT = %v, want the counter value 4096", v)
	}
	if v, ok := rs[0].Rows[1]["AUTO_INCREMENT"]; !ok || v != nil {
		t.Errorf("non-AI table AUTO_INCREMENT must be carried as nil (absent), got %v (present=%v)", v, ok)
	}
}

// TC-MYC-13 (MYC-R013): the table-statistics collector carries TABLE_COLLATION
// (the per-table collation) for Analyzer's utf8mb4-readiness / charset-collation
// -mismatch rule (#684 / Analyzer #4001). The charset family is derivable from
// the collation; information_schema.TABLES has no direct charset column.
func TestMySQLTableStatisticsTableCollation(t *testing.T) {
	c := collectorByID(mysqlCatalog(), "mysql_table_statistics_v1")
	if c == nil {
		t.Fatal("mysql_table_statistics_v1 missing")
	}
	if HasWrite(c.SQL) {
		t.Errorf("mysql_table_statistics_v1 must be read-only: %q", c.SQL)
	}
	if c.HighSensitivity || len(c.SensitiveColumns) != 0 {
		t.Errorf("mysql_table_statistics_v1 carries no sensitive payload; got HighSensitivity=%v cols=%v", c.HighSensitivity, c.SensitiveColumns)
	}
	up := strings.ToUpper(c.SQL)
	if !strings.Contains(up, "INFORMATION_SCHEMA.TABLES") {
		t.Errorf("mysql_table_statistics_v1 must read information_schema.TABLES; SQL=%q", c.SQL)
	}
	if !strings.Contains(up, "TABLE_COLLATION") {
		t.Errorf("mysql_table_statistics_v1 must select TABLE_COLLATION; SQL=%q", c.SQL)
	}

	runner := func(_ context.Context, sql string) ([]map[string]any, error) {
		if strings.Contains(sql, "information_schema.TABLES") {
			return []map[string]any{
				{"TABLE_SCHEMA": "app", "TABLE_NAME": "orders", "TABLE_COLLATION": "utf8mb4_0900_ai_ci"},
			}, nil
		}
		return []map[string]any{}, nil
	}
	rs := Collect(context.Background(), runner, []Collector{*c}, true, false)
	if len(rs) != 1 || len(rs[0].Rows) != 1 {
		t.Fatalf("unexpected result: %+v", rs)
	}
	if rs[0].Err != "" {
		t.Errorf("collector errored: %q", rs[0].Err)
	}
	if v := rs[0].Rows[0]["TABLE_COLLATION"]; v != "utf8mb4_0900_ai_ci" {
		t.Errorf("TABLE_COLLATION = %v, want utf8mb4_0900_ai_ci", v)
	}
}

// TC-MYC-14 (MYC-R013): the columns collector carries CHARACTER_SET_NAME and
// COLLATION_NAME for Analyzer's utf8mb4-readiness rule (#684 / Analyzer #4001).
// Both are NULL for non-string columns and must be carried through as absent
// (nil) values, never an error.
func TestMySQLColumnsCharsetCollation(t *testing.T) {
	c := collectorByID(mysqlCatalog(), "mysql_columns_v1")
	if c == nil {
		t.Fatal("mysql_columns_v1 missing")
	}
	if HasWrite(c.SQL) {
		t.Errorf("mysql_columns_v1 must be read-only: %q", c.SQL)
	}
	if c.HighSensitivity || len(c.SensitiveColumns) != 0 {
		t.Errorf("mysql_columns_v1 carries no sensitive payload; got HighSensitivity=%v cols=%v", c.HighSensitivity, c.SensitiveColumns)
	}
	up := strings.ToUpper(c.SQL)
	if !strings.Contains(up, "CHARACTER_SET_NAME") {
		t.Errorf("mysql_columns_v1 must select CHARACTER_SET_NAME; SQL=%q", c.SQL)
	}
	if !strings.Contains(up, "COLLATION_NAME") {
		t.Errorf("mysql_columns_v1 must select COLLATION_NAME; SQL=%q", c.SQL)
	}

	// A string column carries charset/collation; a non-string column (e.g. int)
	// carries NULL for both, carried through as nil — never an error.
	runner := func(_ context.Context, sql string) ([]map[string]any, error) {
		if strings.Contains(sql, "information_schema.COLUMNS") {
			return []map[string]any{
				{"TABLE_SCHEMA": "app", "TABLE_NAME": "orders", "COLUMN_NAME": "note", "CHARACTER_SET_NAME": "utf8mb3", "COLLATION_NAME": "utf8mb3_general_ci"},
				{"TABLE_SCHEMA": "app", "TABLE_NAME": "orders", "COLUMN_NAME": "id", "CHARACTER_SET_NAME": nil, "COLLATION_NAME": nil},
			}, nil
		}
		return []map[string]any{}, nil
	}
	rs := Collect(context.Background(), runner, []Collector{*c}, true, false)
	if len(rs) != 1 || len(rs[0].Rows) != 2 {
		t.Fatalf("unexpected result: %+v", rs)
	}
	if rs[0].Err != "" {
		t.Errorf("collector errored on a NULL charset/collation: %q", rs[0].Err)
	}
	if v := rs[0].Rows[0]["CHARACTER_SET_NAME"]; v != "utf8mb3" {
		t.Errorf("string column CHARACTER_SET_NAME = %v, want utf8mb3", v)
	}
	if v := rs[0].Rows[0]["COLLATION_NAME"]; v != "utf8mb3_general_ci" {
		t.Errorf("string column COLLATION_NAME = %v, want utf8mb3_general_ci", v)
	}
	if v, ok := rs[0].Rows[1]["CHARACTER_SET_NAME"]; !ok || v != nil {
		t.Errorf("non-string column CHARACTER_SET_NAME must be carried as nil (absent), got %v (present=%v)", v, ok)
	}
	if v, ok := rs[0].Rows[1]["COLLATION_NAME"]; !ok || v != nil {
		t.Errorf("non-string column COLLATION_NAME must be carried as nil (absent), got %v (present=%v)", v, ok)
	}
}

// TC-MYC-05 (INV-MYC-03, MYC-R004/R008): no query-text column or locked-key data
// escapes the redaction contract — catalog-wide, case-insensitive on the SQL, and
// SensitiveColumns must carry the exact result-row case so runtime redaction (an
// exact map-key nil) actually fires.
func TestMySQLCatalogNoUnredactedSensitiveColumn(t *testing.T) {
	// sensitive column -> its exact result-row case.
	sensitive := map[string]string{
		"INFO":        "INFO",
		"DIGEST_TEXT": "DIGEST_TEXT",
		"TRX_QUERY":   "trx_query",
		"LOCK_DATA":   "LOCK_DATA",
	}
	for _, c := range mysqlCatalog() {
		up := strings.ToUpper(c.SQL)
		for qt, exactCase := range sensitive {
			// Match the column as a select-list token (preceded by a comma or
			// followed by FROM), not a substring of another identifier like
			// INFORMATION_SCHEMA.
			if strings.Contains(up, ", "+qt) || strings.Contains(up, " "+qt+" FROM") || strings.Contains(up, " "+qt+",") {
				if !c.HighSensitivity {
					t.Errorf("collector %q selects %s but is not HighSensitivity", c.ID, qt)
				}
				has := false
				for _, col := range c.SensitiveColumns {
					if col == exactCase {
						has = true
					}
				}
				if !has {
					t.Errorf("collector %q selects %s but %q (exact result-row case) is not in SensitiveColumns %v", c.ID, qt, exactCase, c.SensitiveColumns)
				}
			}
		}
	}
}

// TC-MYC-06 (#543): the InnoDB-transactions collector carries live SQL text in
// trx_query (lowercase result key) and must be HighSensitivity with it redacted.
func TestMySQLInnodbTransactionsRedaction(t *testing.T) {
	var trx *Collector
	for i := range mysqlCatalog() {
		if mysqlCatalog()[i].ID == "mysql_innodb_transactions_v1" {
			trx = &mysqlCatalog()[i]
		}
	}
	if trx == nil {
		t.Fatal("mysql_innodb_transactions_v1 missing")
	}
	if !trx.HighSensitivity {
		t.Error("mysql_innodb_transactions_v1 must be HighSensitivity (carries trx_query)")
	}
	found := false
	for _, col := range trx.SensitiveColumns {
		if col == "trx_query" {
			found = true
		}
	}
	if !found {
		t.Error("mysql_innodb_transactions_v1 must list trx_query (lowercase) in SensitiveColumns")
	}
	runner := func(_ context.Context, sql string) ([]map[string]any, error) {
		if strings.Contains(sql, "INNODB_TRX") {
			return []map[string]any{{"trx_id": "1831", "trx_query": "UPDATE accounts SET balance=balance+5 WHERE id=1"}}, nil
		}
		return []map[string]any{}, nil
	}
	rs := Collect(context.Background(), runner, []Collector{*trx}, true, false)
	if len(rs) != 1 || len(rs[0].Rows) != 1 {
		t.Fatalf("unexpected result: %+v", rs)
	}
	if v := rs[0].Rows[0]["trx_query"]; v != nil {
		t.Errorf("trx_query must be redacted to nil, got %v", v)
	}
}

// TC-MYC-07 (#543): the data-locks collector carries the locked index-key value in
// LOCK_DATA (row data) and must be HighSensitivity with it redacted, while native
// lock descriptors are preserved.
func TestMySQLDataLocksRedaction(t *testing.T) {
	var dl *Collector
	for i := range mysqlCatalog() {
		if mysqlCatalog()[i].ID == "mysql_data_locks_v1" {
			dl = &mysqlCatalog()[i]
		}
	}
	if dl == nil {
		t.Fatal("mysql_data_locks_v1 missing")
	}
	if !dl.HighSensitivity {
		t.Error("mysql_data_locks_v1 must be HighSensitivity (carries LOCK_DATA)")
	}
	found := false
	for _, col := range dl.SensitiveColumns {
		if col == "LOCK_DATA" {
			found = true
		}
	}
	if !found {
		t.Error("mysql_data_locks_v1 must list LOCK_DATA in SensitiveColumns")
	}
	runner := func(_ context.Context, sql string) ([]map[string]any, error) {
		if strings.Contains(sql, "data_locks") {
			return []map[string]any{{"OBJECT_NAME": "accounts", "LOCK_MODE": "X,REC_NOT_GAP", "LOCK_STATUS": "WAITING", "LOCK_DATA": "1"}}, nil
		}
		return []map[string]any{}, nil
	}
	rs := Collect(context.Background(), runner, []Collector{*dl}, true, false)
	if len(rs) != 1 || len(rs[0].Rows) != 1 {
		t.Fatalf("unexpected result: %+v", rs)
	}
	row := rs[0].Rows[0]
	if v := row["LOCK_DATA"]; v != nil {
		t.Errorf("LOCK_DATA must be redacted to nil, got %v", v)
	}
	// Native descriptors preserved.
	if row["OBJECT_NAME"] != "accounts" || row["LOCK_STATUS"] != "WAITING" {
		t.Errorf("native lock descriptors must be preserved, got %+v", row)
	}
}

// TC-MYC-23 (MYC-R019): the replica-lag collector reads
// performance_schema.replication_applier_status_by_worker and derives a single
// per-channel replica_lag_seconds (the perf_schema analogue of
// Seconds_Behind_Source) for Analyzer's replica-lag rule (#676 / Analyzer
// #4007). It is a read-only SELECT needing only the baseline SELECT, PROCESS
// role — not SHOW REPLICA STATUS / REPLICATION CLIENT — and is not
// HighSensitivity. On a replica the row carries a replica_lag_seconds value.
func TestMySQLReplicaLagCollector(t *testing.T) {
	c := collectorByID(mysqlCatalog(), "mysql_replica_lag_v1")
	if c == nil {
		t.Fatal("mysql_replica_lag_v1 missing")
	}
	if HasWrite(c.SQL) {
		t.Errorf("mysql_replica_lag_v1 must be a read-only SELECT: %q", c.SQL)
	}
	if c.HighSensitivity || len(c.SensitiveColumns) != 0 {
		t.Errorf("mysql_replica_lag_v1 carries no sensitive payload; got HighSensitivity=%v cols=%v", c.HighSensitivity, c.SensitiveColumns)
	}
	if c.ElevatedPrivilege != "" {
		t.Errorf("mysql_replica_lag_v1 must need no privilege beyond baseline SELECT, PROCESS; got ElevatedPrivilege=%q", c.ElevatedPrivilege)
	}
	up := strings.ToUpper(c.SQL)
	// perf_schema derivation, NOT SHOW REPLICA STATUS / SHOW SLAVE STATUS.
	if strings.Contains(up, "SHOW REPLICA STATUS") || strings.Contains(up, "SHOW SLAVE STATUS") {
		t.Errorf("mysql_replica_lag_v1 must NOT use SHOW REPLICA/SLAVE STATUS (non-SELECT, needs REPLICATION CLIENT); SQL=%q", c.SQL)
	}
	if !strings.Contains(up, "REPLICATION_APPLIER_STATUS_BY_WORKER") {
		t.Errorf("mysql_replica_lag_v1 must read performance_schema.replication_applier_status_by_worker; SQL=%q", c.SQL)
	}
	if !strings.Contains(up, "REPLICA_LAG_SECONDS") {
		t.Errorf("mysql_replica_lag_v1 must derive a replica_lag_seconds column; SQL=%q", c.SQL)
	}

	// A replica: the worker table returns a row; the derived replica_lag_seconds
	// value is carried through.
	runner := func(_ context.Context, sql string) ([]map[string]any, error) {
		if strings.Contains(sql, "replication_applier_status_by_worker") {
			return []map[string]any{
				{"CHANNEL_NAME": "", "SERVICE_STATE": "ON", "replica_lag_seconds": "12"},
			}, nil
		}
		return []map[string]any{}, nil
	}
	rs := Collect(context.Background(), runner, []Collector{*c}, true, false)
	if len(rs) != 1 || len(rs[0].Rows) != 1 {
		t.Fatalf("unexpected result: %+v", rs)
	}
	if rs[0].Err != "" {
		t.Errorf("collector errored on a replica: %q", rs[0].Err)
	}
	if v := rs[0].Rows[0]["replica_lag_seconds"]; v != "12" {
		t.Errorf("replica replica_lag_seconds = %v, want the derived lag value 12", v)
	}
}

// TC-MYC-24 (MYC-R019 / MYC-R007): on a source / standalone (non-replica) the
// performance_schema.replication_applier_status_by_worker table is EMPTY, so the
// replica-lag collector returns a successful zero-row result carrying NO
// replica_lag_seconds value (absent, not a fabricated zero) and NO error — it
// must never fail the collection cycle.
func TestMySQLReplicaLagNotAReplicaAbsent(t *testing.T) {
	c := collectorByID(mysqlCatalog(), "mysql_replica_lag_v1")
	if c == nil {
		t.Fatal("mysql_replica_lag_v1 missing")
	}
	// Non-replica: the worker table is empty -> zero rows, no error.
	runner := func(_ context.Context, _ string) ([]map[string]any, error) {
		return []map[string]any{}, nil
	}
	rs := Collect(context.Background(), runner, []Collector{*c}, true, false)
	if len(rs) != 1 {
		t.Fatalf("unexpected result count: %+v", rs)
	}
	if rs[0].Err != "" {
		t.Errorf("not-a-replica must be a successful empty result, got Err=%q", rs[0].Err)
	}
	if rs[0].Status != "" {
		t.Errorf("not-a-replica must be a plain empty success (no skipped/degraded status), got Status=%q Reason=%q", rs[0].Status, rs[0].Reason)
	}
	if len(rs[0].Rows) != 0 {
		t.Errorf("not-a-replica must yield zero rows (no replica_lag_seconds value present), got %d rows: %+v", len(rs[0].Rows), rs[0].Rows)
	}
}

// #640 Replay Tier-2 — mysql_index_inventory_v1 collects the per-index
// CARDINALITY (the engine's precomputed distinct-value estimate) from the same
// information_schema.STATISTICS read, carrying a NULL (un-ANALYZEd) value
// through as absent, never erroring. Read-only, no sensitive payload.
func TestMySQLIndexInventoryCardinality(t *testing.T) {
	c := collectorByID(mysqlCatalog(), "mysql_index_inventory_v1")
	if c == nil {
		t.Fatal("mysql_index_inventory_v1 missing")
	}
	if HasWrite(c.SQL) {
		t.Errorf("mysql_index_inventory_v1 must be read-only: %q", c.SQL)
	}
	if c.HighSensitivity || len(c.SensitiveColumns) != 0 {
		t.Errorf("mysql_index_inventory_v1 carries no sensitive payload; got HighSensitivity=%v cols=%v", c.HighSensitivity, c.SensitiveColumns)
	}
	up := strings.ToUpper(c.SQL)
	if !strings.Contains(up, "INFORMATION_SCHEMA.STATISTICS") {
		t.Errorf("must read information_schema.STATISTICS; SQL=%q", c.SQL)
	}
	if !strings.Contains(up, "CARDINALITY") {
		t.Errorf("#640 Tier-2: mysql_index_inventory_v1 must select CARDINALITY; SQL=%q", c.SQL)
	}

	// A NULL CARDINALITY (index not yet ANALYZEd) is carried through as nil.
	runner := func(_ context.Context, sql string) ([]map[string]any, error) {
		if strings.Contains(strings.ToUpper(sql), "INFORMATION_SCHEMA.STATISTICS") {
			return []map[string]any{
				{"TABLE_SCHEMA": "app", "TABLE_NAME": "orders", "INDEX_NAME": "idx_cust", "SEQ_IN_INDEX": "1", "COLUMN_NAME": "cust_id", "NON_UNIQUE": "1", "CARDINALITY": "1234"},
				{"TABLE_SCHEMA": "app", "TABLE_NAME": "fresh", "INDEX_NAME": "idx_new", "SEQ_IN_INDEX": "1", "COLUMN_NAME": "k", "NON_UNIQUE": "1", "CARDINALITY": nil},
			}, nil
		}
		return []map[string]any{}, nil
	}
	rs := Collect(context.Background(), runner, []Collector{*c}, true, false)
	if len(rs) != 1 || rs[0].Err != "" {
		t.Fatalf("unexpected result: %+v", rs)
	}
	if v := rs[0].Rows[0]["CARDINALITY"]; v != "1234" {
		t.Errorf("populated CARDINALITY = %v, want 1234", v)
	}
	if v, ok := rs[0].Rows[1]["CARDINALITY"]; !ok || v != nil {
		t.Errorf("NULL CARDINALITY must carry through as nil; got ok=%v v=%v", ok, v)
	}
}

// #640 Replay Tier-2 — mysql_innodb_index_stats_v1 reads the InnoDB persistent
// per-index-prefix cardinality (mysql.innodb_index_stats, n_diff_pfxNN rows):
// read-only system-schema metadata, filtered to the cardinality rows, no
// sensitive payload.
func TestMySQLInnodbIndexStats(t *testing.T) {
	c := collectorByID(mysqlCatalog(), "mysql_innodb_index_stats_v1")
	if c == nil {
		t.Fatal("mysql_innodb_index_stats_v1 missing")
	}
	if HasWrite(c.SQL) {
		t.Errorf("must be read-only: %q", c.SQL)
	}
	if c.HighSensitivity || len(c.SensitiveColumns) != 0 {
		t.Errorf("innodb_index_stats carries no sensitive payload")
	}
	up := strings.ToUpper(c.SQL)
	if !strings.Contains(up, "MYSQL.INNODB_INDEX_STATS") {
		t.Errorf("must read mysql.innodb_index_stats; SQL=%q", c.SQL)
	}
	if !strings.Contains(c.SQL, "n_diff_pfx") {
		t.Errorf("must filter to n_diff_pfx%% cardinality rows; SQL=%q", c.SQL)
	}
	runner := func(_ context.Context, sql string) ([]map[string]any, error) {
		if strings.Contains(strings.ToUpper(sql), "MYSQL.INNODB_INDEX_STATS") {
			return []map[string]any{
				{"database_name": "app", "table_name": "orders", "index_name": "idx_cust", "stat_name": "n_diff_pfx01", "stat_value": "900", "sample_size": "20"},
			}, nil
		}
		return []map[string]any{}, nil
	}
	rs := Collect(context.Background(), runner, []Collector{*c}, true, false)
	if len(rs) != 1 || rs[0].Err != "" || len(rs[0].Rows) != 1 {
		t.Fatalf("unexpected result: %+v", rs)
	}
	if v := rs[0].Rows[0]["stat_value"]; v != "900" {
		t.Errorf("stat_value = %v, want 900", v)
	}
}
