package pgqueries

import "testing"

// REDACT-R002 / REDACT-R003 (pg_stat_statements_v1, #188): RedactStatementSecrets
// masks structured credential literals in statement text while leaving everything
// else untouched. Written failing before the implementation exists.
func TestRedactStatementSecrets(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "create role password (the reported leak)",
			in:   "CREATE ROLE signals WITH LOGIN PASSWORD 'monitor_pass'",
			want: "CREATE ROLE signals WITH LOGIN PASSWORD '<redacted>'",
		},
		{
			name: "alter user encrypted password",
			in:   "ALTER USER bob ENCRYPTED PASSWORD 'sekret-123'",
			want: "ALTER USER bob ENCRYPTED PASSWORD '<redacted>'",
		},
		{
			name: "alter role password with special chars",
			in:   "ALTER ROLE admin PASSWORD 'p@ss w0rd!$%'",
			want: "ALTER ROLE admin PASSWORD '<redacted>'",
		},
		{
			name: "conninfo password= in dblink",
			in:   "SELECT dblink('host=db port=5432 password=topsecret dbname=app', 'select 1')",
			want: "SELECT dblink('host=db port=5432 password=<redacted> dbname=app', 'select 1')",
		},
		{
			name: "create subscription connection conninfo",
			in:   "CREATE SUBSCRIPTION s CONNECTION 'host=h dbname=d user=u password=hunter2' PUBLICATION p",
			want: "CREATE SUBSCRIPTION s CONNECTION 'host=h dbname=d user=u password=<redacted>' PUBLICATION p",
		},
		{
			name: "parameterized DML is unchanged (no over-redaction)",
			in:   "SELECT * FROM users WHERE id = $1 AND token = $2",
			want: "SELECT * FROM users WHERE id = $1 AND token = $2",
		},
		{
			name: "the word password without a value is unchanged",
			in:   "SELECT count(*) FROM audit WHERE event = 'password_reset'",
			want: "SELECT count(*) FROM audit WHERE event = 'password_reset'",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactStatementSecrets(tc.in)
			if got != tc.want {
				t.Errorf("RedactStatementSecrets(%q)\n  got:  %q\n  want: %q", tc.in, got, tc.want)
			}
			if tc.want != tc.in && got == tc.in {
				t.Errorf("secret was NOT redacted: %q", got)
			}
		})
	}
}
