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
		"mysql_innodb_metrics_v1",
		"mysql_innodb_transactions_v1",
		"mysql_metadata_locks_v1",
		"mysql_processlist_v1",
		"mysql_replica_applier_status_v1",
		"mysql_replication_status_v1",
		"mysql_schemata_v1",
		"mysql_statement_digests_v1",
		"mysql_table_statistics_v1",
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
