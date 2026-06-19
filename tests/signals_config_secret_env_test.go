package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/elevarq/signals/internal/config"
)

// ---------------------------------------------------------------------------
// Env-var additions for secret-free-in-env operation:
//   - SIGNALS_API_TOKEN_FILE: API token via file (Docker secret).
//   - SIGNALS_TARGET_SSLROOTCERT_FILE: CA cert path for verify-full TLS.
//
// Rationale: the single-target-from-env path is the documented container
// convention; these two additions close the gap where the prod Docker
// example had to fall back to YAML or env-embedded secrets.
// ---------------------------------------------------------------------------

func TestConfigAPITokenFromFile(t *testing.T) {
	origDir, _ := os.Getwd()
	dir := t.TempDir()
	_ = os.Chdir(dir)
	defer func() { _ = os.Chdir(origDir) }()

	tokenPath := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenPath, []byte("s3cret-token"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	t.Setenv("SIGNALS_API_TOKEN_FILE", tokenPath)

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.API.APIToken != "s3cret-token" {
		t.Errorf("APIToken: got %q, want %q", cfg.API.APIToken, "s3cret-token")
	}
}

// Docker secret files conventionally end with a newline. It must be
// stripped so the token does not fail HTTP header-value validation.
func TestConfigAPITokenFileStripsTrailingNewline(t *testing.T) {
	origDir, _ := os.Getwd()
	dir := t.TempDir()
	_ = os.Chdir(dir)
	defer func() { _ = os.Chdir(origDir) }()

	tokenPath := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenPath, []byte("token-with-newline\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	t.Setenv("SIGNALS_API_TOKEN_FILE", tokenPath)

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.API.APIToken != "token-with-newline" {
		t.Errorf("APIToken: got %q, want %q (trailing newline must be stripped)",
			cfg.API.APIToken, "token-with-newline")
	}
}

// When both _FILE and the raw env are set, _FILE wins. Matches the
// convention used by the official postgres image (POSTGRES_PASSWORD_FILE
// takes precedence over POSTGRES_PASSWORD).
func TestConfigAPITokenFileOverridesEnv(t *testing.T) {
	origDir, _ := os.Getwd()
	dir := t.TempDir()
	_ = os.Chdir(dir)
	defer func() { _ = os.Chdir(origDir) }()

	tokenPath := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenPath, []byte("from-file"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	t.Setenv("SIGNALS_API_TOKEN", "from-env")
	t.Setenv("SIGNALS_API_TOKEN_FILE", tokenPath)

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.API.APIToken != "from-file" {
		t.Errorf("APIToken: got %q, want %q (file must take precedence over env)",
			cfg.API.APIToken, "from-file")
	}
}

// Baseline: the pre-existing SIGNALS_API_TOKEN path must still work
// for backward compatibility.
func TestConfigAPITokenFromEnvBaseline(t *testing.T) {
	origDir, _ := os.Getwd()
	dir := t.TempDir()
	_ = os.Chdir(dir)
	defer func() { _ = os.Chdir(origDir) }()

	t.Setenv("SIGNALS_API_TOKEN", "direct-env-token")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.API.APIToken != "direct-env-token" {
		t.Errorf("APIToken: got %q, want %q", cfg.API.APIToken, "direct-env-token")
	}
}

// A broken _FILE path is a hard error — silently falling back to env
// would mask a deployment mistake (someone expected file-based and got
// env-based instead).
func TestConfigAPITokenFileMissingIsError(t *testing.T) {
	origDir, _ := os.Getwd()
	dir := t.TempDir()
	_ = os.Chdir(dir)
	defer func() { _ = os.Chdir(origDir) }()

	t.Setenv("SIGNALS_API_TOKEN_FILE", filepath.Join(dir, "does-not-exist"))

	_, err := config.Load("")
	if err == nil {
		t.Fatal("Load: expected error for missing API token file, got nil")
	}
}

func TestConfigTargetSSLRootCertFileFromEnv(t *testing.T) {
	origDir, _ := os.Getwd()
	dir := t.TempDir()
	_ = os.Chdir(dir)
	defer func() { _ = os.Chdir(origDir) }()

	// The file does not need to exist — the collector reads it lazily
	// when it connects. We're only testing the config-surface wiring.
	t.Setenv("SIGNALS_TARGET_HOST", "pg.internal")
	t.Setenv("SIGNALS_TARGET_USER", "monitor")
	t.Setenv("SIGNALS_TARGET_SSLMODE", "verify-full")
	t.Setenv("SIGNALS_TARGET_SSLROOTCERT_FILE", "/etc/signals/ca.crt")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(cfg.Targets) != 1 {
		t.Fatalf("Targets: got %d, want 1", len(cfg.Targets))
	}
	if cfg.Targets[0].SSLRootCertFile != "/etc/signals/ca.crt" {
		t.Errorf("SSLRootCertFile: got %q, want %q",
			cfg.Targets[0].SSLRootCertFile, "/etc/signals/ca.crt")
	}
	if cfg.Targets[0].SSLMode != "verify-full" {
		t.Errorf("SSLMode: got %q, want %q", cfg.Targets[0].SSLMode, "verify-full")
	}
}
