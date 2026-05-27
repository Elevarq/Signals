package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// pg_stat_user_functions_v1 — per-function execution counters.
// Populated only when track_functions is 'pl' or 'all'.
//
// Specification: specifications/collectors/pg_stat_user_functions_v1.md
// Cross-cutter:  specifications/delta-semantics.md
// ---------------------------------------------------------------------------

func TestStatUserFunctionsCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_stat_user_functions_v1")
	if q == nil {
		t.Fatal("pg_stat_user_functions_v1 is not registered")
	}
	if q.Category != "functions" {
		t.Errorf("category: got %q, want %q", q.Category, "functions")
	}
}

func TestStatUserFunctionsCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_stat_user_functions_v1")
	if q == nil {
		t.Fatal("pg_stat_user_functions_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_stat_user_functions_v1 failed linter: %v", err)
	}
}

func TestStatUserFunctionsCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_stat_user_functions_v1")
	if q == nil {
		t.Fatal("pg_stat_user_functions_v1 not registered")
	}
	if q.Cadence != pgqueries.Cadence1h {
		t.Errorf("cadence: got %v, want Cadence1h", q.Cadence)
	}
}

func TestStatUserFunctionsCollectorRetention(t *testing.T) {
	q := pgqueries.ByID("pg_stat_user_functions_v1")
	if q == nil {
		t.Fatal("pg_stat_user_functions_v1 not registered")
	}
	if q.RetentionClass != pgqueries.RetentionMedium {
		t.Errorf("retention: got %q, want RetentionMedium", q.RetentionClass)
	}
}

func TestStatUserFunctionsCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_stat_user_functions_v1")
	if q == nil {
		t.Fatal("pg_stat_user_functions_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

func TestStatUserFunctionsCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_stat_user_functions_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_stat_user_functions_v1 must be included on PG 14")
	}
}

func TestStatUserFunctionsCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_stat_user_functions_v1")
	if q == nil {
		t.Fatal("pg_stat_user_functions_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_stat_user_functions_v1 must have ORDER BY for deterministic output")
	}
}

func TestStatUserFunctionsCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_stat_user_functions_v1")
	if q == nil {
		t.Fatal("pg_stat_user_functions_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_stat_user_functions_v1 must not use SELECT *")
	}
}

func TestStatUserFunctionsCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_stat_user_functions_v1")
	if q == nil {
		t.Fatal("pg_stat_user_functions_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"funcid", "schemaname", "funcname",
		"calls", "total_time", "self_time",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_stat_user_functions_v1 must include column %q", col)
		}
	}
}

func TestStatUserFunctionsCollectorUsesPgStatUserFunctions(t *testing.T) {
	q := pgqueries.ByID("pg_stat_user_functions_v1")
	if q == nil {
		t.Fatal("pg_stat_user_functions_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_stat_user_functions") {
		t.Error("pg_stat_user_functions_v1 must query pg_stat_user_functions")
	}
}

// Ordering must be by workload share (total_time) so top-N consumers see the
// most relevant rows first.
func TestStatUserFunctionsCollectorOrdersByTotalTime(t *testing.T) {
	q := pgqueries.ByID("pg_stat_user_functions_v1")
	if q == nil {
		t.Fatal("pg_stat_user_functions_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	orderIdx := strings.LastIndex(sql, "order by")
	if orderIdx < 0 {
		t.Fatal("missing ORDER BY")
	}
	orderClause := sql[orderIdx:]
	if !strings.Contains(orderClause, "total_time") {
		t.Error("pg_stat_user_functions_v1 must ORDER BY total_time for top-N semantics")
	}
}

// Cumulative semantics — raw values, no server-side deltas.
func TestStatUserFunctionsCollectorNoServerSideDelta(t *testing.T) {
	q := pgqueries.ByID("pg_stat_user_functions_v1")
	if q == nil {
		t.Fatal("pg_stat_user_functions_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	if strings.Contains(sql, "lag(") || strings.Contains(sql, "lag (") {
		t.Error("pg_stat_user_functions_v1 must not use LAG() — deltas computed analyzer-side")
	}
}
