package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// pg_prepared_xacts_v1 — prepared (two-phase-commit) transactions with
// derived age. Orphaned 2PC holds back xmin and blocks vacuum.
//
// Specification: specifications/collectors/pg_prepared_xacts_v1.md
// ---------------------------------------------------------------------------

func TestPreparedXactsCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_prepared_xacts_v1")
	if q == nil {
		t.Fatal("pg_prepared_xacts_v1 is not registered")
	}
	if q.Category != "wraparound" {
		t.Errorf("category: got %q, want %q", q.Category, "wraparound")
	}
}

func TestPreparedXactsCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_prepared_xacts_v1")
	if q == nil {
		t.Fatal("pg_prepared_xacts_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_prepared_xacts_v1 failed linter: %v", err)
	}
}

func TestPreparedXactsCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_prepared_xacts_v1")
	if q == nil {
		t.Fatal("pg_prepared_xacts_v1 not registered")
	}
	if q.Cadence != pgqueries.Cadence1h {
		t.Errorf("cadence: got %v, want Cadence1h", q.Cadence)
	}
}

func TestPreparedXactsCollectorRetention(t *testing.T) {
	q := pgqueries.ByID("pg_prepared_xacts_v1")
	if q == nil {
		t.Fatal("pg_prepared_xacts_v1 not registered")
	}
	if q.RetentionClass != pgqueries.RetentionMedium {
		t.Errorf("retention: got %q, want RetentionMedium", q.RetentionClass)
	}
}

func TestPreparedXactsCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_prepared_xacts_v1")
	if q == nil {
		t.Fatal("pg_prepared_xacts_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

func TestPreparedXactsCollectorIncludedOnPG10(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 10,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_prepared_xacts_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_prepared_xacts_v1 must be included on PG 10+")
	}
}

func TestPreparedXactsCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_prepared_xacts_v1")
	if q == nil {
		t.Fatal("pg_prepared_xacts_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_prepared_xacts_v1 must have ORDER BY for deterministic output")
	}
}

func TestPreparedXactsCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_prepared_xacts_v1")
	if q == nil {
		t.Fatal("pg_prepared_xacts_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_prepared_xacts_v1 must not use SELECT *")
	}
}

func TestPreparedXactsCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_prepared_xacts_v1")
	if q == nil {
		t.Fatal("pg_prepared_xacts_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"transaction", "gid", "prepared", "owner", "database",
		"age_seconds", "age_xids",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_prepared_xacts_v1 must include column %q", col)
		}
	}
}

func TestPreparedXactsCollectorUsesPgPreparedXacts(t *testing.T) {
	q := pgqueries.ByID("pg_prepared_xacts_v1")
	if q == nil {
		t.Fatal("pg_prepared_xacts_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_prepared_xacts") {
		t.Error("pg_prepared_xacts_v1 must query pg_prepared_xacts")
	}
}

// Derived age_seconds is computed server-side — the analyzer does not redo it.
func TestPreparedXactsCollectorComputesAgeServerSide(t *testing.T) {
	q := pgqueries.ByID("pg_prepared_xacts_v1")
	if q == nil {
		t.Fatal("pg_prepared_xacts_v1 not registered")
	}
	if !containsCI(q.SQL, "EXTRACT") || !containsCI(q.SQL, "prepared") {
		t.Error("pg_prepared_xacts_v1 must compute age_seconds via EXTRACT(EPOCH FROM now() - prepared)")
	}
}
