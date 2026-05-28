// Tests for R075 (revised 2026-05, issue #6): high-sensitivity
// collectors are default-on with an opt-out, and the live
// pg_stat_activity query-text collectors are properly classified
// HighSensitivity=true.

package tests

import (
	"testing"

	"github.com/elevarq/arq-signals/internal/config"
	"github.com/elevarq/arq-signals/internal/pgqueries"
)

func TestDefaultConfigHighSensitivityIsDefaultOn(t *testing.T) {
	cfg := config.DefaultConfig()
	if !cfg.Signals.HighSensitivityCollectorsEnabled {
		t.Fatal("DefaultConfig().Signals.HighSensitivityCollectorsEnabled = false, want true " +
			"(R075 revised 2026-05: high-sensitivity collectors run by default, opt-out skips them)")
	}
}

func TestLiveQueryTextCollectorsAreHighSensitivity(t *testing.T) {
	ids := []string{
		"long_running_txns_v1",
		"blocking_locks_v1",
		"idle_in_txn_offenders_v1",
		"wraparound_blockers_v1",
	}
	for _, id := range ids {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Errorf("%s not registered", id)
			continue
		}
		if !q.HighSensitivity {
			t.Errorf("%s.HighSensitivity = false, want true — it emits live pg_stat_activity query text and must be classified per R075", id)
		}
	}
}

// Note: PR #13's TestFilterDropsHighSensitivityWhenOptedOut asserted
// the four live-sensitive collectors were dropped when opted out. That
// behavior was corrected by issue #6 v2 (redact path): the collectors
// now stay eligible when opted out and have their sensitive columns
// nulled at execution time. The replacement coverage lives in
// tests/signals_redact_columns_test.go.
