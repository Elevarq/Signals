package db

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

// Engine identity on snapshots (Elevarq/Signals#532, ECD-R005, migration 007).
//
// The storage model is engine-neutral; this verifies the one discriminator the
// model gained round-trips through the common persistence/read path, and that a
// pre-#532 (empty-engine) write is normalized to the PostgreSQL default so
// legacy snapshots keep their exact v1 meaning (INV-ECD-02).
func TestSnapshotEngineIdentityRoundTrip(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "engine.db"), false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	tid, err := store.UpsertTarget("ss1", "ss.example", 3306, "admin", "memsql", "require", "", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)

	// A SingleStore collection persisted through the COMMON atomic path:
	// snapshot(engine=singlestore) + one collector run + its result payload.
	snap := Snapshot{
		ID: "snap-ss-1", TargetID: tid, CollectedAt: now,
		PGVersion: "8.5.22", Engine: "singlestore",
		Payload: json.RawMessage(`{}`), SizeBytes: 2,
	}
	runs := []QueryRun{{
		ID: "run-1", TargetID: tid, SnapshotID: snap.ID,
		QueryID: "singlestore_identity_v1", CollectedAt: now, PGVersion: "8.5.22",
		RowCount: 1, CreatedAt: now, Status: "success",
	}}
	results := []QueryResult{{RunID: "run-1", Payload: []byte(`{"memsql_version":"8.5.22"}` + "\n"), SizeBytes: 28}}
	if err := store.InsertCollectionAtomic(snap, runs, results); err != nil {
		t.Fatalf("InsertCollectionAtomic: %v", err)
	}

	got, err := store.GetSnapshotByID("snap-ss-1")
	if err != nil || got == nil {
		t.Fatalf("GetSnapshotByID: %v (got %v)", err, got)
	}
	if got.Engine != "singlestore" {
		t.Errorf("GetSnapshotByID engine = %q, want singlestore", got.Engine)
	}
	byTarget, err := store.GetSnapshotsByTarget(tid, "", "")
	if err != nil || len(byTarget) != 1 || byTarget[0].Engine != "singlestore" {
		t.Errorf("GetSnapshotsByTarget engine not carried: %+v (err %v)", byTarget, err)
	}
	latest, err := store.GetLatestSnapshotsPerTarget()
	if err != nil {
		t.Fatalf("GetLatestSnapshotsPerTarget: %v", err)
	}
	found := false
	for _, s := range latest {
		if s.ID == "snap-ss-1" {
			found = true
			if s.Engine != "singlestore" {
				t.Errorf("latest snapshot engine = %q, want singlestore", s.Engine)
			}
		}
	}
	if !found {
		t.Errorf("snap-ss-1 absent from GetLatestSnapshotsPerTarget")
	}

	// A pre-#532 caller that does not set Engine must store the PostgreSQL
	// default (v1-compatible) — proven via InsertSnapshot.
	if err := store.InsertSnapshot(Snapshot{
		ID: "snap-legacy", TargetID: tid, CollectedAt: now,
		PGVersion: "17", Payload: json.RawMessage(`{}`), SizeBytes: 2,
	}); err != nil {
		t.Fatalf("InsertSnapshot legacy: %v", err)
	}
	legacy, err := store.GetSnapshotByID("snap-legacy")
	if err != nil || legacy == nil {
		t.Fatalf("GetSnapshotByID legacy: %v", err)
	}
	if legacy.Engine != "postgresql" {
		t.Errorf("legacy snapshot engine = %q, want postgresql (default normalization)", legacy.Engine)
	}
}
