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

// TC-MDC-02 (MDC-R005): expected MariaDB collector set (7, all green under SELECT,PROCESS).
func TestMariaDBCatalogExpectedSet(t *testing.T) {
	want := []string{
		"mariadb_engines_v1",
		"mariadb_global_status_v1",
		"mariadb_global_variables_v1",
		"mariadb_identity_v1",
		"mariadb_processlist_v1",
		"mariadb_schemata_v1",
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
