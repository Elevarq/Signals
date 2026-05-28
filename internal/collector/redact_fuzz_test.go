package collector

import (
	"strings"
	"testing"
)

// FuzzRedactDSN feeds arbitrary strings into the DSN redactor and asserts
// two structural properties:
//
//  1. It never panics.
//  2. If the input contains a libpq-form `password=<value>` token, the
//     output contains a `password=****` token. (We check the redacted
//     MARKER is present, not that the value is absent — short or
//     symbol-shaped passwords can incidentally appear as substrings of
//     legitimate output like "password=****", which makes value-absence
//     checks unreliable.)
//  3. If the input has a URL of the form `postgres://user:pw@host`, the
//     output contains `:****@`.
//
// Closes Scorecard FuzzingID for the project (issue #32) and exercises
// the redaction logic against malformed and ill-shaped inputs.
func FuzzRedactDSN(f *testing.F) {
	f.Add("postgres://app:hunter2@db.internal:5432/myapp")
	f.Add("postgresql://app:hunter2@db.internal/myapp?sslmode=require")
	f.Add("host=db.internal port=5432 user=app password=hunter2 dbname=myapp")
	f.Add("password=topsecret host=db port=5432")
	f.Add("")
	f.Add("not a dsn at all")
	f.Add("postgres://no-password@host/db")
	f.Add("postgres://@host/db")
	f.Add("password=")
	f.Add("password=a")

	f.Fuzz(func(t *testing.T, dsn string) {
		out := RedactDSN(dsn)

		// libpq key=value form: walk the tokens the same way RedactDSN
		// does — only a space-separated token whose prefix is
		// "password=" counts as a real key.
		for _, part := range strings.Fields(dsn) {
			if !strings.HasPrefix(part, "password=") {
				continue
			}
			if part == "password=" {
				// Empty value: nothing to redact, output unchanged.
				continue
			}
			// The output, parsed the same way, must contain a
			// `password=****` token (the redactor's redacted marker).
			found := false
			for _, op := range strings.Fields(out) {
				if op == "password=****" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("RedactDSN(%q) did not produce a password=**** token; got %q", dsn, out)
			}
			break
		}

		// URL userinfo form: scheme://user:pw@host should become
		// scheme://user:****@host. Check the marker is present.
		for _, scheme := range []string{"postgres://", "postgresql://"} {
			if !strings.HasPrefix(dsn, scheme) {
				continue
			}
			rest := dsn[len(scheme):]
			at := strings.Index(rest, "@")
			if at == -1 {
				break
			}
			userinfo := rest[:at]
			if !strings.Contains(userinfo, ":") {
				break
			}
			// userinfo has a colon → there is a password, possibly empty.
			// Output should redact to `:****@` somewhere in the URL.
			if !strings.Contains(out, ":****@") {
				t.Errorf("RedactDSN(%q) did not redact URL password to ':****@'; got %q", dsn, out)
			}
			break
		}
	})
}
