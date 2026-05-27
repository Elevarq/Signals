package circuit

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// R097 / circuit-breaker — per-target state machine.
//
// Spec:        specifications/circuit-breaker.md
// Acceptance:  TC-CIRC-01..08
// ---------------------------------------------------------------------------

func newDeterministic(t *testing.T, failThreshold int, cooldown time.Duration) (*Manager, *clock) {
	t.Helper()
	c := &clock{now: time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)}
	m := NewManager(failThreshold, cooldown)
	m.now = c.Now
	return m, c
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// TC-CIRC-01: closed → open after fail_threshold consecutive failures.
func TestCircuit_AutoOpensAfterThreshold(t *testing.T) {
	m, _ := newDeterministic(t, 3, time.Minute)

	if d := m.ShouldCollect("prod"); d.Skip {
		t.Fatal("unknown target should start closed and not skip")
	}
	if d := m.ShouldCollect("prod"); d.State != StateClosed {
		t.Errorf("initial state: got %q, want %q", d.State, StateClosed)
	}

	// Two failures — still closed.
	m.RecordFailure("prod")
	m.RecordFailure("prod")
	if d := m.ShouldCollect("prod"); d.State != StateClosed {
		t.Errorf("after 2 failures: got %q, want %q", d.State, StateClosed)
	}

	// Third failure crosses the threshold.
	m.RecordFailure("prod")
	d := m.ShouldCollect("prod")
	if d.State != StateOpen {
		t.Errorf("after 3 failures: got %q, want %q", d.State, StateOpen)
	}
	if !d.Skip {
		t.Errorf("open state must produce Skip=true")
	}
	if d.Reason != "circuit_open" {
		t.Errorf("reason: got %q, want %q", d.Reason, "circuit_open")
	}
}

// TC-CIRC-02: open → closed automatically after cooldown.
func TestCircuit_OpenAutoRecoversAfterCooldown(t *testing.T) {
	m, c := newDeterministic(t, 3, time.Minute)

	for i := 0; i < 3; i++ {
		m.RecordFailure("prod")
	}
	if d := m.ShouldCollect("prod"); d.State != StateOpen {
		t.Fatalf("precondition: state should be open; got %q", d.State)
	}

	// Cooldown not yet elapsed.
	c.advance(30 * time.Second)
	if d := m.ShouldCollect("prod"); d.State != StateOpen {
		t.Errorf("before cooldown: got %q, want %q", d.State, StateOpen)
	}

	// Crosses the cooldown boundary.
	c.advance(31 * time.Second)
	d := m.ShouldCollect("prod")
	if d.State != StateClosed {
		t.Errorf("after cooldown: got %q, want %q", d.State, StateClosed)
	}
	if d.Skip {
		t.Errorf("closed state must not skip")
	}
}

// Spec: "consecutiveFails is preserved across the open-cooldown
// boundary so a single recovery cycle failure re-opens the circuit
// immediately."
func TestCircuit_SingleFailureAfterCooldownReopens(t *testing.T) {
	m, c := newDeterministic(t, 3, time.Minute)
	for i := 0; i < 3; i++ {
		m.RecordFailure("prod")
	}
	c.advance(61 * time.Second) // cooldown elapsed; state goes closed
	if d := m.ShouldCollect("prod"); d.State != StateClosed {
		t.Fatalf("precondition: state should be closed after cooldown; got %q", d.State)
	}

	// One more failure must re-open immediately (counter was preserved).
	m.RecordFailure("prod")
	if d := m.ShouldCollect("prod"); d.State != StateOpen {
		t.Errorf("after single post-cooldown failure: got %q, want %q", d.State, StateOpen)
	}
}

// RecordSuccess resets the counter so a healthy cycle after partial
// failures doesn't open the circuit.
func TestCircuit_SuccessResetsFailureCounter(t *testing.T) {
	m, _ := newDeterministic(t, 3, time.Minute)
	m.RecordFailure("prod")
	m.RecordFailure("prod")
	m.RecordSuccess("prod")
	m.RecordFailure("prod")
	m.RecordFailure("prod") // counter only at 2 since success reset it
	if d := m.ShouldCollect("prod"); d.State != StateClosed {
		t.Errorf("counter should have been reset; got %q", d.State)
	}
}

// TC-CIRC-05: manual paused overrides auto state (INV-CIRC-02).
func TestCircuit_ManualPauseOverridesOpen(t *testing.T) {
	m, _ := newDeterministic(t, 3, time.Minute)
	for i := 0; i < 3; i++ {
		m.RecordFailure("prod")
	}
	if d := m.ShouldCollect("prod"); d.State != StateOpen {
		t.Fatalf("precondition: state should be open; got %q", d.State)
	}

	if err := m.Pause("prod", "investigating incident", ""); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	d := m.ShouldCollect("prod")
	if d.State != StatePaused {
		t.Errorf("manual pause must override open; got %q", d.State)
	}
	if d.Reason != "circuit_paused" {
		t.Errorf("reason: got %q, want %q", d.Reason, "circuit_paused")
	}
}

// TC-CIRC-06: resume from paused returns to closed.
func TestCircuit_ResumeFromPausedReturnsToClosed(t *testing.T) {
	m, _ := newDeterministic(t, 3, time.Minute)
	if err := m.Pause("prod", "test", ""); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	m.Resume("prod", "")
	if d := m.ShouldCollect("prod"); d.State != StateClosed {
		t.Errorf("resume should return to closed; got %q", d.State)
	}
}

// Resume should also reset the consecutive-fail counter — otherwise
// the operator's resume could be immediately followed by an open
// transition that pre-existed the pause.
func TestCircuit_ResumeResetsFailureCounter(t *testing.T) {
	m, _ := newDeterministic(t, 3, time.Minute)
	m.RecordFailure("prod")
	m.RecordFailure("prod")
	_ = m.Pause("prod", "", "")
	m.Resume("prod", "")

	// Two more failures: should NOT re-open since resume reset counter.
	m.RecordFailure("prod")
	m.RecordFailure("prod")
	if d := m.ShouldCollect("prod"); d.State != StateClosed {
		t.Errorf("resume must reset counter; got %q", d.State)
	}
}

func TestCircuit_PauseRejectsLongReason(t *testing.T) {
	m, _ := newDeterministic(t, 3, time.Minute)
	long := strings.Repeat("x", MaxReasonLength+1)
	if err := m.Pause("prod", long, ""); err == nil {
		t.Errorf("expected error for reason > %d chars", MaxReasonLength)
	}
	// State must not have changed on rejection.
	if d := m.ShouldCollect("prod"); d.State != StateClosed {
		t.Errorf("rejected pause must leave state unchanged; got %q", d.State)
	}
}

func TestCircuit_PauseIsIdempotent(t *testing.T) {
	m, c := newDeterministic(t, 3, time.Minute)
	_ = m.Pause("prod", "first", "")
	first := m.Snapshot("prod")

	c.advance(time.Minute)
	_ = m.Pause("prod", "second", "")
	second := m.Snapshot("prod")

	if second.State != StatePaused {
		t.Errorf("state: got %q, want %q", second.State, StatePaused)
	}
	if second.PausedReason != "second" {
		t.Errorf("reason was not updated: got %q", second.PausedReason)
	}
	if !second.PausedAt.After(first.PausedAt) {
		t.Errorf("PausedAt should have advanced on idempotent pause")
	}
}

// Per-target isolation: pausing A doesn't affect B.
func TestCircuit_PerTargetIsolation(t *testing.T) {
	m, _ := newDeterministic(t, 3, time.Minute)
	_ = m.Pause("a", "test", "")
	if d := m.ShouldCollect("b"); d.State != StateClosed {
		t.Errorf("target b should be closed; got %q", d.State)
	}
}

// Snapshot of a never-seen target returns the default closed state.
func TestCircuit_SnapshotUnknownTargetIsClosed(t *testing.T) {
	m, _ := newDeterministic(t, 3, time.Minute)
	s := m.Snapshot("never-seen")
	if s.State != StateClosed {
		t.Errorf("unknown target snapshot: got %q, want %q", s.State, StateClosed)
	}
}

// AllSnapshots / Targets reflect every interacted-with target.
func TestCircuit_AllSnapshots(t *testing.T) {
	m, _ := newDeterministic(t, 3, time.Minute)
	_ = m.Pause("a", "x", "")
	m.RecordFailure("b")

	all := m.AllSnapshots()
	if len(all) != 2 {
		t.Errorf("expected 2 entries; got %d", len(all))
	}
	if all["a"].State != StatePaused {
		t.Errorf("a state: got %q, want %q", all["a"].State, StatePaused)
	}
	if all["b"].State != StateClosed {
		t.Errorf("b state (after 1 failure): got %q, want %q", all["b"].State, StateClosed)
	}

	targets := m.Targets()
	if len(targets) != 2 || targets[0] != "a" || targets[1] != "b" {
		t.Errorf("Targets() must be sorted; got %v", targets)
	}
}

// onChange callback fires on every transition, outside the lock.
func TestCircuit_OnChangeFiresOnTransitions(t *testing.T) {
	m, _ := newDeterministic(t, 3, time.Minute)

	type event struct {
		target   string
		from, to State
	}
	var (
		mu     sync.Mutex
		events []event
	)
	m.SetOnChange(func(target string, from, to State, meta TransitionMeta) {
		mu.Lock()
		events = append(events, event{target, from, to})
		mu.Unlock()
	})

	m.RecordFailure("prod") // counter only — no transition
	m.RecordFailure("prod")
	m.RecordFailure("prod")      // closed → open
	_ = m.Pause("prod", "x", "") // open → paused
	m.Resume("prod", "")         // paused → closed

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 3 {
		t.Fatalf("expected 3 transitions; got %d (%+v)", len(events), events)
	}
	expect := []event{
		{"prod", StateClosed, StateOpen},
		{"prod", StateOpen, StatePaused},
		{"prod", StatePaused, StateClosed},
	}
	for i, e := range expect {
		if events[i] != e {
			t.Errorf("transition %d: got %+v, want %+v", i, events[i], e)
		}
	}
}

// AllStates exhaustively covers the State enum.
func TestAllStates_Exhaustive(t *testing.T) {
	want := map[State]bool{StateClosed: true, StateOpen: true, StatePaused: true}
	if len(AllStates) != len(want) {
		t.Fatalf("AllStates length: got %d, want %d", len(AllStates), len(want))
	}
	for _, s := range AllStates {
		if !want[s] {
			t.Errorf("unexpected state in AllStates: %q", s)
		}
	}
}
