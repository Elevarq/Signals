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

// makeR083Handler builds an HTTP handler with R083 Mode B wiring.
// When mode != managed the controlPlaneToken is ignored and the
// server only honours the local API token.
func makeR083Handler(t *testing.T, mode, controlPlaneTokenFile string) http.Handler {
	t.Helper()
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "r083.db"), false)
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
	t.Cleanup(func() { _ = store.Close() })

	exporter := export.NewBuilder(store, "test-id")
	coll := collector.New(store, nil, 24*time.Hour, 30)

	deps := &api.Deps{
		DB:        store,
		Collector: coll,
		Exporter:  exporter,
	}
	if mode == config.ModeManaged && controlPlaneTokenFile != "" {
		signals := config.SignalsConfig{
			Mode:                  config.ModeManaged,
			ControlPlaneTokenFile: controlPlaneTokenFile,
		}
		deps.ControlPlaneTokenFn = func() string {
			tok, _ := config.ResolveControlPlaneToken(signals)
			return tok
		}
	}

	srv := api.NewServer("127.0.0.1:0", 10*time.Second, 10*time.Second, testAPIToken, deps)
	return srv.Handler()
}

func writeTokenFile(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "control-plane.token")
	if err := os.WriteFile(path, []byte(contents+"\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	return path
}

// testCPToken / rotatedCPToken are runtime-constructed low-entropy
// strings used as fake control-plane bearer tokens in this file.
// They satisfy R083's 32-char minimum without embedding a
// high-entropy alphanumeric literal in source — that pattern trips
// the gitleaks scanner's generic-api-key rule even when the value
// is obviously test-only.
var (
	testCPToken    = strings.Repeat("b", 32)
	rotatedCPToken = strings.Repeat("c", 32)
)

// ---------------------------------------------------------------------------
// R083: ValidateStrict + ValidateModeBTokens
// ---------------------------------------------------------------------------

// TestR083ModeDefaultsStandalone — TC-SIG-081
// Traces: ARQ-SIGNALS-R083 / TC-SIG-081
func TestR083ModeDefaultsStandalone(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.Signals.Mode != config.ModeStandalone {
		t.Errorf("default mode = %q, want %q", cfg.Signals.Mode, config.ModeStandalone)
	}
	cfg.Targets = []config.TargetConfig{
		{Name: "primary", Host: "h", DBName: "d", User: "u", Enabled: true, SSLMode: "verify-full"},
	}
	if _, err := config.ValidateStrict(cfg); err != nil {
		t.Errorf("default config should validate: %v", err)
	}
}

// TestR083ManagedRequiresToken — TC-SIG-082
// Traces: ARQ-SIGNALS-R083 / TC-SIG-082
func TestR083ManagedRequiresToken(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Signals.Mode = config.ModeManaged
	cfg.Targets = []config.TargetConfig{
		{Name: "primary", Host: "h", DBName: "d", User: "u", Enabled: true, SSLMode: "verify-full"},
	}
	_, err := config.ValidateStrict(cfg)
	if err == nil {
		t.Fatal("expected validation error when mode=managed without token source")
	}
	if !strings.Contains(err.Error(), "no control-plane token is configured") {
		t.Errorf("error wording must point at the missing token: %v", err)
	}
}

// TestR083TokensMustDiffer — TC-SIG-083
// Traces: ARQ-SIGNALS-R083 / TC-SIG-083
func TestR083TokensMustDiffer(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Signals.Mode = config.ModeManaged
	cfg.Signals.ControlPlaneTokenFile = "/dev/null" // any non-empty source
	apiToken := strings.Repeat("a", 32)             // 32 chars, low entropy
	err := config.ValidateModeBTokens(cfg, apiToken, apiToken)
	if err == nil {
		t.Fatal("expected error when arq token equals api token")
	}
	if !strings.Contains(err.Error(), "must differ from api.token") {
		t.Errorf("error wording must call out the duplication: %v", err)
	}
}

// TestR083TokenLengthFloor — TC-SIG-084
// Traces: ARQ-SIGNALS-R083 / TC-SIG-084
func TestR083TokenLengthFloor(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Signals.Mode = config.ModeManaged
	cfg.Signals.ControlPlaneTokenFile = "/dev/null"
	apiToken := strings.Repeat("a", 32)
	short := strings.Repeat("b", config.MinControlPlaneTokenLength-1)
	err := config.ValidateModeBTokens(cfg, apiToken, short)
	if err == nil {
		t.Fatal("expected error for short control-plane token")
	}
	if !strings.Contains(err.Error(), "at least") {
		t.Errorf("error wording must reference the length floor: %v", err)
	}
}

// TestR083FileAndEnvMutuallyExclusive — TC-SIG-085
// Traces: ARQ-SIGNALS-R083 / TC-SIG-085
func TestR083FileAndEnvMutuallyExclusive(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Signals.Mode = config.ModeManaged
	cfg.Signals.ControlPlaneTokenFile = "/etc/foo"
	cfg.Signals.ControlPlaneTokenEnv = "BAR"
	cfg.Targets = []config.TargetConfig{
		{Name: "primary", Host: "h", DBName: "d", User: "u", Enabled: true, SSLMode: "verify-full"},
	}
	_, err := config.ValidateStrict(cfg)
	if err == nil {
		t.Fatal("expected validation error when both _file and _env are set")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error wording: %v", err)
	}
}

// ---------------------------------------------------------------------------
// R083: auth + actor end-to-end
// ---------------------------------------------------------------------------

// TestR083APITokenStaysLocalOperator — TC-SIG-086
// Traces: ARQ-SIGNALS-R083 / TC-SIG-086
func TestR083APITokenStaysLocalOperator(t *testing.T) {
	tokenFile := writeTokenFile(t, testCPToken)
	handler := makeR083Handler(t, config.ModeManaged, tokenFile)

	out := captureAuditLogs(t, func() {
		req := httptest.NewRequest("POST", "/collect/now", nil)
		req.Header.Set("Authorization", "Bearer "+testAPIToken)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202", w.Code)
		}
	})
	if !strings.Contains(out, "actor=local_operator") {
		t.Errorf("api token should produce actor=local_operator:\n%s", out)
	}
	if strings.Contains(out, "actor=control_plane") {
		t.Errorf("api token must never produce actor=control_plane:\n%s", out)
	}
}

// TestR083ControlPlaneTokenSetsControlPlaneActor — TC-SIG-087
// Traces: ARQ-SIGNALS-R083 / TC-SIG-087
func TestR083ControlPlaneTokenSetsControlPlaneActor(t *testing.T) {
	tokenFile := writeTokenFile(t, testCPToken)
	handler := makeR083Handler(t, config.ModeManaged, tokenFile)

	out := captureAuditLogs(t, func() {
		req := httptest.NewRequest("POST", "/collect/now", nil)
		req.Header.Set("Authorization", "Bearer "+testCPToken)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202; body=%s", w.Code, w.Body.String())
		}
	})
	if !strings.Contains(out, "actor=control_plane") {
		t.Errorf("control-plane token should produce actor=control_plane:\n%s", out)
	}
}

// TestR083StandaloneIgnoresControlPlaneToken — TC-SIG-088
// Traces: ARQ-SIGNALS-R083 / TC-SIG-088
func TestR083StandaloneIgnoresControlPlaneToken(t *testing.T) {
	// mode=standalone: even with a control-plane-token-shaped
	// secret in the request, only api.token is consulted. The
	// control-plane token is ignored at auth time.
	handler := makeR083Handler(t, config.ModeStandalone, "")

	req := httptest.NewRequest("POST", "/collect/now", nil)
	req.Header.Set("Authorization", "Bearer "+testCPToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("standalone mode + control-plane token = %d, want 401", w.Code)
	}
}

// TestR083UnknownTokenStill401 — TC-SIG-089
// Traces: ARQ-SIGNALS-R083 / TC-SIG-089
func TestR083UnknownTokenStill401(t *testing.T) {
	tokenFile := writeTokenFile(t, testCPToken)
	handler := makeR083Handler(t, config.ModeManaged, tokenFile)

	req := httptest.NewRequest("POST", "/collect/now", nil)
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("z", 32))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unknown token = %d, want 401", w.Code)
	}
}

// TestR083TokenRotation — TC-SIG-090
// Traces: ARQ-SIGNALS-R083 / TC-SIG-090
func TestR083TokenRotation(t *testing.T) {
	tokenFile := writeTokenFile(t, testCPToken)
	handler := makeR083Handler(t, config.ModeManaged, tokenFile)

	// First request: original token works.
	req := httptest.NewRequest("POST", "/collect/now", nil)
	req.Header.Set("Authorization", "Bearer "+testCPToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("pre-rotation status = %d, want 202", w.Code)
	}

	// Rotate: replace the file's contents with a new token.
	if err := os.WriteFile(tokenFile, []byte(rotatedCPToken+"\n"), 0o600); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	// Old token must no longer authenticate.
	req = httptest.NewRequest("POST", "/collect/now", nil)
	req.Header.Set("Authorization", "Bearer "+testCPToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("post-rotation, old token still works: %d", w.Code)
	}

	// New token authenticates without restart.
	req = httptest.NewRequest("POST", "/collect/now", nil)
	req.Header.Set("Authorization", "Bearer "+rotatedCPToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Errorf("post-rotation, new token failed: %d, body=%s", w.Code, w.Body.String())
	}
}

// TestR083ExportEventsCarryActor — TC-SIG-092
// Traces: ARQ-SIGNALS-R083 / TC-SIG-092
func TestR083ExportEventsCarryActor(t *testing.T) {
	tokenFile := writeTokenFile(t, testCPToken)
	handler := makeR083Handler(t, config.ModeManaged, tokenFile)

	out := captureAuditLogs(t, func() {
		req := httptest.NewRequest("GET", "/export", nil)
		req.Header.Set("Authorization", "Bearer "+testCPToken)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
	})

	if !strings.Contains(out, "audit_event=export_requested") {
		t.Errorf("missing export_requested event:\n%s", out)
	}
	if !strings.Contains(out, "audit_event=export_completed") {
		t.Errorf("missing export_completed event:\n%s", out)
	}
	// Both should carry actor=control_plane since the call used
	// the control-plane token.
	if strings.Count(out, "actor=control_plane") < 2 {
		t.Errorf("expected actor=control_plane on both export events:\n%s", out)
	}
}

// TestR083AuditLogsContainNoTokenValue — TC-SIG-091 (no-secret guard)
// Traces: ARQ-SIGNALS-R083 / TC-SIG-091 / INV-SIGNALS-07
func TestR083AuditLogsContainNoTokenValue(t *testing.T) {
	tokenFile := writeTokenFile(t, testCPToken)
	handler := makeR083Handler(t, config.ModeManaged, tokenFile)

	out := captureAuditLogs(t, func() {
		// One success and one failure to exercise both auth paths.
		for _, hdr := range []string{
			"Bearer " + testAPIToken,
			"Bearer " + testCPToken,
			"Bearer not-a-known-token",
		} {
			req := httptest.NewRequest("POST", "/collect/now", nil)
			req.Header.Set("Authorization", hdr)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}
	})

	for _, banned := range []string{
		testAPIToken,
		testCPToken,
		tokenFile, // path itself shouldn't surface either
	} {
		if strings.Contains(out, banned) {
			t.Errorf("audit stream leaked %q:\n%s", banned, out)
		}
	}
}
