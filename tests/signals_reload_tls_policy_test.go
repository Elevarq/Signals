package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/config"
)

// F05 / #711 / SIGNALS-R130: every path that installs effective config
// (startup, POST /reload, SIGHUP, doctor) must run the SAME authoritative
// validation — ValidateStrict AND ValidateProdTLS — via config.ValidateEffective,
// under the running immutable environment policy. Before R130 the reload paths
// ran ValidateStrict only, so an env=prod + password-auth target edited to
// sslmode=disable was accepted and installed, defeating the production TLS policy.

func prodTLSTarget(mode string) config.TargetConfig {
	return config.TargetConfig{
		Name: "primary", Host: "db", Port: 5432, DBName: "app", User: "u",
		SSLMode: mode, Enabled: true,
	}
}

// Startup under env=prod rejects a weak sslmode — the baseline ValidateEffective
// must be at least as strict as the startup ValidateProdTLS it replaces.
func TestValidateEffective_StartupProdRejectsWeakTLS(t *testing.T) {
	for _, mode := range []string{"disable", "allow", "prefer", "require"} {
		t.Run(mode, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Env = "prod"
			cfg.Targets = []config.TargetConfig{prodTLSTarget(mode)}
			if _, err := config.ValidateEffective("prod", cfg); err == nil {
				t.Errorf("ValidateEffective accepted env=prod sslmode=%q — must enforce prod TLS policy", mode)
			}
		})
	}
}

// The core F05 fix: a reload whose config file downgrades env prod->dev must
// STILL be validated under the running prod policy, so the insecure-TLS edit
// is rejected. A ValidateStrict-only path (or one that trusted the edited env)
// would accept this.
func TestValidateEffective_ProdEnvDowngradeAtReloadRejectsWeakTLS(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Env = "dev" // operator edited the file down from prod
	cfg.Targets = []config.TargetConfig{prodTLSTarget("disable")}

	// ValidateStrict alone accepts it (proves the gap exists)...
	if _, err := config.ValidateStrict(cfg); err != nil {
		t.Fatalf("precondition: ValidateStrict should accept the downgraded-env config, got %v", err)
	}
	// ...but under the running prod policy, ValidateEffective must reject.
	if _, err := config.ValidateEffective("prod", cfg); err == nil {
		t.Fatal("ValidateEffective honoured the downgraded env=dev and accepted sslmode=disable — a reload must not relax the running prod TLS policy")
	}
}

// verify-ca/verify-full without a CA file is rejected on every install path.
func TestValidateEffective_VerifyWithoutCARejected(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Env = "prod"
	cfg.Targets = []config.TargetConfig{prodTLSTarget("verify-full")} // no SSLRootCertFile
	if _, err := config.ValidateEffective("prod", cfg); err == nil {
		t.Fatal("ValidateEffective accepted verify-full without sslrootcert_file")
	}
}

// A genuinely valid prod config (strong TLS + CA) passes unchanged — the fix
// rejects insecure edits without blocking legitimate reloads.
func TestValidateEffective_ValidProdPasses(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Env = "prod"
	tgt := prodTLSTarget("verify-full")
	tgt.SSLRootCertFile = "/etc/ssl/certs/ca.pem"
	cfg.Targets = []config.TargetConfig{tgt}
	if _, err := config.ValidateEffective("prod", cfg); err != nil {
		t.Fatalf("ValidateEffective rejected a valid prod config: %v", err)
	}
}

// Raising the environment at reload (dev running -> prod in the file) is
// honoured: the stricter policy applies, so a weak target is rejected.
func TestValidateEffective_EnvRaiseHonoured(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Env = "prod"
	cfg.Targets = []config.TargetConfig{prodTLSTarget("require")}
	if _, err := config.ValidateEffective("dev", cfg); err == nil {
		t.Fatal("ValidateEffective ignored the raised env=prod and accepted sslmode=require")
	}
}

// ValidateEffective still surfaces ValidateStrict hard errors (it is the
// authoritative superset, not a replacement that drops strict checks).
func TestValidateEffective_PropagatesStrictErrors(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Env = "prod"
	cfg.Targets = []config.TargetConfig{prodTLSTarget("secure")} // invalid libpq enum
	_, err := config.ValidateEffective("prod", cfg)
	if err == nil {
		t.Fatal("ValidateEffective accepted an invalid sslmode enum — strict checks must still run")
	}
	if !strings.Contains(err.Error(), "secure") {
		t.Errorf("error does not name the offending strict value: %v", err)
	}
}
