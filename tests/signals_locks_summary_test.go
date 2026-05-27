package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// pg_locks_summary_v1 — aggregated (locktype, mode, granted) counts with
// max wait duration for waiting rows. Exposes lock pressure without
// per-relation or per-tuple identity.
//
// Specification: specifications/collectors/pg_locks_summary_v1.md
// ---------------------------------------------------------------------------

func TestLocksSummaryCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_locks_summary_v1")
	if q == nil {
		t.Fatal("pg_locks_summary_v1 is not registered")
	}
	if q.Category != "activity" {
		t.Errorf("category: got %q, want %q", q.Category, "activity")
	}
}

func TestLocksSummaryCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_locks_summary_v1")
	if q == nil {
		t.Fatal("pg_locks_summary_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_locks_summary_v1 failed linter: %v", err)
	}
}

func TestLocksSummaryCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_locks_summary_v1")
	if q == nil {
		t.Fatal("pg_locks_summary_v1 not registered")
	}
	if q.Cadence != pgqueries.Cadence5m {
		t.Errorf("cadence: got %v, want Cadence5m", q.Cadence)
	}
}

func TestLocksSummaryCollectorRetention(t *testing.T) {
	q := pgqueries.ByID("pg_locks_summary_v1")
	if q == nil {
		t.Fatal("pg_locks_summary_v1 not registered")
	}
	if q.RetentionClass != pgqueries.RetentionShort {
		t.Errorf("retention: got %q, want RetentionShort", q.RetentionClass)
	}
}

func TestLocksSummaryCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_locks_summary_v1")
	if q == nil {
		t.Fatal("pg_locks_summary_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

func TestLocksSummaryCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_locks_summary_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_locks_summary_v1 must be included on PG 14")
	}
}

func TestLocksSummaryCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_locks_summary_v1")
	if q == nil {
		t.Fatal("pg_locks_summary_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_locks_summary_v1 must have ORDER BY for deterministic output")
	}
}

func TestLocksSummaryCollectorUsesPgLocks(t *testing.T) {
	q := pgqueries.ByID("pg_locks_summary_v1")
	if q == nil {
		t.Fatal("pg_locks_summary_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_locks") {
		t.Error("pg_locks_summary_v1 must query pg_locks")
	}
}

// Aggregation by (locktype, mode, granted) is the spec's core invariant.
func TestLocksSummaryCollectorGroupsByLockKeyTuple(t *testing.T) {
	q := pgqueries.ByID("pg_locks_summary_v1")
	if q == nil {
		t.Fatal("pg_locks_summary_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	if !strings.Contains(sql, "group by") {
		t.Fatal("pg_locks_summary_v1 must aggregate — expected GROUP BY clause")
	}
	// All three grouping keys must appear in the GROUP BY area of the SQL.
	groupIdx := strings.LastIndex(sql, "group by")
	after := sql[groupIdx:]
	for _, k := range []string{"locktype", "mode", "granted"} {
		if !strings.Contains(after, k) {
			t.Errorf("pg_locks_summary_v1 GROUP BY must include %q", k)
		}
	}
}

func TestLocksSummaryCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_locks_summary_v1")
	if q == nil {
		t.Fatal("pg_locks_summary_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"locktype", "mode", "granted",
		"count", "max_wait_seconds", "distinct_pids",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_locks_summary_v1 must include column %q", col)
		}
	}
}

// Privacy invariant: no per-relation OIDs, no tuple identifiers, no
// transaction IDs in the output.
func TestLocksSummaryCollectorNoPerRelationIdentity(t *testing.T) {
	q := pgqueries.ByID("pg_locks_summary_v1")
	if q == nil {
		t.Fatal("pg_locks_summary_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	// The SQL may reference these for FILTER-ing but must not emit them
	// as output columns (i.e. "AS relation" or "AS tuple" etc.).
	for _, leak := range []string{"as relation", "as tuple", "as transactionid", "as virtualxid"} {
		if strings.Contains(sql, leak) {
			t.Errorf("pg_locks_summary_v1 must not expose %q as an output column — aggregated-only", leak)
		}
	}
}

// Excludes the collector's own backend to avoid self-counting.
func TestLocksSummaryCollectorExcludesSelf(t *testing.T) {
	q := pgqueries.ByID("pg_locks_summary_v1")
	if q == nil {
		t.Fatal("pg_locks_summary_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_backend_pid") {
		t.Error("pg_locks_summary_v1 must exclude its own backend via pg_backend_pid()")
	}
}
