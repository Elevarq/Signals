package tests

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elevarq/signals/internal/api"
	"github.com/elevarq/signals/internal/collector"
	"github.com/elevarq/signals/internal/db"
	"github.com/elevarq/signals/internal/export"
	"github.com/elevarq/signals/internal/metrics"
)

// makeMetricsTestHandler builds an api.Handler with metrics either
// disabled (registry == nil, path == "") or enabled at the given path
// with a fresh registry. Returns the registry so tests can poke it
// directly.
func makeMetricsTestHandler(t *testing.T, enabled bool, path string) (http.Handler, *metrics.Registry, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "metrics.db")
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
	coll := collector.New(store, nil, 1*time.Hour, 30)

	var reg *metrics.Registry
	if enabled {
		reg = metrics.New()
	}

	deps := &api.Deps{
		DB:          store,
		Collector:   coll,
		Exporter:    exporter,
		Metrics:     reg,
		MetricsPath: path,
	}
	srv := api.NewServer("127.0.0.1:0", 10*time.Second, 10*time.Second, testAPIToken, deps)
	return srv.Handler(), reg, func() { _ = store.Close() }
}

// ---------------------------------------------------------------------------
// R079: /metrics endpoint default-off and on-path behaviour
// ---------------------------------------------------------------------------

// TestMetricsEndpoint404WhenDisabled verifies the safe default: when
// metrics are not explicitly enabled the endpoint is not registered at
// all. A scrape against /metrics returns 404, not silent success.
// Traces: SIGNALS-R079
func TestMetricsEndpoint404WhenDisabled(t *testing.T) {
	handler, reg, cleanup := makeMetricsTestHandler(t, false, "")
	defer cleanup()
	if reg != nil {
		t.Fatal("registry should be nil when metrics are disabled")
	}

	req := httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("/metrics with metrics disabled: status = %d, want 404", w.Code)
	}
}

// TestMetricsEndpointReturnsPromFormat verifies that an enabled
// /metrics endpoint serves the Prometheus text exposition format and,
// once each metric has been sampled at least once, exposes every
// R079-required metric name. Vec metrics in client_golang only emit
// lines after a first observation; the test mirrors that contract.
// Traces: SIGNALS-R079
func TestMetricsEndpointReturnsPromFormat(t *testing.T) {
	handler, reg, cleanup := makeMetricsTestHandler(t, true, "/metrics")
	defer cleanup()

	// Sample every vec at least once and bump the unlabelled gauges.
	reg.ObserveCollection("primary", "success", 0.1)
	reg.ObserveCollectionFailure("primary", "internal")
	reg.AddCollectorOutcomes("primary", 1,
		map[string]int{"timeout": 1},
		map[string]int{"config_disabled": 1},
	)
	reg.RecordExport("success", 0.01)
	reg.RecordExportFailure("builder_error")
	reg.IncSQLitePersistenceFailure()
	reg.SetLastSuccessfulCollection("primary", 1700000000)
	reg.SetHighSensitivityEnabled(true)

	req := httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") && !strings.Contains(ct, "openmetrics") {
		t.Errorf("Content-Type = %q, expected Prometheus text format", ct)
	}

	body, _ := io.ReadAll(w.Body)
	out := string(body)

	// Every metric in R079 must now appear (HELP line at minimum).
	required := []string{
		"signals_collection_cycles_total",
		"signals_collection_failures_total",
		"signals_collection_duration_seconds",
		"signals_collectors_succeeded_total",
		"signals_collectors_failed_total",
		"signals_collectors_skipped_total",
		"signals_export_requests_total",
		"signals_export_failures_total",
		"signals_export_duration_seconds",
		"signals_sqlite_persistence_failures_total",
		"signals_last_successful_collection_timestamp",
		"signals_high_sensitivity_collectors_enabled",
	}
	for _, m := range required {
		if !strings.Contains(out, "# HELP "+m) {
			t.Errorf("metric %q missing HELP line in /metrics output", m)
		}
	}
}

// TestMetricsEndpointRequiresAuth verifies the endpoint inherits the
// API's bearer-token auth model. Without a token, the response is 401.
// Traces: SIGNALS-R079
func TestMetricsEndpointRequiresAuth(t *testing.T) {
	handler, _, cleanup := makeMetricsTestHandler(t, true, "/metrics")
	defer cleanup()

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("/metrics without auth: status = %d, want 401", w.Code)
	}
}

// ---------------------------------------------------------------------------
// R079: metrics output never contains SQL, secrets, or PG payloads
// ---------------------------------------------------------------------------

// TestMetricsOutputContainsNoSQLOrSecrets exercises the registry by
// recording a representative set of operations, then scans the rendered
// /metrics body for substrings that would only appear if a sensitive
// payload had leaked into the metric set. The test is conservative
// (false positives would mean we pollute legitimate metric names) but
// the listed substrings have no benign reason to appear.
// Traces: SIGNALS-R079
func TestMetricsOutputContainsNoSQLOrSecrets(t *testing.T) {
	handler, reg, cleanup := makeMetricsTestHandler(t, true, "/metrics")
	defer cleanup()

	// Pretend the daemon has been busy.
	reg.ObserveCollection("primary", "success", 0.42)
	reg.AddCollectorOutcomes("primary", 12,
		map[string]int{"permission_denied": 1},
		map[string]int{"config_disabled": 4},
	)
	reg.RecordExport("success", 0.05)
	reg.RecordExportFailure("builder_error")
	reg.IncSQLitePersistenceFailure()
	reg.SetLastSuccessfulCollection("primary", float64(time.Now().Unix()))
	reg.SetHighSensitivityEnabled(true)

	req := httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Body)
	out := string(body)

	// Substrings that should never appear in a healthy metrics dump.
	forbidden := []string{
		"SELECT ", "select ", "FROM ", "from ",
		"INSERT ", "DELETE ", "UPDATE ",
		"pg_stat_", "pg_get_",
		"password", "secret", "api_token", "Bearer ",
		"postgres://", "host=", "dbname=",
	}
	for _, f := range forbidden {
		if strings.Contains(out, f) {
			t.Errorf("forbidden substring %q appeared in /metrics output", f)
		}
	}
}

// ---------------------------------------------------------------------------
// R079: counters update on collection success/failure and export
// success/failure
// ---------------------------------------------------------------------------

// TestMetricsCountersUpdateOnExport drives a real export request
// through the API handler with metrics enabled and asserts that the
// export counter and duration histogram both moved.
// Traces: SIGNALS-R079
func TestMetricsCountersUpdateOnExport(t *testing.T) {
	handler, _, cleanup := makeMetricsTestHandler(t, true, "/metrics")
	defer cleanup()

	// Drive a successful export.
	req := httptest.NewRequest("GET", "/export", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export returned %d", w.Code)
	}

	// And a failure (invalid target_id).
	req = httptest.NewRequest("GET", "/export?target_id=notanumber", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad target_id, got %d", w.Code)
	}

	// Now scrape /metrics and verify both samples are present.
	req = httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Body)
	out := string(body)

	for _, want := range []string{
		`signals_export_requests_total{status="success"} 1`,
		`signals_export_requests_total{status="failed"} 1`,
		`signals_export_failures_total{error_category="invalid_target_id"} 1`,
		`signals_export_duration_seconds_count{status="success"} 1`,
		`signals_export_duration_seconds_count{status="failed"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in metrics output, got:\n%s", want, out)
		}
	}
}

// TestMetricsCountersUpdateOnCollection drives the recorder helpers
// that the collector cycle defer calls and asserts the counter samples
// appear with the expected labels.
// Traces: SIGNALS-R079
func TestMetricsCountersUpdateOnCollection(t *testing.T) {
	handler, reg, cleanup := makeMetricsTestHandler(t, true, "/metrics")
	defer cleanup()

	// Simulate a successful cycle and a failed cycle on different targets.
	reg.ObserveCollection("primary", "success", 1.5)
	reg.AddCollectorOutcomes("primary", 7,
		nil,
		map[string]int{"config_disabled": 4},
	)
	reg.SetLastSuccessfulCollection("primary", 1700000000)

	reg.ObserveCollection("standby", "failed", 0.2)
	reg.ObserveCollectionFailure("standby", "connect_error")

	req := httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	body, _ := io.ReadAll(w.Body)
	out := string(body)

	for _, want := range []string{
		`signals_collection_cycles_total{status="success",target="primary"} 1`,
		`signals_collection_cycles_total{status="failed",target="standby"} 1`,
		`signals_collection_failures_total{reason="connect_error",target="standby"} 1`,
		`signals_collectors_succeeded_total{target="primary"} 7`,
		`signals_collectors_skipped_total{reason="config_disabled",target="primary"} 4`,
		`signals_last_successful_collection_timestamp{target="primary"} 1.7e+09`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in metrics output, got:\n%s", want, out)
		}
	}
}

// TestMetricsHighSensitivityGauge verifies the gauge tracks the R075
// gate state — auditors can see at a glance whether the daemon is
// running with high-sensitivity collection enabled.
// Traces: SIGNALS-R079 / SIGNALS-R075
func TestMetricsHighSensitivityGauge(t *testing.T) {
	handler, reg, cleanup := makeMetricsTestHandler(t, true, "/metrics")
	defer cleanup()

	reg.SetHighSensitivityEnabled(true)
	body := scrapeMetrics(t, handler)
	if !strings.Contains(body, "signals_high_sensitivity_collectors_enabled 1") {
		t.Errorf("expected gauge=1 when enabled, got:\n%s", body)
	}

	reg.SetHighSensitivityEnabled(false)
	body = scrapeMetrics(t, handler)
	if !strings.Contains(body, "signals_high_sensitivity_collectors_enabled 0") {
		t.Errorf("expected gauge=0 when disabled, got:\n%s", body)
	}
}

func scrapeMetrics(t *testing.T, handler http.Handler) string {
	t.Helper()
	req := httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	body, _ := io.ReadAll(w.Body)
	return string(body)
}
