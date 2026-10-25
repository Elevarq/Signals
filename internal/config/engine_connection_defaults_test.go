package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests derive from specifications/engine-connection-defaults.acceptance.md (#623).

// envTargetByHost returns the single env-configured target with the given host.
func envTargetByHost(t *testing.T, cfg Config, host string) TargetConfig {
	t.Helper()
	for _, tt := range cfg.Targets {
		if tt.Host == host {
			return tt
		}
	}
	t.Fatalf("no env target with host %q in %+v", host, cfg.Targets)
	return TargetConfig{}
}

// loadEnvTarget applies the env overrides for a single host/engine and returns
// the resolved target. SIGNALS_TARGET_PORT/DBNAME are set from the args ("" = unset).
func loadEnvTarget(t *testing.T, engine, port, dbname string) TargetConfig {
	t.Helper()
	host := "env.example"
	t.Setenv("SIGNALS_TARGET_HOST", host)
	t.Setenv("SIGNALS_TARGET_ENGINE", engine)
	t.Setenv("SIGNALS_TARGET_PORT", port)
	t.Setenv("SIGNALS_TARGET_DBNAME", dbname)
	cfg := DefaultConfig()
	if err := applyEnvOverrides(&cfg); err != nil {
		t.Fatalf("applyEnvOverrides(engine=%q): %v", engine, err)
	}
	return envTargetByHost(t, cfg, host)
}

// TC-ECONN-01: engine-only env config resolves the correct default port.
func TestEnvDefaultPortAllEngines(t *testing.T) {
	cases := []struct {
		engine string
		want   int
	}{
		{"", 5432}, // omitted -> PostgreSQL
		{"postgresql", 5432},
		{"mysql", 3306},
		{"mariadb", 3306},
		{"singlestore", 3306},
		{"clickhouse", 9000},
	}
	for _, c := range cases {
		c := c
		name := c.engine
		if name == "" {
			name = "(omitted)"
		}
		t.Run(name, func(t *testing.T) {
			tgt := loadEnvTarget(t, c.engine, "", "")
			if tgt.Port != c.want {
				t.Errorf("engine %q default port = %d, want %d", c.engine, tgt.Port, c.want)
			}
		})
	}
}

// TC-ECONN-02: only PostgreSQL defaults the database to `postgres`.
func TestEnvDatabaseDefaultOnlyPostgreSQL(t *testing.T) {
	for _, engine := range []string{"mysql", "mariadb", "singlestore", "clickhouse"} {
		t.Run(engine, func(t *testing.T) {
			tgt := loadEnvTarget(t, engine, "", "")
			if tgt.DBName != "" {
				t.Errorf("engine %q with no dbname: DBName = %q, want empty (no postgres leak)", engine, tgt.DBName)
			}
		})
	}
	for _, engine := range []string{"", "postgresql"} {
		name := engine
		if name == "" {
			name = "(omitted)"
		}
		t.Run("postgres-default/"+name, func(t *testing.T) {
			tgt := loadEnvTarget(t, engine, "", "")
			if tgt.DBName != "postgres" {
				t.Errorf("engine %q with no dbname: DBName = %q, want postgres", engine, tgt.DBName)
			}
		})
	}
}

// TC-ECONN-03: explicit port and database always win.
func TestEnvExplicitPortAndDBWin(t *testing.T) {
	tgt := loadEnvTarget(t, "mysql", "33099", "appdb")
	if tgt.Port != 33099 {
		t.Errorf("explicit port = %d, want 33099", tgt.Port)
	}
	if tgt.DBName != "appdb" {
		t.Errorf("explicit dbname = %q, want appdb", tgt.DBName)
	}
	// Explicit database on a non-PostgreSQL engine is honored too.
	ch := loadEnvTarget(t, "clickhouse", "", "metrics")
	if ch.DBName != "metrics" {
		t.Errorf("clickhouse explicit dbname = %q, want metrics", ch.DBName)
	}
	if ch.Port != 9000 {
		t.Errorf("clickhouse default port = %d, want 9000", ch.Port)
	}
}

// validTarget returns a target with every non-dbname required field set, so a
// ValidateStrict run isolates the dbname rule.
func validTarget(name, engine, dbname string) TargetConfig {
	return TargetConfig{
		Name: name, Host: "h", Port: 3306, Engine: engine, DBName: dbname,
		User: "u", PasswordEnv: "PW",
	}
}

func hasDBNameError(warnErr error) bool {
	return warnErr != nil && strings.Contains(warnErr.Error(), "dbname is required")
}

// TC-ECONN-04: dbname is required only for PostgreSQL.
func TestDBNameRequiredOnlyForPostgreSQL(t *testing.T) {
	base := DefaultConfig()

	t.Run("non-postgresql empty dbname is accepted", func(t *testing.T) {
		for _, engine := range []string{"mysql", "mariadb", "singlestore", "clickhouse"} {
			cfg := base
			cfg.Targets = []TargetConfig{validTarget("t", engine, "")}
			_, err := ValidateStrict(cfg)
			if hasDBNameError(err) {
				t.Errorf("engine %q empty dbname must NOT be a dbname error, got: %v", engine, err)
			}
		}
	})

	t.Run("postgresql empty dbname is a hard error", func(t *testing.T) {
		cfg := base
		cfg.Targets = []TargetConfig{validTarget("t", "postgresql", "")}
		_, err := ValidateStrict(cfg)
		if !hasDBNameError(err) {
			t.Errorf("PostgreSQL empty dbname must be a dbname hard error, got: %v", err)
		}
	})

	t.Run("omitted engine (legacy) empty dbname is a hard error", func(t *testing.T) {
		cfg := base
		cfg.Targets = []TargetConfig{validTarget("t", "", "")}
		_, err := ValidateStrict(cfg)
		if !hasDBNameError(err) {
			t.Errorf("legacy (empty engine) empty dbname must be a dbname hard error, got: %v", err)
		}
	})
}

// TC-ECONN-05: YAML and env resolve equivalent ports for an engine-only target.
func TestYAMLEnvPortParity(t *testing.T) {
	// YAML target with engine but no port.
	yaml := `
database:
  path: /tmp/x.db
api:
  listen_addr: "127.0.0.1:8080"
  token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
targets:
  - name: ch-yaml
    engine: clickhouse
    host: ch.example
    dbname: ""
    user: u
    enabled: true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "signals.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	// Ensure no env target interferes.
	t.Setenv("SIGNALS_TARGET_HOST", "")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var yamlTgt *TargetConfig
	for i := range cfg.Targets {
		if cfg.Targets[i].Name == "ch-yaml" {
			yamlTgt = &cfg.Targets[i]
		}
	}
	if yamlTgt == nil {
		t.Fatalf("yaml target not loaded: %+v", cfg.Targets)
	}
	if yamlTgt.Port != 9000 {
		t.Errorf("YAML clickhouse engine-only port = %d, want 9000 (parity with env)", yamlTgt.Port)
	}
	// Env equivalent resolves the same.
	envTgt := loadEnvTarget(t, "clickhouse", "", "")
	if envTgt.Port != yamlTgt.Port {
		t.Errorf("env port %d != yaml port %d for clickhouse engine-only", envTgt.Port, yamlTgt.Port)
	}
}

// TC-ECONN-06: an unsupported explicit engine is rejected, never downgraded.
func TestUnsupportedEngineRejected(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Targets = []TargetConfig{validTarget("t", "oracle", "db")}
	_, err := ValidateStrict(cfg)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Errorf("unsupported engine must be a hard error, got: %v", err)
	}
}
