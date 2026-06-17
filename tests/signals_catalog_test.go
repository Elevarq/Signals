package tests

import (
	"sort"
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// TestCatalogMinimumCount verifies that the query catalog contains at
// least 9 registered queries.
// Traces: ARQ-SIGNALS-R002 / TC-SIG-004
func TestCatalogMinimumCount(t *testing.T) {
	all := pgqueries.All()
	if len(all) < 9 {
		t.Fatalf("expected >= 9 registered queries, got %d", len(all))
	}
}

// TestCatalogRequiredQueries verifies all 9 required query IDs are
// present in the catalog.
// Traces: ARQ-SIGNALS-R003 / TC-SIG-005
func TestCatalogRequiredQueries(t *testing.T) {
	required := []string{
		"pg_version_v1",
		"pg_settings_v1",
		"pg_stat_activity_v1",
		"pg_stat_database_v1",
		"pg_stat_user_tables_v1",
		"pg_stat_user_indexes_v1",
		"pg_statio_user_tables_v1",
		"pg_statio_user_indexes_v1",
		"pg_stat_statements_v1",
	}

	all := pgqueries.All()
	idSet := make(map[string]bool, len(all))
	for _, q := range all {
		idSet[q.ID] = true
	}

	for _, id := range required {
		if !idSet[id] {
			t.Errorf("required query %q is missing from the catalog", id)
		}
	}
}

// TestCatalogAllPassLint verifies every registered query passes the linter.
// Traces: ARQ-SIGNALS-R002 / TC-SIG-004
func TestCatalogAllPassLint(t *testing.T) {
	for _, q := range pgqueries.All() {
		if err := pgqueries.LintQuery(q.SQL); err != nil {
			t.Errorf("query %q failed lint: %v", q.ID, err)
		}
	}
}

// TestPgSettingsV1SelectShape pins the column list of the
// pg_settings_v1 collector. context, vartype, boot_val, reset_val
// are the load-bearing additions for the downstream Elevarq Analyzer
// without context, downstream cannot distinguish
// USERSET from POSTMASTER GUCs without a hardcoded allowlist.
// sourcefile and sourceline are intentionally excluded — they are
// NULL on managed platforms (FC-01).
func TestPgSettingsV1SelectShape(t *testing.T) {
	var def pgqueries.QueryDef
	for _, q := range pgqueries.All() {
		if q.ID == "pg_settings_v1" {
			def = q
			break
		}
	}
	if def.ID == "" {
		t.Fatal("pg_settings_v1 not found in catalog")
	}
	sql := def.SQL
	required := []string{
		"name", "setting", "unit", "category", "source", "pending_restart",
		"context", "vartype", "boot_val", "reset_val",
		"min_val", "max_val", "enumvals", "short_desc",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_settings_v1 SQL missing column %q\nSQL: %s", col, sql)
		}
	}
	// Cross-platform safety: sourcefile / sourceline must NOT be in
	// the SELECT (FC-01 — NULL on managed platforms).
	for _, col := range []string{"sourcefile", "sourceline"} {
		if strings.Contains(sql, col) {
			t.Errorf("pg_settings_v1 SQL must NOT include %q (FC-01: NULL on managed platforms)", col)
		}
	}
}

// TestCatalogSorted verifies that All() returns queries sorted by ID.
// Traces: ARQ-SIGNALS-R002 / TC-SIG-004
func TestCatalogSorted(t *testing.T) {
	all := pgqueries.All()
	ids := make([]string, len(all))
	for i, q := range all {
		ids[i] = q.ID
	}
	if !sort.StringsAreSorted(ids) {
		t.Errorf("All() result is not sorted by ID: %v", ids)
	}
}
