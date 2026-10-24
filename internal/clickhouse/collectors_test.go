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
