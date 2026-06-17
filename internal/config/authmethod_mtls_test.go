package config

import (
	"strings"
	"testing"
)

// mtlsTarget returns a base mtls target that passes the generic checks, so
// each test mutates only the field under examination.
func mtlsTarget() TargetConfig {
	tgt := baseValidTarget()
	tgt.AuthMethod = AuthMethodMTLS
	tgt.SSLMode = "verify-full"
	tgt.SSLRootCertFile = "/etc/ca.pem"
	tgt.SSLCert = "/etc/arq/client.crt"
	tgt.SSLKey = "/etc/arq/client.key"
	return tgt
}

// keystone NFR003 — mtls is a supported method whose effective value
// round-trips.
func TestEffectiveAuthMethodMTLS(t *testing.T) {
	tgt := baseValidTarget()
	tgt.AuthMethod = AuthMethodMTLS
	if got := tgt.EffectiveAuthMethod(); got != AuthMethodMTLS {
		t.Fatalf("EffectiveAuthMethod() = %q, want %q", got, AuthMethodMTLS)
	}
}

// AC-MTLS-001 (config facet) — a well-formed mtls target validates clean.
func TestValidateAcceptsMTLS(t *testing.T) {
	if _, err := ValidateStrict(baseValidConfig(mtlsTarget())); err != nil {
		t.Fatalf("a well-formed mtls target should validate clean, got: %v", err)
	}
}

// AC-MTLS-004 / FC-MTLS-001 — mtls requires both sslcert and sslkey.
func TestValidateRejectsMTLSWithoutCertOrKey(t *testing.T) {
	cases := map[string]func(*TargetConfig){
		"no sslcert": func(t *TargetConfig) { t.SSLCert = "" },
		"no sslkey":  func(t *TargetConfig) { t.SSLKey = "" },
		"neither":    func(t *TargetConfig) { t.SSLCert = ""; t.SSLKey = "" },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			tgt := mtlsTarget()
			mut(&tgt)
			_, err := ValidateStrict(baseValidConfig(tgt))
			if err == nil {
				t.Fatalf("expected hard error for mtls %s, got nil", name)
			}
			if !strings.Contains(err.Error(), "sslcert") || !strings.Contains(err.Error(), "sslkey") {
				t.Errorf("error should name sslcert and sslkey; got: %v", err)
			}
		})
	}
}

// AC-MTLS-005 / FC-MTLS-004 — mtls requires verify-full in every environment.
func TestValidateRejectsMTLSWithoutVerifyFull(t *testing.T) {
	for _, mode := range []string{"", "require", "prefer", "verify-ca"} {
		t.Run("sslmode="+mode, func(t *testing.T) {
			tgt := mtlsTarget()
			tgt.SSLMode = mode
			_, err := ValidateStrict(baseValidConfig(tgt))
			if err == nil {
				t.Fatalf("expected hard error for mtls sslmode=%q, got nil", mode)
			}
			if !strings.Contains(err.Error(), "verify-full") {
				t.Errorf("error should require verify-full; got: %v", err)
			}
		})
	}
}

// AC-MTLS-006 / FC-MTLS-005 — mtls + any inline password source is a hard
// config error; authentication is by client certificate.
func TestValidateRejectsMTLSWithPasswordSource(t *testing.T) {
	for _, src := range []string{"password_file", "password_env", "pgpass_file"} {
		t.Run(src, func(t *testing.T) {
			tgt := mtlsTarget()
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
				t.Fatalf("expected hard error for mtls + %s, got nil", src)
			}
			if !strings.Contains(err.Error(), "certificate") {
				t.Errorf("error should explain auth is by client certificate; got: %v", err)
			}
		})
	}
}
