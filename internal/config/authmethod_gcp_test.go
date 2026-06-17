package config

import (
	"strings"
	"testing"
)

// AC-GCP-001 (config facet) / keystone NFR003 — gcp_cloudsql_iam is a
// supported method whose effective value round-trips.
func TestEffectiveAuthMethodGCPCloudSQLIAM(t *testing.T) {
	tgt := baseValidTarget()
	tgt.AuthMethod = AuthMethodGCPCloudSQLIAM
	if got := tgt.EffectiveAuthMethod(); got != AuthMethodGCPCloudSQLIAM {
		t.Fatalf("EffectiveAuthMethod() = %q, want %q", got, AuthMethodGCPCloudSQLIAM)
	}
}

// FC-GCP-003 (keystone FC005) — gcp_cloudsql_iam + a stored password
// source is a hard config error naming the target.
func TestValidateRejectsGCPWithPasswordSource(t *testing.T) {
	for _, src := range []string{"password_file", "password_env", "pgpass_file"} {
		t.Run(src, func(t *testing.T) {
			tgt := baseValidTarget()
			tgt.AuthMethod = AuthMethodGCPCloudSQLIAM
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
				t.Fatalf("expected hard error for gcp_cloudsql_iam + %s, got nil", src)
			}
			if !strings.Contains(err.Error(), "passwordless") {
				t.Errorf("error should explain gcp_cloudsql_iam is passwordless; got: %v", err)
			}
		})
	}
}

// FC-GCP-004 (keystone FC006) — gcp_cloudsql_iam requires verify-full in
// every environment, including dev (direct-libpq path).
func TestValidateRejectsGCPWithoutVerifyFull(t *testing.T) {
	for _, mode := range []string{"", "require", "prefer", "verify-ca"} {
		t.Run("sslmode="+mode, func(t *testing.T) {
			tgt := baseValidTarget()
			tgt.AuthMethod = AuthMethodGCPCloudSQLIAM
			tgt.SSLMode = mode
			_, err := ValidateStrict(baseValidConfig(tgt))
			if err == nil {
				t.Fatalf("expected hard error for gcp_cloudsql_iam + sslmode=%q, got nil", mode)
			}
			if !strings.Contains(err.Error(), "verify-full") {
				t.Errorf("error should require verify-full; got: %v", err)
			}
		})
	}
}

// FC-GCP-004 negative — verify-full passes, and a missing
// gcp_impersonate_service_account must NOT warn (ambient ADC is the common
// case).
func TestValidateAcceptsGCPVerifyFull(t *testing.T) {
	tgt := baseValidTarget()
	tgt.AuthMethod = AuthMethodGCPCloudSQLIAM
	tgt.SSLMode = "verify-full"
	tgt.SSLRootCertFile = "/etc/ca.pem"
	warnings, err := ValidateStrict(baseValidConfig(tgt))
	if err != nil {
		t.Fatalf("expected clean validation for gcp_cloudsql_iam + verify-full, got: %v", err)
	}
	for _, w := range warnings {
		if strings.Contains(w, "impersonate") || strings.Contains(w, "identity") {
			t.Errorf("missing impersonation SA must not warn at startup; got: %q", w)
		}
	}
}

// gcp_cloudsql_iam accepts an optional impersonation service account.
func TestValidateAcceptsGCPWithImpersonation(t *testing.T) {
	tgt := baseValidTarget()
	tgt.AuthMethod = AuthMethodGCPCloudSQLIAM
	tgt.SSLMode = "verify-full"
	tgt.SSLRootCertFile = "/etc/ca.pem"
	tgt.GCPImpersonateServiceAccount = "collector@my-proj.iam.gserviceaccount.com"
	if _, err := ValidateStrict(baseValidConfig(tgt)); err != nil {
		t.Fatalf("gcp_cloudsql_iam + impersonation SA should validate clean, got: %v", err)
	}
}
