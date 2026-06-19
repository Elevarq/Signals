// Per-target minimum snapshot interval enforcement (R091, R092).
//
// Spec: features/signals/specification.md
//   SIGNALS-R091 (min snapshot interval, default 60s, per
//                     logical target = targets.name)
//   SIGNALS-R092 (--force / force=true bypass — opt-in only)
//   FC-10            (non-positive min_interval is a config error)
//   INV-SIGNALS-15   (skip leaves zero rows)
//
// The decision is a pure function so it is exhaustively unit-
// testable without spinning up a real PG. The collector wires it
// into `collectTarget` before any PG connection or DB write so
// the skip path is fast and side-effect-free (only an audit log
// entry is emitted).

package collector

import (
	"fmt"
	"time"
)

// DefaultMinSnapshotInterval is the v1 default — 60 seconds. The
// daemon's configuration validator rejects zero or negative values
// (FC-10). Operators who genuinely want every poll to result in a
// collection set `signals.poll_interval >= min_snapshot_interval`.
const DefaultMinSnapshotInterval = 60 * time.Second

// ShouldSkipForMinInterval decides whether a per-target collection
// must skip because R091's `min_snapshot_interval` has not yet
// elapsed since the last completed snapshot for that target.
//
// Inputs:
//   - lastCollectedAt: zero-time → target has no completed snapshot
//     yet; the rule is vacuous and the cycle MUST run (TC-SIG-110).
//   - minInterval: positive duration; zero or negative panics
//     (FC-10) — the configuration validator should have prevented
//     this from reaching here.
//   - now: cycle start time. Tests pass deterministic values; the
//     production caller passes time.Now().
//   - force: R092 override. true bypasses the interval check
//     unconditionally (TC-SIG-115).
//
// Returns:
//   - skip: true when the target must be skipped this cycle.
//   - elapsed: now - lastCollectedAt; reported alongside the
//     audit event so an operator can see exactly how much of the
//     interval is still unmet. Zero when lastCollectedAt is the
//     zero time.
//
// The boundary is inclusive on the run side: at the exact moment
// elapsed == minInterval the collection runs. Any prior moment
// skips. (TC-SIG-112 pins this.)
func ShouldSkipForMinInterval(lastCollectedAt time.Time, minInterval time.Duration, now time.Time, force bool) (bool, time.Duration) {
	if minInterval <= 0 {
		panic(fmt.Sprintf("min_snapshot_interval must be positive (FC-10); got %v", minInterval))
	}
	if force {
		return false, 0
	}
	if lastCollectedAt.IsZero() {
		return false, 0
	}
	elapsed := now.Sub(lastCollectedAt)
	if elapsed < minInterval {
		return true, elapsed
	}
	return false, elapsed
}
