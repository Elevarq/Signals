package mysqlwire

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/engineid"
	"github.com/go-sql-driver/mysql"
)

// Tests derive from specifications/singlestore-wire-connect.acceptance.md.

func TestBuildDSN(t *testing.T) {
	t.Run("TC-WIRE-01 defaults", func(t *testing.T) {
		dsn := BuildDSN(ConnConfig{Host: "db.local", User: "signals_ro", Password: "s3cret"})
		for _, want := range []string{"tcp(db.local:3306)", "tls=preferred", "signals_ro"} {
			if !strings.Contains(dsn, want) {
				t.Fatalf("DSN %q missing %q", dsn, want)
			}
		}
		// WIRE-R002: InterpolateParams must be false. FormatDSN omits default
		// values, so verify by parsing the DSN back rather than string match.
		parsed, err := mysql.ParseDSN(dsn)
		if err != nil {
			t.Fatalf("ParseDSN: %v", err)
		}
		if parsed.InterpolateParams {
			t.Fatalf("WIRE-R002: InterpolateParams must be false")
		}
	})
	t.Run("TC-WIRE-02 explicit port + tls", func(t *testing.T) {
		dsn := BuildDSN(ConnConfig{Host: "ss.example", Port: 3307, User: "u", TLS: "skip-verify"})
		for _, want := range []string{"tcp(ss.example:3307)", "tls=skip-verify"} {
			if !strings.Contains(dsn, want) {
				t.Fatalf("DSN %q missing %q", dsn, want)
			}
		}
	})
}

func TestRedacted(t *testing.T) {
	// TC-WIRE-06: password must never appear in the loggable DSN.
	got := Redacted(ConnConfig{Host: "h", User: "u", Password: "s3cret"})
	if strings.Contains(got, "s3cret") {
		t.Fatalf("INV-WIRE-02: redacted DSN leaked the password: %q", got)
	}
}

// fakeScalar builds a ScalarQuery from a map of query-substring -> (value,err).
func fakeScalar(responses map[string]struct {
	val string
	err error
}) ScalarQuery {
	return func(_ context.Context, q string) (string, error) {
		for sub, r := range responses {
			if strings.Contains(q, sub) {
				return r.val, r.err
			}
		}
		return "", errors.New("unexpected query: " + q)
	}
}

func TestIdentify(t *testing.T) {
	errUnknownVar := errors.New("Unknown system variable 'memsql_version'")

	t.Run("TC-WIRE-03 singlestore", func(t *testing.T) {
		engine, version, err := Identify(context.Background(), fakeScalar(map[string]struct {
			val string
			err error
		}{
			"memsql_version":  {val: "8.5.22"},
			"version_comment": {val: "SingleStore"},
			"VERSION()":       {val: "5.7.32"},
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if engine != engineid.EngineSingleStore || version != "8.5.22" {
			t.Fatalf("got (%q,%q), want (singlestore,8.5.22)", engine, version)
		}
	})

	t.Run("TC-WIRE-04 mysql, memsql rejected", func(t *testing.T) {
		engine, _, err := Identify(context.Background(), fakeScalar(map[string]struct {
			val string
			err error
		}{
			"memsql_version":  {err: errUnknownVar},
			"version_comment": {val: "MySQL Community Server - GPL"},
			"VERSION()":       {val: "8.0.36"},
		}))
		if err != nil {
			t.Fatalf("WIRE-R003: memsql error must be tolerated, got %v", err)
		}
		if engine != engineid.EngineMySQL {
			t.Fatalf("engine = %q, want mysql", engine)
		}
	})

	t.Run("TC-WIRE-05 indeterminate fails closed", func(t *testing.T) {
		engine, _, err := Identify(context.Background(), fakeScalar(map[string]struct {
			val string
			err error
		}{
			"memsql_version":  {err: errUnknownVar},
			"version_comment": {err: errors.New("denied")},
			"VERSION()":       {val: "5.7.32"},
		}))
		if err == nil {
			t.Fatalf("expected ErrIndeterminate, got engine=%q", engine)
		}
		if engine != "" {
			t.Fatalf("fail-closed must not guess, got %q", engine)
		}
	})
}
