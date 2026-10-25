package singlestore

import (
	"context"
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

// TC-MDC-02 (MDC-R005): expected MariaDB collector set (17, all green under SELECT,PROCESS).
func TestMariaDBCatalogExpectedSet(t *testing.T) {
	want := []string{
		"mariadb_columns_v1",
		"mariadb_engines_v1",
		"mariadb_galera_status_v1",
		"mariadb_global_status_v1",
		"mariadb_global_variables_v1",
		"mariadb_identity_v1",
		"mariadb_index_inventory_v1",
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
	rs := Collect(context.Background(), runner, []Collector{*pl}, true)
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
	rs := Collect(context.Background(), runner, []Collector{*trx}, true)
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
	rs := Collect(context.Background(), runner, []Collector{*dl}, true)
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
