package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// Elevarq/Arq-Signals#210: pg_stat_database_v1 emits the PG 14+
// session/timing fields — real columns on PG 14+ (per-major override),
// typed NULL stubs on PG < 14 (default SQL). Unblocks the connection-
// churn detector.

var sessionFields = []string{
	"session_time",
	"active_time",
	"idle_in_transaction_time",
	"sessions",
	"sessions_abandoned",
	"sessions_fatal",
	"sessions_killed",
}

// On every supported major (14–18) the resolved SQL selects the real
// session columns and registers an override.
func TestStatDatabaseSessionsRealOnPG14Plus(t *testing.T) {
	for _, m := range []int{14, 15, 16, 17, 18} {
		out := pgqueries.Filter(pgqueries.FilterParams{PGMajorVersion: m})
		q := findQueryByID(out, "pg_stat_database_v1")
		if q == nil {
			t.Fatalf("pg_stat_database_v1 missing from PG %d filter result", m)
		}
		for _, f := range sessionFields {
			if !strings.Contains(q.SQL, f) {
				t.Errorf("PG %d SQL must select %q, got:\n%s", m, f, q.SQL)
			}
		}
		// Real columns on PG 14+, not the NULL stubs the default uses.
		if strings.Contains(q.SQL, "NULL::bigint AS sessions") {
			t.Errorf("PG %d SQL must emit the real sessions column, not a NULL stub", m)
		}
		if !pgqueries.HasOverride(m, "pg_stat_database_v1") {
			t.Errorf("PG %d should have a pg_stat_database_v1 override registered", m)
		}
	}
}

// The default SQL (PG < 14 path) exposes the session columns as typed
// NULL stubs so the column set stays stable across majors.
func TestStatDatabaseSessionsNullStubInDefault(t *testing.T) {
	q := pgqueries.ByID("pg_stat_database_v1")
	if q == nil {
		t.Fatal("pg_stat_database_v1 not registered")
	}
	stubs := []string{
		"NULL::double precision AS session_time",
		"NULL::double precision AS active_time",
		"NULL::double precision AS idle_in_transaction_time",
		"NULL::bigint AS sessions",
		"NULL::bigint AS sessions_abandoned",
		"NULL::bigint AS sessions_fatal",
		"NULL::bigint AS sessions_killed",
	}
	for _, s := range stubs {
		if !strings.Contains(q.SQL, s) {
			t.Errorf("default SQL must expose %q for canonical schema, got:\n%s", s, q.SQL)
		}
	}
}

// Both the default and the PG 14+ override pass the read-only safety
// linter (ARQ-SIGNALS-R002).
func TestStatDatabaseSessionsSQLLints(t *testing.T) {
	if err := pgqueries.LintQuery(pgqueries.ByID("pg_stat_database_v1").SQL); err != nil {
		t.Errorf("default pg_stat_database_v1 SQL failed linter: %v", err)
	}
	for _, m := range []int{14, 15, 16, 17, 18} {
		out := pgqueries.Filter(pgqueries.FilterParams{PGMajorVersion: m})
		q := findQueryByID(out, "pg_stat_database_v1")
		if err := pgqueries.LintQuery(q.SQL); err != nil {
			t.Errorf("PG %d pg_stat_database_v1 SQL failed linter: %v", m, err)
		}
	}
}

// The logical ID and the always-present base columns are stable across
// majors (R081 stable-ID + canonical schema).
func TestStatDatabaseStableBaseColumns(t *testing.T) {
	for _, m := range []int{13, 14, 18} {
		out := pgqueries.Filter(pgqueries.FilterParams{PGMajorVersion: m})
		q := findQueryByID(out, "pg_stat_database_v1")
		if q == nil {
			t.Fatalf("pg_stat_database_v1 missing from PG %d filter result", m)
		}
		for _, base := range []string{"datname", "numbackends", "xact_commit", "stats_reset"} {
			if !strings.Contains(q.SQL, base) {
				t.Errorf("PG %d SQL must keep base column %q", m, base)
			}
		}
		// Session columns present in every variant (real or NULL stub).
		for _, f := range sessionFields {
			if !strings.Contains(q.SQL, f) {
				t.Errorf("PG %d SQL must include session column %q (real or NULL stub)", m, f)
			}
		}
	}
}
