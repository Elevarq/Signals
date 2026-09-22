package db

import (
	"path/filepath"
	"testing"
)

// TestCircuitStatePersistence (#455) covers the upsert / list / delete
// round-trip of the persisted circuit-breaker state.
func TestCircuitStatePersistence(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "circuit.db"), false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if err := store.UpsertCircuitState("t-open", "open", "2026-05-12T12:00:00Z", "", "", "2026-05-12T12:00:00Z"); err != nil {
		t.Fatalf("upsert open: %v", err)
	}
	if err := store.UpsertCircuitState("t-paused", "paused", "2026-05-12T12:05:00Z", "maintenance", "alice", "2026-05-12T12:05:00Z"); err != nil {
		t.Fatalf("upsert paused: %v", err)
	}

	states, err := store.GetCircuitStates()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("got %d states, want 2", len(states))
	}
	// ORDER BY target_name → t-open, t-paused.
	if states[0].TargetName != "t-open" || states[0].State != "open" {
		t.Errorf("state[0] = %+v", states[0])
	}
	if states[1].State != "paused" || states[1].Reason != "maintenance" || states[1].Actor != "alice" {
		t.Errorf("state[1] = %+v", states[1])
	}

	// Upsert overwrites in place.
	if err := store.UpsertCircuitState("t-open", "paused", "2026-05-12T13:00:00Z", "r", "bob", "2026-05-12T13:00:00Z"); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	states, _ = store.GetCircuitStates()
	if len(states) != 2 {
		t.Fatalf("after re-upsert got %d states, want 2", len(states))
	}

	// Delete (transition back to closed) removes the row.
	if err := store.DeleteCircuitState("t-open"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	states, _ = store.GetCircuitStates()
	if len(states) != 1 || states[0].TargetName != "t-paused" {
		t.Fatalf("after delete got %+v, want only t-paused", states)
	}
}
