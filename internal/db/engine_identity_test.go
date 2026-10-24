package db

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// Tests derive from specifications/engine-identity.acceptance.md.

// TC-EID-04 / TC-EID-05 (EID-R003, INV-EID-01): migration 007 applies; an
// engine persists and round-trips; an empty engine normalizes to postgresql
// on write so a legacy/empty identity is never stored or read back.
func TestSnapshotEnginePersistsAndNormalizes(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "engine.db"), false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(); err != nil { // migration 007 must apply cleanly
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := store.UpsertTarget("t", "h", 5432, "db", "u", "disable", "none", "", true); err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}

	cases := []struct {
		id, in, want string
	}{
		{"snap-empty", "", "postgresql"},          // TC-EID-04: empty -> postgresql
		{"snap-pg", "postgresql", "postgresql"},   // TC-EID-05: explicit round-trip
		{"snap-ss", "singlestore", "singlestore"}, // db layer stores any engine verbatim
	}
	for _, c := range cases {
		if err := store.InsertSnapshot(Snapshot{
			ID: c.id, TargetID: 1, CollectedAt: "2026-10-23T00:00:00Z",
			PGVersion: "16.0", Engine: c.in,
			Payload: json.RawMessage(`{}`), SizeBytes: 2,
		}); err != nil {
			t.Fatalf("InsertSnapshot %s: %v", c.id, err)
		}
		got, err := store.GetSnapshotByID(c.id)
		if err != nil || got == nil {
			t.Fatalf("GetSnapshotByID %s: %v (nil=%v)", c.id, err, got == nil)
		}
		if got.Engine != c.want {
			t.Errorf("snapshot %s engine: got %q, want %q", c.id, got.Engine, c.want)
		}
	}
}
