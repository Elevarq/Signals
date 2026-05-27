package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// pg_stat_activity_summary_v1 — aggregated session-state counts and age
// distributions. Deliberately emits no query text, user names, client
// addresses, or session PIDs.
//
// Specification: specifications/collectors/pg_stat_activity_summary_v1.md
// ---------------------------------------------------------------------------

func TestStatActivitySummaryCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_stat_activity_summary_v1")
	if q == nil {
		t.Fatal("pg_stat_activity_summary_v1 is not registered")
	}
	if q.Category != "activity" {
		t.Errorf("category: got %q, want %q", q.Category, "activity")
	}
}

func TestStatActivitySummaryCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_stat_activity_summary_v1")
	if q == nil {
		t.Fatal("pg_stat_activity_summary_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_stat_activity_summary_v1 failed linter: %v", err)
	}
}

func TestStatActivitySummaryCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_stat_activity_summary_v1")
	if q == nil {
		t.Fatal("pg_stat_activity_summary_v1 not registered")
	}
	if q.Cadence != pgqueries.Cadence5m {
		t.Errorf("cadence: got %v, want Cadence5m", q.Cadence)
	}
}

func TestStatActivitySummaryCollectorRetention(t *testing.T) {
	q := pgqueries.ByID("pg_stat_activity_summary_v1")
	if q == nil {
		t.Fatal("pg_stat_activity_summary_v1 not registered")
	}
	if q.RetentionClass != pgqueries.RetentionShort {
		t.Errorf("retention: got %q, want RetentionShort", q.RetentionClass)
	}
}

func TestStatActivitySummaryCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_stat_activity_summary_v1")
	if q == nil {
		t.Fatal("pg_stat_activity_summary_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultScalar {
		t.Errorf("ResultKind: got %q, want scalar (single aggregated row)", q.ResultKind)
	}
}

func TestStatActivitySummaryCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_stat_activity_summary_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_stat_activity_summary_v1 must be included on PG 14")
	}
}

func TestStatActivitySummaryCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_stat_activity_summary_v1")
	if q == nil {
		t.Fatal("pg_stat_activity_summary_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"total_backends",
		"active_count", "idle_count",
		"idle_in_transaction_count", "idle_in_transaction_aborted_count",
		"fastpath_count", "disabled_count", "waiting_count",
		"oldest_xact_age_seconds", "oldest_query_age_seconds",
		"oldest_backend_xmin_age_xids",
		"active_gt_1min", "active_gt_5min", "active_gt_1h",
		"long_idle_in_txn_count",
		"by_backend_type", "by_wait_event_type",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_stat_activity_summary_v1 must include column %q", col)
		}
	}
}

func TestStatActivitySummaryCollectorUsesPgStatActivity(t *testing.T) {
	q := pgqueries.ByID("pg_stat_activity_summary_v1")
	if q == nil {
		t.Fatal("pg_stat_activity_summary_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_stat_activity") {
		t.Error("pg_stat_activity_summary_v1 must query pg_stat_activity")
	}
}

// Privacy invariant: the aggregated row must not expose per-session
// identity — no pid / usename / application_name / client_addr / query
// output columns.
func TestStatActivitySummaryCollectorNoPerSessionIdentity(t *testing.T) {
	q := pgqueries.ByID("pg_stat_activity_summary_v1")
	if q == nil {
		t.Fatal("pg_stat_activity_summary_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, leak := range []string{
		"as pid", "as usename", "as application_name",
		"as client_addr", "as query",
	} {
		if strings.Contains(sql, leak) {
			t.Errorf("pg_stat_activity_summary_v1 must not emit per-session identifier %q", leak)
		}
	}
}

// Excludes the collector's own backend to avoid self-counting.
func TestStatActivitySummaryCollectorExcludesSelf(t *testing.T) {
	q := pgqueries.ByID("pg_stat_activity_summary_v1")
	if q == nil {
		t.Fatal("pg_stat_activity_summary_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_backend_pid") {
		t.Error("pg_stat_activity_summary_v1 must exclude its own backend via pg_backend_pid()")
	}
}

// by_backend_type / by_wait_event_type must be JSON objects.
func TestStatActivitySummaryCollectorEmitsJSONAggregates(t *testing.T) {
	q := pgqueries.ByID("pg_stat_activity_summary_v1")
	if q == nil {
		t.Fatal("pg_stat_activity_summary_v1 not registered")
	}
	if !containsCI(q.SQL, "jsonb_object_agg") {
		t.Error("pg_stat_activity_summary_v1 must use jsonb_object_agg for by_backend_type / by_wait_event_type")
	}
}
