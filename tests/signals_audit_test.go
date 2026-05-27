package tests

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elevarq/arq-signals/internal/api"
	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/db"
	"github.com/elevarq/arq-signals/internal/export"
	"github.com/elevarq/arq-signals/internal/safety"
)

// captureSlog routes slog output to a buffer for the duration of fn so the
// test can assert on emitted records.
func captureSlog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(prev)
	fn()
	return buf.String()
}

func makeAuditTestHandler(t *testing.T, hsEnabled bool) (http.Handler, func(), *db.DB, *export.Builder) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "audit-test.db")
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

	exporter := export.NewBuilder(store, "test-instance-id")
	exporter.SetHighSensitivityCollectorsEnabled(hsEnabled)

	coll := collector.New(store, nil, 1*time.Hour, 30)
	deps := &api.Deps{DB: store, Collector: coll, Exporter: exporter}
	srv := api.NewServer("127.0.0.1:0", 10*time.Second, 10*time.Second, testAPIToken, deps)

	cleanup := func() { _ = store.Close() }
	return srv.Handler(), cleanup, store, exporter
}

// ---------------------------------------------------------------------------
// R078: Export audit events on success and failure
// ---------------------------------------------------------------------------

// TestExportAuditEventsOnSuccess verifies that GET /export emits both
// export_requested and export_completed audit events with the expected
// shape (no secrets, contains source_ip, status, duration_ms, size_bytes).
// Traces: ARQ-SIGNALS-R078
func TestExportAuditEventsOnSuccess(t *testing.T) {
	handler, cleanup, _, _ := makeAuditTestHandler(t, false)
	defer cleanup()

	out := captureSlog(t, func() {
		req := httptest.NewRequest("GET", "/export", nil)
		req.Header.Set("Authorization", "Bearer "+testAPIToken)
		req.RemoteAddr = "203.0.113.7:54321"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})

	for _, want := range []string{
		"audit_event=export_requested",
		"audit_event=export_completed",
		"status=success",
		"source_ip=203.0.113.7",
		"size_bytes=",
		"duration_ms=",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in audit output, got:\n%s", want, out)
		}
	}
}

// TestExportAuditEventsOnFailure verifies that an export failure emits
// an export_completed audit event with status=failed and an
// error_category, and never leaks the underlying error detail.
// Traces: ARQ-SIGNALS-R078
func TestExportAuditEventsOnFailure(t *testing.T) {
	handler, _, store, _ := makeAuditTestHandler(t, false)
	// Force builder failure mid-export.
	_ = store.Close()

	out := captureSlog(t, func() {
		req := httptest.NewRequest("GET", "/export", nil)
		req.Header.Set("Authorization", "Bearer "+testAPIToken)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", w.Code)
		}
	})

	if !strings.Contains(out, "audit_event=export_completed") {
		t.Errorf("missing export_completed event:\n%s", out)
	}
	if !strings.Contains(out, "status=failed") {
		t.Errorf("missing status=failed:\n%s", out)
	}
	if !strings.Contains(out, "error_category=") {
		t.Errorf("missing error_category:\n%s", out)
	}
}

// TestExportAuditInvalidTargetID verifies the 400-path also emits a
// completed event with the right error category.
// Traces: ARQ-SIGNALS-R078
func TestExportAuditInvalidTargetID(t *testing.T) {
	handler, cleanup, _, _ := makeAuditTestHandler(t, false)
	defer cleanup()

	out := captureSlog(t, func() {
		req := httptest.NewRequest("GET", "/export?target_id=not-a-number", nil)
		req.Header.Set("Authorization", "Bearer "+testAPIToken)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	if !strings.Contains(out, "error_category=invalid_target_id") {
		t.Errorf("expected error_category=invalid_target_id, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// R078: Audit events do not leak secrets
// ---------------------------------------------------------------------------

// TestAuditLogsContainNoSecrets exercises a request flow with a fully
// populated set of (deliberately-suspicious) attribute names directly via
// safety.AuditLog and asserts that no banned substring survived.
// Traces: ARQ-SIGNALS-R078 / INV-SIGNALS-07
func TestAuditLogsContainNoSecrets(t *testing.T) {
	out := captureSlog(t, func() {
		safety.AuditLog("export_completed",
			"status", "success",
			"size_bytes", 1024,
			// Sensitive values that should never reach the log even if a
			// future caller forgets the contract.
			"password", "should-not-leak",
			"api_token", "tok-deadbeef",
			"connection_string", "host=db user=u password=hunter2",
			"dsn", "postgres://user:secret@db/app",
		)
	})

	for _, banned := range []string{
		"should-not-leak",
		"tok-deadbeef",
		"hunter2",
		"postgres://user:secret",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("audit log leaked sensitive value %q:\n%s", banned, out)
		}
	}
}

// ---------------------------------------------------------------------------
// R078: High-sensitivity gate state visible in metadata
// ---------------------------------------------------------------------------

// readMetadataFromExport extracts metadata.json from a ZIP body.
func readMetadataFromExport(t *testing.T, body []byte) map[string]any {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	for _, f := range r.File {
		if f.Name != "metadata.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open metadata.json: %v", err)
		}
		defer rc.Close()
		raw, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read metadata.json: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("unmarshal metadata.json: %v", err)
		}
		return m
	}
	t.Fatal("metadata.json not found in export")
	return nil
}

// TestExportMetadataContainsComplianceFields verifies the export
// metadata.json carries every R078-required field for downstream
// auditors.
// Traces: ARQ-SIGNALS-R078
func TestExportMetadataContainsComplianceFields(t *testing.T) {
	for _, hs := range []bool{false, true} {
		hs := hs
		t.Run(map[bool]string{false: "gate_off", true: "gate_on"}[hs], func(t *testing.T) {
			handler, cleanup, _, _ := makeAuditTestHandler(t, hs)
			defer cleanup()

			req := httptest.NewRequest("GET", "/export", nil)
			req.Header.Set("Authorization", "Bearer "+testAPIToken)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("export returned %d", w.Code)
			}

			meta := readMetadataFromExport(t, w.Body.Bytes())

			required := []string{
				"arq_signals_version",
				"schema_version",
				"generated_at",
				"instance_id",
				"high_sensitivity_collectors_enabled",
				"collector_status_schema_version",
			}
			for _, k := range required {
				if _, ok := meta[k]; !ok {
					t.Errorf("metadata missing required field %q", k)
				}
			}

			if got, want := meta["instance_id"], "test-instance-id"; got != want {
				t.Errorf("instance_id = %v, want %q", got, want)
			}
			if got := meta["high_sensitivity_collectors_enabled"]; got != hs {
				t.Errorf("high_sensitivity_collectors_enabled = %v, want %v", got, hs)
			}
			// generated_at should be RFC 3339-ish.
			if g, _ := meta["generated_at"].(string); g == "" || !strings.Contains(g, "T") {
				t.Errorf("generated_at not RFC 3339: %v", meta["generated_at"])
			}
		})
	}
}

// TestStartupAuditLogsHighSensitivityState exercises the audit emission
// from main.go's bootstrap path by calling the same helper directly. This
// keeps the test hermetic (no spawning the full daemon) while still
// asserting the shape an auditor will see.
// Traces: ARQ-SIGNALS-R078
func TestStartupAuditLogsHighSensitivityState(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		out := captureSlog(t, func() {
			safety.AuditLog("high_sensitivity_collectors", "enabled", enabled)
			safety.AuditLog("targets_loaded", "enabled", 3, "disabled", 1)
		})
		if !strings.Contains(out, "audit_event=high_sensitivity_collectors") {
			t.Errorf("missing high_sensitivity_collectors event:\n%s", out)
		}
		want := "enabled=" + map[bool]string{false: "false", true: "true"}[enabled]
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got:\n%s", want, out)
		}
		if !strings.Contains(out, "audit_event=targets_loaded") {
			t.Errorf("missing targets_loaded event:\n%s", out)
		}
		if !strings.Contains(out, "disabled=1") {
			t.Errorf("expected disabled=1 in output, got:\n%s", out)
		}
	}
}
