package conntest

import (
	"context"
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/config"
)

// #722 / F16 (POC): the PostgreSQL-protocol connect test reports a
// non-PostgreSQL engine target as EXPLICITLY unsupported, rather than running
// it through pgx and producing a misleading TLS/auth failure for the wrong
// protocol.
func TestConfigTarget_NonPGEngineReportedUnsupported(t *testing.T) {
	res := configResolver()
	for _, engine := range []string{config.EngineMySQL, config.EngineMariaDB, config.EngineSingleStore, config.EngineClickHouse} {
		tgt := config.TargetConfig{
			Name: engine + "-t", Host: "h.invalid", Port: 3306, DBName: "db", User: "u",
			Engine: engine, Enabled: true,
		}
		r := testConfigTarget(context.Background(), tgt, res, Options{})
		if r.Category != CategoryUnsupportedEngine {
			t.Errorf("engine %q: category = %q, want unsupported_engine", engine, r.Category)
		}
		if !strings.Contains(r.Detail, engine) {
			t.Errorf("engine %q: detail should name the engine: %q", engine, r.Detail)
		}
	}
}

// A PostgreSQL target is NOT reported unsupported (control).
func TestConfigTarget_PostgreSQLNotUnsupported(t *testing.T) {
	tgt := config.TargetConfig{Name: "pg", Host: "h.invalid", Port: 5432, DBName: "db", User: "u", Enabled: true}
	r := testConfigTarget(context.Background(), tgt, configResolver(), Options{})
	if r.Category == CategoryUnsupportedEngine {
		t.Error("a PostgreSQL target was reported unsupported")
	}
}
