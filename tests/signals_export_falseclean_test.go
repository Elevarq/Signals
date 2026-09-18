// Tests for the FC-05 no-false-clean export contract (SIGNALS-R125).
//
// Spec: features/signals/specification.md — SIGNALS-R006, SIGNALS-R125,
//
//	FC-05, INV-SIGNALS-25
//
// Acceptance: features/signals/acceptance-tests.md —
//
//	TC-SIG-024, TC-SIG-127, TC-SIG-128, TC-SIG-129
//
// A 2xx export must never present a failed or absent collection as a
// clean, complete snapshot. For the DEFAULT scope, no successful data
// is a refusal (HTTP 422) that distinguishes "no collection yet" from
// "last collection failed: <category>". The forensic --all scope stays
// permissive but every emitted metadata.json carries a machine-detectable
// collection_status marker. These tests pin the producer-side wire
// contract Analyzer#1885/#1887 rely on.
package tests

import (
	"archive/zip"
	"bytes"
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

// makeTestHandlerWithStore is makeTestHandler but returns the backing
// store so a test can seed snapshots and cycle-outcome records and then
// exercise the real GET /export handler against them.
func makeTestHandlerWithStore(t *testing.T) (http.Handler, *db.DB, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "falseclean-test.db")
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
	return srv.Handler(), store, func() { _ = store.Close() }
}

// seedOneSuccessfulSnapshot registers a target and persists one
// successful cycle so the default export has real data to package.
func seedOneSuccessfulSnapshot(t *testing.T, store *db.DB) {
	t.Helper()
	if _, err := store.UpsertTarget("target-A", "host-a", 5432, "postgres", "elevarq", "disable", "NONE", "", true); err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	snap := db.Snapshot{ID: "snap-A1", TargetID: 1, CollectedAt: "2026-07-28T12:00:00Z", PGVersion: "PostgreSQL 18.0", Payload: json.RawMessage(`{}`)}
	runs := []db.QueryRun{{
		ID: "run-snap-A1", TargetID: 1, SnapshotID: "snap-A1", QueryID: "pg_settings_v1",
		CollectedAt: snap.CollectedAt, PGVersion: snap.PGVersion, CreatedAt: snap.CollectedAt, Status: "success",
	}}
	results := []db.QueryResult{{
		RunID: "run-snap-A1", Payload: []byte("{\"name\":\"shared_buffers\",\"setting\":\"128MB\"}\n"), SizeBytes: 50,
	}}
	if err := store.InsertCollectionAtomic(snap, runs, results); err != nil {
		t.Fatalf("InsertCollectionAtomic: %v", err)
	}
}

func doExport(t *testing.T, handler http.Handler, query string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", "/export"+query, nil)
	r.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

// TestExportRefusesDefaultNoData — a fresh system (no snapshots, no
// cycle outcome) refuses the default export with 422 no_collection_yet
// (TC-SIG-024). RED on current code, which returns 200 + empty ZIP.
func TestExportRefusesDefaultNoData(t *testing.T) {
	handler, _, cleanup := makeTestHandlerWithStore(t)
	defer cleanup()

	w := doExport(t, handler, "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("GET /export (fresh) status = %d, want 422 (no false-clean)", w.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode refusal body: %v", err)
	}
	if body["reason"] != "no_collection_yet" {
		t.Errorf("reason = %q, want no_collection_yet", body["reason"])
	}
	if ct := w.Header().Get("Content-Type"); ct == "application/zip" {
		t.Errorf("Content-Type = %q; a refusal must not return a ZIP", ct)
	}
}

// TestExportRefusesDefaultFailedCycle — after a fully-failed cycle the
// default export refuses with 422 last_collection_failed naming the
// category (TC-SIG-127). RED on current code (returns clean 200 ZIP).
func TestExportRefusesDefaultFailedCycle(t *testing.T) {
	handler, store, cleanup := makeTestHandlerWithStore(t)
	defer cleanup()

	if _, err := store.UpsertTarget("target-A", "host-a", 5432, "postgres", "elevarq", "disable", "NONE", "", true); err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	if err := store.RecordCycleOutcome("target-A", "failed", "connect_error"); err != nil {
		t.Fatalf("RecordCycleOutcome: %v", err)
	}

	w := doExport(t, handler, "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("GET /export (failed cycle) status = %d, want 422", w.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode refusal body: %v", err)
	}
	if body["reason"] != "last_collection_failed" {
		t.Errorf("reason = %q, want last_collection_failed", body["reason"])
	}
	if body["error"] == "" {
		t.Errorf("error message empty; must name the failure category")
	}
}

// TestExportDefaultWithDataUnchanged — a default export with successful
// data still returns 200 + a valid ZIP whose metadata.collection_status
// is "ok" (TC-SIG-128, regression guard).
func TestExportDefaultWithDataUnchanged(t *testing.T) {
	handler, store, cleanup := makeTestHandlerWithStore(t)
	defer cleanup()
	seedOneSuccessfulSnapshot(t, store)

	w := doExport(t, handler, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /export (with data) status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("Content-Type = %q, want application/zip", ct)
	}
	meta := metadataFromZipBytes(t, w.Body.Bytes())
	if cs, _ := meta["collection_status"].(string); cs != "ok" {
		t.Errorf("collection_status = %q, want ok", cs)
	}
}

// TestExportAllScopeMarksCollectionStatus — the forensic --all scope is
// permissive (200 ZIP) even with no successful data, but its
// metadata.collection_status marks the emptiness/failure so it is never
// a silent clean snapshot (TC-SIG-129).
func TestExportAllScopeMarksCollectionStatus(t *testing.T) {
	handler, store, cleanup := makeTestHandlerWithStore(t)
	defer cleanup()

	if _, err := store.UpsertTarget("target-A", "host-a", 5432, "postgres", "elevarq", "disable", "NONE", "", true); err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	if err := store.RecordCycleOutcome("target-A", "failed", "safety_check"); err != nil {
		t.Fatalf("RecordCycleOutcome: %v", err)
	}

	w := doExport(t, handler, "?all=true")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /export?all=true status = %d, want 200 (forensic scope not refused)", w.Code)
	}
	meta := metadataFromZipBytes(t, w.Body.Bytes())
	cs, _ := meta["collection_status"].(string)
	if cs != "last_collection_failed" && cs != "no_collection_yet" {
		t.Errorf("collection_status = %q, want last_collection_failed or no_collection_yet (must not be a silent clean snapshot)", cs)
	}
}

// metadataFromZipBytes decodes metadata.json out of a ZIP response body.
func metadataFromZipBytes(t *testing.T, b []byte) map[string]any {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	return readZipFileJSON(t, zr, "metadata.json")
}
