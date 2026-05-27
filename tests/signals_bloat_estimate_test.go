package tests

import (
	"strings"
	"testing"
	"time"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// bloat_estimate_v1 — statistical table-bloat estimate, no pgstattuple.
//
// Specification: specifications/collectors/bloat_estimate_v1.md
// Acceptance:    specifications/collectors/bloat_estimate_v1.acceptance.md
// ---------------------------------------------------------------------------

// TC-BLOAT-01.
func TestBloatEstimateRegistered(t *testing.T) {
	q := pgqueries.ByID("bloat_estimate_v1")
	if q == nil {
		t.Fatal("bloat_estimate_v1 is not registered")
	}
	if q.Category != "tables" {
		t.Errorf("category: got %q, want %q", q.Category, "tables")
	}
	if q.Cadence != pgqueries.Cadence6h {
		t.Errorf("cadence: got %v, want Cadence6h", q.Cadence)
	}
	if q.RetentionClass != pgqueries.RetentionMedium {
		t.Errorf("retention: got %q, want RetentionMedium", q.RetentionClass)
	}
	if q.Timeout != 30*time.Second {
		t.Errorf("timeout: got %v, want 30s", q.Timeout)
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
	if q.MinPGVersion != 0 {
		t.Errorf("MinPGVersion: got %d, want 0 (no gate)", q.MinPGVersion)
	}
	if q.RequiresExtension != "" {
		t.Errorf("RequiresExtension: got %q, want empty", q.RequiresExtension)
	}
}

// TC-BLOAT-02.
func TestBloatEstimatePassesLinter(t *testing.T) {
	q := pgqueries.ByID("bloat_estimate_v1")
	if q == nil {
		t.Fatal("bloat_estimate_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("bloat_estimate_v1 failed linter: %v", err)
	}
}

// TC-BLOAT-03.
func TestBloatEstimateScopeFilters(t *testing.T) {
	q := pgqueries.ByID("bloat_estimate_v1")
	if q == nil {
		t.Fatal("bloat_estimate_v1 not registered")
	}
	if !strings.Contains(q.SQL, "relkind IN ('r', 'm', 'p')") {
		t.Error("SQL must filter relkind IN ('r', 'm', 'p')")
	}
	if !strings.Contains(q.SQL, "'pg_catalog'") {
		t.Error("SQL must exclude pg_catalog")
	}
	if !strings.Contains(q.SQL, "'information_schema'") {
		t.Error("SQL must exclude information_schema")
	}
	if !strings.Contains(q.SQL, "'pg_toast'") {
		t.Error("SQL must exclude pg_toast")
	}
	if !strings.Contains(q.SQL, `'pg\_temp\_%'`) {
		t.Error("SQL must exclude pg_temp_* via NOT LIKE")
	}
	if !strings.Contains(q.SQL, `'pg\_toast\_temp\_%'`) {
		t.Error("SQL must exclude pg_toast_temp_* via NOT LIKE")
	}
}

// TC-BLOAT-04.
func TestBloatEstimateOutputColumns(t *testing.T) {
	q := pgqueries.ByID("bloat_estimate_v1")
	if q == nil {
		t.Fatal("bloat_estimate_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"schemaname", "tablename", "table_oid", "relkind",
		"actual_size_bytes", "expected_size_bytes",
		"bloat_bytes", "bloat_ratio",
		"reltuples", "n_live_tup", "n_dead_tup",
		"last_autovacuum", "stats_missing",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("SQL must reference column %q", col)
		}
	}
}

// TC-BLOAT-05.
func TestBloatEstimateNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("bloat_estimate_v1")
	if q == nil {
		t.Fatal("bloat_estimate_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("bloat_estimate_v1 must not use SELECT *")
	}
}

// TC-BLOAT-06.
func TestBloatEstimateDeterministicOrder(t *testing.T) {
	q := pgqueries.ByID("bloat_estimate_v1")
	if q == nil {
		t.Fatal("bloat_estimate_v1 not registered")
	}
	if !strings.Contains(q.SQL, "ORDER BY e.schemaname, e.tablename") {
		t.Error("outer SELECT must ORDER BY e.schemaname, e.tablename")
	}
}

// TC-BLOAT-07.
func TestBloatEstimateFormulaConstants(t *testing.T) {
	q := pgqueries.ByID("bloat_estimate_v1")
	if q == nil {
		t.Fatal("bloat_estimate_v1 not registered")
	}
	// TUPLE_HDR = 23, NULL_BMP = 4, ALIGN_PAD = 8, PAGE_HDR = 24.
	required := []string{"23", "4", "8", "24"}
	for _, c := range required {
		if !strings.Contains(q.SQL, c) {
			t.Errorf("SQL must reference formula constant %q", c)
		}
	}
	if !strings.Contains(q.SQL, "current_setting('block_size')") {
		t.Error("SQL must use current_setting('block_size') for the page size")
	}
}

// TC-BLOAT-08.
func TestBloatEstimateNoPgstattupleDependency(t *testing.T) {
	q := pgqueries.ByID("bloat_estimate_v1")
	if q == nil {
		t.Fatal("bloat_estimate_v1 not registered")
	}
	if q.RequiresExtension != "" {
		t.Errorf("must have no extension requirement, got %q", q.RequiresExtension)
	}
	sql := strings.ToLower(q.SQL)
	forbidden := []string{"pgstattuple", "pgstatindex", "pgstattuple_approx"}
	for _, ref := range forbidden {
		if strings.Contains(sql, ref) {
			t.Errorf("SQL must not reference %q — keep this collector extension-free", ref)
		}
	}
}

// TC-BLOAT-09.
func TestBloatEstimateIncludedOnAllSupportedMajors(t *testing.T) {
	for _, major := range []int{14, 15, 16, 17, 18} {
		filtered := pgqueries.Filter(pgqueries.FilterParams{
			PGMajorVersion: major,
			Extensions:     []string{},
		})
		found := false
		for _, q := range filtered {
			if q.ID == "bloat_estimate_v1" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("bloat_estimate_v1 must be included on PG %d", major)
		}
	}
}

// Sanity: the CTE structure (page / widths / base / estimated)
// must be present. Rewriting the SQL without these CTEs would
// re-inline the formula and is out of scope for v1.
func TestBloatEstimateUsesCTEs(t *testing.T) {
	q := pgqueries.ByID("bloat_estimate_v1")
	if q == nil {
		t.Fatal("bloat_estimate_v1 not registered")
	}
	for _, cte := range []string{"page AS", "widths AS", "base AS", "estimated AS"} {
		if !strings.Contains(q.SQL, cte) {
			t.Errorf("SQL must define the %q CTE", cte)
		}
	}
}
