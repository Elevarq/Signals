// Package circuit implements the per-target circuit-breaker state
// machine described in R097.
//
// Spec:        specifications/circuit-breaker.md
// Acceptance:  TC-CIRC-01..08 (operator-safety surfaces)
//
// State is in-memory only — Manager has no persistence. Daemon
// restart resets every target to `closed`. Past pause/resume events
// live in the audit log so the operator trail survives.
package circuit

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// State enumerates the three circuit states. Lower-case wire form
// matches the JSON / metric / audit contracts in the spec.
type State string

const (
	StateClosed State = "closed"
	StateOpen   State = "open"
	StatePaused State = "paused"
)

// AllStates enumerates every state in a stable order. Used by the
// /metrics gauge to emit one (target, state) row per state so
// operators can alert on `state="open"` directly.
var AllStates = []State{StateClosed, StateOpen, StatePaused}

const (
	// DefaultFailThreshold is the consecutive-failure count that
	// trips `closed → open` when the operator hasn't overridden.
	DefaultFailThreshold = 3

	// DefaultOpenCooldown is how long an auto-opened circuit stays
	// open before auto-recovering to `closed`.
	DefaultOpenCooldown = 5 * time.Minute

	// MaxReasonLength caps the operator-provided pause reason.
	// Aligns with FC-CIRC-01 / FC-17.
	MaxReasonLength = 256
)

// Snapshot is an immutable view of a target's circuit state, safe
// to use outside the Manager's lock.
type Snapshot struct {
	State        State
	PausedAt     time.Time
	PausedReason string
	// PausedActor is the operator (token actor) who issued the most
	// recent Pause. Empty when the target has never been paused or
	// when Pause was called without an actor.
	PausedActor string
}

// targetCircuit holds the runtime state for one target. Access
// goes through Manager.mu — these fields are not independently safe.
type targetCircuit struct {
	state            State
	consecutiveFails int
	openedAt         time.Time
	pausedAt         time.Time
	pausedReason     string
	pausedActor      string
}

// Decision is the outcome of ShouldCollect. Skip == true means the
// caller MUST NOT run a cycle for this target. Reason carries the
// audit `reason_category` value (`circuit_open` / `circuit_paused`).
type Decision struct {
	Skip   bool
	State  State
	Reason string
}

// Manager is the per-target circuit-breaker state machine. Safe
// for concurrent use.
type Manager struct {
	mu            sync.Mutex
	circuits      map[string]*targetCircuit
	failThreshold int
	openCooldown  time.Duration

	// now is injectable for deterministic tests.
	now func() time.Time

	// onChange is invoked after every state transition, *outside*
	// the lock. Used by the collector wiring to emit audit events
	// and update the Prometheus gauge. `meta` carries the operator-
	// supplied actor + reason on Pause transitions; empty otherwise.
	// Optional.
	onChange func(target string, from, to State, meta TransitionMeta)
}

// TransitionMeta carries operator-supplied context that's relevant
// for manual transitions (`pause` carries actor + reason; other
// transitions leave both empty). The collector wiring attaches
// these to the audit event so a single `circuit_paused` event
// carries the full causal record (issue #88 — no more dual-event
// audit-correlation hazard).
type TransitionMeta struct {
	Actor  string
	Reason string
}

// NewManager creates a Manager. Zero / negative thresholds fall
// back to the documented defaults.
func NewManager(failThreshold int, openCooldown time.Duration) *Manager {
	if failThreshold <= 0 {
		failThreshold = DefaultFailThreshold
	}
	if openCooldown <= 0 {
		openCooldown = DefaultOpenCooldown
	}
	return &Manager{
		circuits:      map[string]*targetCircuit{},
		failThreshold: failThreshold,
		openCooldown:  openCooldown,
		now:           time.Now,
	}
}

// SetOnChange registers a callback for state transitions. Replaces
// any prior callback. The callback runs outside the manager lock —
// safe to call back into the manager from inside the callback.
//
// The `meta` arg carries operator context on Pause transitions
// (actor + reason); for auto transitions and Resume both fields
// are empty. Callbacks attach this to their audit emission so the
// single canonical `circuit_paused` event carries the full causal
// record (issue #88).
func (m *Manager) SetOnChange(fn func(target string, from, to State, meta TransitionMeta)) {
	m.mu.Lock()
	m.onChange = fn
	m.mu.Unlock()
}

// ShouldCollect performs any time-based transitions (open →
// closed via cooldown) and returns the resulting Decision.
//
// The function NEVER blocks on I/O. Callers can invoke it in any
// hot path without worrying about lock contention.
func (m *Manager) ShouldCollect(target string) Decision {
	m.mu.Lock()
	tc := m.lookupOrCreate(target)
	before := tc.state

	// open → closed after cooldown. We intentionally preserve
	// consecutiveFails so a single recovery-cycle failure
	// re-opens the circuit immediately (spec: "next cycle IS the
	// probe").
	if tc.state == StateOpen && m.now().Sub(tc.openedAt) >= m.openCooldown {
		tc.state = StateClosed
	}

	dec := Decision{State: tc.state}
	switch tc.state {
	case StateOpen:
		dec.Skip = true
		dec.Reason = "circuit_open"
	case StatePaused:
		dec.Skip = true
		dec.Reason = "circuit_paused"
	}
	after := tc.state
	cb := m.onChange
	m.mu.Unlock()

	if before != after && cb != nil {
		// Auto transitions (cooldown -> closed) carry no operator
		// context.
		cb(target, before, after, TransitionMeta{})
	}
	return dec
}

// RecordFailure is called by the collector after a cycle returns
// a non-nil error. Increments the consecutive-fail counter and may
// transition closed → open.
func (m *Manager) RecordFailure(target string) {
	m.mu.Lock()
	tc := m.lookupOrCreate(target)
	before := tc.state
	tc.consecutiveFails++
	if tc.state == StateClosed && tc.consecutiveFails >= m.failThreshold {
		tc.state = StateOpen
		tc.openedAt = m.now()
	}
	after := tc.state
	cb := m.onChange
	m.mu.Unlock()
	if before != after && cb != nil {
		cb(target, before, after, TransitionMeta{})
	}
}

// RecordSuccess is called by the collector after a successful
// cycle. Resets the consecutive-fail counter.
func (m *Manager) RecordSuccess(target string) {
	m.mu.Lock()
	tc := m.lookupOrCreate(target)
	tc.consecutiveFails = 0
	m.mu.Unlock()
}

// Pause sets the target's state to paused with the given reason
// and (optional) operator actor identifier. Idempotent — pausing
// an already-paused target updates reason / actor / timestamp.
// Returns an error when reason exceeds MaxReasonLength
// (FC-CIRC-01).
//
// The actor flows through TransitionMeta to onChange so the
// canonical `circuit_paused` audit event carries the operator
// identity without needing a separate `_request` event
// (issue #88).
func (m *Manager) Pause(target, reason, actor string) error {
	if len(reason) > MaxReasonLength {
		return fmt.Errorf("reason exceeds %d chars", MaxReasonLength)
	}
	if reason == "" {
		reason = "manual operator pause"
	}
	m.mu.Lock()
	tc := m.lookupOrCreate(target)
	before := tc.state
	tc.state = StatePaused
	tc.pausedAt = m.now()
	tc.pausedReason = reason
	tc.pausedActor = actor
	cb := m.onChange
	m.mu.Unlock()
	if before != StatePaused && cb != nil {
		cb(target, before, StatePaused, TransitionMeta{Actor: actor, Reason: reason})
	}
	return nil
}

// Resume returns the target to closed, clearing the paused reason
// and resetting the consecutive-fail counter. Idempotent. `actor`
// is the operator identity (empty when triggered from an internal
// caller); flows through TransitionMeta so the audit event carries
// who resumed the target.
func (m *Manager) Resume(target, actor string) {
	m.mu.Lock()
	tc := m.lookupOrCreate(target)
	before := tc.state
	tc.state = StateClosed
	tc.consecutiveFails = 0
	tc.pausedReason = ""
	tc.pausedAt = time.Time{}
	tc.pausedActor = ""
	cb := m.onChange
	m.mu.Unlock()
	if before != StateClosed && cb != nil {
		cb(target, before, StateClosed, TransitionMeta{Actor: actor})
	}
}

// Snapshot returns the current state for a target plus pause
// metadata if applicable.
func (m *Manager) Snapshot(target string) Snapshot {
	m.mu.Lock()
	tc := m.lookupOrCreate(target)
	s := Snapshot{State: tc.state, PausedAt: tc.pausedAt, PausedReason: tc.pausedReason, PausedActor: tc.pausedActor}
	m.mu.Unlock()
	return s
}

// AllSnapshots returns the state for every target the manager has
// seen. Returned map keys are stable in ascending order via
// sort.Strings on the returned slice from Targets().
func (m *Manager) AllSnapshots() map[string]Snapshot {
	m.mu.Lock()
	out := make(map[string]Snapshot, len(m.circuits))
	for name, tc := range m.circuits {
		out[name] = Snapshot{State: tc.state, PausedAt: tc.pausedAt, PausedReason: tc.pausedReason, PausedActor: tc.pausedActor}
	}
	m.mu.Unlock()
	return out
}

// Targets returns the names of every target the manager has seen
// in sorted order. Helpful for tests and CLI output.
func (m *Manager) Targets() []string {
	m.mu.Lock()
	out := make([]string, 0, len(m.circuits))
	for name := range m.circuits {
		out = append(out, name)
	}
	m.mu.Unlock()
	sort.Strings(out)
	return out
}

// lookupOrCreate must be called with m.mu held.
func (m *Manager) lookupOrCreate(target string) *targetCircuit {
	tc, ok := m.circuits[target]
	if !ok {
		tc = &targetCircuit{state: StateClosed}
		m.circuits[target] = tc
	}
	return tc
}
