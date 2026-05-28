// Tests for R075 (revised v2, issue #6): the four live pg_stat_activity
// collectors carry SensitiveColumns and stay eligible when opted out
// (redact path); the DDL-definition collectors keep their skip-on-opt-out
// behavior (no SensitiveColumns).

package tests

import (
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// The 4 live-query collectors must declare which columns to redact.
func TestLiveQueryCollectorsHaveSensitiveColumns(t *testing.T) {
	want := map[string][]string{
		"long_running_txns_v1":     {"query_snippet"},
		"blocking_locks_v1":        {"blocked_query", "blocking_query"},
		"idle_in_txn_offenders_v1": {"query_snippet"},
		"wraparound_blockers_v1":   {"query_snippet"},
	}
	for id, expected := range want {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Errorf("%s not registered", id)
			continue
		}
		if !q.HighSensitivity {
			t.Errorf("%s.HighSensitivity should be true", id)
		}
		if len(q.SensitiveColumns) != len(expected) {
			t.Errorf("%s.SensitiveColumns = %v, want %v", id, q.SensitiveColumns, expected)
			continue
		}
		got := map[string]bool{}
		for _, c := range q.SensitiveColumns {
			got[c] = true
		}
		for _, c := range expected {
			if !got[c] {
				t.Errorf("%s.SensitiveColumns missing %q (got %v)", id, c, q.SensitiveColumns)
			}
		}
	}
}

// When opted out, the redact-path collectors STAY eligible (Filter does
// not drop them) — they will run and have their sensitive columns
// zeroed at execution time. The skip-path collectors (DDL definitions)
// are still dropped.
func TestFilterKeepsRedactPathCollectorsWhenOptedOut(t *testing.T) {
	off := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion:         18,
		HighSensitivityEnabled: false,
	})
	got := map[string]bool{}
	for _, q := range off {
		got[q.ID] = true
	}

	mustBeEligible := []string{
		"long_running_txns_v1",
		"blocking_locks_v1",
		"idle_in_txn_offenders_v1",
		"wraparound_blockers_v1",
	}
	for _, id := range mustBeEligible {
		if !got[id] {
			t.Errorf("%s must stay eligible when opted out (redact path) — Filter dropped it", id)
		}
	}

	mustBeDropped := []string{
		"pg_views_definitions_v1",
		"pg_matviews_definitions_v1",
		"pg_triggers_definitions_v1",
		"pg_functions_definitions_v1",
	}
	for _, id := range mustBeDropped {
		if got[id] {
			t.Errorf("%s must be dropped when opted out (skip path) — Filter kept it", id)
		}
	}
}

// HighSensitivityIDs (the gated set EA-R001 writes as skipped) must
// include only the skip-path collectors, not the redact-path ones.
func TestHighSensitivityIDsExcludesRedactPath(t *testing.T) {
	gated := pgqueries.HighSensitivityIDs(pgqueries.FilterParams{
		PGMajorVersion:         18,
		HighSensitivityEnabled: false,
	})
	g := map[string]bool{}
	for _, id := range gated {
		g[id] = true
	}
	redactPath := []string{
		"long_running_txns_v1",
		"blocking_locks_v1",
		"idle_in_txn_offenders_v1",
		"wraparound_blockers_v1",
	}
	for _, id := range redactPath {
		if g[id] {
			t.Errorf("%s appears in HighSensitivityIDs but it is a redact-path collector — it must not be recorded as skipped/config_disabled", id)
		}
	}
}
