package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// pg_attribute_storage_v1 — per-column storage configuration (attstorage,
// attcompression, average width). Drives TOAST-amplification and
// vector-column-storage advice.
//
// Specification: specifications/collectors/pg_attribute_storage_v1.md
// ---------------------------------------------------------------------------

func TestAttributeStorageCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_attribute_storage_v1")
	if q == nil {
		t.Fatal("pg_attribute_storage_v1 is not registered")
		return
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
}

func TestAttributeStorageCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_attribute_storage_v1")
	if q == nil {
		t.Fatal("pg_attribute_storage_v1 not registered")
		return
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_attribute_storage_v1 failed linter: %v", err)
	}
}

func TestAttributeStorageCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_attribute_storage_v1")
	if q == nil {
		t.Fatal("pg_attribute_storage_v1 not registered")
		return
	}
	if q.Cadence != pgqueries.CadenceDaily {
		t.Errorf("cadence: got %v, want CadenceDaily (24h)", q.Cadence)
	}
}

func TestAttributeStorageCollectorRetention(t *testing.T) {
	q := pgqueries.ByID("pg_attribute_storage_v1")
	if q == nil {
		t.Fatal("pg_attribute_storage_v1 not registered")
		return
	}
	if q.RetentionClass != pgqueries.RetentionMedium {
		t.Errorf("retention: got %q, want RetentionMedium", q.RetentionClass)
	}
}

func TestAttributeStorageCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_attribute_storage_v1")
	if q == nil {
		t.Fatal("pg_attribute_storage_v1 not registered")
		return
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

// attcompression was introduced in PG 14. The spec notes PG <14 would
// need a separate collector variant; for this v1 we require PG 14+.
func TestAttributeStorageCollectorMinPGVersion(t *testing.T) {
	q := pgqueries.ByID("pg_attribute_storage_v1")
	if q == nil {
		t.Fatal("pg_attribute_storage_v1 not registered")
		return
	}
	if q.MinPGVersion != 14 {
		t.Errorf("MinPGVersion: got %d, want 14 (attcompression requires PG 14+)", q.MinPGVersion)
	}
}

func TestAttributeStorageCollectorExcludedOnPG13(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 13,
		Extensions:     []string{},
	})
	for _, q := range filtered {
		if q.ID == "pg_attribute_storage_v1" {
			t.Error("pg_attribute_storage_v1 must be excluded on PG 13")
		}
	}
}

func TestAttributeStorageCollectorExcludesSystemSchemas(t *testing.T) {
	q := pgqueries.ByID("pg_attribute_storage_v1")
	if q == nil {
		t.Fatal("pg_attribute_storage_v1 not registered")
		return
	}
	sql := strings.ToLower(q.SQL)
	for _, schema := range []string{"pg_catalog", "information_schema", "pg_toast", "pg_temp_"} {
		if !strings.Contains(sql, schema) {
			t.Errorf("pg_attribute_storage_v1 must filter out %q", schema)
		}
	}
}

func TestAttributeStorageCollectorFiltersSystemColumns(t *testing.T) {
	q := pgqueries.ByID("pg_attribute_storage_v1")
	if q == nil {
		t.Fatal("pg_attribute_storage_v1 not registered")
		return
	}
	sql := strings.ToLower(q.SQL)
	if !strings.Contains(sql, "attnum > 0") {
		t.Error("pg_attribute_storage_v1 must filter attnum > 0 to exclude system columns")
	}
}

func TestAttributeStorageCollectorFiltersDroppedColumns(t *testing.T) {
	q := pgqueries.ByID("pg_attribute_storage_v1")
	if q == nil {
		t.Fatal("pg_attribute_storage_v1 not registered")
		return
	}
	sql := strings.ToLower(q.SQL)
	if !strings.Contains(sql, "attisdropped") {
		t.Error("pg_attribute_storage_v1 must filter out dropped columns")
	}
}

func TestAttributeStorageCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_attribute_storage_v1")
	if q == nil {
		t.Fatal("pg_attribute_storage_v1 not registered")
		return
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_attribute_storage_v1 must have ORDER BY for deterministic output")
	}
}

func TestAttributeStorageCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_attribute_storage_v1")
	if q == nil {
		t.Fatal("pg_attribute_storage_v1 not registered")
		return
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_attribute_storage_v1 must not use SELECT *")
	}
}

func TestAttributeStorageCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_attribute_storage_v1")
	if q == nil {
		t.Fatal("pg_attribute_storage_v1 not registered")
		return
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"relid", "schemaname", "relname",
		"attnum", "attname", "atttypid", "atttypname",
		"attstorage", "attcompression",
		"atttypmod", "attnotnull", "attstattarget", "avg_width",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_attribute_storage_v1 must include column %q", col)
		}
	}
}

func TestAttributeStorageCollectorUsesPgAttribute(t *testing.T) {
	q := pgqueries.ByID("pg_attribute_storage_v1")
	if q == nil {
		t.Fatal("pg_attribute_storage_v1 not registered")
		return
	}
	if !containsCI(q.SQL, "pg_attribute") {
		t.Error("pg_attribute_storage_v1 must use pg_attribute")
	}
}

// avg_width comes from pg_stats; LEFT JOIN because not every column has stats.
func TestAttributeStorageCollectorLeftJoinsStats(t *testing.T) {
	q := pgqueries.ByID("pg_attribute_storage_v1")
	if q == nil {
		t.Fatal("pg_attribute_storage_v1 not registered")
		return
	}
	sql := strings.ToLower(q.SQL)
	if !strings.Contains(sql, "left join") || !strings.Contains(sql, "pg_stats") {
		t.Error("pg_attribute_storage_v1 must LEFT JOIN pg_stats so missing stats yield NULL, not drop rows")
	}
}
