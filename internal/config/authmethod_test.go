package config

import (
	"strings"
	"testing"
	"time"
)

// baseValidTarget returns a target that passes the generic ValidateStrict
// checks (name/host/user/dbname present), so a test can mutate only the
// auth-method fields and isolate the credential-provider validation.
func baseValidTarget() TargetConfig {
	return TargetConfig{
		Name:    "t1",
		Host:    "db.example.com",
		Port:    5432,
		DBName:  "appdb",
		User:    "monitor",
		Enabled: true,
	}
}

// baseValidConfig wraps a target in an otherwise-valid Config so the only
// validation outcomes come from the target under test.
func baseValidConfig(tgt TargetConfig) Config {
	return Config{
		Env:      "dev",
		Database: DatabaseConfig{Path: "/tmp/signals-test.db"},
		API:      APIConfig{ListenAddr: "127.0.0.1:8081"},
		Signals: SignalsConfig{
			PollInterval:        time.Minute,
			TargetTimeout:       60 * time.Second,
			QueryTimeout:        10 * time.Second,
			MinSnapshotInterval: 60 * time.Second,
		},
		Targets: []TargetConfig{tgt},
	}
}

// AC-AWS-001 / keystone NFR003 — a target with no auth_method behaves
// exactly as a password target and validates clean.
func TestEffectiveAuthMethodDefaultsToPassword(t *testing.T) {
	tgt := baseValidTarget()
	if got := tgt.EffectiveAuthMethod(); got != AuthMethodPassword {
		t.Fatalf("empty auth_method: EffectiveAuthMethod() = %q, want %q", got, AuthMethodPassword)
	}
	tgt.AuthMethod = AuthMethodAWSRDSIAM
	if got := tgt.EffectiveAuthMethod(); got != AuthMethodAWSRDSIAM {
		t.Fatalf("EffectiveAuthMethod() = %q, want %q", got, AuthMethodAWSRDSIAM)
	}
}

// FC-AWS-003 (keystone FC005) — token method + a stored password source
// is a hard config error naming the target.
func TestValidateRejectsAWSWithPasswordSource(t *testing.T) {
	for _, src := range []string{"password_file", "password_env", "pgpass_file"} {
		t.Run(src, func(t *testing.T) {
			tgt := baseValidTarget()
			tgt.AuthMethod = AuthMethodAWSRDSIAM
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
				t.Fatalf("expected hard error for aws_rds_iam + %s, got nil", src)
			}
			if !strings.Contains(err.Error(), "passwordless") {
				t.Errorf("error should explain aws_rds_iam is passwordless; got: %v", err)
			}
		})
	}
}

// FC-AWS-004 (keystone FC006) — token method requires verify-full in
// every environment, including dev.
func TestValidateRejectsAWSWithoutVerifyFull(t *testing.T) {
	for _, mode := range []string{"", "require", "prefer", "verify-ca"} {
		t.Run("sslmode="+mode, func(t *testing.T) {
			tgt := baseValidTarget()
			tgt.AuthMethod = AuthMethodAWSRDSIAM
			tgt.SSLMode = mode
			tgt.Region = "us-east-1"
			_, err := ValidateStrict(baseValidConfig(tgt))
			if err == nil {
				t.Fatalf("expected hard error for aws_rds_iam + sslmode=%q, got nil", mode)
			}
			if !strings.Contains(err.Error(), "verify-full") {
				t.Errorf("error should require verify-full; got: %v", err)
			}
		})
	}
}

// FC-AWS-004 negative — verify-full passes.
func TestValidateAcceptsAWSVerifyFull(t *testing.T) {
	tgt := baseValidTarget()
	tgt.AuthMethod = AuthMethodAWSRDSIAM
	tgt.SSLMode = "verify-full"
	tgt.SSLRootCertFile = "/etc/ca.pem"
	tgt.Region = "us-east-1"
	warnings, err := ValidateStrict(baseValidConfig(tgt))
	if err != nil {
		t.Fatalf("expected clean validation for aws_rds_iam + verify-full + region, got: %v", err)
	}
	for _, w := range warnings {
		if strings.Contains(w, "region") {
			t.Errorf("unexpected region warning when region is set: %q", w)
		}
	}
}

// Resolved region decision — missing region (config + env) is a startup
// WARNING, never a hard error.
func TestValidateWarnsButDoesNotFailOnMissingAWSRegion(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	tgt := baseValidTarget()
	tgt.AuthMethod = AuthMethodAWSRDSIAM
	tgt.SSLMode = "verify-full"
	tgt.SSLRootCertFile = "/etc/ca.pem"
	warnings, err := ValidateStrict(baseValidConfig(tgt))
	if err != nil {
		t.Fatalf("missing region must not be a hard error (fail-soft); got: %v", err)
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "region") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a region warning, got warnings: %v", warnings)
	}
}

// Keystone FC001 — an auth_method this build does not implement is a
// hard error that names the supported set.
func TestValidateRejectsUnsupportedAuthMethod(t *testing.T) {
	tgt := baseValidTarget()
	tgt.AuthMethod = "kerberos" // not a recognised auth_method in any build
	_, err := ValidateStrict(baseValidConfig(tgt))
	if err == nil {
		t.Fatalf("expected hard error for unsupported auth_method, got nil")
	}
	if !strings.Contains(err.Error(), "kerberos") || !strings.Contains(err.Error(), AuthMethodAWSRDSIAM) {
		t.Errorf("error should name the bad method and the supported set; got: %v", err)
	}
}
