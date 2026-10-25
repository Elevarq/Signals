package clickhouse

import (
	"strings"
	"testing"
)

// Tests derive from specifications/clickhouse-grants.acceptance.md (#565).

// TC-CHG-01: exactly the two query-workload collectors read the optional
// (cross-user) query-log tables; every other collector reads only baseline
// metadata system.* tables. Guards the least-privilege boundary against a new
// collector silently needing the optional query-log grant.
func TestOptionalGrantSurfaceIsQueryLogsOnly(t *testing.T) {
	wantOptional := map[string]bool{
		"clickhouse_query_log_v1":        true,
		"clickhouse_query_thread_log_v1": true,
	}
	for _, c := range Catalog() {
		readsQueryLog := strings.Contains(c.SQL, "system.query_log") || strings.Contains(c.SQL, "system.query_thread_log")
		if readsQueryLog && !wantOptional[c.ID] {
			t.Errorf("collector %q reads a query-log table but is not a known optional-grant collector; the baseline role would skip it unexpectedly (update clickhouse-grants.md)", c.ID)
		}
		if wantOptional[c.ID] && !readsQueryLog {
			t.Errorf("collector %q is expected to read a query-log table but does not", c.ID)
		}
	}
	// Both optional collectors must be present in the catalog.
	for id := range wantOptional {
		found := false
		for _, c := range Catalog() {
			if c.ID == id {
				found = true
			}
		}
		if !found {
			t.Errorf("expected optional query-log collector %q missing from catalog", id)
		}
	}
}

// TC-CHG-02: the query-log collector is HighSensitivity with `query` redacted
// (defense in depth over the optional grant); the non-sensitive identity is kept.
func TestQueryLogCollectorRedactsQuery(t *testing.T) {
	var ql *Collector
	for i := range Catalog() {
		if Catalog()[i].ID == "clickhouse_query_log_v1" {
			c := Catalog()[i]
			ql = &c
		}
	}
	if ql == nil {
		t.Fatal("clickhouse_query_log_v1 missing")
	}
	if !ql.HighSensitivity {
		t.Error("clickhouse_query_log_v1 must be HighSensitivity (carries query text)")
	}
	redacted := false
	for _, col := range ql.SensitiveColumns {
		if col == "query" {
			redacted = true
		}
	}
	if !redacted {
		t.Error("clickhouse_query_log_v1 must list query in SensitiveColumns")
	}
	// The non-sensitive typed identity is retained (not redacted).
	for _, col := range ql.SensitiveColumns {
		if col == "normalized_query_hash" {
			t.Error("normalized_query_hash is the non-sensitive identity and must NOT be redacted")
		}
	}
}
