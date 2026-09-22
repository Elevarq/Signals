package tests

import (
	"testing"
	"time"

	"github.com/elevarq/signals/internal/circuit"
	"github.com/elevarq/signals/internal/collector"
)

// TestCircuitStateSurvivesRestart (#455) proves an auto-tripped circuit is
// persisted and rehydrated across a daemon restart, instead of silently
// resuming collection.
func TestCircuitStateSurvivesRestart(t *testing.T) {
	store := openTestDB(t)

	// First daemon: trip the target's circuit to open. The collector's
	// onChange hook persists the transition.
	c1 := collector.New(store, nil, time.Hour, 30)
	for i := 0; i < circuit.DefaultFailThreshold; i++ {
		c1.Circuit().RecordFailure("pg1")
	}
	if dec := c1.Circuit().ShouldCollect("pg1"); !dec.Skip || dec.State != circuit.StateOpen {
		t.Fatalf("expected circuit open after %d failures; got %+v", circuit.DefaultFailThreshold, dec)
	}

	// Simulate a restart: a fresh collector over the same store starts with
	// all circuits closed until it rehydrates.
	c2 := collector.New(store, nil, time.Hour, 30)
	if dec := c2.Circuit().ShouldCollect("pg1"); dec.Skip {
		t.Fatal("a fresh circuit should start closed before restore")
	}
	if err := c2.RestoreCircuitState(); err != nil {
		t.Fatalf("RestoreCircuitState: %v", err)
	}
	if dec := c2.Circuit().ShouldCollect("pg1"); !dec.Skip || dec.State != circuit.StateOpen {
		t.Errorf("circuit state did not survive restart: %+v", dec)
	}
}

// TestResumeDeletesPersistedRow (#455): a paused target is persisted, and
// resuming it (transition back to closed) removes the persisted row so it is
// collectable again after a restart.
func TestResumeDeletesPersistedRow(t *testing.T) {
	store := openTestDB(t)

	c1 := collector.New(store, nil, time.Hour, 30)
	if err := c1.Circuit().Pause("pg1", "maintenance", "alice"); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	states, _ := store.GetCircuitStates()
	if len(states) != 1 || states[0].State != "paused" || states[0].Actor != "alice" {
		t.Fatalf("expected one persisted paused row for pg1; got %+v", states)
	}

	c1.Circuit().Resume("pg1", "alice") // paused → closed → deletes the row

	states, _ = store.GetCircuitStates()
	for _, s := range states {
		if s.TargetName == "pg1" {
			t.Errorf("resumed target pg1 should have no persisted circuit row; got %+v", s)
		}
	}
}
