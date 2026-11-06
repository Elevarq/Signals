package singlestore

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
)

// Tests derive from specifications/mariadb-collectors.acceptance.md.

// TC-MDC-01 (MDC-R001, INV-MDC-01): every MariaDB catalog statement is read-only.
func TestMariaDBCatalogReadOnly(t *testing.T) {
	for _, c := range mariadbCatalog() {
		if HasWrite(c.SQL) {
			t.Errorf("collector %q is not a read-only SELECT: %q", c.ID, c.SQL)
		}
		if c.ID == "" || c.Category == "" || strings.TrimSpace(c.SQL) == "" {
			t.Errorf("collector has empty id/category/sql: %+v", c)
		}
	}
}

// TC-MDC-02 (MDC-R005): expected MariaDB collector set (all green under SELECT,PROCESS).
func TestMariaDBCatalogExpectedSet(t *testing.T) {
	want := []string{
		"mariadb_columns_v1",
		"mariadb_engines_v1",
		"mariadb_galera_status_v1",
		"mariadb_global_status_v1",
		"mariadb_global_variables_v1",
		"mariadb_identity_v1",
		"mariadb_index_inventory_v1",
		"mariadb_innodb_deadlocks_v1",
		"mariadb_innodb_lock_waits_v1",
		"mariadb_innodb_locks_v1",
		"mariadb_innodb_metrics_v1",
		"mariadb_innodb_transactions_v1",
		"mariadb_metadata_locks_v1",
		"mariadb_processlist_v1",
		"mariadb_replication_status_v1",
		"mariadb_schemata_v1",
		"mariadb_statement_digests_v1",
		"mariadb_table_statistics_v1",
		"mariadb_user_accounts_v1",
		"mariadb_user_privileges_v1",
	}
	var got []string
	for _, c := range mariadbCatalog() {
		got = append(got, c.ID)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("mariadb catalog set = %v, want %v", got, want)
	}
}

// TC-MDC-15 (MDC-R012): global_status carries the InnoDB buffer-pool read
// counters for Analyzer's buffer-pool-read-pressure rate rule (#656 / #3918).
func TestMariaDBGlobalStatusBufferPoolReads(t *testing.T) {
	gs := collectorByID(mariadbCatalog(), "mariadb_global_status_v1")
	if gs == nil {
		t.Fatal("mariadb_global_status_v1 missing")
	}
	for _, v := range []string{"Innodb_buffer_pool_reads", "Innodb_buffer_pool_read_requests"} {
		if !strings.Contains(gs.SQL, "'"+v+"'") {
			t.Errorf("mariadb_global_status_v1 IN-list must contain %q; SQL=%q", v, gs.SQL)
		}
	}
}

// TC-MDC-16 (MDC-R013): the deadlock counter is collected unconditionally (no
// ENABLED=1 filter) with its ENABLED flag surfaced, so the signal is reliable
// regardless of the InnoDB metrics lock-module default (#656 / #3918).
func TestMariaDBInnodbDeadlocksCollector(t *testing.T) {
	c := collectorByID(mariadbCatalog(), "mariadb_innodb_deadlocks_v1")
	if c == nil {
		t.Fatal("mariadb_innodb_deadlocks_v1 missing")
	}
	if HasWrite(c.SQL) {
		t.Errorf("mariadb_innodb_deadlocks_v1 must be read-only: %q", c.SQL)
	}
	if c.HighSensitivity || len(c.SensitiveColumns) != 0 {
		t.Errorf("mariadb_innodb_deadlocks_v1 carries no sensitive payload; got HighSensitivity=%v cols=%v", c.HighSensitivity, c.SensitiveColumns)
	}
	up := strings.ToUpper(c.SQL)
	if !strings.Contains(up, "INNODB_METRICS") || !strings.Contains(c.SQL, "'lock_deadlocks'") {
		t.Errorf("mariadb_innodb_deadlocks_v1 must select lock_deadlocks from INNODB_METRICS; SQL=%q", c.SQL)
	}
	if strings.Contains(up, "ENABLED=1") || strings.Contains(up, "ENABLED = 1") {
		t.Errorf("mariadb_innodb_deadlocks_v1 must NOT filter on ENABLED=1 (row must always return with its ENABLED flag); SQL=%q", c.SQL)
	}
	if !strings.Contains(up, "ENABLED") {
		t.Errorf("mariadb_innodb_deadlocks_v1 must surface ENABLED so the consumer can detect a disabled metric; SQL=%q", c.SQL)
	}
}

// TC-MDC-03 (MDC-R003/R004, INV-MDC-02): processlist redacts INFO.
func TestMariaDBProcesslistRedaction(t *testing.T) {
	var pl *Collector
	for i := range mariadbCatalog() {
		if mariadbCatalog()[i].ID == "mariadb_processlist_v1" {
			pl = &mariadbCatalog()[i]
		}
	}
	if pl == nil {
		t.Fatal("mariadb_processlist_v1 missing")
	}
	if !pl.HighSensitivity {
		t.Error("mariadb_processlist_v1 must be HighSensitivity")
	}
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

// TC-MDC-04 (MDC-R005): CatalogFor routes mariadb to the MariaDB catalog, no leak.
func TestCatalogForMariaDB(t *testing.T) {
	ids := func(cs []Collector) map[string]bool {
		m := map[string]bool{}
		for _, c := range cs {
			m[c.ID] = true
		}
		return m
	}
	md := ids(CatalogFor("mariadb"))
	if len(md) != len(mariadbCatalog()) || !md["mariadb_identity_v1"] {
		t.Errorf("CatalogFor(mariadb) wrong: %v", md)
	}
	// No cross-engine leak.
	if md["mysql_identity_v1"] || md["singlestore_identity_v1"] {
		t.Error("CatalogFor(mariadb) leaked a collector from another engine")
	}
	if ids(CatalogFor("mysql"))["mariadb_identity_v1"] {
		t.Error("CatalogFor(mysql) leaked a mariadb collector")
	}
}

// mariadbCollectorByID is a small test helper.
func mariadbCollectorByID(id string) *Collector {
	for i := range mariadbCatalog() {
		if mariadbCatalog()[i].ID == id {
			c := mariadbCatalog()[i]
			return &c
		}
	}
	return nil
}

// TC-MDC-06 (#616): the InnoDB-transactions collector carries live SQL text in
// trx_query (lowercase result key) and must be HighSensitivity with it redacted.
func TestMariaDBInnodbTransactionsRedaction(t *testing.T) {
	trx := mariadbCollectorByID("mariadb_innodb_transactions_v1")
	if trx == nil {
		t.Fatal("mariadb_innodb_transactions_v1 missing")
	}
	if !trx.HighSensitivity {
		t.Error("mariadb_innodb_transactions_v1 must be HighSensitivity (carries trx_query)")
	}
	found := false
	for _, col := range trx.SensitiveColumns {
		if col == "trx_query" {
			found = true
		}
	}
	if !found {
		t.Error("mariadb_innodb_transactions_v1 must list trx_query (lowercase) in SensitiveColumns")
	}
	runner := func(_ context.Context, sql string) ([]map[string]any, error) {
		if strings.Contains(sql, "INNODB_TRX") {
			return []map[string]any{{"trx_id": "33", "trx_query": "UPDATE accounts SET balance=balance+5 WHERE id=1"}}, nil
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

// TC-MDC-07 (#616): the InnoDB-locks collector carries the locked index-key value
// in lock_data (row data, lowercase result key) and must be HighSensitivity with
// it redacted, while native lock descriptors are preserved.
func TestMariaDBInnodbLocksRedaction(t *testing.T) {
	dl := mariadbCollectorByID("mariadb_innodb_locks_v1")
	if dl == nil {
		t.Fatal("mariadb_innodb_locks_v1 missing")
	}
	if !dl.HighSensitivity {
		t.Error("mariadb_innodb_locks_v1 must be HighSensitivity (carries lock_data)")
	}
	found := false
	for _, col := range dl.SensitiveColumns {
		if col == "lock_data" {
			found = true
		}
	}
	if !found {
		t.Error("mariadb_innodb_locks_v1 must list lock_data (lowercase) in SensitiveColumns")
	}
	runner := func(_ context.Context, sql string) ([]map[string]any, error) {
		if strings.Contains(sql, "INNODB_LOCKS") {
			return []map[string]any{{"lock_mode": "X", "lock_table": "`signals_fixture`.`accounts`", "lock_data": "1"}}, nil
		}
		return []map[string]any{}, nil
	}
	rs := Collect(context.Background(), runner, []Collector{*dl}, true, false)
	if len(rs) != 1 || len(rs[0].Rows) != 1 {
		t.Fatalf("unexpected result: %+v", rs)
	}
	row := rs[0].Rows[0]
	if v := row["lock_data"]; v != nil {
		t.Errorf("lock_data must be redacted to nil, got %v", v)
	}
	if row["lock_mode"] != "X" || row["lock_table"] != "`signals_fixture`.`accounts`" {
		t.Errorf("native lock descriptors must be preserved, got %+v", row)
	}
}

// TC-MDC-08 (INV-MDC-03, MDC-R004/R008): no query-text column or locked-key data
// escapes the redaction contract — catalog-wide, case-insensitive on the SQL,
// exact result-row case for SensitiveColumns membership.
func TestMariaDBCatalogNoUnredactedSensitiveColumn(t *testing.T) {
	sensitive := map[string]string{
		"INFO":        "INFO",
		"DIGEST_TEXT": "DIGEST_TEXT",
		"TRX_QUERY":   "trx_query",
		"LOCK_DATA":   "lock_data",
	}
	for _, c := range mariadbCatalog() {
		up := strings.ToUpper(c.SQL)
		for qt, exactCase := range sensitive {
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

// perfSchemaRunner dispatches the digest collector SQL to digestRows/digestErr
// and the @@performance_schema probe to probeVal/probeErr, recording whether the
// probe was ever issued. The digest SQL is identified by its table name; the
// probe by "@@performance_schema".
func perfSchemaRunner(digestRows []map[string]any, digestErr error, probeVal any, probeErr error, probed *bool) QueryRunner {
	return func(_ context.Context, sql string) ([]map[string]any, error) {
		switch {
		case strings.Contains(sql, "@@performance_schema"):
			*probed = true
			if probeErr != nil {
				return nil, probeErr
			}
			return []map[string]any{{"performance_schema": probeVal}}, nil
		case strings.Contains(sql, "events_statements_summary_by_digest"):
			return digestRows, digestErr
		default:
			// Every other collector in the catalog returns an empty success so
			// Collect runs the whole catalog without incident.
			return []map[string]any{}, nil
		}
	}
}

// TC-MDC-11 (MDC-R010/R011): perf_schema OFF -> statement-digest collector is a
// DEGRADED / NOT_ASSESSED result (skipped + perf_schema_disabled), never a silent
// success or a failure.
func TestMariaDBStatementDigestsPerfSchemaOffDegrades(t *testing.T) {
	probed := false
	run := perfSchemaRunner(nil, nil, "0", nil, &probed)
	rs := Collect(context.Background(), run, mariadbCatalog(), false, false)
	d := resultByID(rs, "mariadb_statement_digests_v1")
	if d == nil {
		t.Fatal("digest collector result missing")
	}
	if !probed {
		t.Error("expected the @@performance_schema probe to fire on a 0-row result")
	}
	if d.Err != "" {
		t.Errorf("perf_schema-off degrade must not be a failure, got Err=%q", d.Err)
	}
	if d.Status != "skipped" || d.Reason != ReasonPerfSchemaDisabled {
		t.Errorf("want status=skipped reason=%q, got status=%q reason=%q", ReasonPerfSchemaDisabled, d.Status, d.Reason)
	}
}

// TC-MDC-12 (MDC-R010): perf_schema ON with 0 rows stays a legitimate empty
// success ("no slow queries"), not a degrade.
func TestMariaDBStatementDigestsPerfSchemaOnEmptyIsSuccess(t *testing.T) {
	probed := false
	run := perfSchemaRunner(nil, nil, "1", nil, &probed)
	rs := Collect(context.Background(), run, mariadbCatalog(), false, false)
	d := resultByID(rs, "mariadb_statement_digests_v1")
	if d == nil {
		t.Fatal("digest collector result missing")
	}
	if !probed {
		t.Error("expected the probe to fire on a 0-row result to disambiguate")
	}
	if d.Err != "" || d.Status != "" || d.Reason != "" {
		t.Errorf("perf_schema ON + 0 rows must be a plain success, got Err=%q status=%q reason=%q", d.Err, d.Status, d.Reason)
	}
}

// TC-MDC-13 (MDC-R010): a collector that returned rows is never probed and never
// degraded.
func TestMariaDBStatementDigestsRowsPresentNotProbed(t *testing.T) {
	probed := false
	rows := []map[string]any{{"DIGEST": "abc", "COUNT_STAR": "5"}}
	run := perfSchemaRunner(rows, nil, "0", nil, &probed)
	rs := Collect(context.Background(), run, mariadbCatalog(), false, false)
	d := resultByID(rs, "mariadb_statement_digests_v1")
	if d == nil {
		t.Fatal("digest collector result missing")
	}
	if probed {
		t.Error("a non-empty digest result must NOT be probed")
	}
	if d.Status != "" || d.Reason != "" || len(d.Rows) != 1 {
		t.Errorf("rows-present result must be untouched, got status=%q reason=%q rows=%d", d.Status, d.Reason, len(d.Rows))
	}
}

// TC-MDC-14 (MDC-R011): a probe that errors never downgrades the legitimate 0-row
// success to a failure or skip.
func TestMariaDBStatementDigestsProbeErrorStaysSuccess(t *testing.T) {
	probed := false
	run := perfSchemaRunner(nil, nil, nil, errors.New("probe boom"), &probed)
	rs := Collect(context.Background(), run, mariadbCatalog(), false, false)
	d := resultByID(rs, "mariadb_statement_digests_v1")
	if d == nil {
		t.Fatal("digest collector result missing")
	}
	if !probed {
		t.Error("expected the probe to be attempted")
	}
	if d.Err != "" || d.Status != "" || d.Reason != "" {
		t.Errorf("a probe error must leave the 0-row success intact, got Err=%q status=%q reason=%q", d.Err, d.Status, d.Reason)
	}
}

// MDC-R010: the digest collector declares the perf_schema gate so the runtime
// knows to probe it.
func TestMariaDBStatementDigestsIsPerfSchemaGated(t *testing.T) {
	d := collectorByID(mariadbCatalog(), "mariadb_statement_digests_v1")
	if d == nil {
		t.Fatal("digest collector missing from catalog")
	}
	if !d.PerfSchemaGated {
		t.Error("mariadb_statement_digests_v1 must be PerfSchemaGated (MDC-R010)")
	}
}

// TC-MDC-17 (MDC-R014): the table-statistics collector carries AUTO_INCREMENT
// (the current counter value) for Analyzer's AUTO_INCREMENT-exhaustion-risk rule
// (#677 / Analyzer #4004). It is NULL for a table with no AUTO_INCREMENT column
// and must be carried through as an absent (nil) value, never an error.
func TestMariaDBTableStatisticsAutoIncrement(t *testing.T) {
	c := collectorByID(mariadbCatalog(), "mariadb_table_statistics_v1")
	if c == nil {
		t.Fatal("mariadb_table_statistics_v1 missing")
	}
	if HasWrite(c.SQL) {
		t.Errorf("mariadb_table_statistics_v1 must be read-only: %q", c.SQL)
	}
	if c.HighSensitivity || len(c.SensitiveColumns) != 0 {
		t.Errorf("mariadb_table_statistics_v1 carries no sensitive payload; got HighSensitivity=%v cols=%v", c.HighSensitivity, c.SensitiveColumns)
	}
	up := strings.ToUpper(c.SQL)
	if !strings.Contains(up, "INFORMATION_SCHEMA.TABLES") {
		t.Errorf("mariadb_table_statistics_v1 must read information_schema.TABLES; SQL=%q", c.SQL)
	}
	if !strings.Contains(up, "AUTO_INCREMENT") {
		t.Errorf("mariadb_table_statistics_v1 must select AUTO_INCREMENT; SQL=%q", c.SQL)
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

// TC-MDC-18 (MDC-R015): the table-statistics collector carries TABLE_COLLATION
// (the per-table collation) for Analyzer's utf8mb4-readiness / charset-collation
// -mismatch rule (#684 / Analyzer #4001). The charset family is derivable from
// the collation; information_schema.TABLES has no direct charset column.
func TestMariaDBTableStatisticsTableCollation(t *testing.T) {
	c := collectorByID(mariadbCatalog(), "mariadb_table_statistics_v1")
	if c == nil {
		t.Fatal("mariadb_table_statistics_v1 missing")
	}
	if HasWrite(c.SQL) {
		t.Errorf("mariadb_table_statistics_v1 must be read-only: %q", c.SQL)
	}
	if c.HighSensitivity || len(c.SensitiveColumns) != 0 {
		t.Errorf("mariadb_table_statistics_v1 carries no sensitive payload; got HighSensitivity=%v cols=%v", c.HighSensitivity, c.SensitiveColumns)
	}
	up := strings.ToUpper(c.SQL)
	if !strings.Contains(up, "INFORMATION_SCHEMA.TABLES") {
		t.Errorf("mariadb_table_statistics_v1 must read information_schema.TABLES; SQL=%q", c.SQL)
	}
	if !strings.Contains(up, "TABLE_COLLATION") {
		t.Errorf("mariadb_table_statistics_v1 must select TABLE_COLLATION; SQL=%q", c.SQL)
	}

	runner := func(_ context.Context, sql string) ([]map[string]any, error) {
		if strings.Contains(sql, "information_schema.TABLES") {
			return []map[string]any{
				{"TABLE_SCHEMA": "app", "TABLE_NAME": "orders", "TABLE_COLLATION": "utf8mb4_general_ci"},
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
	if v := rs[0].Rows[0]["TABLE_COLLATION"]; v != "utf8mb4_general_ci" {
		t.Errorf("TABLE_COLLATION = %v, want utf8mb4_general_ci", v)
	}
}

// TC-MDC-19 (MDC-R015): the columns collector carries CHARACTER_SET_NAME and
// COLLATION_NAME for Analyzer's utf8mb4-readiness rule (#684 / Analyzer #4001).
// Both are NULL for non-string columns and must be carried through as absent
// (nil) values, never an error.
func TestMariaDBColumnsCharsetCollation(t *testing.T) {
	c := collectorByID(mariadbCatalog(), "mariadb_columns_v1")
	if c == nil {
		t.Fatal("mariadb_columns_v1 missing")
	}
	if HasWrite(c.SQL) {
		t.Errorf("mariadb_columns_v1 must be read-only: %q", c.SQL)
	}
	if c.HighSensitivity || len(c.SensitiveColumns) != 0 {
		t.Errorf("mariadb_columns_v1 carries no sensitive payload; got HighSensitivity=%v cols=%v", c.HighSensitivity, c.SensitiveColumns)
	}
	up := strings.ToUpper(c.SQL)
	if !strings.Contains(up, "CHARACTER_SET_NAME") {
		t.Errorf("mariadb_columns_v1 must select CHARACTER_SET_NAME; SQL=%q", c.SQL)
	}
	if !strings.Contains(up, "COLLATION_NAME") {
		t.Errorf("mariadb_columns_v1 must select COLLATION_NAME; SQL=%q", c.SQL)
	}

	// A string column carries charset/collation; a non-string column (e.g. int)
	// carries NULL for both, carried through as nil — never an error.
	runner := func(_ context.Context, sql string) ([]map[string]any, error) {
		if strings.Contains(sql, "information_schema.COLUMNS") {
			return []map[string]any{
				{"TABLE_SCHEMA": "app", "TABLE_NAME": "orders", "COLUMN_NAME": "note", "CHARACTER_SET_NAME": "latin1", "COLLATION_NAME": "latin1_swedish_ci"},
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
	if v := rs[0].Rows[0]["CHARACTER_SET_NAME"]; v != "latin1" {
		t.Errorf("string column CHARACTER_SET_NAME = %v, want latin1", v)
	}
	if v := rs[0].Rows[0]["COLLATION_NAME"]; v != "latin1_swedish_ci" {
		t.Errorf("string column COLLATION_NAME = %v, want latin1_swedish_ci", v)
	}
	if v, ok := rs[0].Rows[1]["CHARACTER_SET_NAME"]; !ok || v != nil {
		t.Errorf("non-string column CHARACTER_SET_NAME must be carried as nil (absent), got %v (present=%v)", v, ok)
	}
	if v, ok := rs[0].Rows[1]["COLLATION_NAME"]; !ok || v != nil {
		t.Errorf("non-string column COLLATION_NAME must be carried as nil (absent), got %v (present=%v)", v, ok)
	}
}

func collectorByID(cat []Collector, id string) *Collector {
	for i := range cat {
		if cat[i].ID == id {
			return &cat[i]
		}
	}
	return nil
}
