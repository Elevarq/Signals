// Tests for FDW option redaction.
//
// Spec: specifications/collectors/fdw_*_v1.md (REDACT-R001..R003)

package pgqueries

import (
	"reflect"
	"testing"
)

func TestRedactFDWOptions_EmptyInput(t *testing.T) {
	got := RedactFDWOptions(nil)
	if len(got) != 0 {
		t.Errorf("nil input should produce empty map; got %v", got)
	}
	got = RedactFDWOptions([]string{})
	if len(got) != 0 {
		t.Errorf("empty slice should produce empty map; got %v", got)
	}
}

// TestRedactFDWOptions_RedactsSensitiveKeys is the load-bearing
// safety check: every keyword in the redaction list (or a
// substring match of one) MUST replace its value with the
// "<redacted>" sentinel. Adding a real-world example for each
// pattern doubles as documentation of which option names get
// redacted.
func TestRedactFDWOptions_RedactsSensitiveKeys(t *testing.T) {
	cases := []struct {
		input string
		want  string // expected value in the output map; "<redacted>" for sensitive keys
	}{
		// password family
		{"password=hunter2", "<redacted>"},
		{"PASSWORD=HUNTER2", "<redacted>"},
		{"db_password=hunter2", "<redacted>"},
		{"proxy-password=hunter2", "<redacted>"},
		{"dbpass=hunter2", "<redacted>"},
		// generic secret / token / credential
		{"secret=topsecret", "<redacted>"},
		{"client_secret=abc123", "<redacted>"},
		{"token=eyJhbGciOi...", "<redacted>"},
		{"bearer_token=xyz", "<redacted>"},
		{"credential=opaque", "<redacted>"},
		{"aws_credential=AKIA...", "<redacted>"},
		// keys (private/API/SSL)
		{"private_key=-----BEGIN-----", "<redacted>"},
		{"api_key=k_1234", "<redacted>"},
		{"sslkey=/etc/ssl/postgres.key", "<redacted>"},
		// connection strings
		{"connstr=postgres://user:pw@h/db", "<redacted>"},
		{"connection_string=postgres://...", "<redacted>"},
		{"conn_string=postgres://...", "<redacted>"},
		// auth-class
		{"auth=basic", "<redacted>"},
		{"auth_method=oauth", "<redacted>"},
		// libpq service
		{"service=prod-replica", "<redacted>"},
		// SSL cert paths (the cert file is public, but the path
		// can leak topology — redact along with the rest).
		{"sslcert=/etc/ssl/client.crt", "<redacted>"},
		{"sslrootcert=/etc/ssl/ca.crt", "<redacted>"},
	}
	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			got := RedactFDWOptions([]string{c.input})
			// Find the key (preserved unmodified) and verify the value.
			if len(got) != 1 {
				t.Fatalf("expected 1 entry; got %d (%v)", len(got), got)
			}
			for k, v := range got {
				if v != c.want {
					t.Errorf("key=%q: got value %q, want %q", k, v, c.want)
				}
			}
		})
	}
}

// TestRedactFDWOptions_PreservesNonSensitive — routine FDW options
// that are NOT credentials must round-trip with their values intact.
// This pins the list of safely-passing keys so a future broadening
// of the redact list doesn't accidentally smear "<redacted>" over
// `host`, `port`, `dbname`, etc.
func TestRedactFDWOptions_PreservesNonSensitive(t *testing.T) {
	cases := []struct {
		input string
		key   string
		val   string
	}{
		{"host=db.example.com", "host", "db.example.com"},
		{"hostaddr=10.0.0.1", "hostaddr", "10.0.0.1"},
		{"port=5432", "port", "5432"},
		{"dbname=production", "dbname", "production"},
		{"user=remote_reader", "user", "remote_reader"},
		{"username=remote_reader", "username", "remote_reader"},
		{"updatable=true", "updatable", "true"},
		{"truncatable=false", "truncatable", "false"},
		{"fetch_size=100", "fetch_size", "100"},
		{"use_remote_estimate=true", "use_remote_estimate", "true"},
		{"schema_name=public", "schema_name", "public"},
		{"table_name=accounts", "table_name", "accounts"},
		{"options=-c search_path=foo", "options", "-c search_path=foo"},
		{"sslmode=require", "sslmode", "require"},
		// Connect-timeout is a number, not a credential.
		{"connect_timeout=10", "connect_timeout", "10"},
	}
	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			got := RedactFDWOptions([]string{c.input})
			if got[c.key] != c.val {
				t.Errorf("key=%q: got %q, want %q (full map: %v)", c.key, got[c.key], c.val, got)
			}
		})
	}
}

// TestRedactFDWOptions_ValuesContainingEquals — Postgres allows the
// value side of a libpq option to contain `=` (e.g.
// `options=-c search_path=foo`). Only the FIRST `=` separates key
// from value.
func TestRedactFDWOptions_ValuesContainingEquals(t *testing.T) {
	got := RedactFDWOptions([]string{"options=-c search_path=foo,bar"})
	want := "-c search_path=foo,bar"
	if got["options"] != want {
		t.Errorf("got %q, want %q", got["options"], want)
	}
}

// TestRedactFDWOptions_MalformedEntriesSkipped — defensive coverage.
// Postgres does not emit malformed `text[]` entries, but if it did,
// the redactor must skip them (never crash; never produce {"":""}
// entries that downstream consumers might mis-parse).
func TestRedactFDWOptions_MalformedEntriesSkipped(t *testing.T) {
	got := RedactFDWOptions([]string{"", "no_equals_sign", "=value_only", "ok=present"})
	if _, has := got["ok"]; !has {
		t.Errorf("well-formed entry should be kept; got %v", got)
	}
	if got["ok"] != "present" {
		t.Errorf("well-formed value mangled; got %v", got)
	}
	if len(got) != 1 {
		t.Errorf("malformed entries should be skipped; got %v", got)
	}
}

// TestRedactFDWOptions_ResultIsDeterministic asserts that running
// the redactor twice on the same input produces equal maps. Map
// iteration order in Go is randomised; this test pins value
// equality, not iteration order — and `SortedFDWOptionKeys` is the
// helper that callers should use when stable iteration matters.
func TestRedactFDWOptions_ResultIsDeterministic(t *testing.T) {
	in := []string{
		"host=db.example.com",
		"port=5432",
		"password=hunter2",
		"user=remote",
	}
	a := RedactFDWOptions(in)
	b := RedactFDWOptions(in)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("redactor not deterministic: %v vs %v", a, b)
	}
}

// TestSortedFDWOptionKeys gives callers a stable iteration order
// over the redacted output. Useful for snapshot ZIP normalisation
// and for tests pinning rendered output.
func TestSortedFDWOptionKeys(t *testing.T) {
	got := SortedFDWOptionKeys(map[string]string{
		"port":     "5432",
		"host":     "h",
		"user":     "u",
		"dbname":   "d",
		"password": "<redacted>",
	})
	want := []string{"dbname", "host", "password", "port", "user"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestSortedFDWOptionKeys_Empty defensive.
func TestSortedFDWOptionKeys_Empty(t *testing.T) {
	if got := SortedFDWOptionKeys(nil); len(got) != 0 {
		t.Errorf("nil input should produce empty slice; got %v", got)
	}
	if got := SortedFDWOptionKeys(map[string]string{}); len(got) != 0 {
		t.Errorf("empty map should produce empty slice; got %v", got)
	}
}
