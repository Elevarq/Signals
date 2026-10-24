package config

import (
	"strings"
	"testing"
)

// Tests derive from specifications/engine-identity.acceptance.md.

// TC-EID-01 / TC-EID-02 (EID-R001, INV-EID-01): empty engine resolves to
// postgresql; an explicit supported engine is preserved.
func TestEffectiveEngine(t *testing.T) {
	if got := (TargetConfig{}).EffectiveEngine(); got != EnginePostgreSQL {
		t.Fatalf("empty engine: got %q, want %q", got, EnginePostgreSQL)
	}
	if got := (TargetConfig{Engine: EnginePostgreSQL}).EffectiveEngine(); got != EnginePostgreSQL {
		t.Fatalf("explicit postgresql: got %q", got)
	}
}

// TC-EID-03 (EID-R002, failure condition): a configured engine without a
// collection backend is rejected — never silently downgraded to postgresql.
func TestValidateRejectsUnsupportedEngine(t *testing.T) {
	// An engine string with no collection backend (not in SupportedEngines)
	// must be rejected, never silently downgraded to postgresql. postgresql,
	// singlestore, mysql, and mariadb are all supported now, so use a
	// genuinely-unsupported engine string.
	tgt := TargetConfig{
		Name: "t", Host: "h", User: "u", Port: 5432,
		SSLMode: "require", Enabled: true, Engine: "oracle",
	}
	_, err := ValidateStrict(baseValidConfig(tgt))
	if err == nil {
		t.Fatal("expected ValidateStrict to reject engine=oracle, got nil")
	}
	if !strings.Contains(err.Error(), "oracle") || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("error must name the unsupported engine: %v", err)
	}
}

// mysql is a supported collection engine; it must NOT trip the engine gate.
func TestValidateAcceptsMySQLEngine(t *testing.T) {
	tgt := TargetConfig{
		Name: "t", Host: "h", User: "u", Port: 3306,
		SSLMode: "require", Enabled: true, Engine: EngineMySQL,
	}
	_, err := ValidateStrict(baseValidConfig(tgt))
	if err != nil && strings.Contains(err.Error(), "engine") {
		t.Fatalf("engine=mysql must not trip the engine gate: %v", err)
	}
}

// mariadb is a supported collection engine; it must NOT trip the engine gate.
func TestValidateAcceptsMariaDBEngine(t *testing.T) {
	tgt := TargetConfig{
		Name: "t", Host: "h", User: "u", Port: 3306,
		SSLMode: "require", Enabled: true, Engine: EngineMariaDB,
	}
	_, err := ValidateStrict(baseValidConfig(tgt))
	if err != nil && strings.Contains(err.Error(), "engine") {
		t.Fatalf("engine=mariadb must not trip the engine gate: %v", err)
	}
}

// TC-EID-02 (negative control): engine=postgresql must NOT trip the engine
// gate (it is in SupportedEngines).
func TestValidateAcceptsPostgreSQLEngine(t *testing.T) {
	tgt := TargetConfig{
		Name: "t", Host: "h", User: "u", Port: 5432,
		SSLMode: "require", Enabled: true, Engine: EnginePostgreSQL,
	}
	_, err := ValidateStrict(baseValidConfig(tgt))
	if err != nil && strings.Contains(err.Error(), "engine") {
		t.Fatalf("engine=postgresql must not trip the engine gate: %v", err)
	}
}
