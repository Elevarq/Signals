package main

import (
	"testing"
	"time"
)

// #731 (audit A7a) — the daemon must JOIN the in-flight collector goroutine
// (within a bounded deadline) before closing the store on shutdown, on BOTH the
// signal and the server-error path. joinCollector is the join primitive: it
// returns true when the collector finished within the deadline and false when
// the deadline elapsed, so a misbehaving dependency can never hang exit.
//
// Spec: specifications/collector-shutdown-join.md (SHUT-R001..R003).

// SHUT — the collector that finishes before the deadline is joined (true).
func TestJoinCollector_FinishesBeforeDeadline(t *testing.T) {
	done := make(chan struct{})
	close(done) // collector already stopped
	if !joinCollector(done, time.Second) {
		t.Fatal("joinCollector must return true when the collector has finished")
	}
}

// SHUT — the collector that finishes slightly before the deadline is still joined.
func TestJoinCollector_FinishesJustInTime(t *testing.T) {
	done := make(chan struct{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(done)
	}()
	if !joinCollector(done, time.Second) {
		t.Fatal("joinCollector must wait for an in-flight collector and return true")
	}
}

// SHUT — a collector that never finishes times out (false) rather than hanging
// exit; the deadline is honoured.
func TestJoinCollector_TimesOutWhenCollectorHangs(t *testing.T) {
	done := make(chan struct{}) // never closed — collector stuck
	start := time.Now()
	if joinCollector(done, 50*time.Millisecond) {
		t.Fatal("joinCollector must return false when the collector does not finish")
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Fatalf("joinCollector returned before its deadline (%v < 50ms)", elapsed)
	}
}

// SHUT — the shutdown deadline is a sane, bounded, non-zero value.
func TestCollectorShutdownTimeout_Bounded(t *testing.T) {
	if collectorShutdownTimeout <= 0 {
		t.Fatalf("collectorShutdownTimeout must be positive, got %v", collectorShutdownTimeout)
	}
	if collectorShutdownTimeout > time.Minute {
		t.Fatalf("collectorShutdownTimeout unreasonably large (%v) — exit could stall", collectorShutdownTimeout)
	}
}
