package singlestore

import "testing"

// Tests derive from specifications/singlestore-grants.acceptance.md.

func TestClusterElevatedCollectors(t *testing.T) {
	// TC-SSG-01 / INV-SSG-01 / SSG-R002: exactly cluster_status and mv_events
	// require CLUSTER (confirmed live — MV_CLUSTER_STATUS + MV_EVENTS return
	// ERROR 1227 under baseline SELECT+PROCESS); every other collector runs under
	// the baseline role.
	wantElevated := map[string]string{
		"singlestore_cluster_status_v1": "CLUSTER",
		"singlestore_mv_events_v1":      "CLUSTER",
	}
	seen := map[string]bool{}
	for _, c := range Catalog() {
		if want, ok := wantElevated[c.ID]; ok {
			if c.ElevatedPrivilege != want {
				t.Fatalf("%s must require %q, got %q", c.ID, want, c.ElevatedPrivilege)
			}
			seen[c.ID] = true
			continue
		}
		if c.ElevatedPrivilege != "" {
			t.Fatalf("SSG: collector %s unexpectedly elevated (%q); baseline is SELECT+PROCESS", c.ID, c.ElevatedPrivilege)
		}
	}
	for id := range wantElevated {
		if !seen[id] {
			t.Fatalf("expected elevated collector %s not found in catalog", id)
		}
	}
}

// TC-SSG-02 is RETIRED (#577/#589): the hand-captured golden fixture
// (testdata/singlestore_collect_fixture.json) and its replay test drifted from
// the real catalog and could not prove their capture revision, so they were
// never a verifiable gate. The full catalog shape + engine=singlestore assembly
// + redaction are now proven every CI run by the integration-singlestore live
// qualification (real daemon -> production exporter against the pinned Dev Image
// service container; specifications/singlestore-live-qualification.md
// SLQ-R006..R010), which replays the current catalog rather than a frozen
// snapshot. The deterministic serialization leg lives in singlestore_qual_test.go.
