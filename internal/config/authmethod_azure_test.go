package config

import (
	"strings"
	"testing"
)

// AC-AZURE-001 (config facet) / keystone NFR003 — azure_entra is a
// supported method whose effective value round-trips.
func TestEffectiveAuthMethodAzureEntra(t *testing.T) {
	tgt := baseValidTarget()
	tgt.AuthMethod = AuthMethodAzureEntra
	if got := tgt.EffectiveAuthMethod(); got != AuthMethodAzureEntra {
		t.Fatalf("EffectiveAuthMethod() = %q, want %q", got, AuthMethodAzureEntra)
	}
}

// FC-AZURE-003 (keystone FC005) — azure_entra + a stored password source
// is a hard config error naming the target.
func TestValidateRejectsAzureWithPasswordSource(t *testing.T) {
	for _, src := range []string{"password_file", "password_env", "pgpass_file"} {
		t.Run(src, func(t *testing.T) {
			tgt := baseValidTarget()
			tgt.AuthMethod = AuthMethodAzureEntra
			tgt.SSLMode = "verify-full"
			tgt.SSLRootCertFile = "/etc/ca.pem"
			switch src {
			case "password_file":
				tgt.PasswordFile = "/etc/pw"
			case "password_env":
				tgt.PasswordEnv = "PW"
			case "pgpass_file":
				tgt.PgpassFile = "/etc/pgpass"
			}
			_, err := ValidateStrict(baseValidConfig(tgt))
			if err == nil {
				t.Fatalf("expected hard error for azure_entra + %s, got nil", src)
			}
			if !strings.Contains(err.Error(), "passwordless") {
				t.Errorf("error should explain azure_entra is passwordless; got: %v", err)
			}
		})
	}
}

// FC-AZURE-004 (keystone FC006) — azure_entra requires verify-full in
// every environment, including dev.
func TestValidateRejectsAzureWithoutVerifyFull(t *testing.T) {
	for _, mode := range []string{"", "require", "prefer", "verify-ca"} {
		t.Run("sslmode="+mode, func(t *testing.T) {
			tgt := baseValidTarget()
			tgt.AuthMethod = AuthMethodAzureEntra
			tgt.SSLMode = mode
			_, err := ValidateStrict(baseValidConfig(tgt))
			if err == nil {
				t.Fatalf("expected hard error for azure_entra + sslmode=%q, got nil", mode)
			}
			if !strings.Contains(err.Error(), "verify-full") {
				t.Errorf("error should require verify-full; got: %v", err)
			}
		})
	}
}

// FC-AZURE-004 negative — verify-full passes, and no client_id is required
// (a missing azure_client_id must NOT produce a warning: single-identity
// and system-assigned hosts are the common case).
func TestValidateAcceptsAzureVerifyFull(t *testing.T) {
	tgt := baseValidTarget()
	tgt.AuthMethod = AuthMethodAzureEntra
	tgt.SSLMode = "verify-full"
	tgt.SSLRootCertFile = "/etc/ca.pem"
	warnings, err := ValidateStrict(baseValidConfig(tgt))
	if err != nil {
		t.Fatalf("expected clean validation for azure_entra + verify-full, got: %v", err)
	}
	for _, w := range warnings {
		if strings.Contains(w, "client_id") || strings.Contains(w, "identity") {
			t.Errorf("missing azure_client_id must not warn at startup; got: %q", w)
		}
	}
}

// azure_entra accepts an optional azure_client_id without complaint.
func TestValidateAcceptsAzureWithClientID(t *testing.T) {
	tgt := baseValidTarget()
	tgt.AuthMethod = AuthMethodAzureEntra
	tgt.SSLMode = "verify-full"
	tgt.SSLRootCertFile = "/etc/ca.pem"
	tgt.AzureClientID = "11111111-2222-3333-4444-555555555555"
	if _, err := ValidateStrict(baseValidConfig(tgt)); err != nil {
		t.Fatalf("azure_entra + azure_client_id should validate clean, got: %v", err)
	}
}
