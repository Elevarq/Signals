package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TC-SIG-127 — R112 / INV-SIGNALS-22: a request presenting a valid
// bearer token authenticates regardless of how many invalid attempts
// have accumulated for its source IP. A shared-IP attacker (NAT,
// proxy, co-located pod) flooding bad tokens must not be able to lock
// the legitimate operator out of pause/resume/export.
func TestTokenAuthValidTokenBypassesLockout(t *testing.T) {
	const (
		apiToken = "valid-operator-token"
		ip       = "10.0.0.5"
	)
	limiter := newTokenRateLimiter()

	// Poison the bucket: drive the IP well past the lockout threshold.
	for i := 0; i < tokenMaxFailures*3; i++ {
		limiter.recordFailure(ip)
	}
	if limiter.allow(ip) {
		t.Fatalf("precondition: IP should be locked out after %d failures", tokenMaxFailures*3)
	}

	mw := tokenAuthMiddleware(apiToken, nil, limiter)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/collect/pause", nil)
	req.RemoteAddr = ip + ":54321"
	req.Header.Set("Authorization", "Bearer "+apiToken)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("valid token from a locked-out IP must authenticate: got status %d, want 200", rec.Code)
	}
}

// TC-SIG-127 — R112: the lockout still applies to invalid attempts, so
// brute-force throttling is preserved (acceptance: cost equal or
// better than before).
func TestTokenAuthInvalidTokenStillThrottled(t *testing.T) {
	const (
		apiToken = "valid-operator-token"
		ip       = "10.0.0.6"
	)
	limiter := newTokenRateLimiter()
	for i := 0; i < tokenMaxFailures; i++ {
		limiter.recordFailure(ip)
	}

	mw := tokenAuthMiddleware(apiToken, nil, limiter)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/collect/pause", nil)
	req.RemoteAddr = ip + ":54321"
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("invalid token from a locked-out IP must be throttled: got status %d, want 429", rec.Code)
	}
}

// TC-SIG-127 — R112: a valid token never increments or is gated by the
// failure counter, even on first contact from a clean IP.
func TestTokenAuthValidTokenDoesNotRecordFailure(t *testing.T) {
	const (
		apiToken = "valid-operator-token"
		ip       = "10.0.0.7"
	)
	limiter := newTokenRateLimiter()

	mw := tokenAuthMiddleware(apiToken, nil, limiter)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/collect/pause", nil)
	req.RemoteAddr = ip + ":54321"
	req.Header.Set("Authorization", "Bearer "+apiToken)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	limiter.mu.Lock()
	_, present := limiter.attempts[ip]
	limiter.mu.Unlock()
	if present {
		t.Fatal("a successful auth must not leave a failure entry for the IP")
	}
}
