// Tests for the operator-visible per-target last error on /status
// (SIGNALS-R128, #681).
//
// Spec:        features/signals/specification.md — SIGNALS-R128
// Acceptance:  features/signals/acceptance-tests.md — TC-SIG-142
//
// Before #681 a fully-failed collection cycle was log-only: /status had
// no per-target last-error, so a collection failure on a healthy DB was
// unreportable without shelling into the container. These tests pin the
// surfacing (category + redacted message + timestamp) and the clearing
// of the marker after a later success, and assert no secret leaks.
package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fetchStatusTargets hits GET /status and returns the targets array.
func fetchStatusTargets(t *testing.T, handler http.Handler) []map[string]any {
	t.Helper()
	r := httptest.NewRequest("GET", "/status", nil)
	r.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /status: %v", err)
	}
	rawTargets, _ := body["targets"].([]any)
	out := make([]map[string]any, 0, len(rawTargets))
	for _, rt := range rawTargets {
		if m, ok := rt.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// TestStatusSurfacesAndClearsLastError — TC-SIG-142. A failed last cycle
// surfaces last_error{category,message,at}; a later success clears it.
func TestStatusSurfacesAndClearsLastError(t *testing.T) {
	store, handler, cleanup := makeStatusTestStack(t)
	defer cleanup()

	if _, err := store.UpsertTarget("target-A", "host-a", 5432, "postgres", "elevarq", "disable", "NONE", "", true); err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}

	// A fully-failed cycle persisted the operator reason code + the
	// (already-redacted) real error message (SIGNALS-R128). The message
	// carries no secret — redaction happened at persist time.
	redactedMsg := "connect target-A: failed to connect to `postgres://signals:****@db.internal:5432/app`: EOF"
	if err := store.RecordCycleOutcome("target-A", "failed", "connect_error", "target_unreachable", redactedMsg); err != nil {
		t.Fatalf("RecordCycleOutcome (failed): %v", err)
	}

	targets := fetchStatusTargets(t, handler)
	if len(targets) != 1 {
		t.Fatalf("/status targets = %d, want 1", len(targets))
	}
	le, ok := targets[0]["last_error"].(map[string]any)
	if !ok {
		t.Fatalf("target carries no last_error after a failed cycle — the #681 regression (failure unreportable over the API). Got: %v", targets[0])
	}
	if le["category"] != "target_unreachable" {
		t.Errorf("last_error.category = %v, want target_unreachable", le["category"])
	}
	if msg, _ := le["message"].(string); msg != redactedMsg {
		t.Errorf("last_error.message = %q, want the redacted real error %q", msg, redactedMsg)
	}
	if at, _ := le["at"].(string); at == "" {
		t.Errorf("last_error.at is empty, want an RFC3339 timestamp")
	}
	// INV-SIGNALS-07: no secret anywhere in the /status body.
	full := statusBody(t, handler)
	if strings.Contains(full, "SuperSecret") || strings.Contains(full, ":signals@") {
		t.Errorf("/status body leaked credential material: %s", full)
	}

	// A later successful cycle clears the marker (FC-12).
	if err := store.RecordCycleOutcome("target-A", "success", "", "", ""); err != nil {
		t.Fatalf("RecordCycleOutcome (success): %v", err)
	}
	targets = fetchStatusTargets(t, handler)
	if _, present := targets[0]["last_error"]; present {
		t.Errorf("last_error still present after a successful cycle; want it cleared (FC-12). Got: %v", targets[0]["last_error"])
	}
}

// TestStatusNoLastErrorWhenNeverFailed — a target that never failed (or
// never ran) carries no last_error key.
func TestStatusNoLastErrorWhenNeverFailed(t *testing.T) {
	store, handler, cleanup := makeStatusTestStack(t)
	defer cleanup()

	if _, err := store.UpsertTarget("target-ok", "host-ok", 5432, "postgres", "elevarq", "disable", "NONE", "", true); err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	targets := fetchStatusTargets(t, handler)
	if len(targets) != 1 {
		t.Fatalf("/status targets = %d, want 1", len(targets))
	}
	if _, present := targets[0]["last_error"]; present {
		t.Errorf("last_error present for a target that never failed; want absent")
	}
}

// statusBody returns the raw /status response body for leak assertions.
func statusBody(t *testing.T, handler http.Handler) string {
	t.Helper()
	r := httptest.NewRequest("GET", "/status", nil)
	r.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w.Body.String()
}
