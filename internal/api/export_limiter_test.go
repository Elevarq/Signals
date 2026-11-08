package api

import "testing"

// #730 (A6) — concurrent full-history exports are admission-bounded so excess
// requests are rejected (HTTP 429) rather than piling up history + buffered
// ZIPs past the memory budget.
//
// Spec: specifications/bounded-export.md (BEX-R001..R002).

// BEX-R001 — the limiter admits up to N concurrent exports and rejects the
// next, and a release frees a slot.
func TestExportLimiter_AdmitsUpToLimitThenRejects(t *testing.T) {
	lim := newExportLimiter(2)
	if !lim.tryAcquire() {
		t.Fatal("1st acquire should be admitted")
	}
	if !lim.tryAcquire() {
		t.Fatal("2nd acquire should be admitted")
	}
	if lim.tryAcquire() {
		t.Fatal("3rd acquire must be rejected at limit 2")
	}
	lim.release()
	if !lim.tryAcquire() {
		t.Fatal("after a release a slot should be available again")
	}
	// Clean up the two held slots.
	lim.release()
	lim.release()
}

// BEX-R002 — a limit <= 0 means unlimited: newExportLimiter returns nil and a
// nil limiter admits everything without panicking.
func TestExportLimiter_NilIsUnlimited(t *testing.T) {
	lim := newExportLimiter(0)
	if lim != nil {
		t.Fatal("max_concurrent_exports <= 0 must yield a nil (unlimited) limiter")
	}
	for i := 0; i < 100; i++ {
		if !lim.tryAcquire() {
			t.Fatal("a nil limiter must admit every request")
		}
	}
	lim.release() // must not panic
}
