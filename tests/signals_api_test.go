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
	"github.com/elevarq/signals/internal/db"
	"github.com/elevarq/signals/internal/export"
)

const testAPIToken = "test-token-12345"

// makeTestHandler creates a full middleware-wrapped HTTP handler backed by a
// temp DB, using the production api.NewServer stack and the exported Handler() method.
func makeTestHandler(t *testing.T) (http.Handler, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "api-test.db")
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

	// Create a real Collector with no targets (it will never run, but
	// provides CollectNow() and LastCollected() without panicking).
	coll := collector.New(store, nil, 1*time.Hour, 30)

	deps := &api.Deps{
		DB:        store,
		Collector: coll,
		Exporter:  exporter,
	}

	srv := api.NewServer("127.0.0.1:0", 10*time.Second, 10*time.Second, testAPIToken, deps)
	handler := srv.Handler()

	return handler, func() { _ = store.Close() }
}

// TestHealthEndpoint verifies GET /health returns 200 with {"status":"ok"} without auth.
// Traces: SIGNALS-R011 / TC-SIG-015
func TestHealthEndpoint(t *testing.T) {
	handler, cleanup := makeTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /health status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want %q", body["status"], "ok")
	}
}

// TestStatusEndpointRequiresAuth verifies GET /status without bearer token returns 401.
// Traces: SIGNALS-R011 / TC-SIG-016
func TestStatusEndpointRequiresAuth(t *testing.T) {
	handler, cleanup := makeTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /status without auth: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestStatusEndpointResponse verifies GET /status with valid token returns 200
// and the response body contains expected fields but NOT secret_type or secret_ref.
// Traces: SIGNALS-R011 / TC-SIG-016
func TestStatusEndpointResponse(t *testing.T) {
	handler, cleanup := makeTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/status", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /status status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode /status response: %v", err)
	}

	// Verify required fields exist.
	for _, field := range []string{"instance_id", "version", "target_count", "snapshot_count"} {
		if _, ok := body[field]; !ok {
			t.Errorf("/status response missing field %q", field)
		}
	}

	// Verify secret_type and secret_ref are NOT in the response body.
	if _, ok := body["secret_type"]; ok {
		t.Error("/status response contains secret_type — credential source details should be hidden")
	}
	if _, ok := body["secret_ref"]; ok {
		t.Error("/status response contains secret_ref — credential path details should be hidden")
	}
}

// TestCollectNowEndpoint verifies POST /collect/now with valid token returns 202.
// Traces: SIGNALS-R011 / TC-SIG-017
func TestCollectNowEndpoint(t *testing.T) {
	handler, cleanup := makeTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest("POST", "/collect/now", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("POST /collect/now status = %d, want %d", w.Code, http.StatusAccepted)
	}
}

// TestExportEndpoint verifies GET /export with valid token returns application/zip content type.
// Traces: SIGNALS-R011 / TC-SIG-018
func TestExportEndpoint(t *testing.T) {
	handler, cleanup := makeTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/export", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /export status = %d, want %d", w.Code, http.StatusOK)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/zip" {
		t.Errorf("GET /export Content-Type = %q, want %q", ct, "application/zip")
	}
}

// TestExportEndpointReturnsErrorOnFailure verifies that when the export
// builder fails midway, the handler returns a JSON 500 with no ZIP headers
// — not a 200 OK with a truncated/invalid ZIP body. This guards against
// silent data corruption on the consumer side.
// Traces: SIGNALS-R011 / TC-SIG-018
func TestExportEndpointReturnsErrorOnFailure(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "broken-export.db")
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
	coll := collector.New(store, nil, 1*time.Hour, 30)
	deps := &api.Deps{DB: store, Collector: coll, Exporter: exporter}
	srv := api.NewServer("127.0.0.1:0", 10*time.Second, 10*time.Second, testAPIToken, deps)
	handler := srv.Handler()

	// Close the store so the exporter's queries fail — simulates a mid-export
	// I/O error. Buffering means the failure must surface as a 500, not a 200
	// with a corrupt ZIP.
	_ = store.Close()

	req := httptest.NewRequest("GET", "/export", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on export failure, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct == "application/zip" {
		t.Errorf("response should not advertise Content-Type=application/zip on failure, got %q", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); cd != "" {
		t.Errorf("response should not have Content-Disposition on failure, got %q", cd)
	}
}

// TestExportEndpointRequiresAuth verifies GET /export without token returns 401.
// Traces: SIGNALS-R011 / TC-SIG-018
func TestExportEndpointRequiresAuth(t *testing.T) {
	handler, cleanup := makeTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/export", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /export without auth: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestCollectNowEndpointRequiresAuth verifies POST /collect/now without token returns 401.
// Traces: SIGNALS-R011 / TC-SIG-017
func TestCollectNowEndpointRequiresAuth(t *testing.T) {
	handler, cleanup := makeTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest("POST", "/collect/now", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("POST /collect/now without auth: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}
