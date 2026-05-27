package tests

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elevarq/arq-signals/internal/api"
	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/config"
	"github.com/elevarq/arq-signals/internal/db"
	"github.com/elevarq/arq-signals/internal/export"
)

// ---------------------------------------------------------------------------
// HTTP-level tests for the three new authenticated endpoints (issue #91):
//
//   POST /collect/pause   (R097)
//   POST /collect/resume  (R097)
//   POST /reload          (R100)
//
// Asserts: bearer-auth enforcement, audit-event emission, and the
// status-code contracts in the specs.
// ---------------------------------------------------------------------------

// pauseReloadTestServer builds a minimal in-process API server with a
// real Collector, real DB, and one configured target so the
// pause/resume/reload code paths exercise their real branches.
func pauseReloadTestServer(t *testing.T) (server *httptest.Server, configPath string, cleanup func()) {
	t.Helper()
	dir := t.TempDir()

	// Write a config file the /reload handler can re-read.
	configPath = filepath.Join(dir, "signals.yaml")
	contents := `signals:
  poll_interval: 5m
  min_snapshot_interval: 60s
targets:
  - name: prod
    host: 127.0.0.1
    port: 5432
    dbname: postgres
    user: arq
    sslmode: disable
`
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	store, err := db.Open(filepath.Join(dir, "test.db"), false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := store.Migrate(); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate: %v", err)
	}

	tgts := []config.TargetConfig{
		{Name: "prod", Host: "127.0.0.1", Port: 5432, DBName: "postgres", User: "arq", SSLMode: "disable", Enabled: true},
	}
	coll := collector.New(store, tgts, 1*time.Hour, 30)
	exp := export.NewBuilder(store, "test-instance-id")

	deps := &api.Deps{
		DB:         store,
		Collector:  coll,
		Exporter:   exp,
		Targets:    tgts,
		ConfigPath: configPath,
	}
	srv := api.NewServer("127.0.0.1:0", 10*time.Second, 10*time.Second, testAPIToken, deps)
	ts := httptest.NewServer(srv.Handler())

	return ts, configPath, func() {
		ts.Close()
		_ = store.Close()
	}
}

// post sends a POST with the given bearer token (empty = none) and
// returns the response. The caller closes the body.
func post(t *testing.T, ts *httptest.Server, path, body, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	return resp
}

// --- Bearer-auth enforcement ----------------------------------------------

// TestPauseRequiresBearerToken — POST /collect/pause without a token
// must return 401, not modify any state.
func TestPauseRequiresBearerToken(t *testing.T) {
	ts, _, cleanup := pauseReloadTestServer(t)
	defer cleanup()

	resp := post(t, ts, "/collect/pause", `{"target":"prod"}`, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no-token POST /collect/pause: status %d, want 401", resp.StatusCode)
	}
}

func TestPauseRejectsWrongToken(t *testing.T) {
	ts, _, cleanup := pauseReloadTestServer(t)
	defer cleanup()

	resp := post(t, ts, "/collect/pause", `{"target":"prod"}`, "not-the-right-token")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong-token POST /collect/pause: status %d, want 401", resp.StatusCode)
	}
}

func TestResumeRequiresBearerToken(t *testing.T) {
	ts, _, cleanup := pauseReloadTestServer(t)
	defer cleanup()

	resp := post(t, ts, "/collect/resume", `{"target":"prod"}`, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no-token POST /collect/resume: status %d, want 401", resp.StatusCode)
	}
}

func TestReloadRequiresBearerToken(t *testing.T) {
	ts, _, cleanup := pauseReloadTestServer(t)
	defer cleanup()

	resp := post(t, ts, "/reload", `{}`, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no-token POST /reload: status %d, want 401", resp.StatusCode)
	}
}

// --- Happy paths -----------------------------------------------------------

func TestPauseValidTokenSucceeds(t *testing.T) {
	ts, _, cleanup := pauseReloadTestServer(t)
	defer cleanup()

	resp := post(t, ts, "/collect/pause",
		`{"target":"prod","reason":"investigating incident"}`, testAPIToken)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("valid-token POST /collect/pause: status %d, want 200", resp.StatusCode)
	}
}

func TestResumeValidTokenSucceeds(t *testing.T) {
	ts, _, cleanup := pauseReloadTestServer(t)
	defer cleanup()

	resp := post(t, ts, "/collect/resume", `{"target":"prod"}`, testAPIToken)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("valid-token POST /collect/resume: status %d, want 200", resp.StatusCode)
	}
}

func TestReloadValidTokenSucceeds(t *testing.T) {
	ts, _, cleanup := pauseReloadTestServer(t)
	defer cleanup()

	resp := post(t, ts, "/reload", `{}`, testAPIToken)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("valid-token POST /reload: status %d, want 200", resp.StatusCode)
	}
}

// --- Spec contract: FC-CIRC-02 ---------------------------------------------

// Resume with an unknown target name returns 400 with the configured
// target list — strict, unlike pause's permissive behaviour.
func TestResumeUnknownTargetReturnsBadRequest(t *testing.T) {
	ts, _, cleanup := pauseReloadTestServer(t)
	defer cleanup()

	resp := post(t, ts, "/collect/resume",
		`{"target":"does-not-exist"}`, testAPIToken)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown-target POST /collect/resume: status %d, want 400", resp.StatusCode)
	}
}

// Pause with an unknown target is permissive (no 4xx). Issue #95 added
// an audit event for the no-op; here we only assert the response shape.
func TestPauseUnknownTargetIsPermissive(t *testing.T) {
	ts, _, cleanup := pauseReloadTestServer(t)
	defer cleanup()

	resp := post(t, ts, "/collect/pause",
		`{"target":"new-target-not-yet-in-config"}`, testAPIToken)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("unknown-target POST /collect/pause: status %d, want 200 (permissive)", resp.StatusCode)
	}
}

// --- Audit event capture ---------------------------------------------------

func TestPauseEmitsAuditEvent(t *testing.T) {
	ts, _, cleanup := pauseReloadTestServer(t)
	defer cleanup()

	out := captureSlog(t, func() {
		resp := post(t, ts, "/collect/pause",
			`{"target":"prod","reason":"audit-test"}`, testAPIToken)
		resp.Body.Close()
	})

	// The canonical event after PR #98 (#88) is `circuit_paused`
	// carrying both actor + reason.
	if !strings.Contains(out, "audit_event=circuit_paused") {
		t.Errorf("expected circuit_paused audit event; got:\n%s", out)
	}
	if !strings.Contains(out, "reason=audit-test") {
		t.Errorf("audit event must carry the operator-supplied reason; got:\n%s", out)
	}
}

func TestReloadEmitsAuditEvent(t *testing.T) {
	ts, _, cleanup := pauseReloadTestServer(t)
	defer cleanup()

	out := captureSlog(t, func() {
		resp := post(t, ts, "/reload", `{}`, testAPIToken)
		resp.Body.Close()
	})

	if !strings.Contains(out, "audit_event=config_reload_requested") {
		t.Errorf("expected config_reload_requested audit event; got:\n%s", out)
	}
	if !strings.Contains(out, "audit_event=config_reload_applied") {
		t.Errorf("expected config_reload_applied audit event; got:\n%s", out)
	}
}

// --- Issue #95: pause-of-unknown-target audit event ------------------------

func TestPauseUnknownTargetEmitsNoopAuditEvent(t *testing.T) {
	ts, _, cleanup := pauseReloadTestServer(t)
	defer cleanup()

	out := captureSlog(t, func() {
		resp := post(t, ts, "/collect/pause",
			`{"target":"does-not-exist"}`, testAPIToken)
		resp.Body.Close()
	})

	if !strings.Contains(out, "audit_event=circuit_pause_noop") {
		t.Errorf("expected circuit_pause_noop audit event; got:\n%s", out)
	}
	if !strings.Contains(out, "reason_category=unknown_target") {
		t.Errorf("audit event must classify the no-op; got:\n%s", out)
	}
}

// --- Issue #94: empty-fleet response is `[]`, not `null` -------------------

func TestPauseAllOnEmptyFleetReturnsEmptyArray(t *testing.T) {
	// Build a server with NO enabled targets so resolvePauseTargets
	// returns an empty list. The response must be {"paused":[]},
	// not {"paused":null}.
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "test.db"), false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	coll := collector.New(store, nil, 1*time.Hour, 30)
	deps := &api.Deps{
		DB: store, Collector: coll,
		Exporter:   export.NewBuilder(store, "test-instance-id"),
		ConfigPath: "/dev/null",
	}
	srv := api.NewServer("127.0.0.1:0", 10*time.Second, 10*time.Second, testAPIToken, deps)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := post(t, ts, "/collect/pause", `{}`, testAPIToken)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	body := make([]byte, 256)
	n, _ := resp.Body.Read(body)
	got := string(body[:n])
	if strings.Contains(got, `"paused":null`) {
		t.Errorf("empty-fleet pause-all serialised paused as null; got:\n%s", got)
	}
	if !strings.Contains(got, `"paused":[]`) {
		t.Errorf("empty-fleet pause-all must serialise paused as []; got:\n%s", got)
	}
}
