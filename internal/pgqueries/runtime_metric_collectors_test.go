// Copyright (c) 2026 Scantr LLC. All rights reserved.
// Elevarq is a trade name of Scantr LLC.
// This file is part of Elevarq Signals. Use is governed by the
// commercial license at LICENSE in the repository root.

package pgqueries

import "testing"

// TestRuntimeMetricCollectors_InDefaultPack (#459) pins that the two
// runtime-metric collectors feeding the Analyzer rules that shipped dark
// (Elevarq/Analyzer#3203) ARE selected into the default collector pack, so
// they cannot silently drop out of a standard cycle:
//   - temp_io_pressure_v1 (pg_stat_database temp_files/temp_bytes; no gate) —
//     every supported major, feeds query.temp_spill_hotspot.v1 /
//     workload.work_mem_pressure.v1.
//   - pg_stat_io_v1 (pg_stat_io; MinPGVersion 16) — PG16+ only, feeds
//     io.pg_stat_io_pressure.v1 / io.cost.calibration.v2.
//
// The version gate is asserted both ways: absence from a cycle is a stale
// snapshot or a PG<16 target, never a silently-dropped collector.
func TestRuntimeMetricCollectors_InDefaultPack(t *testing.T) {
	inPack := func(major int) map[string]bool {
		ids := map[string]bool{}
		for _, q := range Filter(FilterParams{PGMajorVersion: major}) {
			ids[q.ID] = true
		}
		return ids
	}

	for _, major := range []int{14, 15, 16, 17, 18} {
		ids := inPack(major)
		if !ids["temp_io_pressure_v1"] {
			t.Errorf("PG%d: temp_io_pressure_v1 missing from default pack (no version gate — must always be eligible)", major)
		}
		wantIO := major >= 16
		if got := ids["pg_stat_io_v1"]; got != wantIO {
			t.Errorf("PG%d: pg_stat_io_v1 eligible=%v, want %v (MinPGVersion 16)", major, got, wantIO)
		}
	}
}
