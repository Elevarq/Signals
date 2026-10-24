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
		"mysql_engines_v1",
		"mysql_global_status_v1",
		"mysql_global_variables_v1",
		"mysql_identity_v1",
		"mysql_innodb_metrics_v1",
		"mysql_processlist_v1",
		"mysql_replication_status_v1",
		"mysql_schemata_v1",
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
	rs := Collect(context.Background(), runner, []Collector{*pl}, true)
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

// TC-MYC-05 (INV-MYC-03): no query-text column escapes the redaction contract.
func TestMySQLCatalogNoUnredactedQueryText(t *testing.T) {
	for _, c := range mysqlCatalog() {
		up := strings.ToUpper(c.SQL)
		for _, qt := range []string{"INFO"} {
			// Match the query-text column as a select-list token (preceded by a
			// comma or followed by FROM), not a substring of another identifier
			// like INFORMATION_SCHEMA.
			if strings.Contains(up, ", "+qt) || strings.Contains(up, " "+qt+" FROM") || strings.Contains(up, " "+qt+",") {
				if !c.HighSensitivity {
					t.Errorf("collector %q selects %s but is not HighSensitivity", c.ID, qt)
				}
				has := false
				for _, col := range c.SensitiveColumns {
					if col == qt {
						has = true
					}
				}
				if !has {
					t.Errorf("collector %q selects %s but it is not in SensitiveColumns", c.ID, qt)
				}
			}
		}
	}
}
