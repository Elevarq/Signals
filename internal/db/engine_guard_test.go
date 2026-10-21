package db

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Requirement (#532): a NEW production collection write must carry an explicit
// engine. The snapshots.engine column default ('postgresql') is for
// historical-row/back-compat ONLY — it must never let a new collection silently
// become a PostgreSQL snapshot. InsertCollection (the production new-collection
// write path, used by the collector for both engines) therefore rejects an
// empty engine.
func TestInsertCollection_RejectsImplicitEngine(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "guard.db"), false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	tid, err := store.UpsertTarget("t", "h", 3306, "db", "u", "require", "", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)

	// A new collection with NO engine identity — must be refused, not defaulted.
	err = store.InsertCollection(
		Snapshot{ID: "snap-x", TargetID: tid, CollectedAt: now, Payload: json.RawMessage(`{}`), SizeBytes: 2},
		nil, nil,
	)
	if err == nil {
		t.Fatal("InsertCollection accepted a snapshot with no engine identity — a new production write silently became PostgreSQL")
	}
	if !strings.Contains(err.Error(), "explicit engine identity") {
		t.Errorf("error should explain the explicit-engine requirement, got: %v", err)
	}
	// And nothing was persisted.
	if n, _ := store.CountSnapshots(); n != 0 {
		t.Errorf("a rejected write must persist nothing, got %d snapshots", n)
	}

	// With an explicit engine it succeeds (control).
	if err := store.InsertCollection(
		Snapshot{ID: "snap-ok", TargetID: tid, CollectedAt: now, Engine: "singlestore", Payload: json.RawMessage(`{}`), SizeBytes: 2},
		nil, nil,
	); err != nil {
		t.Fatalf("explicit-engine write should succeed: %v", err)
	}
}
