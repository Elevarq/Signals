package tests

import (
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// #128 — pg_stats_array_range_v1 is in the closed
// RequiresArrayRangeOptIn map. Drift gate: if the map changes or
// the collector is renamed, this test must fire.
func TestArrayRangeCollector_RegisteredInOptInMap(t *testing.T) {
	if !pgqueries.RequiresArrayRangeOptIn["pg_stats_array_range_v1"] {
		t.Errorf("pg_stats_array_range_v1 missing from RequiresArrayRangeOptIn")
	}
}

// #128 — both gates required. The collector must be SKIPPED when
// EITHER HighSensitivityEnabled or CollectArrayRangeHistograms is
// false; only the (true, true) combination admits it.
func TestArrayRangeCollector_TwoGateOptIn(t *testing.T) {
	type tc struct {
		highSens    bool
		arrayRange  bool
		wantInclude bool
	}
	cases := []tc{
		{false, false, false},
		{false, true, false},
		{true, false, false},
		{true, true, true},
	}
	for _, c := range cases {
		out := pgqueries.Filter(pgqueries.FilterParams{
			PGMajorVersion:              16,
			HighSensitivityEnabled:      c.highSens,
			CollectArrayRangeHistograms: c.arrayRange,
		})
		present := false
		for _, q := range out {
			if q.ID == "pg_stats_array_range_v1" {
				present = true
				break
			}
		}
		if present != c.wantInclude {
			t.Errorf("Filter(highSens=%v, arrayRange=%v): pg_stats_array_range_v1 present=%v, want %v",
				c.highSens, c.arrayRange, present, c.wantInclude)
		}
	}
}

// #128 / MinPGVersion=14 — the range_*_histogram columns are PG 14+;
// the collector MUST be excluded on PG 13 even with both gates open.
func TestArrayRangeCollector_PG13Excluded(t *testing.T) {
	out := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion:              13,
		HighSensitivityEnabled:      true,
		CollectArrayRangeHistograms: true,
	})
	for _, q := range out {
		if q.ID == "pg_stats_array_range_v1" {
			t.Errorf("pg_stats_array_range_v1 should not be eligible on PG 13 (MinPGVersion=14)")
		}
	}
}

// #128 / INV-SENS-01 — CollectArrayRangeHistograms cannot WIDEN
// eligibility. With HighSensitivityEnabled=false, setting
// CollectArrayRangeHistograms=true must still skip the collector.
// Pinning this property prevents a future refactor from
// accidentally letting the per-collector flag bypass the
// daemon-wide safety floor.
func TestArrayRangeCollector_PerCollectorFlagCannotWidenEligibility(t *testing.T) {
	out := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion:              16,
		HighSensitivityEnabled:      false,
		CollectArrayRangeHistograms: true,
	})
	for _, q := range out {
		if q.ID == "pg_stats_array_range_v1" {
			t.Errorf("INV-SENS-01 violated — per-collector flag widened eligibility past HighSensitivityEnabled=false")
		}
	}
}
