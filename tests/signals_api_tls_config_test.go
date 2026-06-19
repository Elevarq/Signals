package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/config"
)

// TC-SIG-128 — R113: API TLS is all-or-nothing. ValidateStrict must
// hard-error when exactly one of api.tls_cert_file / api.tls_key_file
// is set, and accept the both-set and both-empty cases.

func tlsBaseConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Database.Path = "/tmp/test.db"
	cfg.API.ListenAddr = "0.0.0.0:8081"
	return cfg
}

func TestValidateStrict_TLSBothEmptyIsLegal(t *testing.T) {
	cfg := tlsBaseConfig(t)
	cfg.API.TLSCertFile = ""
	cfg.API.TLSKeyFile = ""

	if _, err := config.ValidateStrict(cfg); err != nil {
		if strings.Contains(err.Error(), "tls") {
			t.Errorf("plain HTTP (no TLS files) must be legal; got: %v", err)
		}
	}
}

func TestValidateStrict_TLSBothSetIsLegal(t *testing.T) {
	cfg := tlsBaseConfig(t)
	cfg.API.TLSCertFile = "/etc/signals/tls/server.crt"
	cfg.API.TLSKeyFile = "/etc/signals/tls/server.key"

	if _, err := config.ValidateStrict(cfg); err != nil {
		if strings.Contains(err.Error(), "tls") {
			t.Errorf("both TLS files set must be legal at config-validation time; got: %v", err)
		}
	}
}

func TestValidateStrict_TLSOnlyCertIsHardError(t *testing.T) {
	cfg := tlsBaseConfig(t)
	cfg.API.TLSCertFile = "/etc/signals/tls/server.crt"
	cfg.API.TLSKeyFile = ""

	_, err := config.ValidateStrict(cfg)
	if err == nil {
		t.Fatal("setting only api.tls_cert_file must be a hard error")
	}
	if !strings.Contains(err.Error(), "tls_cert_file") || !strings.Contains(err.Error(), "tls_key_file") {
		t.Errorf("error should name both TLS fields; got: %v", err)
	}
}

func TestValidateStrict_TLSOnlyKeyIsHardError(t *testing.T) {
	cfg := tlsBaseConfig(t)
	cfg.API.TLSCertFile = ""
	cfg.API.TLSKeyFile = "/etc/signals/tls/server.key"

	_, err := config.ValidateStrict(cfg)
	if err == nil {
		t.Fatal("setting only api.tls_key_file must be a hard error")
	}
	if !strings.Contains(err.Error(), "tls_cert_file") || !strings.Contains(err.Error(), "tls_key_file") {
		t.Errorf("error should name both TLS fields; got: %v", err)
	}
}

// TC-SIG-128 — R113: env overrides populate the TLS fields (exercised
// through the exported Load path).
func TestLoad_TLSEnvOverrides(t *testing.T) {
	t.Setenv("SIGNALS_API_TLS_CERT_FILE", "/run/tls/server.crt")
	t.Setenv("SIGNALS_API_TLS_KEY_FILE", "/run/tls/server.key")

	// Load with no config file: defaults + env overrides only.
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.API.TLSCertFile != "/run/tls/server.crt" {
		t.Errorf("TLSCertFile from env: got %q", cfg.API.TLSCertFile)
	}
	if cfg.API.TLSKeyFile != "/run/tls/server.key" {
		t.Errorf("TLSKeyFile from env: got %q", cfg.API.TLSKeyFile)
	}
}
