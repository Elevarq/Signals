package collector

import (
	"testing"

	"github.com/elevarq/signals/internal/db"
	"github.com/elevarq/signals/internal/singlestore"
)

// Tests derive from specifications/engine-keyed-reporting.acceptance.md.

// TC-MKR-07 (MKR-R006): a result's measured DurationMS propagates onto the run.
func TestMapSingleStoreCollectionPropagatesDuration(t *testing.T) {
	results := []singlestore.Result{
		{CollectorID: "singlestore_identity_v1", Category: "server", Rows: []map[string]any{{"v": "9.1.1"}}, DurationMS: 42},
	}
	_, runs, _, err := mapSingleStoreCollection("snap", 1, "9.1.1", "2026-10-23T00:00:00Z", results)
	if err != nil {
		t.Fatalf("mapSingleStoreCollection: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	if runs[0].DurationMS != 42 {
		t.Errorf("run DurationMS = %d, want 42 (measured, not 0)", runs[0].DurationMS)
	}
}

// TC-MKR-09 (MKR-R007): an elevated collector's permission-denied error is
// recorded skipped (reason permission_denied); a non-elevated collector with
// the same error is failed.
func TestMapSingleStoreCollectionElevatedPermissionDeniedIsSkipped(t *testing.T) {
	permErr := "Error 1142 (42000): SELECT command denied to user 'mon'@'%' for table 'MV_CLUSTER_STATUS'"
	results := []singlestore.Result{
		{CollectorID: "singlestore_cluster_status_v1", Category: "topology", Err: permErr, ElevatedPrivilege: "CLUSTER"},
		{CollectorID: "singlestore_leaves_v1", Category: "topology", Err: permErr},
	}
	_, runs, _, err := mapSingleStoreCollection("snap", 1, "9.1.1", "2026-10-23T00:00:00Z", results)
	if err != nil {
		t.Fatalf("mapSingleStoreCollection: %v", err)
	}
	byID := map[string]db.QueryRun{}
	for _, r := range runs {
		byID[r.QueryID] = r
	}
	cs := byID["singlestore_cluster_status_v1"]
	if cs.Status != "skipped" || cs.Reason != "permission_denied" {
		t.Errorf("elevated cluster_status run = {status=%q reason=%q}, want {skipped permission_denied}", cs.Status, cs.Reason)
	}
	lv := byID["singlestore_leaves_v1"]
	if lv.Status != "failed" {
		t.Errorf("non-elevated leaves run status = %q, want failed", lv.Status)
	}
}

// TC-MKR-10 (MKR-R004): outcome counts are per-outcome, not len(results).
func TestSSOutcomeCounts(t *testing.T) {
	runs := []db.QueryRun{
		{QueryID: "a", Status: "success"},
		{QueryID: "b", Status: "success"},
		{QueryID: "c", Status: "failed"},
		{QueryID: "d", Status: "skipped"},
	}
	success, failed, skipped := ssOutcomeCounts(runs)
	if success != 2 || failed != 1 || skipped != 1 {
		t.Errorf("ssOutcomeCounts = (%d,%d,%d), want (2,1,1)", success, failed, skipped)
	}
	if success+failed+skipped != len(runs) {
		t.Errorf("counts must sum to run count %d, got %d", len(runs), success+failed+skipped)
	}
}

// TC-MKR-11 (MKR-R003): each failed collector is reported by identity.
func TestSSFailedCollectorsCarryIdentity(t *testing.T) {
	runs := []db.QueryRun{
		{QueryID: "ok1", Status: "success"},
		{QueryID: "bad1", Status: "failed", Error: "boom"},
		{QueryID: "skip1", Status: "skipped", Reason: "permission_denied"},
		{QueryID: "bad2", Status: "failed", Error: "kaboom"},
	}
	failures := ssFailedCollectors(runs)
	if len(failures) != 2 {
		t.Fatalf("want 2 failed collectors, got %d", len(failures))
	}
	ids := map[string]string{}
	for _, f := range failures {
		ids[f.ID] = f.Error
	}
	if ids["bad1"] != "boom" || ids["bad2"] != "kaboom" {
		t.Errorf("failed collectors must name identity+error, got %+v", ids)
	}
	if _, present := ids["skip1"]; present {
		t.Errorf("skipped collector must not be reported as a failure")
	}
}
