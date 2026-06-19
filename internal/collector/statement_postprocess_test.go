package collector

import "testing"

// REDACT-R001 wiring (#188): redactStatementSecretsIfNeeded masks the
// statement-text column for pg_stat_statements_v1 in place, and is a no-op
// for every other collector.
func TestRedactStatementSecretsIfNeeded(t *testing.T) {
	t.Run("redacts pg_stat_statements query column", func(t *testing.T) {
		rows := []map[string]any{
			{"query": "CREATE ROLE signals WITH LOGIN PASSWORD 'monitor_pass'", "calls": int64(1)},
			{"query": "SELECT * FROM t WHERE id = $1", "calls": int64(9)},
		}
		redactStatementSecretsIfNeeded("pg_stat_statements_v1", rows)
		if got := rows[0]["query"]; got != "CREATE ROLE signals WITH LOGIN PASSWORD '<redacted>'" {
			t.Errorf("password not redacted: %q", got)
		}
		if got := rows[1]["query"]; got != "SELECT * FROM t WHERE id = $1" {
			t.Errorf("normal query must be unchanged: %q", got)
		}
		if rows[0]["calls"] != int64(1) {
			t.Errorf("non-text columns must be untouched")
		}
	})

	t.Run("no-op for other collectors", func(t *testing.T) {
		rows := []map[string]any{{"query": "CREATE ROLE x PASSWORD 'leak'"}}
		redactStatementSecretsIfNeeded("pg_stat_activity_v1", rows)
		if got := rows[0]["query"]; got != "CREATE ROLE x PASSWORD 'leak'" {
			t.Errorf("must not touch non-registered collectors; got %q", got)
		}
	})
}
