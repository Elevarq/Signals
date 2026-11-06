package circuit

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// R097-B / circuit-breaker — capped exponential open-cooldown backoff.
//
// Spec:        specifications/circuit-breaker.md (R097-B, INV-CIRC-05/06)
// Acceptance:  TC-CIRC-09..16
// ---------------------------------------------------------------------------

// newBackoff builds a deterministic manager with an explicit max cooldown.
func newBackoff(t *testing.T, failThreshold int, base, maxCooldown time.Duration) (*Manager, *clock) {
	t.Helper()
	c := &clock{now: time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)}
	m := NewManagerWithBackoff(failThreshold, base, maxCooldown)
	m.now = c.Now
	return m, c
}

// tripOpen drives the circuit from closed to open via fail_threshold
// consecutive failures and asserts it is open.
func tripOpen(t *testing.T, m *Manager, c *clock) {
	t.Helper()
	for i := 0; i < m.failThreshold; i++ {
		m.RecordFailure("prod")
	}
	if d := m.ShouldCollect("prod"); d.State != StateOpen {
		t.Fatalf("precondition: expected open; got %q", d.State)
	}
}

// elapseAndProbeFail asserts the circuit is still open just before the given
// cooldown boundary, closes (re-probes) just after it, and re-opens on the
// single post-cooldown probe failure (the fail-counter is preserved across the
// boundary). It leaves the circuit open at the NEXT backoff level.
func elapseAndProbeFail(t *testing.T, m *Manager, c *clock, cooldown time.Duration) {
	t.Helper()
	// Just before the cooldown boundary: still open.
	c.advance(cooldown - time.Second)
	if d := m.ShouldCollect("prod"); d.State != StateOpen {
		t.Fatalf("before cooldown (%s): expected still open; got %q", cooldown, d.State)
	}
	// Cross the boundary: closes.
	c.advance(2 * time.Second)
	if d := m.ShouldCollect("prod"); d.State != StateClosed {
		t.Fatalf("after cooldown (%s): expected closed (probe); got %q", cooldown, d.State)
	}
	// The probe fails -> re-opens at the NEXT backoff level.
	m.RecordFailure("prod")
	if d := m.ShouldCollect("prod"); d.State != StateOpen {
		t.Fatalf("probe failure should re-open; got %q", d.State)
	}
}

// TC-CIRC-09: the open cooldown grows exponentially across consecutive opens.
func TestCircuit_BackoffGrowsExponentially(t *testing.T) {
	base := time.Minute
	m, c := newBackoff(t, 3, base, 10*time.Minute)

	tripOpen(t, m, c) // open cycle 1: cooldown = base (1m)

	// Cycle 1 cooldown must be exactly base: at base-1s still open.
	c.advance(base - time.Second)
	if d := m.ShouldCollect("prod"); d.State != StateOpen {
		t.Fatalf("cycle 1 before base: expected open; got %q", d.State)
	}
	c.advance(2 * time.Second)
	if d := m.ShouldCollect("prod"); d.State != StateClosed {
		t.Fatalf("cycle 1 after base: expected closed; got %q", d.State)
	}
	m.RecordFailure("prod")
	if d := m.ShouldCollect("prod"); d.State != StateOpen {
		t.Fatalf("probe failure should re-open to cycle 2; got %q", d.State)
	}

	// Cycle 2 cooldown must be 2*base (2m): at 2m-1s still open.
	elapseAndProbeFail(t, m, c, 2*base) // -> cycle 3
	// Cycle 3 cooldown must be 4*base (4m).
	elapseAndProbeFail(t, m, c, 4*base) // -> cycle 4
	// Cycle 4 cooldown must be 8*base (8m).
	elapseAndProbeFail(t, m, c, 8*base) // -> cycle 5
}

// TC-CIRC-10: the cooldown is capped and never overflows.
func TestCircuit_BackoffCapped(t *testing.T) {
	base := time.Minute
	capped := 3 * time.Minute
	m, c := newBackoff(t, 3, base, capped)

	tripOpen(t, m, c)                   // cycle 1: 1m
	elapseAndProbeFail(t, m, c, 1*base) // -> cycle 2 (expects 2m)
	elapseAndProbeFail(t, m, c, 2*base) // -> cycle 3 (expects 3m, capped)
	// From here on every cycle is capped at 3m.
	for i := 0; i < 5; i++ {
		elapseAndProbeFail(t, m, c, capped)
	}
	// A huge number of opens must still cap (no overflow, no terminal state).
	for i := 0; i < 60; i++ {
		elapseAndProbeFail(t, m, c, capped)
	}
}

// TC-CIRC-11: RecordSuccess resets the backoff to base.
func TestCircuit_BackoffResetsOnSuccess(t *testing.T) {
	base := time.Minute
	m, c := newBackoff(t, 3, base, 10*time.Minute)

	tripOpen(t, m, c)                   // cycle 1
	elapseAndProbeFail(t, m, c, 1*base) // -> cycle 2
	elapseAndProbeFail(t, m, c, 2*base) // -> cycle 3 (would be 4m next)

	// Close via cooldown, then a SUCCESS resets the backoff.
	c.advance(4 * base)
	if d := m.ShouldCollect("prod"); d.State != StateClosed {
		t.Fatalf("expected closed after cooldown; got %q", d.State)
	}
	m.RecordSuccess("prod")

	// Re-trip: the next open cycle must be back to base (1m), not 8m.
	tripOpen(t, m, c)
	c.advance(base - time.Second)
	if d := m.ShouldCollect("prod"); d.State != StateOpen {
		t.Fatalf("after success reset, cooldown should be base: expected open before base; got %q", d.State)
	}
	c.advance(2 * time.Second)
	if d := m.ShouldCollect("prod"); d.State != StateClosed {
		t.Fatalf("after success reset, cooldown should be base: expected closed after base; got %q", d.State)
	}
}

// TC-CIRC-12 / TC-CIRC-14: Resume resets the backoff; Pause does not.
func TestCircuit_BackoffResetsOnResume(t *testing.T) {
	base := time.Minute
	m, c := newBackoff(t, 3, base, 10*time.Minute)

	tripOpen(t, m, c)                   // cycle 1
	elapseAndProbeFail(t, m, c, 1*base) // -> cycle 2
	elapseAndProbeFail(t, m, c, 2*base) // -> cycle 3

	// Pause does NOT reset the backoff; it's an orthogonal operator state.
	if err := m.Pause("prod", "maint", ""); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	// Resume resets both the fail counter and the backoff.
	m.Resume("prod", "")

	// Re-trip: cooldown back to base.
	tripOpen(t, m, c)
	c.advance(base - time.Second)
	if d := m.ShouldCollect("prod"); d.State != StateOpen {
		t.Fatalf("after resume reset, expected open before base; got %q", d.State)
	}
	c.advance(2 * time.Second)
	if d := m.ShouldCollect("prod"); d.State != StateClosed {
		t.Fatalf("after resume reset, expected closed after base; got %q", d.State)
	}
}

// TC-CIRC-13: the circuit is never terminal — it always eventually re-probes.
func TestCircuit_NeverTerminal(t *testing.T) {
	base := time.Minute
	capped := 5 * time.Minute
	m, c := newBackoff(t, 3, base, capped)

	tripOpen(t, m, c)
	// Many consecutive opens — each one must still close after the capped
	// cooldown, proving there is no give-up state.
	for i := 0; i < 100; i++ {
		// Advance a hair past the maximum possible cooldown (the cap) and
		// assert the target re-probes (closes) every time.
		c.advance(capped + time.Second)
		if d := m.ShouldCollect("prod"); d.State != StateClosed {
			t.Fatalf("iteration %d: circuit must re-probe (close) after capped; got %q", i, d.State)
		}
		m.RecordFailure("prod")
		if d := m.ShouldCollect("prod"); d.State != StateOpen {
			t.Fatalf("iteration %d: probe failure should re-open; got %q", i, d.State)
		}
	}
}

// TC-CIRC-16: a cap smaller than base clamps the cooldown to base.
func TestCircuit_CapBelowBaseClampsToBase(t *testing.T) {
	base := 5 * time.Minute
	capped := 1 * time.Minute // misconfig: cap < base
	m, c := newBackoff(t, 3, base, capped)

	tripOpen(t, m, c)
	// Cooldown must be base (5m), not the (smaller) cap: before base, open.
	c.advance(base - time.Second)
	if d := m.ShouldCollect("prod"); d.State != StateOpen {
		t.Fatalf("cap<base: expected open before base; got %q", d.State)
	}
	c.advance(2 * time.Second)
	if d := m.ShouldCollect("prod"); d.State != StateClosed {
		t.Fatalf("cap<base: expected closed after base; got %q", d.State)
	}
}

// TC-CIRC-15: a restored open circuit resumes at base (openEpochs = 1).
func TestCircuit_RestoreAnchorsAtBase(t *testing.T) {
	base := time.Minute
	m, c := newBackoff(t, 3, base, 10*time.Minute)

	// Restore an open circuit anchored now; its cooldown must be base.
	m.Restore("prod", StateOpen, c.Now(), "", "")
	c.advance(base - time.Second)
	if d := m.ShouldCollect("prod"); d.State != StateOpen {
		t.Fatalf("restored open before base: expected open; got %q", d.State)
	}
	c.advance(2 * time.Second)
	if d := m.ShouldCollect("prod"); d.State != StateClosed {
		t.Fatalf("restored open after base: expected closed; got %q", d.State)
	}
	// A probe failure re-opens at cycle 2 -> 2*base.
	m.RecordFailure("prod")
	if d := m.ShouldCollect("prod"); d.State != StateOpen {
		t.Fatalf("restored probe failure should re-open; got %q", d.State)
	}
	c.advance(2*base - time.Second)
	if d := m.ShouldCollect("prod"); d.State != StateOpen {
		t.Fatalf("restored cycle 2 before 2*base: expected open; got %q", d.State)
	}
	c.advance(2 * time.Second)
	if d := m.ShouldCollect("prod"); d.State != StateClosed {
		t.Fatalf("restored cycle 2 after 2*base: expected closed; got %q", d.State)
	}
}
