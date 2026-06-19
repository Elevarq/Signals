// Tests for issue #16 (Codex Beta review polish):
//   - /status.target_count must reflect only enabled targets.
//   - collector.Reload must propagate a ReconcileEnabledTargets error
//     so a /reload or SIGHUP that can't update DB enablement is reported,
//     not silently logged.

package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/elevarq/signals/internal/api"
	"github.com/elevarq/signals/internal/collector"
	"github.com/elevarq/signals/internal/config"
	"github.com/elevarq/signals/internal/db"
	"github.com/elevarq/signals/internal/export"
)

// Build a real api handler over a real store so we can seed targets and
// hit /status end-to-end without copying makeTestHandler's full body.
func makeStatusTestStack(t *testing.T) (*db.DB, http.Handler, func()) {
	t.Helper()
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "status-test.db"), false)
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
	coll := collector.New(store, nil, time.Hour, 30)
	exp := export.NewBuilder(store, "test-id")
	srv := api.NewServer("127.0.0.1:0", 10*time.Second, 10*time.Second, testAPIToken, &api.Deps{
		DB: store, Collector: coll, Exporter: exp,
	})
	return store, srv.Handler(), func() { _ = store.Close() }
}

// TC-#16 (1): /status.target_count must count only enabled targets so it
// stays consistent with the targetInfo slice (which already filters
// disabled per #7) and with INV-SIGNALS-14.
func TestStatusTargetCountExcludesDisabled(t *testing.T) {
	store, handler, cleanup := makeStatusTestStack(t)
	defer cleanup()

	if _, err := store.UpsertTarget("target-A", "host-a", 5432, "postgres", "arq", "disable", "NONE", "", true); err != nil {
		t.Fatalf("UpsertTarget A: %v", err)
	}
	if _, err := store.UpsertTarget("target-B", "host-b", 5432, "postgres", "arq", "disable", "NONE", "", true); err != nil {
		t.Fatalf("UpsertTarget B: %v", err)
	}
	// Soft-disable B (the reload path would do this on a config change).
	if err := store.ReconcileEnabledTargets([]string{"target-A"}); err != nil {
		t.Fatalf("ReconcileEnabledTargets: %v", err)
	}

	r := httptest.NewRequest("GET", "/status", nil)
	r.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /status status = %d, want 200", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /status: %v", err)
	}

	got := int(body["target_count"].(float64))
	if got != 1 {
		t.Errorf("/status.target_count = %d, want 1 (target-B is disabled and must not be counted)", got)
	}
	if targets, _ := body["targets"].([]any); len(targets) != 1 {
		t.Errorf("/status.targets length = %d, want 1 (must match target_count, INV-SIGNALS-14)", len(targets))
	}
}

// TC-#16 (2): collector.Reload must propagate a reconcile failure, NOT
// log-and-swallow. Forcing the failure: close the store before calling
// Reload, which makes ReconcileEnabledTargets return an error.
func TestReloadPropagatesReconcileFailure(t *testing.T) {
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "reload-test.db"), false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := store.Migrate(); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate: %v", err)
	}
	coll := collector.New(store, nil, time.Hour, 30)

	// Force reconcile to fail by closing the DB beneath the collector.
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err = coll.Reload([]config.TargetConfig{
		{Name: "t1", Host: "h", Port: 5432, DBName: "db", User: "u", SSLMode: "disable", Enabled: true},
	})
	if err == nil {
		t.Fatal("Reload returned nil after store close; reconcile failure must propagate (#16)")
	}
}
