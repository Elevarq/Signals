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

func TestFilterDropsHighSensitivityWhenOptedOut(t *testing.T) {
	livesensIDs := map[string]bool{
		"long_running_txns_v1":     true,
		"blocking_locks_v1":        true,
		"idle_in_txn_offenders_v1": true,
		"wraparound_blockers_v1":   true,
	}

	hasAll := func(qs []pgqueries.QueryDef) bool {
		got := map[string]bool{}
		for _, q := range qs {
			got[q.ID] = true
		}
		for id := range livesensIDs {
			if !got[id] {
				return false
			}
		}
		return true
	}

	// PG 18 + no extensions: the four collectors have no extension
	// dependency and no MinPGVersion above 18, so eligibility is gated
	// only by HighSensitivityEnabled.
	on := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion:         18,
		HighSensitivityEnabled: true,
	})
	if !hasAll(on) {
		t.Errorf("with HighSensitivityEnabled=true, all four live-sensitive collectors must be eligible")
	}

	off := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion:         18,
		HighSensitivityEnabled: false,
	})
	for _, q := range off {
		if livesensIDs[q.ID] {
			t.Errorf("with HighSensitivityEnabled=false, %s leaked through the gate — opt-out must drop high-sensitivity collectors entirely", q.ID)
		}
	}
}
