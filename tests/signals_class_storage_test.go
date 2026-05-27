package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// pg_class_storage_v1 — per-relation storage accounting including TOAST
// sub-relation sizes. Ground truth for the TOAST planner blind-spot and
// bloat detectors.
//
// Specification: specifications/collectors/pg_class_storage_v1.md
// ---------------------------------------------------------------------------

func TestClassStorageCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_class_storage_v1")
	if q == nil {
		t.Fatal("pg_class_storage_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
}

func TestClassStorageCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_class_storage_v1")
	if q == nil {
		t.Fatal("pg_class_storage_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_class_storage_v1 failed linter: %v", err)
	}
}

func TestClassStorageCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_class_storage_v1")
	if q == nil {
		t.Fatal("pg_class_storage_v1 not registered")
	}
	if q.Cadence != pgqueries.CadenceDaily {
		t.Errorf("cadence: got %v, want CadenceDaily (24h)", q.Cadence)
	}
}

func TestClassStorageCollectorRetention(t *testing.T) {
	q := pgqueries.ByID("pg_class_storage_v1")
	if q == nil {
		t.Fatal("pg_class_storage_v1 not registered")
	}
	if q.RetentionClass != pgqueries.RetentionMedium {
		t.Errorf("retention: got %q, want RetentionMedium", q.RetentionClass)
	}
}

func TestClassStorageCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_class_storage_v1")
	if q == nil {
		t.Fatal("pg_class_storage_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

func TestClassStorageCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_class_storage_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_class_storage_v1 must be included on PG 14")
	}
}

func TestClassStorageCollectorExcludesSystemSchemas(t *testing.T) {
	q := pgqueries.ByID("pg_class_storage_v1")
	if q == nil {
		t.Fatal("pg_class_storage_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, schema := range []string{"pg_catalog", "information_schema", "pg_toast", "pg_temp_"} {
		if !strings.Contains(sql, schema) {
			t.Errorf("pg_class_storage_v1 must filter out %q", schema)
		}
	}
}

func TestClassStorageCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_class_storage_v1")
	if q == nil {
		t.Fatal("pg_class_storage_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_class_storage_v1 must have ORDER BY for deterministic output")
	}
}

func TestClassStorageCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_class_storage_v1")
	if q == nil {
		t.Fatal("pg_class_storage_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_class_storage_v1 must not use SELECT *")
	}
}

// Filter must scope to user-visible relation kinds (tables, matviews,
// partitioned parents). TOAST rels are accessed through reltoastrelid.
func TestClassStorageCollectorFiltersRelkind(t *testing.T) {
	q := pgqueries.ByID("pg_class_storage_v1")
	if q == nil {
		t.Fatal("pg_class_storage_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	if !strings.Contains(sql, "relkind in") {
		t.Error("pg_class_storage_v1 must filter by relkind (e.g. 'r','m','p')")
	}
}

func TestClassStorageCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_class_storage_v1")
	if q == nil {
		t.Fatal("pg_class_storage_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"relid", "schemaname", "relname", "relkind",
		"relpersistence", "relispartition", "relhasindex",
		"reltuples", "relpages", "relallvisible",
		"relfrozenxid", "relminmxid",
		"reltoastrelid", "has_toast", "toast_pages", "toast_relpages_index",
		"main_bytes", "toast_bytes", "indexes_bytes", "total_bytes",
		"reloptions", "tablespace",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_class_storage_v1 must include column %q", col)
		}
	}
}

// has_toast is emitted explicitly so the analyzer does not need to
// special-case OID zero.
func TestClassStorageCollectorEmitsHasToastExplicitly(t *testing.T) {
	q := pgqueries.ByID("pg_class_storage_v1")
	if q == nil {
		t.Fatal("pg_class_storage_v1 not registered")
	}
	if !containsCI(q.SQL, "has_toast") {
		t.Error("pg_class_storage_v1 must emit a has_toast boolean column")
	}
}

func TestClassStorageCollectorUsesTotalRelationSize(t *testing.T) {
	q := pgqueries.ByID("pg_class_storage_v1")
	if q == nil {
		t.Fatal("pg_class_storage_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_total_relation_size") {
		t.Error("pg_class_storage_v1 must use pg_total_relation_size() for total_bytes")
	}
}

// Spec override: 60s timeout for large schemas where pg_total_relation_size
// can involve many per-fork lseeks.
func TestClassStorageCollectorTimeoutRaised(t *testing.T) {
	q := pgqueries.ByID("pg_class_storage_v1")
	if q == nil {
		t.Fatal("pg_class_storage_v1 not registered")
	}
	// Expect at least 30s — the baseline collector timeout is 10s.
	if q.Timeout.Seconds() < 30 {
		t.Errorf("Timeout: got %v, want >= 30s for size-function cost on large schemas", q.Timeout)
	}
}
