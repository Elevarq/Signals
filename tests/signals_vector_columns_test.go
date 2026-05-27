package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// pg_vector_columns_v1 — pgvector column inventory.
//
// Gated on the 'vector' extension. When pgvector is absent, the collector
// is filtered out by pgqueries.Filter() — no sentinel, no output, no error.
// When pgvector is installed but no column uses the type (a normal state:
// the extension can be installed without being used), the query returns
// an empty rowset — also normal, not a failure.
//
// Specification: specifications/collectors/pg_vector_columns_v1.md
// ---------------------------------------------------------------------------

func TestVectorColumnsCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_vector_columns_v1")
	if q == nil {
		t.Fatal("pg_vector_columns_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
}

func TestVectorColumnsCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_vector_columns_v1")
	if q == nil {
		t.Fatal("pg_vector_columns_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_vector_columns_v1 failed linter: %v", err)
	}
}

func TestVectorColumnsCollectorRequiresVectorExtension(t *testing.T) {
	q := pgqueries.ByID("pg_vector_columns_v1")
	if q == nil {
		t.Fatal("pg_vector_columns_v1 not registered")
	}
	if q.RequiresExtension != "vector" {
		t.Errorf("RequiresExtension: got %q, want %q", q.RequiresExtension, "vector")
	}
}

func TestVectorColumnsCollectorMinPGVersion(t *testing.T) {
	q := pgqueries.ByID("pg_vector_columns_v1")
	if q == nil {
		t.Fatal("pg_vector_columns_v1 not registered")
	}
	if q.MinPGVersion != 14 {
		t.Errorf("MinPGVersion: got %d, want 14 (attcompression requires PG 14+)", q.MinPGVersion)
	}
}

func TestVectorColumnsCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_vector_columns_v1")
	if q == nil {
		t.Fatal("pg_vector_columns_v1 not registered")
	}
	if q.Cadence != pgqueries.CadenceDaily {
		t.Errorf("cadence: got %v, want CadenceDaily (24h)", q.Cadence)
	}
}

func TestVectorColumnsCollectorRetention(t *testing.T) {
	q := pgqueries.ByID("pg_vector_columns_v1")
	if q == nil {
		t.Fatal("pg_vector_columns_v1 not registered")
	}
	if q.RetentionClass != pgqueries.RetentionMedium {
		t.Errorf("retention: got %q, want RetentionMedium", q.RetentionClass)
	}
}

func TestVectorColumnsCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_vector_columns_v1")
	if q == nil {
		t.Fatal("pg_vector_columns_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

// Extension absent → filtered out entirely. No sentinel, no run.
func TestVectorColumnsCollectorFilteredWhenExtensionAbsent(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 16,
		Extensions:     []string{}, // no extensions installed
	})
	for _, q := range filtered {
		if q.ID == "pg_vector_columns_v1" {
			t.Error("pg_vector_columns_v1 must be filtered out when pgvector is not installed")
		}
	}
}

// Extension present AND PG version sufficient → eligible.
func TestVectorColumnsCollectorEligibleWhenExtensionPresent(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 16,
		Extensions:     []string{"vector"},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_vector_columns_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_vector_columns_v1 must be eligible when pgvector is installed on PG 14+")
	}
}

// Extension present but PG too old → filtered by MinPGVersion.
func TestVectorColumnsCollectorExcludedOnPG13(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 13,
		Extensions:     []string{"vector"},
	})
	for _, q := range filtered {
		if q.ID == "pg_vector_columns_v1" {
			t.Error("pg_vector_columns_v1 must be excluded on PG 13 (attcompression needs PG 14+)")
		}
	}
}

func TestVectorColumnsCollectorExcludesSystemSchemas(t *testing.T) {
	q := pgqueries.ByID("pg_vector_columns_v1")
	if q == nil {
		t.Fatal("pg_vector_columns_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, schema := range []string{"pg_catalog", "information_schema", "pg_toast", "pg_temp_"} {
		if !strings.Contains(sql, schema) {
			t.Errorf("pg_vector_columns_v1 must filter out %q", schema)
		}
	}
}

// Type filter — the three pgvector types.
func TestVectorColumnsCollectorFiltersToVectorTypes(t *testing.T) {
	q := pgqueries.ByID("pg_vector_columns_v1")
	if q == nil {
		t.Fatal("pg_vector_columns_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, typ := range []string{"'vector'", "'halfvec'", "'sparsevec'"} {
		if !strings.Contains(sql, typ) {
			t.Errorf("pg_vector_columns_v1 must filter to vector type family; missing %s", typ)
		}
	}
}

func TestVectorColumnsCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_vector_columns_v1")
	if q == nil {
		t.Fatal("pg_vector_columns_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_vector_columns_v1 must have ORDER BY for deterministic output")
	}
}

func TestVectorColumnsCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_vector_columns_v1")
	if q == nil {
		t.Fatal("pg_vector_columns_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_vector_columns_v1 must not use SELECT *")
	}
}

func TestVectorColumnsCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_vector_columns_v1")
	if q == nil {
		t.Fatal("pg_vector_columns_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"relid", "schemaname", "relname", "attname",
		"atttypname", "dimension", "avg_width",
		"attstorage", "attcompression",
		"likely_toasted", "has_index", "index_types",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_vector_columns_v1 must include column %q", col)
		}
	}
}

func TestVectorColumnsCollectorFiltersSystemColumns(t *testing.T) {
	q := pgqueries.ByID("pg_vector_columns_v1")
	if q == nil {
		t.Fatal("pg_vector_columns_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	if !strings.Contains(sql, "attnum > 0") {
		t.Error("pg_vector_columns_v1 must filter attnum > 0")
	}
	if !strings.Contains(sql, "attisdropped") {
		t.Error("pg_vector_columns_v1 must filter out dropped columns")
	}
}
