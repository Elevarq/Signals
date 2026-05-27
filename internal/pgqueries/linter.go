package pgqueries

import (
	"fmt"
	"regexp"
	"strings"
)

// Safety note: the primary query safety controls are:
//  1. This linter (SELECT-only, no dangerous keywords/functions)
//  2. Session-level default_transaction_read_only=on (secrets.go)
//  3. Per-query read-only transaction (collector.go BeginTx ReadOnly)
//
// The function denylist is defense-in-depth; not a substitute for (2) and (3).

// dangerousWords are SQL keywords that a read-only query must never contain.
var dangerousWords = []string{
	"INSERT", "UPDATE", "DELETE", "DROP", "CREATE", "ALTER",
	"TRUNCATE", "GRANT", "REVOKE", "COPY", "VACUUM", "CALL",
	"DO", "SET", "RESET", "EXECUTE", "REINDEX",
}

// dangerousFuncs are PostgreSQL functions that have side effects or represent
// DoS/abuse risk. Blocked even in read-only transactions as defense-in-depth.
var dangerousFuncs = []string{
	"pg_sleep",
	"pg_terminate_backend",
	"pg_cancel_backend",
	"pg_reload_conf",
	"pg_start_backup",
	"pg_stop_backup",
	"pg_create_restore_point",
	"dblink",
	"dblink_exec",
	"dblink_connect",
}

// dangerousRe matches any of the dangerous words at word boundaries.
var dangerousRe = compileDangerousRe()

// dangerousFuncRe matches dangerous function calls with optional schema prefix.
// e.g. pg_sleep(, public.pg_sleep(, pg_terminate_backend (
var dangerousFuncRe = compileDangerousFuncRe()

func compileDangerousRe() *regexp.Regexp {
	parts := make([]string, len(dangerousWords))
	for i, w := range dangerousWords {
		parts[i] = `\b` + w + `\b`
	}
	return regexp.MustCompile(`(?i)` + strings.Join(parts, "|"))
}

func compileDangerousFuncRe() *regexp.Regexp {
	parts := make([]string, len(dangerousFuncs))
	copy(parts, dangerousFuncs)
	pattern := `(?i)(?:\b\w+\.)?(?:` + strings.Join(parts, "|") + `)\s*\(`
	return regexp.MustCompile(pattern)
}

// LintQuery validates that sql is a safe read-only SELECT statement.
// Returns nil if the query passes, or a descriptive error.
func LintQuery(sql string) error {
	trimmed := strings.TrimSpace(sql)
	if trimmed == "" {
		return fmt.Errorf("empty SQL")
	}

	upper := strings.ToUpper(trimmed)
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return fmt.Errorf("query must start with SELECT or WITH")
	}

	// Check for embedded semicolons (multiple statements).
	// Strip trailing semicolons/whitespace first.
	body := strings.TrimRight(trimmed, "; \t\n\r")
	if strings.Contains(body, ";") {
		return fmt.Errorf("query contains embedded semicolons (multiple statements)")
	}

	if m := dangerousRe.FindString(body); m != "" {
		return fmt.Errorf("query contains disallowed keyword: %s", strings.ToUpper(m))
	}

	if m := dangerousFuncRe.FindString(body); m != "" {
		return fmt.Errorf("query calls disallowed function: %s", strings.TrimSpace(m))
	}

	return nil
}
