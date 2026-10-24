package collector

import (
	"testing"

	"github.com/elevarq/signals/internal/config"
)

// Tests derive from specifications/engine-collection-dispatch.acceptance.md.

func TestSelectBackend(t *testing.T) {
	cases := []struct {
		name   string
		engine string
		want   Backend
	}{
		{"TC-ECD-01 empty -> postgresql", "", BackendPostgreSQL},
		{"TC-ECD-01 postgresql", config.EnginePostgreSQL, BackendPostgreSQL},
		{"TC-ECD-02 singlestore", config.EngineSingleStore, BackendMySQLWire},
		{"TC-ECD-03 mysql", config.EngineMySQL, BackendMySQLWire},
		{"mariadb -> wire", config.EngineMariaDB, BackendMySQLWire},
		{"clickhouse -> clickhouse backend", config.EngineClickHouse, BackendClickHouse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SelectBackend(config.TargetConfig{Engine: tc.engine})
			if got != tc.want {
				t.Fatalf("SelectBackend(engine=%q) = %v, want %v", tc.engine, got, tc.want)
			}
			// TC-ECD-04 / INV-ECD-01: a non-PostgreSQL engine must never map to
			// the PostgreSQL (pgx) backend.
			if tc.engine == config.EngineSingleStore && got == BackendPostgreSQL {
				t.Fatalf("INV-ECD-01: singlestore must not use the pgx backend")
			}
		})
	}
}
