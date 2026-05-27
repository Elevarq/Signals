package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// pg_stat_io_v1 — cumulative per-(backend_type, object, context) I/O counters.
//
// Specification: specifications/collectors/pg_stat_io_v1.md
// Acceptance:    specifications/collectors/pg_stat_io_v1.acceptance.md
// Cross-cutter:  specifications/delta-semantics.md
// ---------------------------------------------------------------------------

func TestStatIoCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_stat_io_v1")
	if q == nil {
		t.Fatal("pg_stat_io_v1 is not registered")
	}
	if q.Category != "io" {
		t.Errorf("category: got %q, want %q", q.Category, "io")
	}
}

func TestStatIoCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_stat_io_v1")
	if q == nil {
		t.Fatal("pg_stat_io_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_stat_io_v1 failed linter: %v", err)
	}
}

func TestStatIoCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_stat_io_v1")
	if q == nil {
		t.Fatal("pg_stat_io_v1 not registered")
	}
	if q.Cadence != pgqueries.Cadence15m {
		t.Errorf("cadence: got %v, want Cadence15m", q.Cadence)
	}
}

func TestStatIoCollectorRetention(t *testing.T) {
	q := pgqueries.ByID("pg_stat_io_v1")
	if q == nil {
		t.Fatal("pg_stat_io_v1 not registered")
	}
	if q.RetentionClass != pgqueries.RetentionMedium {
		t.Errorf("retention: got %q, want RetentionMedium", q.RetentionClass)
	}
}

func TestStatIoCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_stat_io_v1")
	if q == nil {
		t.Fatal("pg_stat_io_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

// MinPGVersion is 16 — pg_stat_io was introduced in PostgreSQL 16.
func TestStatIoCollectorMinPGVersion(t *testing.T) {
	q := pgqueries.ByID("pg_stat_io_v1")
	if q == nil {
		t.Fatal("pg_stat_io_v1 not registered")
	}
	if q.MinPGVersion != 16 {
		t.Errorf("MinPGVersion: got %d, want 16 (pg_stat_io introduced in PG 16)", q.MinPGVersion)
	}
}

// TC-STATIO-02: Filtered out on PG < 16.
func TestStatIoCollectorExcludedOnPG15(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 15,
		Extensions:     []string{},
	})
	for _, q := range filtered {
		if q.ID == "pg_stat_io_v1" {
			t.Error("pg_stat_io_v1 must be excluded on PG 15")
		}
	}
}

// TC-STATIO-01 prerequisite: eligible on PG 16+.
func TestStatIoCollectorIncludedOnPG16(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 16,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_stat_io_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_stat_io_v1 must be included on PG 16")
	}
}

func TestStatIoCollectorIncludedOnPG17(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 17,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_stat_io_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_stat_io_v1 must be included on PG 17")
	}
}

// Deterministic output ordering — acceptance rule invariant.
func TestStatIoCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_stat_io_v1")
	if q == nil {
		t.Fatal("pg_stat_io_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_stat_io_v1 must have ORDER BY for deterministic output")
	}
}

// Explicit column list — invariant.
func TestStatIoCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_stat_io_v1")
	if q == nil {
		t.Fatal("pg_stat_io_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_stat_io_v1 must not use SELECT * (explicit column list for schema stability)")
	}
}

// Required output columns per the spec's output table.
func TestStatIoCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_stat_io_v1")
	if q == nil {
		t.Fatal("pg_stat_io_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"backend_type", "object", "context",
		"reads", "read_time",
		"writes", "write_time",
		"writebacks", "writeback_time",
		"extends", "extend_time",
		"op_bytes", "hits", "evictions", "reuses",
		"fsyncs", "fsync_time",
		"stats_reset",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_stat_io_v1 must include column %q", col)
		}
	}
}

func TestStatIoCollectorUsesPgStatIo(t *testing.T) {
	q := pgqueries.ByID("pg_stat_io_v1")
	if q == nil {
		t.Fatal("pg_stat_io_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_stat_io") {
		t.Error("pg_stat_io_v1 must query pg_stat_io")
	}
}

// TC-STATIO-04: Cumulative semantics — no server-side delta computation
// per delta-semantics.md DS-R002.
func TestStatIoCollectorNoServerSideDelta(t *testing.T) {
	q := pgqueries.ByID("pg_stat_io_v1")
	if q == nil {
		t.Fatal("pg_stat_io_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	if strings.Contains(sql, "lag(") || strings.Contains(sql, "lag (") {
		t.Error("pg_stat_io_v1 must not use LAG() — deltas are computed analyzer-side per delta-semantics.md")
	}
}

// op_bytes is load-bearing: analyzer converts block counts to bytes without
// assuming the default BLCKSZ.
func TestStatIoCollectorEmitsOpBytes(t *testing.T) {
	q := pgqueries.ByID("pg_stat_io_v1")
	if q == nil {
		t.Fatal("pg_stat_io_v1 not registered")
	}
	if !containsCI(q.SQL, "op_bytes") {
		t.Error("pg_stat_io_v1 must emit op_bytes so the analyzer does not assume BLCKSZ")
	}
}
