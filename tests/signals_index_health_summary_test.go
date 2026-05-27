package tests

import (
	"strings"
	"testing"
	"time"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// index_health_summary_v1 — derived index hygiene findings.
//
// Specification: specifications/collectors/index_health_summary_v1.md
// Acceptance:    specifications/collectors/index_health_summary_v1.acceptance.md
// ---------------------------------------------------------------------------

// TC-IDXHEALTH-01.
func TestIndexHealthSummaryRegistered(t *testing.T) {
	q := pgqueries.ByID("index_health_summary_v1")
	if q == nil {
		t.Fatal("index_health_summary_v1 is not registered")
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
}

// TC-IDXHEALTH-02.
func TestIndexHealthSummaryPassesLinter(t *testing.T) {
	q := pgqueries.ByID("index_health_summary_v1")
	if q == nil {
		t.Fatal("index_health_summary_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("index_health_summary_v1 failed linter: %v", err)
	}
}

// TC-IDXHEALTH-03.
func TestIndexHealthSummaryExcludesSystemSchemas(t *testing.T) {
	q := pgqueries.ByID("index_health_summary_v1")
	if q == nil {
		t.Fatal("index_health_summary_v1 not registered")
	}
	if !strings.Contains(q.SQL, "'pg_catalog'") {
		t.Error("SQL must exclude pg_catalog schema")
	}
	if !strings.Contains(q.SQL, "'information_schema'") {
		t.Error("SQL must exclude information_schema")
	}
	if !strings.Contains(q.SQL, "'pg_toast'") {
		t.Error("SQL must exclude pg_toast schema")
	}
	if !strings.Contains(q.SQL, `'pg\_temp\_%'`) {
		t.Error("SQL must exclude pg_temp_* schemas via NOT LIKE")
	}
	if !strings.Contains(q.SQL, `'pg\_toast\_temp\_%'`) {
		t.Error("SQL must exclude pg_toast_temp_* schemas via NOT LIKE")
	}
}

// TC-IDXHEALTH-04.
func TestIndexHealthSummaryOutputColumns(t *testing.T) {
	q := pgqueries.ByID("index_health_summary_v1")
	if q == nil {
		t.Fatal("index_health_summary_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"schemaname", "tablename", "indexname", "index_oid",
		"size_bytes", "idx_scan", "idx_tup_read",
		"is_unique", "is_primary", "is_valid", "is_ready",
		"column_set", "duplicate_of", "redundant_with",
		"health_findings",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("SQL must reference column %q", col)
		}
	}
}

// TC-IDXHEALTH-05.
func TestIndexHealthSummaryNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("index_health_summary_v1")
	if q == nil {
		t.Fatal("index_health_summary_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("index_health_summary_v1 must not use SELECT *")
	}
}

// TC-IDXHEALTH-06.
func TestIndexHealthSummaryDeterministicOrder(t *testing.T) {
	q := pgqueries.ByID("index_health_summary_v1")
	if q == nil {
		t.Fatal("index_health_summary_v1 not registered")
	}
	// The outer query orders by (schemaname, tablename, indexname).
	if !strings.Contains(q.SQL, "ORDER BY m.schemaname, m.tablename, m.indexname") {
		t.Error("outer SELECT must ORDER BY m.schemaname, m.tablename, m.indexname")
	}
}

// TC-IDXHEALTH-07.
func TestIndexHealthSummaryClassificationTagsPresent(t *testing.T) {
	q := pgqueries.ByID("index_health_summary_v1")
	if q == nil {
		t.Fatal("index_health_summary_v1 not registered")
	}
	required := []string{
		"'unused'", "'large_unused'", "'invalid'", "'not_ready'",
		"'redundant'", "'duplicate'",
	}
	for _, tag := range required {
		if !strings.Contains(q.SQL, tag) {
			t.Errorf("SQL must include classification tag literal %s", tag)
		}
	}
}

// TC-IDXHEALTH-08: included on every supported major.
func TestIndexHealthSummaryIncludedOnAllMajors(t *testing.T) {
	for _, major := range []int{14, 15, 16, 17, 18} {
		filtered := pgqueries.Filter(pgqueries.FilterParams{
			PGMajorVersion: major,
			Extensions:     []string{},
		})
		found := false
		for _, q := range filtered {
			if q.ID == "index_health_summary_v1" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("index_health_summary_v1 must be included on PG %d", major)
		}
	}
}

// Sanity: the WITH-clause CTE structure (idx_cols / idx_meta) must
// be present — this is the design baseline; rewriting the SQL
// without these CTEs would re-derive duplicate/redundant logic
// inline and is out of scope for v1.
func TestIndexHealthSummaryUsesCTEs(t *testing.T) {
	q := pgqueries.ByID("index_health_summary_v1")
	if q == nil {
		t.Fatal("index_health_summary_v1 not registered")
	}
	if !strings.Contains(q.SQL, "idx_cols AS") {
		t.Error("SQL must define the idx_cols CTE for column-name resolution")
	}
	if !strings.Contains(q.SQL, "idx_meta AS") {
		t.Error("SQL must define the idx_meta CTE for the per-index baseline row")
	}
}
