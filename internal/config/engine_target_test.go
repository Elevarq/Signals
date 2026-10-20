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
