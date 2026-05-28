// Tests for R109 / INV-SIGNALS-20: disabled or removed targets are
// excluded from the default export and /status, and targets.enabled is
// reconciled against config. Added for issue #7.

package tests

import (
	"testing"

	"github.com/elevarq/arq-signals/internal/db"
	"github.com/elevarq/arq-signals/internal/export"
)

func enabledByName(t *testing.T, store *db.DB) map[string]bool {
	t.Helper()
	targets, err := store.GetTargets()
	if err != nil {
		t.Fatalf("GetTargets: %v", err)
	}
	out := map[string]bool{}
	for _, tg := range targets {
		out[tg.Name] = tg.Enabled
	}
	return out
}

func seedTwoEnabledTargetsWithRuns(t *testing.T, store *db.DB) {
	t.Helper()
	if _, err := store.UpsertTarget("target-A", "host-a", 5432, "postgres", "arq", "disable", "NONE", "", true); err != nil {
		t.Fatalf("UpsertTarget A: %v", err)
	}
	if _, err := store.UpsertTarget("target-B", "host-b", 5432, "postgres", "arq", "disable", "NONE", "", true); err != nil {
		t.Fatalf("UpsertTarget B: %v", err)
	}
	seedRun(t, store, "snap-a", 1, "pg_settings_v1", "2026-04-25T10:00:00Z", "success")
	seedRun(t, store, "snap-b", 2, "pg_settings_v1", "2026-04-25T10:00:00Z", "success")
}

// TC-SIG-125 (unit) — ReconcileEnabledTargets soft-disables removed/
// disabled targets and re-enables present ones.
func TestReconcileEnabledTargets(t *testing.T) {
	store := openTestDB(t)
	seedTwoEnabledTargetsWithRuns(t, store)

	// Only target-A remains enabled in config.
	if err := store.ReconcileEnabledTargets([]string{"target-A"}); err != nil {
		t.Fatalf("ReconcileEnabledTargets: %v", err)
	}
	en := enabledByName(t, store)
	if !en["target-A"] {
		t.Errorf("target-A should be enabled")
	}
	if en["target-B"] {
		t.Errorf("target-B should be soft-disabled after reconcile")
	}

	// B's snapshot must still exist (soft-disable, no deletion).
	all, err := store.GetAllSnapshots("", "")
	if err != nil {
		t.Fatalf("GetAllSnapshots: %v", err)
	}
	var haveB bool
	for _, s := range all {
		if s.ID == "snap-b" {
			haveB = true
		}
	}
	if !haveB {
		t.Errorf("disabling target-B must not delete its snapshots")
	}

	// Re-enabling restores it.
	if err := store.ReconcileEnabledTargets([]string{"target-A", "target-B"}); err != nil {
		t.Fatalf("ReconcileEnabledTargets re-enable: %v", err)
	}
	if !enabledByName(t, store)["target-B"] {
		t.Errorf("target-B should be re-enabled")
	}
}

// TC-SIG-125 (unit) — the default scope excludes a disabled target's runs.
func TestGetLatestRunsPerCollectorExcludesDisabled(t *testing.T) {
	store := openTestDB(t)
	seedTwoEnabledTargetsWithRuns(t, store)

	if err := store.ReconcileEnabledTargets([]string{"target-A"}); err != nil {
		t.Fatalf("ReconcileEnabledTargets: %v", err)
	}
	runs, err := store.GetLatestRunsPerCollector(0)
	if err != nil {
		t.Fatalf("GetLatestRunsPerCollector: %v", err)
	}
	for _, r := range runs {
		if r.TargetID == 2 {
			t.Errorf("disabled target-B (id=2) run leaked into default scope: %+v", r)
		}
	}
	if len(runs) != 1 {
		t.Errorf("expected exactly target-A's run, got %d runs", len(runs))
	}
}

// TC-SIG-125 — disabled target absent from default export, present under --all.
func TestDefaultExportExcludesDisabledTarget(t *testing.T) {
	store := openTestDB(t)
	seedTwoEnabledTargetsWithRuns(t, store)
	if err := store.ReconcileEnabledTargets([]string{"target-A"}); err != nil {
		t.Fatalf("ReconcileEnabledTargets: %v", err)
	}

	_, zr, err := buildExportZIPWithOpts(t, store, export.Options{})
	if err != nil {
		t.Fatalf("WriteTo default: %v", err)
	}
	def := map[string]bool{}
	for _, id := range readZipNDJSONField(t, zr, "snapshots.ndjson", "id") {
		def[id] = true
	}
	if def["snap-b"] {
		t.Errorf("default export must exclude disabled target-B's snapshot snap-b")
	}
	if !def["snap-a"] {
		t.Errorf("default export must include enabled target-A's snapshot snap-a")
	}

	_, zrAll, err := buildExportZIPWithOpts(t, store, export.Options{All: true})
	if err != nil {
		t.Fatalf("WriteTo --all: %v", err)
	}
	all := map[string]bool{}
	for _, id := range readZipNDJSONField(t, zrAll, "snapshots.ndjson", "id") {
		all[id] = true
	}
	if !all["snap-b"] {
		t.Errorf("--all export must still include disabled target-B's snapshot (forensics)")
	}
}
