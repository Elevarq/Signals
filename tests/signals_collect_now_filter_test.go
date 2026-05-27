package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/elevarq/arq-signals/internal/api"
	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/config"
	"github.com/elevarq/arq-signals/internal/db"
	"github.com/elevarq/arq-signals/internal/export"
)

// makeTargetTestHandler builds an api handler with a configured target
// list so the collect/now body validation has something to validate
// against. None of these targets actually connect to a PostgreSQL
// instance — the test surface is the request/response contract, not
// the cycle execution.
func makeTargetTestHandler(t *testing.T, targets []config.TargetConfig) (http.Handler, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "collect-now-filter.db")
	store, err := db.Open(dbPath, false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := store.Migrate(); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := store.EnsureInstanceID(); err != nil {
		_ = store.Close()
		t.Fatalf("EnsureInstanceID: %v", err)
	}

	exporter := export.NewBuilder(store, "test-id")
	// Long interval so the auto-cycle never fires during the test —
	// only explicit CollectNow signals matter.
	coll := collector.New(store, targets, 24*time.Hour, 30)

	deps := &api.Deps{
		DB:        store,
		Collector: coll,
		Exporter:  exporter,
		Targets:   targets,
	}
	srv := api.NewServer("127.0.0.1:0", 10*time.Second, 10*time.Second, testAPIToken, deps)
	return srv.Handler(), func() { _ = store.Close() }
}

func collectNow(t *testing.T, handler http.Handler, body string) (int, map[string]any) {
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

	var decoded map[string]any
	if w.Body.Len() > 0 {
		if err := json.NewDecoder(w.Body).Decode(&decoded); err != nil {
			t.Fatalf("decode response: %v (body=%q)", err, w.Body.String())
		}
	}
	return w.Code, decoded
}

func twoTargets() []config.TargetConfig {
	return []config.TargetConfig{
		{Name: "primary", Host: "h", Port: 5432, DBName: "d", User: "u", Enabled: true},
		{Name: "standby", Host: "h", Port: 5432, DBName: "d", User: "u", Enabled: true},
	}
}

// ---------------------------------------------------------------------------
// R082 Phase 1: optional target narrowing on POST /collect/now
// ---------------------------------------------------------------------------

// TestCollectNowEmptyBodyUnchanged verifies the historical behaviour:
// an empty / missing body still returns 202 and treats the cycle as
// "collect every enabled target". Backward compatibility is the
// foundational R082 Phase 1 invariant.
// Traces: ARQ-SIGNALS-R082 / TC-SIG-069
func TestCollectNowEmptyBodyUnchanged(t *testing.T) {
	handler, cleanup := makeTargetTestHandler(t, twoTargets())
	defer cleanup()

	code, body := collectNow(t, handler, "")
	if code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", code)
	}
	if body["status"] != "collection triggered" {
		t.Errorf("status field = %v, want \"collection triggered\"", body["status"])
	}
	got, _ := body["accepted_targets"].([]any)
	if len(got) != 2 {
		t.Errorf("accepted_targets = %v, want 2 entries (all enabled)", got)
	}
}

// TestCollectNowValidSubset verifies that a request listing a subset
// of configured + enabled targets returns 202 and the response
// echoes back exactly the requested narrow set.
// Traces: ARQ-SIGNALS-R082 / TC-SIG-070
func TestCollectNowValidSubset(t *testing.T) {
	handler, cleanup := makeTargetTestHandler(t, twoTargets())
	defer cleanup()

	code, body := collectNow(t, handler, `{"targets":["primary"]}`)
	if code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", code)
	}
	got, _ := body["accepted_targets"].([]any)
	if len(got) != 1 || got[0] != "primary" {
		t.Errorf("accepted_targets = %v, want [\"primary\"]", got)
	}
}

// TestCollectNowUnknownTarget verifies that a name not in
// signals.yaml's target list is rejected with 400 and a per-name
// reason. The cycle is not triggered.
// Traces: ARQ-SIGNALS-R082 / TC-SIG-071
func TestCollectNowUnknownTarget(t *testing.T) {
	handler, cleanup := makeTargetTestHandler(t, twoTargets())
	defer cleanup()

	code, body := collectNow(t, handler, `{"targets":["primary","does-not-exist"]}`)
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", code)
	}
	rejected, _ := body["rejected_targets"].([]any)
	if len(rejected) != 1 {
		t.Fatalf("rejected_targets = %v, want 1 entry", rejected)
	}
	r := rejected[0].(map[string]any)
	if r["name"] != "does-not-exist" {
		t.Errorf("rejected name = %v, want \"does-not-exist\"", r["name"])
	}
	if r["reason"] != "unknown_target" {
		t.Errorf("rejected reason = %v, want \"unknown_target\"", r["reason"])
	}
}

// TestCollectNowDisabledTarget verifies that a name on a configured
// but disabled target is rejected with reason=disabled_target. R082
// is explicit that disabled targets are never silently dropped from
// the accepted set.
// Traces: ARQ-SIGNALS-R082 / TC-SIG-072
func TestCollectNowDisabledTarget(t *testing.T) {
	targets := []config.TargetConfig{
		{Name: "primary", Host: "h", DBName: "d", User: "u", Enabled: true},
		{Name: "decommissioned", Host: "h", DBName: "d", User: "u", Enabled: false},
	}
	handler, cleanup := makeTargetTestHandler(t, targets)
	defer cleanup()

	code, body := collectNow(t, handler, `{"targets":["decommissioned"]}`)
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", code)
	}
	rejected, _ := body["rejected_targets"].([]any)
	if len(rejected) != 1 {
		t.Fatalf("rejected_targets = %v, want 1 entry", rejected)
	}
	r := rejected[0].(map[string]any)
	if r["name"] != "decommissioned" {
		t.Errorf("rejected name = %v, want \"decommissioned\"", r["name"])
	}
	if r["reason"] != "disabled_target" {
		t.Errorf("rejected reason = %v, want \"disabled_target\"", r["reason"])
	}
}

// TestCollectNowEmptyArray verifies that an explicit empty `targets`
// array is treated as a client bug and rejected with 400. This is
// the bright-line case in R082: absent field means "all enabled",
// empty array means "client wrote bad code". Never silently
// collapse to "all" or "none".
// Traces: ARQ-SIGNALS-R082
func TestCollectNowEmptyArray(t *testing.T) {
	handler, cleanup := makeTargetTestHandler(t, twoTargets())
	defer cleanup()

	code, body := collectNow(t, handler, `{"targets":[]}`)
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", code)
	}
	if body["error"] == nil {
		t.Errorf("expected error message, got %v", body)
	}
}
