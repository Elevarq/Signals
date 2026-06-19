package tests

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/config"
)

// captureAuditLogs swaps slog's default logger for a buffered text
// handler so the test can assert on emitted records. Returns the
// buffer's contents after fn returns.
func captureAuditLogs(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(prev)
	fn()
	return buf.String()
}

func postCollectNow(t *testing.T, handler http.Handler, body string) (int, string) {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest("POST", "/collect/now", nil)
	} else {
		req = httptest.NewRequest("POST", "/collect/now", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// ---------------------------------------------------------------------------
// R082 Phase 2: request_id + reason + audit correlation
// ---------------------------------------------------------------------------

// TestCollectNowAcceptsValidRequestID verifies the happy path: a
// caller-supplied request_id matching the regex is accepted, returned
// in the response body, and surfaced in the audit log.
// Traces: SIGNALS-R082 / TC-SIG-074
func TestCollectNowAcceptsValidRequestID(t *testing.T) {
	handler, cleanup := makeTargetTestHandler(t, twoTargets())
	defer cleanup()

	out := captureAuditLogs(t, func() {
		code, body := postCollectNow(t, handler, `{"request_id":"abc_123"}`)
		if code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202; body=%s", code, body)
		}
		if !strings.Contains(body, `"request_id":"abc_123"`) {
			t.Errorf("response body did not echo request_id: %s", body)
		}
	})

	if !strings.Contains(out, "audit_event=collect_now_requested") {
		t.Errorf("missing collect_now_requested audit event:\n%s", out)
	}
	if !strings.Contains(out, "request_id=abc_123") {
		t.Errorf("audit log missing request_id=abc_123:\n%s", out)
	}
	if !strings.Contains(out, "actor=local_operator") {
		t.Errorf("audit log missing actor=local_operator:\n%s", out)
	}
}

// TestCollectNowGeneratesULIDWhenAbsent verifies that when no
// request_id is supplied, the daemon generates a ULID and returns it
// to the caller. The response gives the operator a value they can
// later grep in audit logs.
// Traces: SIGNALS-R082
func TestCollectNowGeneratesULIDWhenAbsent(t *testing.T) {
	handler, cleanup := makeTargetTestHandler(t, twoTargets())
	defer cleanup()

	code, body := postCollectNow(t, handler, "")
	if code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", code)
	}
	if !strings.Contains(body, `"request_id":"`) {
		t.Errorf("response body should carry generated request_id, got: %s", body)
	}
}

// TestCollectNowRejectsInvalidRequestID verifies that an out-of-spec
// request_id (containing characters outside [A-Za-z0-9_-] or longer
// than 32 chars) is rejected with 400 and a collect_now_rejected
// audit event.
// Traces: SIGNALS-R082 / TC-SIG-075
func TestCollectNowRejectsInvalidRequestID(t *testing.T) {
	handler, cleanup := makeTargetTestHandler(t, twoTargets())
	defer cleanup()

	cases := []struct {
		name string
		body string
	}{
		{"contains_space", `{"request_id":"abc 123"}`},
		{"contains_slash", `{"request_id":"a/b"}`},
		{"too_long", `{"request_id":"` + strings.Repeat("a", 33) + `"}`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := captureAuditLogs(t, func() {
				code, body := postCollectNow(t, handler, c.body)
				if code != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400; body=%s", code, body)
				}
			})
			if !strings.Contains(out, "audit_event=collect_now_rejected") {
				t.Errorf("missing collect_now_rejected event:\n%s", out)
			}
			if !strings.Contains(out, "error=invalid_request_id") {
				t.Errorf("missing error=invalid_request_id attribute:\n%s", out)
			}
		})
	}
}

// TestCollectNowAcceptsValidReason verifies a reason matching the
// charset is accepted and surfaced in the audit log.
// Traces: SIGNALS-R082 / TC-SIG-076
func TestCollectNowAcceptsValidReason(t *testing.T) {
	handler, cleanup := makeTargetTestHandler(t, twoTargets())
	defer cleanup()

	out := captureAuditLogs(t, func() {
		code, _ := postCollectNow(t, handler, `{"reason":"scheduled_cycle"}`)
		if code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202", code)
		}
	})

	if !strings.Contains(out, "audit_event=collect_now_requested") {
		t.Errorf("missing collect_now_requested:\n%s", out)
	}
	if !strings.Contains(out, "reason=scheduled_cycle") {
		t.Errorf("audit log missing reason attribute:\n%s", out)
	}
}

// TestCollectNowRejectsInvalidReason verifies an out-of-spec reason
// (containing characters outside [A-Za-z0-9_-] or longer than 64
// chars) is rejected with 400 and a collect_now_rejected event.
// Traces: SIGNALS-R082 / TC-SIG-077
func TestCollectNowRejectsInvalidReason(t *testing.T) {
	handler, cleanup := makeTargetTestHandler(t, twoTargets())
	defer cleanup()

	cases := []struct {
		name string
		body string
	}{
		{"contains_space", `{"reason":"scheduled cycle"}`},
		{"contains_newline", "{\"reason\":\"sched\\ncycle\"}"},
		{"too_long", `{"reason":"` + strings.Repeat("a", 65) + `"}`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := captureAuditLogs(t, func() {
				code, body := postCollectNow(t, handler, c.body)
				if code != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400; body=%s", code, body)
				}
			})
			if !strings.Contains(out, "audit_event=collect_now_rejected") {
				t.Errorf("missing collect_now_rejected:\n%s", out)
			}
			if !strings.Contains(out, "error=invalid_reason") {
				t.Errorf("missing error=invalid_reason attribute:\n%s", out)
			}
		})
	}
}

// TestCollectNowActorAlwaysLocalOperator verifies the Phase 2 actor
// invariant: every accepted request emits actor=local_operator,
// regardless of whether request_id was supplied. The
// control_plane actor value must NEVER be inferred from request
// shape — that's reserved for Phase 3 once a separate token exists.
// Traces: SIGNALS-R082 / TC-SIG-078
func TestCollectNowActorAlwaysLocalOperator(t *testing.T) {
	handler, cleanup := makeTargetTestHandler(t, twoTargets())
	defer cleanup()

	for _, body := range []string{
		"",
		`{"request_id":"01ABC"}`,
		`{"request_id":"01ABC","reason":"automated"}`,
	} {
		out := captureAuditLogs(t, func() {
			code, _ := postCollectNow(t, handler, body)
			if code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202 (body=%s)", code, body)
			}
		})

		if !strings.Contains(out, "actor=local_operator") {
			t.Errorf("body %q: missing actor=local_operator", body)
		}
		if strings.Contains(out, "actor=control_plane") {
			t.Errorf("body %q: actor must never be control_plane in Phase 2", body)
		}
	}
}

// TestCollectNowRejectedTargetEmitsAudit verifies that a request with
// an unknown / disabled target produces a collect_now_rejected event
// carrying request_id, requested_targets, accepted_targets, and
// rejected_targets so an auditor can correlate the failure.
// Traces: SIGNALS-R082 / TC-SIG-079
func TestCollectNowRejectedTargetEmitsAudit(t *testing.T) {
	targets := []config.TargetConfig{
		{Name: "primary", Host: "h", DBName: "d", User: "u", Enabled: true},
		{Name: "decommissioned", Host: "h", DBName: "d", User: "u", Enabled: false},
	}
	handler, cleanup := makeTargetTestHandler(t, targets)
	defer cleanup()

	out := captureAuditLogs(t, func() {
		code, _ := postCollectNow(t, handler,
			`{"request_id":"abc_123","reason":"automated","targets":["primary","unknown","decommissioned"]}`)
		if code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", code)
		}
	})

	if !strings.Contains(out, "audit_event=collect_now_rejected") {
		t.Fatalf("missing collect_now_rejected:\n%s", out)
	}
	if !strings.Contains(out, "request_id=abc_123") {
		t.Errorf("audit missing request_id correlation:\n%s", out)
	}
	if !strings.Contains(out, "reason=automated") {
		t.Errorf("audit missing reason:\n%s", out)
	}
	if !strings.Contains(out, "actor=local_operator") {
		t.Errorf("audit missing actor:\n%s", out)
	}
}

// TestCollectNowEmptyBodyBackwardCompatPhase2 verifies that the empty-
// body path keeps its Phase 1 HTTP contract (202, accepted_targets =
// all enabled) AND now emits a collect_now_requested audit event with
// a generated request_id. The HTTP response body adds the
// generated request_id field but the original status field stays.
// Traces: SIGNALS-R082
func TestCollectNowEmptyBodyBackwardCompatPhase2(t *testing.T) {
	handler, cleanup := makeTargetTestHandler(t, twoTargets())
	defer cleanup()

	out := captureAuditLogs(t, func() {
		code, body := postCollectNow(t, handler, "")
		if code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202", code)
		}
		if !strings.Contains(body, `"status":"collection triggered"`) {
			t.Errorf("backward-compat status field missing: %s", body)
		}
	})

	if !strings.Contains(out, "audit_event=collect_now_requested") {
		t.Errorf("Phase 2 must emit collect_now_requested even on empty body:\n%s", out)
	}
	if !strings.Contains(out, "requested_targets=all_enabled") {
		t.Errorf("empty body should record requested_targets=all_enabled:\n%s", out)
	}
}

// TestCollectNowAuditContainsNoSecrets pulls together every audit-
// emitting code path with deliberately suspicious input and asserts
// that no secret-shaped substring leaks into the audit stream. R078's
// denylist filter applies to AuditLog calls regardless of caller; this
// test guards Phase 2's new emission sites specifically.
// Traces: SIGNALS-R082 / TC-SIG-080 / INV-SIGNALS-07
func TestCollectNowAuditContainsNoSecrets(t *testing.T) {
	handler, cleanup := makeTargetTestHandler(t, twoTargets())
	defer cleanup()

	bearer := "Bearer " + testAPIToken

	out := captureAuditLogs(t, func() {
		// Several calls hitting different code paths.
		postCollectNow(t, handler, "")
		postCollectNow(t, handler, `{"request_id":"abc","reason":"normal"}`)
		postCollectNow(t, handler, `{"request_id":"a/b"}`)
		postCollectNow(t, handler, `{"targets":[]}`)
		postCollectNow(t, handler, `{"targets":["does-not-exist"]}`)
	})

	for _, banned := range []string{
		testAPIToken,           // raw API token never in audit
		bearer,                 // Authorization header value
		"password=", "secret=", // key=value secret patterns
		"postgres://",                  // DSN
		"BEGIN ", "SELECT ", "INSERT ", // SQL keyword leakage
	} {
		if strings.Contains(out, banned) {
			t.Errorf("audit stream leaked %q:\n%s", banned, out)
		}
	}
}
