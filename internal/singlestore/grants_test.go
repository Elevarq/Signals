package singlestore

import "testing"

// Tests derive from specifications/singlestore-grants.acceptance.md.

func TestOnlyClusterStatusIsElevated(t *testing.T) {
	// TC-SSG-01 / INV-SSG-01: exactly cluster_status requires CLUSTER; all
	// others run under the baseline SELECT+PROCESS role.
	for _, c := range Catalog() {
		if c.ID == "singlestore_cluster_status_v1" {
			if c.ElevatedPrivilege != "CLUSTER" {
				t.Fatalf("cluster_status must require CLUSTER, got %q", c.ElevatedPrivilege)
			}
			continue
		}
		if c.ElevatedPrivilege != "" {
			t.Fatalf("SSG: collector %s unexpectedly elevated (%q); baseline is SELECT+PROCESS", c.ID, c.ElevatedPrivilege)
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
