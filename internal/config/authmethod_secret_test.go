package config

import (
	"strings"
	"testing"
)

// awsSecretRef is a syntactically valid AWS Secrets Manager ARN used across
// the secret_store config-validation tests.
const awsSecretRef = "arn:aws:secretsmanager:eu-west-1:123456789012:secret:prod/pg/monitor-AbCdEf"

// secretStoreTarget returns a base secret_store target that passes the
// generic checks, so each test mutates only the field under examination.
func secretStoreTarget() TargetConfig {
	tgt := baseValidTarget()
	tgt.AuthMethod = AuthMethodSecretStore
	tgt.SSLMode = "verify-full"
	tgt.SSLRootCertFile = "/etc/ca.pem"
	tgt.SecretRef = awsSecretRef
	return tgt
}

// AC-SECRET-001 (config facet) / keystone NFR003 — secret_store is a
// supported method whose effective value round-trips.
func TestEffectiveAuthMethodSecretStore(t *testing.T) {
	tgt := baseValidTarget()
	tgt.AuthMethod = AuthMethodSecretStore
	if got := tgt.EffectiveAuthMethod(); got != AuthMethodSecretStore {
		t.Fatalf("EffectiveAuthMethod() = %q, want %q", got, AuthMethodSecretStore)
	}
}

// AC-SECRET-001 negative path control — a well-formed secret_store target
// validates clean (no inline password, verify-full, valid ref).
func TestValidateAcceptsSecretStore(t *testing.T) {
	if _, err := ValidateStrict(baseValidConfig(secretStoreTarget())); err != nil {
		t.Fatalf("a well-formed secret_store target should validate clean, got: %v", err)
	}
}

// AC-SECRET-005 / FC-SECRET-005 — secret_store + any inline password source
// is a hard config error; the password must come only from the vault.
func TestValidateRejectsSecretStoreWithPasswordSource(t *testing.T) {
	for _, src := range []string{"password_file", "password_env", "pgpass_file"} {
		t.Run(src, func(t *testing.T) {
			tgt := secretStoreTarget()
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
				t.Fatalf("expected hard error for secret_store + %s, got nil", src)
			}
			if !strings.Contains(err.Error(), "vault") {
				t.Errorf("error should explain the password comes from the vault; got: %v", err)
			}
		})
	}
}

// AC-SECRET-006 / FC-SECRET-006 — secret_store requires verify-full in every
// environment, including dev.
func TestValidateRejectsSecretStoreWithoutVerifyFull(t *testing.T) {
	for _, mode := range []string{"", "require", "prefer", "verify-ca"} {
		t.Run("sslmode="+mode, func(t *testing.T) {
			tgt := secretStoreTarget()
			tgt.SSLMode = mode
			tgt.SSLRootCertFile = "/etc/ca.pem"
			_, err := ValidateStrict(baseValidConfig(tgt))
			if err == nil {
				t.Fatalf("expected hard error for secret_store + sslmode=%q, got nil", mode)
			}
			if !strings.Contains(err.Error(), "verify-full") {
				t.Errorf("error should require verify-full; got: %v", err)
			}
		})
	}
}

// AC-SECRET-007 / FC-SECRET-007 — secret_store without secret_ref aborts
// startup with an actionable error.
func TestValidateRejectsSecretStoreWithoutRef(t *testing.T) {
	tgt := secretStoreTarget()
	tgt.SecretRef = ""
	_, err := ValidateStrict(baseValidConfig(tgt))
	if err == nil {
		t.Fatalf("expected hard error for secret_store without secret_ref, got nil")
	}
	if !strings.Contains(err.Error(), "secret_ref") {
		t.Errorf("error should name the missing secret_ref; got: %v", err)
	}
}

// AC-SECRET-002 / FC-SECRET-007 (config facet) — secret_store with a
// secret_ref that matches no accepted shape aborts startup.
func TestValidateRejectsSecretStoreWithBadRef(t *testing.T) {
	tgt := secretStoreTarget()
	tgt.SecretRef = "not-a-valid-reference"
	_, err := ValidateStrict(baseValidConfig(tgt))
	if err == nil {
		t.Fatalf("expected hard error for secret_store with an unrecognised secret_ref, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "arn") {
		t.Errorf("error should name the accepted forms; got: %v", err)
	}
}

// secret_store accepts an optional secret_json_key without complaint.
func TestValidateAcceptsSecretStoreWithJSONKey(t *testing.T) {
	tgt := secretStoreTarget()
	tgt.SecretJSONKey = "password"
	if _, err := ValidateStrict(baseValidConfig(tgt)); err != nil {
		t.Fatalf("secret_store + secret_json_key should validate clean, got: %v", err)
	}
}
