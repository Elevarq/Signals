package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// findQueryByID returns the resolved QueryDef for `id` from a Filter
// result, or nil if not present.
func findQueryByID(qs []pgqueries.QueryDef, id string) *pgqueries.QueryDef {
	for i := range qs {
		if qs[i].ID == id {
			return &qs[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// R081: version-aware query catalog
// ---------------------------------------------------------------------------

// TestPgStatIoOverrideForPG18 verifies that when a target reports
// major=18, Filter returns pg_stat_io_v1 with the override SQL — using
// PG 18's renamed columns (read_bytes/write_bytes/extend_bytes), not
// the removed op_bytes column.
// Traces: ARQ-SIGNALS-R081
func TestPgStatIoOverrideForPG18(t *testing.T) {
	out := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion:         18,
		HighSensitivityEnabled: false,
	})
	q := findQueryByID(out, "pg_stat_io_v1")
	if q == nil {
		t.Fatal("pg_stat_io_v1 missing from PG 18 filter result")
	}
	// PG 18 must reference the new columns.
	for _, mustInclude := range []string{"read_bytes", "write_bytes", "extend_bytes"} {
		if !strings.Contains(q.SQL, mustInclude) {
			t.Errorf("PG 18 SQL for pg_stat_io_v1 must include %q, got:\n%s", mustInclude, q.SQL)
		}
	}
	// op_bytes still appears in the canonical column list, but as a
	// NULL stub on PG 18 (consumers see the union schema).
	if !strings.Contains(q.SQL, "NULL::bigint AS op_bytes") {
		t.Errorf("PG 18 SQL should expose op_bytes as NULL stub for canonical schema, got:\n%s", q.SQL)
	}
	// PG 18 SQL must NOT reference the removed `op_bytes` column from
	// the source view (only as the NULL alias on the SELECT list).
	// Quick check: no bare `op_bytes,` (with no AS) in the FROM-clause
	// area.
	if strings.Contains(q.SQL, "\t\tFROM pg_stat_io") && strings.Contains(q.SQL, "\t\top_bytes,") {
		t.Errorf("PG 18 SQL must not select the removed op_bytes column from pg_stat_io")
	}
}

// TestPgStatIoDefaultForPG17 verifies the default SQL still serves PG
// 16/17 — emitting native op_bytes, NULL stubs for the new columns,
// and using the same canonical column list (R081 normalization).
// Traces: ARQ-SIGNALS-R081
func TestPgStatIoDefaultForPG17(t *testing.T) {
	out := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 17,
	})
	q := findQueryByID(out, "pg_stat_io_v1")
	if q == nil {
		t.Fatal("pg_stat_io_v1 missing from PG 17 filter result")
	}
	if !strings.Contains(q.SQL, "op_bytes,") {
		t.Errorf("PG 17 SQL should select native op_bytes column")
	}
	for _, stub := range []string{
		"NULL::bigint AS read_bytes",
		"NULL::bigint AS write_bytes",
		"NULL::bigint AS extend_bytes",
	} {
		if !strings.Contains(q.SQL, stub) {
			t.Errorf("PG 17 SQL must expose %q for canonical schema, got:\n%s", stub, q.SQL)
		}
	}
	if pgqueries.HasOverride(17, "pg_stat_io_v1") {
		t.Error("PG 17 should not have a pg_stat_io_v1 override registered")
	}
}

// TestPgStatWalOverrideForPG18 verifies the PG 18 SQL preserves the
// canonical column set even though PG 18 dropped wal_write / wal_sync /
// wal_write_time / wal_sync_time from the source view entirely. The
// override emits NULL stubs for those columns so downstream consumers
// see the same schema across majors; the moved counters are now
// available via pg_stat_io.
// Traces: ARQ-SIGNALS-R081
func TestPgStatWalOverrideForPG18(t *testing.T) {
	out := pgqueries.Filter(pgqueries.FilterParams{PGMajorVersion: 18})
	q := findQueryByID(out, "pg_stat_wal_v1")
	if q == nil {
		t.Fatal("pg_stat_wal_v1 missing from PG 18 filter result")
	}
	for _, stub := range []string{
		"NULL::bigint           AS wal_write",
		"NULL::bigint           AS wal_sync",
		"NULL::double precision AS wal_write_time",
		"NULL::double precision AS wal_sync_time",
	} {
		if !strings.Contains(q.SQL, stub) {
			t.Errorf("PG 18 SQL must expose %q for canonical schema, got:\n%s", stub, q.SQL)
		}
	}
	// PG 18 must still emit the columns the view does keep.
	for _, kept := range []string{"wal_records", "wal_fpi", "wal_bytes", "wal_buffers_full", "stats_reset"} {
		if !strings.Contains(q.SQL, kept) {
			t.Errorf("PG 18 SQL must keep native column %q, got:\n%s", kept, q.SQL)
		}
	}
}

// TestStableLogicalIDsAcrossMajors verifies that the same logical id
// (`pg_stat_io_v1`) is returned regardless of major. This is R081's
// stable-ID guarantee — only the SQL underneath changes, never the
// consumer-facing collector identifier.
// Traces: ARQ-SIGNALS-R081
func TestStableLogicalIDsAcrossMajors(t *testing.T) {
	for _, m := range []int{16, 17, 18} {
		out := pgqueries.Filter(pgqueries.FilterParams{PGMajorVersion: m})
		if findQueryByID(out, "pg_stat_io_v1") == nil {
			t.Errorf("pg_stat_io_v1 missing from PG %d filter result", m)
		}
	}
}

// TestSupportedMajors verifies the documented support window and the
// experimental marker behaviour.
// Traces: ARQ-SIGNALS-R081
func TestSupportedMajors(t *testing.T) {
	for _, m := range []int{14, 15, 16, 17, 18} {
		if !pgqueries.IsSupportedMajor(m) {
			t.Errorf("major %d should be supported", m)
		}
		if pgqueries.IsExperimentalMajor(m) {
			t.Errorf("major %d should not be experimental", m)
		}
	}
	if pgqueries.IsSupportedMajor(13) {
		t.Error("PG 13 should not be supported")
	}
	if !pgqueries.IsExperimentalMajor(19) {
		t.Error("PG 19 should be flagged experimental")
	}
	if pgqueries.IsSupportedMajor(19) {
		t.Error("PG 19 should not appear in the supported window")
	}
}

// TestOverrideRejectsUnknownID verifies that registering an override
// for a logical ID that doesn't exist in the default registry is a
// programmer error, caught at panic time. This guards against typos
// in catalog_pgN.go files diverging from canonical IDs.
// Traces: ARQ-SIGNALS-R081
func TestOverrideRejectsUnknownID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when registering override for unknown logical id")
		}
	}()
	pgqueries.RegisterOverride(99, "this_id_does_not_exist", "SELECT 1")
}

// TestPG18OverrideSQLPassesLinter verifies the new PG 18 SQL is
// accepted by the safety linter — same read-only enforcement (R002)
// applies to overrides as to default SQL.
// Traces: ARQ-SIGNALS-R002 / ARQ-SIGNALS-R081
func TestPG18OverrideSQLPassesLinter(t *testing.T) {
	out := pgqueries.Filter(pgqueries.FilterParams{PGMajorVersion: 18})
	for _, id := range []string{"pg_stat_io_v1", "pg_stat_wal_v1"} {
		q := findQueryByID(out, id)
		if q == nil {
			t.Fatalf("%s missing from PG 18 filter result", id)
		}
		if err := pgqueries.LintQuery(q.SQL); err != nil {
			t.Errorf("PG 18 SQL for %s failed linter: %v", id, err)
		}
	}
}
