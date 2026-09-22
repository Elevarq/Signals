package circuit

import (
	"testing"
	"time"
)

// TestRestoreOpenCircuit (#455): a restored open circuit skips, and one
// whose cooldown already elapsed during downtime closes on the first check.
func TestRestoreOpenCircuit(t *testing.T) {
	m, c := newDeterministic(t, 3, 10*time.Minute)

	m.Restore("fresh", StateOpen, c.Now(), "", "")
	if dec := m.ShouldCollect("fresh"); !dec.Skip || dec.State != StateOpen {
		t.Errorf("restored open circuit should skip; got %+v", dec)
	}

	// Opened 20 minutes ago; cooldown (10m) elapsed while the daemon was down.
	m.Restore("elapsed", StateOpen, c.Now().Add(-20*time.Minute), "", "")
	if dec := m.ShouldCollect("elapsed"); dec.Skip {
		t.Errorf("restored open circuit past cooldown should close; got %+v", dec)
	}
}

// TestRestorePausedCircuit (#455): a restored paused circuit stays paused
// regardless of elapsed time (a pause has no cooldown).
func TestRestorePausedCircuit(t *testing.T) {
	m, c := newDeterministic(t, 3, 10*time.Minute)

	m.Restore("t1", StatePaused, c.Now(), "maintenance", "alice")
	if dec := m.ShouldCollect("t1"); !dec.Skip || dec.State != StatePaused {
		t.Errorf("restored paused circuit should skip; got %+v", dec)
	}
	c.advance(1 * time.Hour)
	if dec := m.ShouldCollect("t1"); !dec.Skip || dec.State != StatePaused {
		t.Errorf("paused circuit must stay paused regardless of time; got %+v", dec)
	}
}

// TestRestoreClosedIsNoOp (#455): restoring a closed state leaves the
// target collectable (closed is the default; only open/paused persist).
func TestRestoreClosedIsNoOp(t *testing.T) {
	m, c := newDeterministic(t, 3, 10*time.Minute)
	m.Restore("t1", StateClosed, c.Now(), "", "")
	if dec := m.ShouldCollect("t1"); dec.Skip {
		t.Errorf("restored closed circuit must not skip; got %+v", dec)
	}
}
