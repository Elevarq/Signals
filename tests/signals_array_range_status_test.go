// Test for issue #18: pg_stats_array_range_v1 (and any future
// RequiresArrayRangeOptIn collector) must appear in
// collector_status.json as status=skipped, reason=config_disabled when
// CollectArrayRangeHistograms is false — same status-completeness
// guarantee EA-R001 already provides for version_unsupported,
// extension_missing, and config_disabled (R075).

package tests

import (
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// With high-sensitivity enabled (the new collect-everything default)
// but the array-range per-collector opt-in still off,
// pg_stats_array_range_v1 must surface as config_disabled rather than
// silently vanishing from the eligibility set.
func TestGatedIDsByReasonIncludesArrayRangeOptOut(t *testing.T) {
	gated := pgqueries.GatedIDsByReason(pgqueries.FilterParams{
		PGMajorVersion:              18,
		HighSensitivityEnabled:      true,
		CollectArrayRangeHistograms: false,
	})

	found := false
	for _, id := range gated[pgqueries.GateReasonConfigDisabled] {
		if id == "pg_stats_array_range_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("pg_stats_array_range_v1 must appear under %q when "+
			"CollectArrayRangeHistograms=false (EA-R001 status completeness, "+
			"specifications/collectors/pg_stats_array_range_v1.md). gated = %v",
			pgqueries.GateReasonConfigDisabled, gated)
	}
}

// Symmetric: when the array-range opt-in is true, the collector must
// not be reported as gated by config_disabled (regression guard so the
// new case doesn't fire when it shouldn't).
func TestGatedIDsByReasonExcludesArrayRangeWhenOptedIn(t *testing.T) {
	gated := pgqueries.GatedIDsByReason(pgqueries.FilterParams{
		PGMajorVersion:              18,
		HighSensitivityEnabled:      true,
		CollectArrayRangeHistograms: true,
	})
	for _, id := range gated[pgqueries.GateReasonConfigDisabled] {
		if id == "pg_stats_array_range_v1" {
			t.Errorf("pg_stats_array_range_v1 must NOT appear under config_disabled when the opt-in is true")
		}
	}
}
