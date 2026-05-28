package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/db"
	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// R075: High-sensitivity collectors disabled by default (TC-SIG-062)
// ---------------------------------------------------------------------------

var highSensitivityCollectors = []string{
	"pg_views_definitions_v1",
	"pg_matviews_definitions_v1",
	"pg_triggers_definitions_v1",
	"pg_functions_definitions_v1",
	// pg_stats_extended_v1 emits real customer data values
	// (most_common_vals, histogram_bounds) — gated under the
	// same R075 surface as the application-authored-SQL
	// collectors above.
	"pg_stats_extended_v1",
	// pg_stats_array_range_v1 (#128) emits per-element MCV +
	// range histograms — most sensitive of the pg_stats family.
	// Carries its own per-collector flag (CollectArrayRangeHistograms)
	// in addition to the daemon-wide HighSensitivityEnabled.
	"pg_stats_array_range_v1",
	// pg_statistic_ext_data_mcv_v1 (#171) emits the byte-encoded
	// multivariate MCV blob from pg_statistic_ext_data — the only
	// multivariate-stats kind that carries actual sampled column
	// values. Gated under the same R075 surface as the per-column
	// MCV / histogram collectors.
	"pg_statistic_ext_data_mcv_v1",
	// pg_policies_v1 (#214) emits RLS policy qual / with_check
	// expressions — arbitrary SQL of the same class as the
	// definition collectors above.
	"pg_policies_v1",
	// pg_rules_v1 (#219) emits the rewrite-rule action — arbitrary SQL.
	"pg_rules_v1",
	// NOTE: the 4 live pg_stat_activity collectors (long_running_txns_v1,
	// blocking_locks_v1, idle_in_txn_offenders_v1, wraparound_blockers_v1)
	// are HighSensitivity = true but use the **redact** path (R075 revised
	// 2026-05, issue #6) — they stay eligible when opted out and have their
	// sensitive columns nulled at execution time. They are covered by
	// tests/signals_redact_columns_test.go, not this skip-path list.
}

// TestHighSensitivityCollectorsAreFlagged verifies all four definition
// collectors carry the HighSensitivity flag in the registry.
// Traces: ARQ-SIGNALS-R075
func TestHighSensitivityCollectorsAreFlagged(t *testing.T) {
	for _, id := range highSensitivityCollectors {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Fatalf("%s is not registered", id)
		}
		if !q.HighSensitivity {
			t.Errorf("%s should be flagged HighSensitivity", id)
		}
	}
}

// TestFilterExcludesSkipPathHighSensitivityWhenDisabled verifies that
// when high-sensitivity is opted out, Filter drops the skip-path
// high-sensitivity collectors (whole row is the sensitive payload, no
// SensitiveColumns declared). Redact-path collectors (non-empty
// SensitiveColumns) keep running and have their sensitive columns
// nulled at execution time — covered by tests/signals_redact_columns_test.go.
// Traces: ARQ-SIGNALS-R075 (revised 2026-05, issue #6).
func TestFilterExcludesSkipPathHighSensitivityWhenDisabled(t *testing.T) {
	out := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion:         16,
		HighSensitivityEnabled: false,
	})
	for _, q := range out {
		if q.HighSensitivity && len(q.SensitiveColumns) == 0 {
			t.Errorf("%s is a skip-path high-sensitivity collector but Filter kept it when opted out", q.ID)
		}
	}
}

// TestFilterIncludesHighSensitivityWhenEnabled verifies the opt-in path:
// when the operator enables high-sensitivity collection, all four
// definition queries appear. pg_stats_array_range_v1 (#128) requires
// an additional per-collector flag — both gates set true here so the
// drift-against-highSensitivityCollectors check stays meaningful.
// Traces: ARQ-SIGNALS-R075
func TestFilterIncludesHighSensitivityWhenEnabled(t *testing.T) {
	out := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion:              16,
		HighSensitivityEnabled:      true,
		CollectArrayRangeHistograms: true,
	})
	present := make(map[string]bool, len(out))
	for _, q := range out {
		present[q.ID] = true
	}
	for _, id := range highSensitivityCollectors {
		if !present[id] {
			t.Errorf("%s should be included when HighSensitivityEnabled=true", id)
		}
	}
}

// TestHighSensitivityIDsReturnsGatedSet verifies HighSensitivityIDs returns
// the IDs that would be skipped under the supplied filter parameters.
// Traces: ARQ-SIGNALS-R075
func TestHighSensitivityIDsReturnsGatedSet(t *testing.T) {
	gated := pgqueries.HighSensitivityIDs(pgqueries.FilterParams{
		PGMajorVersion:         16,
		HighSensitivityEnabled: false,
	})
	want := map[string]bool{}
	for _, id := range highSensitivityCollectors {
		want[id] = true
	}
	if len(gated) != len(want) {
		t.Errorf("gated count = %d, want %d (%v)", len(gated), len(want), highSensitivityCollectors)
	}
	for _, id := range gated {
		if !want[id] {
			t.Errorf("unexpected gated ID %q", id)
		}
	}
}

// TestHighSensitivityIDsRespectsMinPGVersion verifies that an old PG that
// does not meet pg_functions_definitions_v1's MinPGVersion=11 doesn't
// appear in the gated set — gating only applies to collectors that would
// otherwise be eligible.
// Traces: ARQ-SIGNALS-R075
func TestHighSensitivityIDsRespectsMinPGVersion(t *testing.T) {
	gated := pgqueries.HighSensitivityIDs(pgqueries.FilterParams{
		PGMajorVersion:         10,
		HighSensitivityEnabled: false,
	})
	for _, id := range gated {
		if id == "pg_functions_definitions_v1" {
			t.Error("pg_functions_definitions_v1 needs PG11; should not appear as gated on PG10")
		}
	}
}

// TestHighSensitivityIDsEmptyWhenEnabled verifies that opting in returns
// no gated IDs (nothing to mark skipped).
// Traces: ARQ-SIGNALS-R075
func TestHighSensitivityIDsEmptyWhenEnabled(t *testing.T) {
	gated := pgqueries.HighSensitivityIDs(pgqueries.FilterParams{
		PGMajorVersion:         16,
		HighSensitivityEnabled: true,
	})
	if len(gated) != 0 {
		t.Errorf("HighSensitivityIDs should be empty when enabled; got %v", gated)
	}
}

// ---------------------------------------------------------------------------
// R072 + R075: BuildStatusFromRuns translates skipped runs to skipped
// status entries with reason=config_disabled.
// ---------------------------------------------------------------------------

// TestBuildStatusFromRunsRendersSkipped verifies a query_run row with
// status=skipped and reason=config_disabled produces a CollectorStatus
// with Attempted=false and the reason preserved.
// Traces: ARQ-SIGNALS-R072 / ARQ-SIGNALS-R075
func TestBuildStatusFromRunsRendersSkipped(t *testing.T) {
	runs := []db.QueryRun{
		{
			QueryID: "pg_views_definitions_v1",
			Status:  "skipped",
			Reason:  "config_disabled",
		},
		{
			QueryID:     "pg_stat_database_v1",
			Status:      "success",
			RowCount:    7,
			DurationMS:  12,
			CollectedAt: "2026-04-24T08:00:00Z",
		},
		{
			QueryID:     "pg_stat_user_tables_v1",
			Status:      "failed",
			Reason:      "permission_denied",
			Error:       "ERROR: permission denied for table mytable",
			DurationMS:  5,
			CollectedAt: "2026-04-24T08:00:00Z",
		},
	}

	statuses := collector.BuildStatusFromRuns(runs)
	byID := make(map[string]collector.CollectorStatus, len(statuses))
	for _, s := range statuses {
		byID[s.ID] = s
	}

	skipped, ok := byID["pg_views_definitions_v1"]
	if !ok {
		t.Fatal("expected status for pg_views_definitions_v1")
	}
	if skipped.Status != "skipped" {
		t.Errorf("Status = %q, want skipped", skipped.Status)
	}
	if skipped.Reason != "config_disabled" {
		t.Errorf("Reason = %q, want config_disabled", skipped.Reason)
	}
	if skipped.Attempted {
		t.Error("skipped Attempted should be false")
	}
	if skipped.DurationMS != 0 || skipped.CollectedAt != "" {
		t.Error("skipped status should not carry duration/collected_at")
	}

	if byID["pg_stat_database_v1"].Status != "success" {
		t.Error("success run did not render as success")
	}
	failed := byID["pg_stat_user_tables_v1"]
	if failed.Status != "failed" {
		t.Errorf("failed run rendered as %q", failed.Status)
	}
	if !strings.Contains(failed.Reason, "permission_denied") {
		t.Errorf("failed reason = %q, want permission_denied", failed.Reason)
	}
}

// TestBuildStatusFromRunsBackfillsLegacyRows verifies that pre-migration
// rows (Status="" but Error set) still classify correctly. Guards against
// regressions when reading older databases.
// Traces: ARQ-SIGNALS-R072
func TestBuildStatusFromRunsBackfillsLegacyRows(t *testing.T) {
	runs := []db.QueryRun{
		{QueryID: "old_success", Error: "", RowCount: 3},
		{QueryID: "old_failure", Error: "ERROR: timeout"},
	}
	statuses := collector.BuildStatusFromRuns(runs)
	byID := make(map[string]string, len(statuses))
	for _, s := range statuses {
		byID[s.ID] = s.Status
	}
	if byID["old_success"] != "success" {
		t.Errorf("legacy success row classified as %q", byID["old_success"])
	}
	if byID["old_failure"] != "failed" {
		t.Errorf("legacy failure row classified as %q", byID["old_failure"])
	}
}
