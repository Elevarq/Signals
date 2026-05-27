package api

import (
	"testing"
	"time"
)

func TestRateLimiterAllowFreshIP(t *testing.T) {
	l := newTokenRateLimiter()
	if !l.allow("1.2.3.4") {
		t.Fatal("fresh IP should be allowed")
	}
}

func TestRateLimiterBlocksAfterMaxFailures(t *testing.T) {
	l := newTokenRateLimiter()
	const ip = "1.2.3.4"
	for i := 0; i < tokenMaxFailures; i++ {
		l.recordFailure(ip)
	}
	if l.allow(ip) {
		t.Fatalf("IP should be blocked after %d failures", tokenMaxFailures)
	}
}

func TestRateLimiterRecordSuccessClearsFailures(t *testing.T) {
	l := newTokenRateLimiter()
	const ip = "1.2.3.4"

	// Push close to but not over the threshold.
	for i := 0; i < tokenMaxFailures-1; i++ {
		l.recordFailure(ip)
	}
	l.recordSuccess(ip)

	// One subsequent failure must not block — counter was reset.
	l.recordFailure(ip)
	if !l.allow(ip) {
		t.Fatal("IP should be allowed after recordSuccess reset the counter")
	}

	l.mu.Lock()
	_, present := l.attempts[ip]
	l.mu.Unlock()
	if !present {
		t.Fatal("recordFailure after recordSuccess should re-create the entry")
	}
}

func TestRateLimiterRecordSuccessIsIdempotent(t *testing.T) {
	l := newTokenRateLimiter()
	// Calling recordSuccess on an unknown IP must not panic.
	l.recordSuccess("never.seen.before")
}

func TestRateLimiterAllowAfterLockoutWindowExpires(t *testing.T) {
	l := newTokenRateLimiter()
	const ip = "1.2.3.4"
	for i := 0; i < tokenMaxFailures; i++ {
		l.recordFailure(ip)
	}

	// Backdate the entry past the lockout window.
	l.mu.Lock()
	l.attempts[ip].lastAttempt = time.Now().Add(-2 * tokenLockoutWindow)
	l.mu.Unlock()

	if !l.allow(ip) {
		t.Fatal("IP should be allowed once lockout window expires")
	}
}

func TestRateLimiterOpportunisticPrune(t *testing.T) {
	l := newTokenRateLimiter()

	// Seed several stale entries from imaginary attacker IPs that never retry.
	stale := time.Now().Add(-2 * tokenLockoutWindow)
	for i := 0; i < 10; i++ {
		ip := "stale-" + string(rune('a'+i))
		l.recordFailure(ip)
		l.mu.Lock()
		l.attempts[ip].lastAttempt = stale
		l.mu.Unlock()
	}

	// A new failure from a fresh IP should prune the stale entries.
	l.recordFailure("fresh.ip")

	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.attempts) != 1 {
		t.Fatalf("expected only the fresh IP to remain, got %d entries", len(l.attempts))
	}
	if _, ok := l.attempts["fresh.ip"]; !ok {
		t.Fatal("fresh.ip entry should remain after prune")
	}
}
