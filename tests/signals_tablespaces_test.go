package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// pg_tablespaces_v1 — tablespaces with per-tablespace GUC overrides and size.
// On hyperscalers only pg_default/pg_global exist; the presence-only output
// still carries meaning (confirms no custom tablespaces).
//
// Specification: specifications/collectors/pg_tablespaces_v1.md
// ---------------------------------------------------------------------------

func TestTablespacesCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_tablespaces_v1")
	if q == nil {
		t.Fatal("pg_tablespaces_v1 is not registered")
	}
	if q.Category != "server" {
		t.Errorf("category: got %q, want %q", q.Category, "server")
	}
}

func TestTablespacesCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_tablespaces_v1")
	if q == nil {
		t.Fatal("pg_tablespaces_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_tablespaces_v1 failed linter: %v", err)
	}
}

func TestTablespacesCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_tablespaces_v1")
	if q == nil {
		t.Fatal("pg_tablespaces_v1 not registered")
	}
	if q.Cadence != pgqueries.CadenceDaily {
		t.Errorf("cadence: got %v, want CadenceDaily (24h)", q.Cadence)
	}
}

func TestTablespacesCollectorRetention(t *testing.T) {
	q := pgqueries.ByID("pg_tablespaces_v1")
	if q == nil {
		t.Fatal("pg_tablespaces_v1 not registered")
	}
	if q.RetentionClass != pgqueries.RetentionMedium {
		t.Errorf("retention: got %q, want RetentionMedium", q.RetentionClass)
	}
}

func TestTablespacesCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_tablespaces_v1")
	if q == nil {
		t.Fatal("pg_tablespaces_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

func TestTablespacesCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_tablespaces_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_tablespaces_v1 must be included on PG 14")
	}
}

func TestTablespacesCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_tablespaces_v1")
	if q == nil {
		t.Fatal("pg_tablespaces_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_tablespaces_v1 must have ORDER BY for deterministic output")
	}
}

func TestTablespacesCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_tablespaces_v1")
	if q == nil {
		t.Fatal("pg_tablespaces_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_tablespaces_v1 must not use SELECT *")
	}
}

func TestTablespacesCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_tablespaces_v1")
	if q == nil {
		t.Fatal("pg_tablespaces_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"spcname", "spcowner_oid", "spcoptions_raw",
		"seq_page_cost", "random_page_cost",
		"effective_io_concurrency", "maintenance_io_concurrency",
		"size_bytes",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_tablespaces_v1 must include column %q", col)
		}
	}
}

func TestTablespacesCollectorUsesPgTablespace(t *testing.T) {
	q := pgqueries.ByID("pg_tablespaces_v1")
	if q == nil {
		t.Fatal("pg_tablespaces_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_tablespace") {
		t.Error("pg_tablespaces_v1 must query pg_tablespace")
	}
}

// The per-tablespace GUCs are extracted from spcoptions via
// pg_options_to_table, not from pg_settings. Important because cluster-level
// pg_settings values do not reflect per-tablespace overrides.
func TestTablespacesCollectorExtractsOptionsViaOptionsToTable(t *testing.T) {
	q := pgqueries.ByID("pg_tablespaces_v1")
	if q == nil {
		t.Fatal("pg_tablespaces_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_options_to_table") {
		t.Error("pg_tablespaces_v1 must use pg_options_to_table to decode spcoptions")
	}
}

func TestTablespacesCollectorEmitsSize(t *testing.T) {
	q := pgqueries.ByID("pg_tablespaces_v1")
	if q == nil {
		t.Fatal("pg_tablespaces_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_tablespace_size") {
		t.Error("pg_tablespaces_v1 must compute size via pg_tablespace_size()")
	}
}
