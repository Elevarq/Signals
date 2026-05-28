package collector

import (
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// R075 (revised v2, issue #6): when high-sensitivity is opted out and
// the collector declares SensitiveColumns, those columns are zeroed in
// every row before persistence. Non-sensitive columns survive.
func TestRedactHighSensitivityColumnsIfNeeded(t *testing.T) {
	q := pgqueries.QueryDef{
		ID:               "long_running_txns_v1",
		HighSensitivity:  true,
		SensitiveColumns: []string{"query_snippet"},
	}
	rows := []map[string]any{
		{"pid": 1234, "wait_event": "ClientRead", "query_snippet": "SELECT secret FROM users"},
		{"pid": 5678, "wait_event": "Lock", "query_snippet": "UPDATE accounts SET balance=9.99"},
	}

	// Opted out: sensitive column is nulled; non-sensitive columns survive.
	redactHighSensitivityColumnsIfNeeded(q, rows, false)
	for i, row := range rows {
		if row["query_snippet"] != nil {
			t.Errorf("row[%d].query_snippet not redacted: %v", i, row["query_snippet"])
		}
		if row["pid"] == nil {
			t.Errorf("row[%d].pid (non-sensitive) was redacted", i)
		}
		if row["wait_event"] == nil {
			t.Errorf("row[%d].wait_event (non-sensitive) was redacted", i)
		}
	}
}

func TestRedactHighSensitivityColumnsNoOpWhenEnabled(t *testing.T) {
	q := pgqueries.QueryDef{
		ID:               "long_running_txns_v1",
		HighSensitivity:  true,
		SensitiveColumns: []string{"query_snippet"},
	}
	rows := []map[string]any{{"query_snippet": "SELECT 1"}}
	redactHighSensitivityColumnsIfNeeded(q, rows, true)
	if rows[0]["query_snippet"] != "SELECT 1" {
		t.Errorf("with HighSensitivityEnabled=true the column must not be redacted; got %v", rows[0]["query_snippet"])
	}
}

func TestRedactHighSensitivityColumnsNoOpWhenSkipCollector(t *testing.T) {
	// A HighSensitivity collector with no SensitiveColumns is the
	// skip-on-opt-out path (DDL definitions etc.). Filter drops it
	// before execution, but the redact function must be a no-op anyway.
	q := pgqueries.QueryDef{
		ID:              "pg_views_definitions_v1",
		HighSensitivity: true,
	}
	rows := []map[string]any{{"definition": "CREATE VIEW v AS SELECT 1"}}
	redactHighSensitivityColumnsIfNeeded(q, rows, false)
	if rows[0]["definition"] != "CREATE VIEW v AS SELECT 1" {
		t.Errorf("collector without SensitiveColumns must not be touched by the redact function; got %v", rows[0]["definition"])
	}
}
