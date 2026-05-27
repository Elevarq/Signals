package safety

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// captureLogs swaps slog's default logger for a buffered handler for the
// duration of the test, returning the captured text output.
func captureLogs(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(prev)
	fn()
	return buf.String()
}

// TestAuditLogEmitsEventKey verifies that AuditLog produces an audit_event
// attribute carrying the event name. Downstream tooling filters on this
// stable key.
// Traces: ARQ-SIGNALS-R078
func TestAuditLogEmitsEventKey(t *testing.T) {
	out := captureLogs(t, func() {
		AuditLog("config_validated", "status", "ok", "warnings", 0)
	})
	if !strings.Contains(out, "audit_event=config_validated") {
		t.Errorf("expected audit_event=config_validated in output, got:\n%s", out)
	}
	if !strings.Contains(out, "status=ok") {
		t.Errorf("expected status=ok, got:\n%s", out)
	}
}

// TestAuditLogDropsForbiddenAttributes verifies that the centralized
// denylist filters attributes whose key suggests a secret. This is the
// belt-and-braces guarantee for R078's "no secrets in audit events"
// invariant — even if a future call site forgets the contract.
// Traces: ARQ-SIGNALS-R078
func TestAuditLogDropsForbiddenAttributes(t *testing.T) {
	out := captureLogs(t, func() {
		AuditLog("export_completed",
			"status", "success",
			"password", "shhh",
			"api_token", "tok-abcdef",
			"connection_string", "host=db user=monitor password=hunter2",
			"dsn", "postgres://u:p@host/db",
			"query_result", []byte("row1\nrow2"),
			"size_bytes", 42,
		)
	})

	for _, banned := range []string{
		"password=shhh", "api_token=", "tok-abcdef",
		"connection_string=", "hunter2",
		"dsn=", "postgres://u:p", "query_result=",
		"row1", "row2",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("forbidden value %q leaked into audit log:\n%s", banned, out)
		}
	}
	// Non-sensitive attrs survive.
	if !strings.Contains(out, "size_bytes=42") {
		t.Errorf("expected size_bytes=42 to survive filter, got:\n%s", out)
	}
}

// TestAuditLogIgnoresNonStringKeys verifies that non-string keys (which
// would not match the slog kv contract anyway) are silently dropped
// instead of panicking.
// Traces: ARQ-SIGNALS-R078
func TestAuditLogIgnoresNonStringKeys(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("AuditLog panicked on non-string key: %v", r)
		}
	}()
	AuditLog("test_event", 12345, "value", "ok", "real_key", "ok")
}

// TestIsDeniedAuditKey verifies the substring match: a denied prefix
// anywhere in the key matches.
// Traces: ARQ-SIGNALS-R078
func TestIsDeniedAuditKey(t *testing.T) {
	denied := []string{
		"password",
		"db_password",
		"api_token",
		"BEARER_TOKEN",
		"Connection_String",
		"target_dsn",
		"query_result_payload",
	}
	for _, k := range denied {
		if !isDeniedAuditKey(k) {
			t.Errorf("expected %q to be denied", k)
		}
	}

	allowed := []string{
		"target", "status", "duration_ms", "size_bytes",
		"snapshot_id", "instance_id", "source_ip",
		"collectors_total", "collectors_failed",
	}
	for _, k := range allowed {
		if isDeniedAuditKey(k) {
			t.Errorf("expected %q to be allowed", k)
		}
	}
}
