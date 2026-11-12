package collector

import "testing"

// #808 — overResultPayloadCap gates whether a run's encoded query_results
// payload exceeds the configured per-run cap. A run over the cap is recorded
// skipped/result_too_large (no payload) so one large OLAP result cannot emit a
// single NDJSON line over the Analyzer's per-line ingest ceiling and fail the
// whole snapshot (Elevarq/Analyzer#4215). capBytes <= 0 disables the cap.
func TestOverResultPayloadCap(t *testing.T) {
	cases := []struct {
		name     string
		capBytes int
		size     int
		want     bool
	}{
		{"disabled zero cap, large payload", 0, 1 << 30, false},
		{"disabled negative cap", -1, 1 << 30, false},
		{"under cap", 32 << 20, 1 << 20, false},
		{"exactly at cap is allowed", 32 << 20, 32 << 20, false},
		{"one byte over cap", 32 << 20, (32 << 20) + 1, true},
		{"far over cap", 32 << 20, 128 << 20, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := overResultPayloadCap(tc.capBytes, tc.size); got != tc.want {
				t.Fatalf("overResultPayloadCap(%d, %d) = %v, want %v", tc.capBytes, tc.size, got, tc.want)
			}
		})
	}
}
