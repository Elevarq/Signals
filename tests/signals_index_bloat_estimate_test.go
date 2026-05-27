package tests

import (
	"strings"
	"testing"
	"time"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// index_bloat_estimate_v1 — statistical index-bloat estimate
// (sibling of bloat_estimate_v1, no pgstattuple required).
//
// Specification: specifications/collectors/index_bloat_estimate_v1.md
// Acceptance:    specifications/collectors/index_bloat_estimate_v1.acceptance.md
// ---------------------------------------------------------------------------

// TC-IDXBLOAT-01.
func TestIndexBloatEstimateRegistered(t *testing.T) {
	q := pgqueries.ByID("index_bloat_estimate_v1")
	if q == nil {
		t.Fatal("index_bloat_estimate_v1 is not registered")
	}
	if q.Category != "indexes" {
		t.Errorf("category: got %q, want %q", q.Category, "indexes")
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

// TC-IDXBLOAT-02.
func TestIndexBloatEstimatePassesLinter(t *testing.T) {
	q := pgqueries.ByID("index_bloat_estimate_v1")
	if q == nil {
		t.Fatal("index_bloat_estimate_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("index_bloat_estimate_v1 failed linter: %v", err)
	}
}

// TC-IDXBLOAT-03.
func TestIndexBloatEstimateScopeFilters(t *testing.T) {
	q := pgqueries.ByID("index_bloat_estimate_v1")
	if q == nil {
		t.Fatal("index_bloat_estimate_v1 not registered")
	}
	if !strings.Contains(q.SQL, "relkind IN ('i', 'I')") {
		t.Error("SQL must filter relkind IN ('i', 'I')")
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

// TC-IDXBLOAT-04.
func TestIndexBloatEstimateOutputColumns(t *testing.T) {
	q := pgqueries.ByID("index_bloat_estimate_v1")
	if q == nil {
		t.Fatal("index_bloat_estimate_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"schemaname", "tablename", "indexname", "index_oid",
		"relkind",
		"actual_size_bytes", "expected_size_bytes",
		"bloat_bytes", "bloat_ratio",
		"reltuples", "is_unique", "is_primary",
		"stats_missing",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("SQL must reference column %q", col)
		}
	}
}

// TC-IDXBLOAT-05.
func TestIndexBloatEstimateNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("index_bloat_estimate_v1")
	if q == nil {
		t.Fatal("index_bloat_estimate_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("index_bloat_estimate_v1 must not use SELECT *")
	}
}

// TC-IDXBLOAT-06.
func TestIndexBloatEstimateDeterministicOrder(t *testing.T) {
	q := pgqueries.ByID("index_bloat_estimate_v1")
	if q == nil {
		t.Fatal("index_bloat_estimate_v1 not registered")
	}
	if !strings.Contains(q.SQL, "ORDER BY e.schemaname, e.tablename, e.indexname") {
		t.Error("outer SELECT must ORDER BY e.schemaname, e.tablename, e.indexname")
	}
}

// TC-IDXBLOAT-07.
func TestIndexBloatEstimateFormulaConstants(t *testing.T) {
	q := pgqueries.ByID("index_bloat_estimate_v1")
	if q == nil {
		t.Fatal("index_bloat_estimate_v1 not registered")
	}
	// INDEX_TUPLE_HDR=8, ITEM_PTR=4, PAGE_HDR=24.
	for _, c := range []string{"8", "4", "24"} {
		if !strings.Contains(q.SQL, c) {
			t.Errorf("SQL must reference formula constant %q", c)
		}
	}
	if !strings.Contains(q.SQL, "current_setting('block_size')") {
		t.Error("SQL must use current_setting('block_size') for the page size")
	}
}

// TC-IDXBLOAT-08.
func TestIndexBloatEstimateNoExtensionDependency(t *testing.T) {
	q := pgqueries.ByID("index_bloat_estimate_v1")
	if q == nil {
		t.Fatal("index_bloat_estimate_v1 not registered")
	}
	if q.RequiresExtension != "" {
		t.Errorf("must have no extension requirement, got %q", q.RequiresExtension)
	}
	sql := strings.ToLower(q.SQL)
	for _, ref := range []string{"pgstattuple", "pgstatindex", "pgstattuple_approx"} {
		if strings.Contains(sql, ref) {
			t.Errorf("SQL must not reference %q — keep this collector extension-free", ref)
		}
	}
}

// TC-IDXBLOAT-09.
func TestIndexBloatEstimateIncludedOnAllSupportedMajors(t *testing.T) {
	for _, major := range []int{14, 15, 16, 17, 18} {
		filtered := pgqueries.Filter(pgqueries.FilterParams{
			PGMajorVersion: major,
			Extensions:     []string{},
		})
		found := false
		for _, q := range filtered {
			if q.ID == "index_bloat_estimate_v1" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("index_bloat_estimate_v1 must be included on PG %d", major)
		}
	}
}

// TC-IDXBLOAT-10: width sum bounded by indnkeyatts (INCLUDE
// columns skipped) so the formula matches the PG-wiki convention.
func TestIndexBloatEstimateWidthSumBoundedByKeyAtts(t *testing.T) {
	q := pgqueries.ByID("index_bloat_estimate_v1")
	if q == nil {
		t.Fatal("index_bloat_estimate_v1 not registered")
	}
	if !strings.Contains(q.SQL, "pos.ord <= i.indnkeyatts") {
		t.Error("SQL must bound width sum by indnkeyatts (skip INCLUDE columns)")
	}
}

// Sanity: the CTE structure (page / idx_widths / base / estimated)
// must be present.
func TestIndexBloatEstimateUsesCTEs(t *testing.T) {
	q := pgqueries.ByID("index_bloat_estimate_v1")
	if q == nil {
		t.Fatal("index_bloat_estimate_v1 not registered")
	}
	for _, cte := range []string{"page AS", "idx_widths AS", "base AS", "estimated AS"} {
		if !strings.Contains(q.SQL, cte) {
			t.Errorf("SQL must define the %q CTE", cte)
		}
	}
}
