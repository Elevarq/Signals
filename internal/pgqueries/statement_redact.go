// Statement-text secret redaction.
//
// pg_stat_statements normalizes constants in ordinary DML to `$N`
// placeholders, but utility statements (CREATE/ALTER ROLE …) are NOT
// normalized, so a literal password survives in the view's `query`
// column. RedactStatementSecrets masks the finite, well-defined set of
// *structured credential syntaxes* in statement text, before the row is
// persisted or exported (REDACT-R001..R003, #188 / INV-SIGNALS-07).
//
// Scope is deliberately limited to structured credential syntax. Secrets
// hard-coded into free-form code (function bodies, arbitrary literals,
// comments) are NOT detected — see #189 (bucket 2): that is undecidable
// and a content scanner would either gut diagnostic value or give false
// assurance.

package pgqueries

import "regexp"

var (
	// `[ENCRYPTED] PASSWORD '<literal>'` in CREATE/ALTER ROLE|USER|GROUP.
	// The literal is a single-quoted SQL string ('' escapes an embedded
	// quote). The keyword prefix is preserved; only the literal is masked.
	passwordLiteralRe = regexp.MustCompile(`(?i)(\b(?:ENCRYPTED\s+)?PASSWORD\s+)'(?:[^']|'')*'`)

	// libpq conninfo `password=<value>` inside dblink / postgres_fdw /
	// CREATE SUBSCRIPTION … CONNECTION text. The value is an unquoted run
	// terminated by whitespace or a closing quote.
	conninfoPasswordRe = regexp.MustCompile(`(?i)(password\s*=\s*)([^\s']+)`)

	// `$N` normalized placeholders — never redacted (avoids mangling DML
	// such as `WHERE password = $1`).
	placeholderRe = regexp.MustCompile(`^\$\d+$`)
)

// RedactStatementSecrets masks structured credential literals — role/user
// PASSWORD literals and libpq conninfo `password=` values — in SQL
// statement text, while leaving normalized DML and non-credential content
// unchanged (REDACT-R002 / REDACT-R003). It does not attempt to find
// secrets in free-form code (#189).
func RedactStatementSecrets(s string) string {
	if s == "" {
		return s
	}
	s = passwordLiteralRe.ReplaceAllString(s, "$1'<redacted>'")
	s = conninfoPasswordRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := conninfoPasswordRe.FindStringSubmatch(m)
		if placeholderRe.MatchString(sub[2]) {
			return m // a normalized placeholder, not a real secret
		}
		return sub[1] + "<redacted>"
	})
	return s
}
