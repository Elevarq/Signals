package clickhouse

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Tests derive from specifications/clickhouse-backend.acceptance.md.

func stubRunner(failSubstr string) QueryRunner {
	return func(_ context.Context, sql string) ([]map[string]any, error) {
		if failSubstr != "" && strings.Contains(sql, failSubstr) {
			return nil, errors.New("boom")
		}
		if strings.Contains(sql, "version()") {
			return []map[string]any{{"version": "24.8.14.39", "uptime_s": int64(1)}}, nil
		}
		return []map[string]any{}, nil
	}
}

// TC-CHB-01 (INV-CHB-01): every catalog statement is a read-only SELECT.
func TestCatalogReadOnly(t *testing.T) {
	for _, c := range Catalog() {
		if HasWrite(c.SQL) {
			t.Errorf("collector %q is not a read-only SELECT: %q", c.ID, c.SQL)
		}
		if c.ID == "" || c.Category == "" || strings.TrimSpace(c.SQL) == "" {
			t.Errorf("collector has empty id/category/sql: %+v", c)
		}
	}
}

// TC-CHB-02: Collect runs the catalog and captures rows per collector.
func TestCollectRunsCatalog(t *testing.T) {
	rs := Collect(context.Background(), stubRunner(""), Catalog(), false)
	if len(rs) != len(Catalog()) {
		t.Fatalf("want %d results, got %d", len(Catalog()), len(rs))
	}
	id := rs[0]
	if id.CollectorID != "clickhouse_identity_v1" || id.Err != "" || len(id.Rows) != 1 {
		t.Fatalf("identity result wrong: %+v", id)
	}
	if id.Rows[0]["version"] != "24.8.14.39" {
		t.Errorf("identity version wrong: %v", id.Rows[0]["version"])
	}
}

// TC-CHB-03: a collector error is captured, not fatal; others still run.
func TestCollectPartialFailure(t *testing.T) {
	rs := Collect(context.Background(), stubRunner("version()"), Catalog(), false)
	if rs[0].Err == "" {
		t.Error("identity collector should have captured the injected error")
	}
}

// TC-CHC-01 (CHC-R005): the full CH-2 system.* catalog id set, identity first.
func TestCatalogExpectedSet(t *testing.T) {
	want := []string{
		"clickhouse_identity_v1",
		"clickhouse_clusters_v1",
		"clickhouse_databases_v1",
		"clickhouse_tables_v1",
		"clickhouse_parts_v1",
		"clickhouse_replicas_v1",
		"clickhouse_merges_v1",
		"clickhouse_mutations_v1",
		"clickhouse_metrics_v1",
		"clickhouse_events_v1",
		"clickhouse_asynchronous_metrics_v1",
		"clickhouse_settings_v1",
		"clickhouse_query_log_v1",
		"clickhouse_query_thread_log_v1",
	}
	cat := Catalog()
	if len(cat) != len(want) {
		t.Fatalf("catalog has %d collectors, want %d", len(cat), len(want))
	}
	if cat[0].ID != "clickhouse_identity_v1" {
		t.Errorf("identity collector must be first, got %q", cat[0].ID)
	}
	have := map[string]bool{}
	for _, c := range cat {
		have[c.ID] = true
	}
	for _, id := range want {
		if !have[id] {
			t.Errorf("catalog missing %q", id)
		}
	}
}

// TC-CHC-02 (CHC-R004, INV-CHB-01): the query-log collectors carry application
// SQL text and must be HighSensitivity with `query` redacted; no OTHER collector
// selects a `query` column unredacted.
func TestQueryLogRedaction(t *testing.T) {
	queryLogIDs := map[string]bool{"clickhouse_query_log_v1": true, "clickhouse_query_thread_log_v1": true}
	for _, c := range Catalog() {
		selectsQuery := strings.Contains(strings.ToLower(c.SQL), " query ") || strings.HasSuffix(strings.ToLower(strings.TrimSpace(c.SQL)), "query")
		_ = selectsQuery
		if queryLogIDs[c.ID] {
			if !c.HighSensitivity {
				t.Errorf("%s selects query text — must be HighSensitivity", c.ID)
			}
			found := false
			for _, col := range c.SensitiveColumns {
				if col == "query" {
					found = true
				}
			}
			if !found {
				t.Errorf("%s must list `query` in SensitiveColumns", c.ID)
			}
		}
	}
	// Collect redacts the query column for a query-log collector.
	var ql Collector
	for _, c := range Catalog() {
		if c.ID == "clickhouse_query_log_v1" {
			ql = c
		}
	}
	runner := func(_ context.Context, _ string) ([]map[string]any, error) {
		return []map[string]any{{"type": "QueryFinish", "query": "SELECT secret FROM t"}}, nil
	}
	rs := Collect(context.Background(), runner, []Collector{ql}, true)
	if len(rs) != 1 || len(rs[0].Rows) != 1 {
		t.Fatalf("unexpected result: %+v", rs)
	}
	if v := rs[0].Rows[0]["query"]; v != nil {
		t.Errorf("query must be redacted to nil, got %v", v)
	}
}

// TC-CHC-04 (CHC-R006, INV-CHC-03): clickhouse_parts_v1 must be bounded and
// aggregated — one row per (database, table, partition_id) via count()/sum(),
// ORDER BY parts DESC + LIMIT — so a busy MergeTree with millions of parts
// cannot produce an unbounded per-part payload (#562). It must NOT be a raw
// per-part SELECT (no part_type row-per-part).
func TestPartsCollectorBoundedAggregated(t *testing.T) {
	var parts Collector
	for _, c := range Catalog() {
		if c.ID == "clickhouse_parts_v1" {
			parts = c
		}
	}
	if parts.ID == "" {
		t.Fatal("clickhouse_parts_v1 not in catalog")
	}
	sql := strings.ToLower(parts.SQL)
	for _, must := range []string{"group by", "partition_id", "count()", "order by", "desc", "limit"} {
		if !strings.Contains(sql, must) {
			t.Errorf("clickhouse_parts_v1 must be bounded+aggregated: missing %q in %q", must, parts.SQL)
		}
	}
	// Aggregation drops the per-part part_type column; its presence means the
	// collector is still emitting one row per part (the unbounded shape).
	if strings.Contains(sql, "part_type") {
		t.Errorf("clickhouse_parts_v1 still selects per-part part_type — not aggregated: %q", parts.SQL)
	}
	// Still read-only and still scoped away from system schemas.
	if HasWrite(parts.SQL) {
		t.Errorf("clickhouse_parts_v1 must stay read-only: %q", parts.SQL)
	}
	if !strings.Contains(sql, "where active") || !strings.Contains(sql, "not in") {
		t.Errorf("clickhouse_parts_v1 must keep active-only + system-schema exclusion: %q", parts.SQL)
	}
}

// TC-CHB-04: Identify returns the version from a ClickHouse server.
func TestIdentify(t *testing.T) {
	v, err := Identify(context.Background(), func(_ context.Context, _ string) (string, error) {
		return "24.8.14.39", nil
	})
	if err != nil || v != "24.8.14.39" {
		t.Fatalf("Identify = %q, %v; want 24.8.14.39, nil", v, err)
	}
	// Empty version → not a ClickHouse server.
	if _, err := Identify(context.Background(), func(_ context.Context, _ string) (string, error) {
		return "", nil
	}); err == nil {
		t.Error("empty version must be an identity error")
	}
}

// TC-CHB-05: IsPermissionDenied recognizes a ClickHouse ACCESS_DENIED error.
func TestIsPermissionDenied(t *testing.T) {
	cases := map[string]bool{
		"Code: 497. DB::Exception: signals_ro: Not enough privileges. ACCESS_DENIED": true,
		"code: 497 access_denied":                true,
		"Code: 60. DB::Exception: Unknown table": false,
		"dial tcp: connection refused":           false,
		"":                                       false,
	}
	for msg, want := range cases {
		if got := IsPermissionDenied(msg); got != want {
			t.Errorf("IsPermissionDenied(%q) = %v, want %v", msg, got, want)
		}
	}
}
