package config

import "testing"

// Tests derive from specifications/singlestore-connection-hardening.acceptance.md.

// TC-SCH-09 (SCH-R006): the environment config path reads SIGNALS_TARGET_ENGINE
// and sets the environment target's engine; omitting it defaults to PostgreSQL
// (backward compatible). The default port is engine-aware when
// SIGNALS_TARGET_PORT is unset.
func TestEnvTargetEngineParity(t *testing.T) {
	t.Run("explicit singlestore engine + default port 3306", func(t *testing.T) {
		t.Setenv("SIGNALS_TARGET_HOST", "ss.example")
		t.Setenv("SIGNALS_TARGET_ENGINE", "singlestore")
		t.Setenv("SIGNALS_TARGET_PORT", "")

		cfg := DefaultConfig()
		if err := applyEnvOverrides(&cfg); err != nil {
			t.Fatalf("applyEnvOverrides: %v", err)
		}
		tgt := envTarget(t, cfg)
		if got := tgt.EffectiveEngine(); got != EngineSingleStore {
			t.Errorf("EffectiveEngine = %q, want singlestore", got)
		}
		if tgt.Port != 3306 {
			t.Errorf("default port = %d, want 3306 for singlestore", tgt.Port)
		}
	})

	t.Run("omitted engine defaults to postgresql + port 5432", func(t *testing.T) {
		t.Setenv("SIGNALS_TARGET_HOST", "pg.example")
		t.Setenv("SIGNALS_TARGET_ENGINE", "")
		t.Setenv("SIGNALS_TARGET_PORT", "")

		cfg := DefaultConfig()
		if err := applyEnvOverrides(&cfg); err != nil {
			t.Fatalf("applyEnvOverrides: %v", err)
		}
		tgt := envTarget(t, cfg)
		if got := tgt.EffectiveEngine(); got != EnginePostgreSQL {
			t.Errorf("EffectiveEngine = %q, want postgresql", got)
		}
		if tgt.Port != 5432 {
			t.Errorf("default port = %d, want 5432 for postgresql", tgt.Port)
		}
	})

	t.Run("explicit port overrides the engine default", func(t *testing.T) {
		t.Setenv("SIGNALS_TARGET_HOST", "ss.example")
		t.Setenv("SIGNALS_TARGET_ENGINE", "singlestore")
		t.Setenv("SIGNALS_TARGET_PORT", "3399")

		cfg := DefaultConfig()
		if err := applyEnvOverrides(&cfg); err != nil {
			t.Fatalf("applyEnvOverrides: %v", err)
		}
		tgt := envTarget(t, cfg)
		if tgt.Port != 3399 {
			t.Errorf("explicit port = %d, want 3399", tgt.Port)
		}
	})
}

// envTarget returns the single environment-configured target, failing if the
// env path did not create exactly one.
func envTarget(t *testing.T, cfg Config) TargetConfig {
	t.Helper()
	var envTargets []TargetConfig
	for _, tt := range cfg.Targets {
		if tt.Host == "ss.example" || tt.Host == "pg.example" {
			envTargets = append(envTargets, tt)
		}
	}
	if len(envTargets) != 1 {
		t.Fatalf("expected exactly one env target, got %d (%+v)", len(envTargets), cfg.Targets)
	}
	return envTargets[0]
}
