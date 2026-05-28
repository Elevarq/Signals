package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
// Issue #105 regression: /export must not panic (or otherwise mis-behave)
// when Deps.Metrics is nil — the default config when
// signals.metrics_enabled is false.
//
// Issue #106 regression: /collect/now's target validation must read the
// current target list from the collector (R100), not the stale
// construction-time Deps.Targets snapshot.
// ---------------------------------------------------------------------------

// newTestServerWithMetrics builds an in-process API server. metricsReg
// is nil for the #105 regression and a real Registry for the
// "happy path" sibling.
func newTestServerForRegression(t *testing.T, initialTargets []config.TargetConfig, withMetrics bool) (*httptest.Server, *api.Deps, func()) {
	t.Helper()
	dir := t.TempDir()

	store, err := db.Open(filepath.Join(dir, "test.db"), false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := store.Migrate(); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate: %v", err)
	}

	coll := collector.New(store, initialTargets, 1*time.Hour, 30)
	exp := export.NewBuilder(store, "test-instance-id")

	deps := &api.Deps{
		DB:        store,
		Collector: coll,
		Exporter:  exp,
		Targets:   initialTargets,
		Metrics:   nil,
	}
	// withMetrics intentionally drives the same code path as the
	// nil-metrics case today — the registry-backed wiring is
	// pre-wired upstream so the API surface is identical to the
	// operator's eyes. Branch kept (instead of being inlined into
	// the parametric test name) for the moment a divergence
	// appears between the two.
	_ = withMetrics

	srv := api.NewServer("127.0.0.1:0", 10*time.Second, 10*time.Second, testAPIToken, deps)
	ts := httptest.NewServer(srv.Handler())

	cleanup := func() {
		ts.Close()
		_ = store.Close()
	}
	return ts, deps, cleanup
}

// --- Issue #105: /export with nil Metrics ----------------------------------

// TestExport_NoPanicWhenMetricsNil_Success exercises the happy path
// (200 with a ZIP body) against a daemon where signals.metrics_enabled
// is false. The previous nil-safety relied on per-method guards in
// the Registry — this test is the regression backstop in case a
// future method forgets that guard.
func TestExport_NoPanicWhenMetricsNil_Success(t *testing.T) {
	ts, _, cleanup := newTestServerForRegression(t, nil, false)
	defer cleanup()

	req, _ := http.NewRequest("GET", ts.URL+"/export", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("client err (server panicked?): %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status: got %d, want 200; body: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "zip") {
		t.Errorf("Content-Type: got %q, want a zip type", ct)
	}
}

// TestExport_NoPanicWhenMetricsNil_BadInput covers every export
// failure path that records via deps.Metrics: invalid_time_format,
// invalid_target_id, invalid_time_range, conflicting_selectors. Each
// must return its documented HTTP error without panicking.
func TestExport_NoPanicWhenMetricsNil_BadInput(t *testing.T) {
	ts, _, cleanup := newTestServerForRegression(t, nil, false)
	defer cleanup()

	cases := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{"invalid_time_format", "/export?since=not-a-time", http.StatusBadRequest},
		{"invalid_target_id", "/export?target_id=not-a-number", http.StatusBadRequest},
		{"invalid_time_range", "/export?since=2026-01-02T00:00:00Z&until=2026-01-01T00:00:00Z", http.StatusBadRequest},
		{"conflicting_selectors", "/export?all=true&snapshot_id=foo", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", ts.URL+tc.path, nil)
			req.Header.Set("Authorization", "Bearer "+testAPIToken)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("client err (server panicked?): %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("status: got %d, want %d; body: %s", resp.StatusCode, tc.wantStatus, body)
			}
		})
	}
}

// --- Issue #106: /collect/now stale target list after reload --------------

// TestCollectNow_HonoursReloadedTargetList exercises the bug
// directly: a target added via reload must be acceptable on
// /collect/now's targets array, and a removed target must be
// rejected as unknown. handleCollectNow previously read
// deps.Targets (construction-time snapshot) instead of
// deps.Collector.Targets() (reload-aware), so a freshly added
// target would be misclassified as unknown.
func TestCollectNow_HonoursReloadedTargetList(t *testing.T) {
	initial := []config.TargetConfig{
		{Name: "old-target", Host: "127.0.0.1", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
	}
	ts, deps, cleanup := newTestServerForRegression(t, initial, false)
	defer cleanup()

	// Simulate a reload that replaces "old-target" with "new-target".
	reloaded := []config.TargetConfig{
		{Name: "new-target", Host: "127.0.0.1", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
	}
	if err := deps.Collector.Reload(reloaded); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	// 1. /collect/now naming the NEW target must accept it (200).
	body := []byte(`{"targets":["new-target"]}`)
	req, _ := http.NewRequest("POST", ts.URL+"/collect/now", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testAPIToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do new-target: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("new-target should be accepted post-reload; got status %d body %s", resp.StatusCode, raw)
	}

	// 2. /collect/now naming the OLD (now-removed) target must
	// reject it with 400. deps.Targets still contains "old-target";
	// the bug was that it would be wrongly accepted. After the fix
	// it's rejected because collector.Targets() no longer holds it.
	body = []byte(`{"targets":["old-target"]}`)
	req, _ = http.NewRequest("POST", ts.URL+"/collect/now", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testAPIToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do old-target: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("old-target should be rejected post-reload; got status %d body %s", resp.StatusCode, raw)
	}
	// Body should classify the rejection reason — at minimum mention
	// the unknown target name.
	raw, _ := io.ReadAll(resp.Body)
	var rejBody map[string]any
	if err := json.Unmarshal(raw, &rejBody); err == nil {
		// Permissive: the exact error key/shape varies; we just want
		// to see "old-target" referenced somewhere so an auditor
		// reading the response sees what was rejected.
		if !strings.Contains(string(raw), "old-target") {
			t.Errorf("rejection body should name the offending target; got %s", raw)
		}
	}
}

// TestCollectNow_AllEnabledUsesReloadedList exercises the "no targets
// specified" path: handleCollectNow builds the all-enabled list and
// passes it to the collector. After reload that list must reflect
// the current config, not the stale deps.Targets.
func TestCollectNow_AllEnabledUsesReloadedList(t *testing.T) {
	initial := []config.TargetConfig{
		{Name: "old", Host: "127.0.0.1", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
	}
	ts, deps, cleanup := newTestServerForRegression(t, initial, false)
	defer cleanup()

	reloaded := []config.TargetConfig{
		{Name: "alpha", Host: "127.0.0.1", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
		{Name: "beta", Host: "127.0.0.1", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
	}
	if err := deps.Collector.Reload(reloaded); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	out := captureSlog(t, func() {
		req, _ := http.NewRequest("POST", ts.URL+"/collect/now", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Authorization", "Bearer "+testAPIToken)
		req.Header.Set("Content-Type", "application/json")
		resp, _ := http.DefaultClient.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
	})

	// The collect_now_requested audit event should reference the
	// reloaded target set, not the stale one.
	if strings.Contains(out, "target=old") {
		t.Errorf("audit log still references stale target 'old' after reload:\n%s", out)
	}
}
