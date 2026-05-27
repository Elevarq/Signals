package tests

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/db"
	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// R037: Dynamic column capture for version-sensitive collectors (TC-SIG-044)
// ---------------------------------------------------------------------------

// TestPgStatStatementsUsesDynamicCapture verifies that the
// pg_stat_statements_v1 query uses a wildcard projection (either
// `SELECT *` or `SELECT <alias>.*` over the pg_stat_statements view)
// instead of a fixed column list, enabling cross-version compatibility.
// The R106 self-filter requires aliasing pg_stat_statements so the
// wildcard is table-qualified.
func TestPgStatStatementsUsesDynamicCapture(t *testing.T) {
	q := pgqueries.ByID("pg_stat_statements_v1")
	if q == nil {
		t.Fatal("pg_stat_statements_v1 not registered in catalog")
	}

	sql := strings.TrimSpace(q.SQL)
	upper := strings.ToUpper(sql)

	// Accept either the bare wildcard or any table-qualified wildcard
	// over pg_stat_statements (e.g. `SELECT s.* FROM pg_stat_statements s`).
	bareWildcard := strings.HasPrefix(upper, "SELECT *")
	qualifiedWildcard := wildcardOverPgStatStatementsRe.MatchString(sql)
	if !bareWildcard && !qualifiedWildcard {
		t.Errorf("pg_stat_statements_v1 should use a wildcard projection over pg_stat_statements for dynamic capture, got: %s",
			sql[:min(len(sql), 80)])
	}
}

// wildcardOverPgStatStatementsRe matches `SELECT <alias>.* FROM
// pg_stat_statements ...`. Go's RE2 engine does not support
// backreferences, so the check is anchored to the table name; the
// alias-equality is enforced implicitly by SQL parsing at execution
// time.
var wildcardOverPgStatStatementsRe = regexp.MustCompile(`(?is)select\s+\w+\.\*\s+from\s+pg_stat_statements\b`)

// TestPgStatStatementsDoesNotRankOrLimit verifies that Signals remains
// a raw collection layer. Analyzer owns top-N selection.
func TestPgStatStatementsDoesNotRankOrLimit(t *testing.T) {
	q := pgqueries.ByID("pg_stat_statements_v1")
	if q == nil {
		t.Fatal("pg_stat_statements_v1 not registered in catalog")
	}

	upper := strings.ToUpper(q.SQL)
	for _, clause := range []string{"ORDER BY", "LIMIT"} {
		if strings.Contains(upper, clause) {
			t.Errorf("pg_stat_statements_v1 must not contain %s; Analyzer owns ranking/selection: %s", clause, q.SQL)
		}
	}
}

// TestPgStatStatementsPassesLinter verifies that the dynamic SELECT *
// query still passes the static SQL linter.
func TestPgStatStatementsPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_stat_statements_v1")
	if q == nil {
		t.Fatal("pg_stat_statements_v1 not registered")
	}

	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_stat_statements_v1 failed linter: %v", err)
	}
}

// TestPgStatStatementsRequiresExtension verifies that the collector
// is gated on the pg_stat_statements extension.
func TestPgStatStatementsRequiresExtension(t *testing.T) {
	q := pgqueries.ByID("pg_stat_statements_v1")
	if q == nil {
		t.Fatal("pg_stat_statements_v1 not registered")
	}

	if q.RequiresExtension != "pg_stat_statements" {
		t.Errorf("RequiresExtension: got %q, want %q",
			q.RequiresExtension, "pg_stat_statements")
	}
}

// TestDynamicColumnsPreservedInNDJSON verifies that NDJSON encoding
// faithfully preserves whatever column names are in the input maps,
// supporting dynamic result capture without fixed field assumptions.
func TestDynamicColumnsPreservedInNDJSON(t *testing.T) {
	// Simulate rows with different column sets (as would happen
	// across PG versions).
	rows := []map[string]any{
		{
			"userid":               10,
			"queryid":              int64(123456),
			"calls":                42,
			"total_exec_time":      1.5,
			"shared_blk_read_time": 0.3, // PG 17+ column name
			"extra_future_column":  "new_value",
		},
	}

	payload, compressed, size, err := db.EncodeNDJSON(rows)
	if err != nil {
		t.Fatalf("EncodeNDJSON: %v", err)
	}
	if size == 0 {
		t.Fatal("encoded size is 0")
	}

	decoded, err := db.DecodeNDJSON(payload, compressed)
	if err != nil {
		t.Fatalf("DecodeNDJSON: %v", err)
	}

	if len(decoded) != 1 {
		t.Fatalf("expected 1 row, got %d", len(decoded))
	}

	row := decoded[0]

	// Verify all dynamic columns are preserved.
	for _, col := range []string{"userid", "queryid", "calls",
		"total_exec_time", "shared_blk_read_time", "extra_future_column"} {
		if _, ok := row[col]; !ok {
			t.Errorf("column %q missing from decoded row — dynamic capture not preserving all columns", col)
		}
	}

	// Verify the extra/future column value is correct.
	if v, ok := row["extra_future_column"]; !ok || v != "new_value" {
		t.Errorf("extra_future_column: got %v, want %q", v, "new_value")
	}
}

// TestZeroRowDynamicCapture verifies that a dynamic query returning
// zero rows produces a valid (non-nil) empty payload.
func TestZeroRowDynamicCapture(t *testing.T) {
	rows := []map[string]any{} // zero rows

	payload, _, _, err := db.EncodeNDJSON(rows)
	if err != nil {
		t.Fatalf("EncodeNDJSON: %v", err)
	}
	if payload == nil {
		t.Fatal("payload is nil for zero rows — should be empty but non-nil")
	}
}

// ---------------------------------------------------------------------------
// R038: Query failure isolation (TC-SIG-045)
// ---------------------------------------------------------------------------

// TestSavepointIsolationInCollectorSource verifies that the collector
// uses SAVEPOINTs to isolate query failures, preventing one failed
// query from aborting the entire transaction.
func TestSavepointIsolationInCollectorSource(t *testing.T) {
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "collector", "collector.go"))

	if !strings.Contains(src, "SAVEPOINT") {
		t.Fatal("collector.go does not use SAVEPOINTs — a single query failure will abort all remaining queries")
	}
	if !strings.Contains(src, "ROLLBACK TO SAVEPOINT") {
		t.Fatal("collector.go does not ROLLBACK TO SAVEPOINT on failure — transaction will remain aborted")
	}
}

// ---------------------------------------------------------------------------
// R039: Dynamic capture preserves safety model (TC-SIG-046)
// ---------------------------------------------------------------------------

// TestDynamicCaptureQueryIsReadOnly verifies that the dynamic
// pg_stat_statements query does not contain write operations.
func TestDynamicCaptureQueryIsReadOnly(t *testing.T) {
	q := pgqueries.ByID("pg_stat_statements_v1")
	if q == nil {
		t.Fatal("pg_stat_statements_v1 not registered")
	}

	upper := strings.ToUpper(q.SQL)
	for _, keyword := range []string{"INSERT", "UPDATE", "DELETE", "DROP", "CREATE", "ALTER"} {
		if strings.Contains(upper, keyword) {
			t.Errorf("pg_stat_statements_v1 contains disallowed keyword: %s", keyword)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
