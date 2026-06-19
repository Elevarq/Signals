package collector

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/elevarq/signals/internal/config"
)

// TC-SIG-126 — R111: diagnostic DSN values are libpq-quoted; no field
// value (including a resolved password) can introduce, override, or
// remove another connection parameter (INV-SIGNALS-21).
func TestBuildSafeDSNQuotesValues(t *testing.T) {
	cases := []struct {
		name     string
		password string
		dbname   string
		user     string
	}{
		{name: "plain", password: "secret", dbname: "appdb", user: "monitor"},
		{name: "password with spaces", password: "pa ss word", dbname: "appdb", user: "monitor"},
		{name: "password with injection attempt", password: "x sslmode=disable host=evil.example.com", dbname: "appdb", user: "monitor"},
		{name: "password with single quote", password: "pa'ss", dbname: "appdb", user: "monitor"},
		{name: "password with backslash", password: `pa\ss`, dbname: "appdb", user: "monitor"},
		{name: "password with quote backslash mix", password: `' \' sslmode=disable x\`, dbname: "appdb", user: "monitor"},
		{name: "dbname with space", password: "secret", dbname: "my db", user: "monitor"},
		{name: "user with quote", password: "secret", dbname: "appdb", user: "mon'itor"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SIGNALS_TEST_PW", tc.password)
			tgt := config.TargetConfig{
				Name:        "t1",
				Host:        "db.example.com",
				Port:        5432,
				DBName:      tc.dbname,
				User:        tc.user,
				PasswordEnv: "SIGNALS_TEST_PW",
			}

			dsn, err := BuildSafeDSN(tgt)
			if err != nil {
				t.Fatalf("BuildSafeDSN: %v", err)
			}

			cfg, err := pgx.ParseConfig(dsn)
			if err != nil {
				t.Fatalf("assembled DSN does not parse: %v (dsn with password redacted: %s)", err, RedactDSN(dsn))
			}

			if cfg.Host != "db.example.com" {
				t.Errorf("host re-targeted: got %q, want %q", cfg.Host, "db.example.com")
			}
			if cfg.Port != 5432 {
				t.Errorf("port changed: got %d, want 5432", cfg.Port)
			}
			if cfg.Database != tc.dbname {
				t.Errorf("dbname round-trip: got %q, want %q", cfg.Database, tc.dbname)
			}
			if cfg.User != tc.user {
				t.Errorf("user round-trip: got %q, want %q", cfg.User, tc.user)
			}
			if cfg.Password != tc.password {
				t.Errorf("password round-trip: got %q, want %q", cfg.Password, tc.password)
			}
			// sslmode defaults to prefer (R111: injection must not
			// downgrade TLS posture). Under prefer, pgx sets a TLS
			// primary config; sslmode=disable smuggled via a field
			// value would leave TLSConfig nil.
			if cfg.TLSConfig == nil {
				t.Errorf("TLS posture downgraded: TLSConfig is nil (sslmode=disable injected?)")
			}
		})
	}
}

// TC-SIG-126 — R111: RedactDSN fully masks a libpq-quoted password
// value, even when it contains spaces or escaped quotes (no secret
// fragment survives the redaction; R024, INV-SIGNALS-07).
func TestRedactDSNQuotedPassword(t *testing.T) {
	t.Setenv("SIGNALS_TEST_PW", "pa ss '; sslmode=disable")
	tgt := config.TargetConfig{
		Name:        "t1",
		Host:        "db.example.com",
		Port:        5432,
		DBName:      "appdb",
		User:        "monitor",
		PasswordEnv: "SIGNALS_TEST_PW",
	}
	dsn, err := BuildSafeDSN(tgt)
	if err != nil {
		t.Fatalf("BuildSafeDSN: %v", err)
	}
	red := RedactDSN(dsn)
	if strings.Contains(red, "pa ss") || strings.Contains(red, "sslmode=disable") {
		t.Errorf("redacted DSN leaks password fragment: %q", red)
	}
	if !strings.Contains(red, "password=****") {
		t.Errorf("redacted DSN missing password=**** marker: %q", red)
	}
	// Non-secret fields must remain visible for diagnostics.
	if !strings.Contains(red, "host='db.example.com'") {
		t.Errorf("redacted DSN dropped host: %q", red)
	}
}

// TC-SIG-126 — R111: peer/trust targets (no secret source) emit no
// password parameter at all.
func TestBuildSafeDSNNoPassword(t *testing.T) {
	tgt := config.TargetConfig{
		Name:   "t1",
		Host:   "db.example.com",
		Port:   5432,
		DBName: "appdb",
		User:   "monitor",
	}
	dsn, err := BuildSafeDSN(tgt)
	if err != nil {
		t.Fatalf("BuildSafeDSN: %v", err)
	}
	if strings.Contains(dsn, "password") {
		t.Errorf("DSN contains a password parameter for a passwordless target: %s", RedactDSN(dsn))
	}
	if _, err := pgx.ParseConfig(dsn); err != nil {
		t.Fatalf("assembled DSN does not parse: %v", err)
	}
}
