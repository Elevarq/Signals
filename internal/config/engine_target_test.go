package config

import (
	"strings"
	"testing"
)

// Tests derive from specifications/engine-target-config.acceptance.md.

func TestEffectiveEngineDefaultsToPostgreSQL(t *testing.T) {
	// TC-ETC-01
	tgt := baseValidTarget()
	if got := tgt.EffectiveEngine(); got != EnginePostgreSQL {
		t.Fatalf("ETC-R001: empty engine EffectiveEngine() = %q, want %q", got, EnginePostgreSQL)
	}
	if _, err := ValidateStrict(baseValidConfig(tgt)); err != nil {
		t.Fatalf("INV-ETC-01: config with no engine must validate, got %v", err)
	}
}

func TestEffectiveEngineSingleStoreAccepted(t *testing.T) {
	// TC-ETC-02
	tgt := baseValidTarget()
	tgt.Engine = EngineSingleStore
	if got := tgt.EffectiveEngine(); got != EngineSingleStore {
		t.Fatalf("EffectiveEngine() = %q, want singlestore", got)
	}
	if _, err := ValidateStrict(baseValidConfig(tgt)); err != nil {
		t.Fatalf("ETC-R003: supported engine must validate, got %v", err)
	}
}

func TestUnsupportedEngineRejected(t *testing.T) {
	// TC-ETC-03
	tgt := baseValidTarget()
	tgt.Engine = "oracle"
	_, err := ValidateStrict(baseValidConfig(tgt))
	if err == nil {
		t.Fatal("ETC-R002: unsupported engine must be rejected")
	}
	msg := err.Error()
	if !strings.Contains(msg, "engine") || !strings.Contains(msg, "oracle") {
		t.Fatalf("error must name engine and the bad value, got %q", msg)
	}
}

func TestEngineMatchingIsExact(t *testing.T) {
	// TC-ETC-04: wrong case is not normalized.
	tgt := baseValidTarget()
	tgt.Engine = "SingleStore"
	if _, err := ValidateStrict(baseValidConfig(tgt)); err == nil {
		t.Fatal("INV-ETC-02: engine must match the exact lowercase constant")
	}
}

// TestEnvTargetEngineParity verifies the SIGNALS_TARGET_* env config path can
// express the target engine (#555): empty is the backward-compatible
// PostgreSQL default, and SINGLESTORE is accepted — parity with the YAML path.
func TestEnvTargetEngineParity(t *testing.T) {
	t.Run("default_postgresql", func(t *testing.T) {
		t.Setenv("SIGNALS_TARGET_HOST", "pg.local")
		cfg := &Config{}
		if err := applyEnvOverrides(cfg); err != nil {
			t.Fatalf("applyEnvOverrides: %v", err)
		}
		if len(cfg.Targets) != 1 {
			t.Fatalf("want 1 env target, got %d", len(cfg.Targets))
		}
		if got := cfg.Targets[0].EffectiveEngine(); got != EnginePostgreSQL {
			t.Fatalf("no SIGNALS_TARGET_ENGINE: EffectiveEngine()=%q, want %q (backward compat)", got, EnginePostgreSQL)
		}
	})
	t.Run("singlestore", func(t *testing.T) {
		t.Setenv("SIGNALS_TARGET_HOST", "ss.local")
		t.Setenv("SIGNALS_TARGET_ENGINE", EngineSingleStore)
		cfg := &Config{}
		if err := applyEnvOverrides(cfg); err != nil {
			t.Fatalf("applyEnvOverrides: %v", err)
		}
		if len(cfg.Targets) != 1 {
			t.Fatalf("want 1 env target, got %d", len(cfg.Targets))
		}
		if cfg.Targets[0].Engine != EngineSingleStore {
			t.Fatalf("SIGNALS_TARGET_ENGINE=singlestore: target.Engine=%q, want %q", cfg.Targets[0].Engine, EngineSingleStore)
		}
		if got := cfg.Targets[0].EffectiveEngine(); got != EngineSingleStore {
			t.Fatalf("EffectiveEngine()=%q, want singlestore", got)
		}
	})
}
