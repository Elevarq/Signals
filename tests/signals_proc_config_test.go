package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// pg_proc_config_v1 — functions with per-function SET overrides
// (pg_proc.proconfig IS NOT NULL).
//
// Specification: specifications/collectors/pg_proc_config_v1.md
// ---------------------------------------------------------------------------

func TestProcConfigCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_proc_config_v1")
	if q == nil {
		t.Fatal("pg_proc_config_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
}

func TestProcConfigCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_proc_config_v1")
	if q == nil {
		t.Fatal("pg_proc_config_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_proc_config_v1 failed linter: %v", err)
	}
}

func TestProcConfigCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_proc_config_v1")
	if q == nil {
		t.Fatal("pg_proc_config_v1 not registered")
	}
	if q.Cadence != pgqueries.CadenceDaily {
		t.Errorf("cadence: got %v, want CadenceDaily (24h)", q.Cadence)
	}
}

func TestProcConfigCollectorRetention(t *testing.T) {
	q := pgqueries.ByID("pg_proc_config_v1")
	if q == nil {
		t.Fatal("pg_proc_config_v1 not registered")
	}
	if q.RetentionClass != pgqueries.RetentionMedium {
		t.Errorf("retention: got %q, want RetentionMedium", q.RetentionClass)
	}
}

func TestProcConfigCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_proc_config_v1")
	if q == nil {
		t.Fatal("pg_proc_config_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

func TestProcConfigCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_proc_config_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_proc_config_v1 must be included on PG 14")
	}
}

func TestProcConfigCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_proc_config_v1")
	if q == nil {
		t.Fatal("pg_proc_config_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_proc_config_v1 must have ORDER BY for deterministic output")
	}
}

func TestProcConfigCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_proc_config_v1")
	if q == nil {
		t.Fatal("pg_proc_config_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_proc_config_v1 must not use SELECT *")
	}
}

func TestProcConfigCollectorExcludesSystemSchemas(t *testing.T) {
	q := pgqueries.ByID("pg_proc_config_v1")
	if q == nil {
		t.Fatal("pg_proc_config_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, schema := range []string{"pg_catalog", "information_schema", "pg_toast", "pg_temp_"} {
		if !strings.Contains(sql, schema) {
			t.Errorf("pg_proc_config_v1 must filter out %q", schema)
		}
	}
}

func TestProcConfigCollectorFiltersNullProconfig(t *testing.T) {
	q := pgqueries.ByID("pg_proc_config_v1")
	if q == nil {
		t.Fatal("pg_proc_config_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	// Must emit only rows where proconfig is populated.
	if !strings.Contains(sql, "proconfig is not null") {
		t.Error("pg_proc_config_v1 must filter proconfig IS NOT NULL — functions without overrides are out of scope")
	}
}

func TestProcConfigCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_proc_config_v1")
	if q == nil {
		t.Fatal("pg_proc_config_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"funcid", "schemaname", "funcname", "proargtypes_oids",
		"prolang_name", "provolatile", "proisstrict", "prosecdef", "proconfig",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_proc_config_v1 must include column %q", col)
		}
	}
}

func TestProcConfigCollectorUsesPgProc(t *testing.T) {
	q := pgqueries.ByID("pg_proc_config_v1")
	if q == nil {
		t.Fatal("pg_proc_config_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_proc") {
		t.Error("pg_proc_config_v1 must query pg_proc")
	}
}
