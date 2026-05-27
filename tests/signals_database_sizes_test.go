package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// database_sizes_v1 — extended per-database accounting. These tests cover
// the additive extension over the original (database_name, size_bytes,
// connection_limit, xid_age) shape: catalog identity, encoding, locale,
// tablespace, mxid_age. The original columns remain for backward compat.
//
// The original spec note said pg_database_v1 would supersede
// database_sizes_v1; per the latest decision, database_sizes_v1 is
// extended in place and pg_database_v1 is retired.
//
// Specification: specifications/collectors/database_sizes_v1.md
// ---------------------------------------------------------------------------

// TC-DBSIZE-01: Legacy columns preserved — analyzer/export consumers that
// read the original column names keep working.
func TestDatabaseSizesCollectorLegacyColumnsPreserved(t *testing.T) {
	q := pgqueries.ByID("database_sizes_v1")
	if q == nil {
		t.Fatal("database_sizes_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, col := range []string{"database_name", "size_bytes", "connection_limit", "xid_age"} {
		if !strings.Contains(sql, col) {
			t.Errorf("database_sizes_v1 must keep legacy column %q for backward compat", col)
		}
	}
}

// TC-DBSIZE-02: Extended columns present — the pg_database_v1-style
// catalog identity.
func TestDatabaseSizesCollectorExtendedColumns(t *testing.T) {
	q := pgqueries.ByID("database_sizes_v1")
	if q == nil {
		t.Fatal("database_sizes_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"datid", "datdba_oid", "encoding_name",
		"datcollate", "datctype", "datallowconn",
		"tablespace_name", "dat_minmxid_age",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("database_sizes_v1 must include extended column %q", col)
		}
	}
}

// Scope filter retained — templates stay excluded (changing this would
// break analyzer consumers that assume non-template-only output).
func TestDatabaseSizesCollectorExcludesTemplates(t *testing.T) {
	q := pgqueries.ByID("database_sizes_v1")
	if q == nil {
		t.Fatal("database_sizes_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	if !strings.Contains(sql, "datistemplate = false") {
		t.Error("database_sizes_v1 must retain the template-exclusion filter")
	}
}

// tablespace_name comes from a LEFT JOIN on pg_tablespace.
func TestDatabaseSizesCollectorJoinsTablespace(t *testing.T) {
	q := pgqueries.ByID("database_sizes_v1")
	if q == nil {
		t.Fatal("database_sizes_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	if !strings.Contains(sql, "pg_tablespace") {
		t.Error("database_sizes_v1 must join pg_tablespace for tablespace_name")
	}
	if !strings.Contains(sql, "left join") {
		t.Error("pg_tablespace join must be LEFT JOIN so any row with a missing tablespace row doesn't vanish")
	}
}

func TestDatabaseSizesCollectorMxidAge(t *testing.T) {
	q := pgqueries.ByID("database_sizes_v1")
	if q == nil {
		t.Fatal("database_sizes_v1 not registered")
	}
	if !containsCI(q.SQL, "mxid_age") {
		t.Error("database_sizes_v1 must include mxid_age(datminmxid) for multixact wraparound visibility")
	}
}

func TestDatabaseSizesCollectorEncodingReadable(t *testing.T) {
	q := pgqueries.ByID("database_sizes_v1")
	if q == nil {
		t.Fatal("database_sizes_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_encoding_to_char") {
		t.Error("database_sizes_v1 must use pg_encoding_to_char() so encoding_name is human-readable, not an integer")
	}
}

// Linter must still pass after the extension.
func TestDatabaseSizesCollectorLinterStillPasses(t *testing.T) {
	q := pgqueries.ByID("database_sizes_v1")
	if q == nil {
		t.Fatal("database_sizes_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("extended database_sizes_v1 failed linter: %v", err)
	}
}

// Ordering invariant preserved — biggest first.
func TestDatabaseSizesCollectorOrderingPreserved(t *testing.T) {
	q := pgqueries.ByID("database_sizes_v1")
	if q == nil {
		t.Fatal("database_sizes_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	orderIdx := strings.LastIndex(sql, "order by")
	if orderIdx < 0 {
		t.Fatal("database_sizes_v1 missing ORDER BY")
	}
	after := sql[orderIdx:]
	if !strings.Contains(after, "pg_database_size") || !strings.Contains(after, "desc") {
		t.Error("database_sizes_v1 ORDER BY must remain pg_database_size(...) DESC")
	}
}
