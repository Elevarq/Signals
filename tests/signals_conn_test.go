package tests

import (
	"testing"

	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/config"
)

// TestBuildConnConfigValid verifies that BuildConnConfig creates a valid pgx.ConnConfig
// from a TargetConfig with host, port, dbname, and user.
// Traces: ARQ-SIGNALS-R001 / TC-SIG-001
func TestBuildConnConfigValid(t *testing.T) {
	tgt := config.TargetConfig{
		Name:   "test-target",
		Host:   "db.example.com",
		Port:   5433,
		DBName: "mydb",
		User:   "monitor",
		// No password source => peer/trust auth, ResolvePassword returns "".
	}

	cfg, err := collector.BuildConnConfig(tgt)
	if err != nil {
		t.Fatalf("BuildConnConfig returned error: %v", err)
	}

	if cfg.Host != "db.example.com" {
		t.Errorf("Host = %q, want %q", cfg.Host, "db.example.com")
	}
	if cfg.Port != 5433 {
		t.Errorf("Port = %d, want %d", cfg.Port, 5433)
	}
	if cfg.Database != "mydb" {
		t.Errorf("Database = %q, want %q", cfg.Database, "mydb")
	}
	if cfg.User != "monitor" {
		t.Errorf("User = %q, want %q", cfg.User, "monitor")
	}
}

// TestBuildConnConfigApplicationName verifies that BuildConnConfig sets application_name
// to "arq-signals" in the runtime parameters.
// Traces: ARQ-SIGNALS-R001 / TC-SIG-001
func TestBuildConnConfigApplicationName(t *testing.T) {
	tgt := config.TargetConfig{
		Name:   "test-target",
		Host:   "localhost",
		Port:   5432,
		DBName: "postgres",
		User:   "arq",
	}

	cfg, err := collector.BuildConnConfig(tgt)
	if err != nil {
		t.Fatalf("BuildConnConfig returned error: %v", err)
	}

	appName, ok := cfg.RuntimeParams["application_name"]
	if !ok {
		t.Fatal("application_name not set in RuntimeParams")
	}
	if appName != "arq-signals" {
		t.Errorf("application_name = %q, want %q", appName, "arq-signals")
	}
}

// TestBuildConnConfigDefaultPort verifies that BuildConnConfig defaults to port 5432
// when the target config has port == 0.
// Traces: ARQ-SIGNALS-R001 / TC-SIG-001
func TestBuildConnConfigDefaultPort(t *testing.T) {
	tgt := config.TargetConfig{
		Name:   "test-target",
		Host:   "localhost",
		Port:   0, // should default to 5432
		DBName: "postgres",
		User:   "arq",
	}

	cfg, err := collector.BuildConnConfig(tgt)
	if err != nil {
		t.Fatalf("BuildConnConfig returned error: %v", err)
	}

	if cfg.Port != 5432 {
		t.Errorf("Port = %d, want default 5432", cfg.Port)
	}
}

// TestBuildConnConfigEmptyHostError verifies that BuildConnConfig returns an error
// when the host is empty.
// Traces: ARQ-SIGNALS-R001 / TC-SIG-001
func TestBuildConnConfigEmptyHostError(t *testing.T) {
	tgt := config.TargetConfig{
		Name:   "bad-target",
		Host:   "",
		Port:   5432,
		DBName: "postgres",
		User:   "arq",
	}

	_, err := collector.BuildConnConfig(tgt)
	if err == nil {
		t.Fatal("expected error for empty host, got nil")
	}
}

// TestBuildConnConfigReadOnlyParam verifies that BuildConnConfig sets
// default_transaction_read_only=on in runtime parameters.
// Traces: ARQ-SIGNALS-R013 / TC-SIG-019
func TestBuildConnConfigReadOnlyParam(t *testing.T) {
	tgt := config.TargetConfig{
		Name:   "ro-target",
		Host:   "localhost",
		Port:   5432,
		DBName: "postgres",
		User:   "arq",
	}

	cfg, err := collector.BuildConnConfig(tgt)
	if err != nil {
		t.Fatalf("BuildConnConfig returned error: %v", err)
	}

	val, ok := cfg.RuntimeParams["default_transaction_read_only"]
	if !ok {
		t.Fatal("default_transaction_read_only not set in RuntimeParams")
	}
	if val != "on" {
		t.Errorf("default_transaction_read_only = %q, want %q", val, "on")
	}
}

// TestBuildConnConfigEscapesUnsafeFields verifies that values containing
// spaces, equals signs, single quotes, or backslashes are preserved verbatim
// rather than being parsed as additional connection options. This guards
// against connection-string injection from operator-controlled fields.
// Traces: ARQ-SIGNALS-R024
func TestBuildConnConfigEscapesUnsafeFields(t *testing.T) {
	tgt := config.TargetConfig{
		Name:    "injection-test",
		Host:    "db.example.com",
		Port:    5432,
		DBName:  "mydb sslmode=disable", // attempt to smuggle a setting
		User:    "user'with\\weird=chars",
		SSLMode: "verify-full",
	}

	cfg, err := collector.BuildConnConfig(tgt)
	if err != nil {
		t.Fatalf("BuildConnConfig returned error: %v", err)
	}

	if cfg.Database != "mydb sslmode=disable" {
		t.Errorf("Database = %q, want literal %q", cfg.Database, "mydb sslmode=disable")
	}
	if cfg.User != "user'with\\weird=chars" {
		t.Errorf("User = %q, want literal %q", cfg.User, "user'with\\weird=chars")
	}
	// sslmode set on the target should win over a smuggled value.
	if cfg.TLSConfig == nil {
		t.Error("TLSConfig should be set when sslmode=verify-full")
	}
}
