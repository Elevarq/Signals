package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// pg_stat_wal_v1 — cluster-wide WAL generation and write/sync counters.
//
// Specification: specifications/collectors/pg_stat_wal_v1.md
// Cross-cutter:  specifications/delta-semantics.md
// ---------------------------------------------------------------------------

func TestStatWalCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_stat_wal_v1")
	if q == nil {
		t.Fatal("pg_stat_wal_v1 is not registered")
	}
	if q.Category != "server" {
		t.Errorf("category: got %q, want %q", q.Category, "server")
	}
}

func TestStatWalCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_stat_wal_v1")
	if q == nil {
		t.Fatal("pg_stat_wal_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_stat_wal_v1 failed linter: %v", err)
	}
}

func TestStatWalCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_stat_wal_v1")
	if q == nil {
		t.Fatal("pg_stat_wal_v1 not registered")
	}
	if q.Cadence != pgqueries.Cadence15m {
		t.Errorf("cadence: got %v, want Cadence15m", q.Cadence)
	}
}

func TestStatWalCollectorRetention(t *testing.T) {
	q := pgqueries.ByID("pg_stat_wal_v1")
	if q == nil {
		t.Fatal("pg_stat_wal_v1 not registered")
	}
	if q.RetentionClass != pgqueries.RetentionMedium {
		t.Errorf("retention: got %q, want RetentionMedium", q.RetentionClass)
	}
}

func TestStatWalCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_stat_wal_v1")
	if q == nil {
		t.Fatal("pg_stat_wal_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultScalar {
		t.Errorf("ResultKind: got %q, want scalar (single-row view)", q.ResultKind)
	}
}

func TestStatWalCollectorMinPGVersion(t *testing.T) {
	q := pgqueries.ByID("pg_stat_wal_v1")
	if q == nil {
		t.Fatal("pg_stat_wal_v1 not registered")
	}
	if q.MinPGVersion != 14 {
		t.Errorf("MinPGVersion: got %d, want 14 (pg_stat_wal introduced in PG 14)", q.MinPGVersion)
	}
}

// TC-WAL-02: filtered out on PG < 14.
func TestStatWalCollectorExcludedOnPG13(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 13,
		Extensions:     []string{},
	})
	for _, q := range filtered {
		if q.ID == "pg_stat_wal_v1" {
			t.Error("pg_stat_wal_v1 must be excluded on PG 13")
		}
	}
}

func TestStatWalCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_stat_wal_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_stat_wal_v1 must be included on PG 14")
	}
}

func TestStatWalCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_stat_wal_v1")
	if q == nil {
		t.Fatal("pg_stat_wal_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_stat_wal_v1 must not use SELECT * (column set is stable PG14–17)")
	}
}

func TestStatWalCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_stat_wal_v1")
	if q == nil {
		t.Fatal("pg_stat_wal_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"wal_records", "wal_fpi", "wal_bytes",
		"wal_buffers_full", "wal_write", "wal_sync",
		"wal_write_time", "wal_sync_time", "stats_reset",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_stat_wal_v1 must include column %q", col)
		}
	}
}

func TestStatWalCollectorUsesPgStatWal(t *testing.T) {
	q := pgqueries.ByID("pg_stat_wal_v1")
	if q == nil {
		t.Fatal("pg_stat_wal_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_stat_wal") {
		t.Error("pg_stat_wal_v1 must query pg_stat_wal")
	}
}

// Cumulative semantics — no server-side delta per delta-semantics.md DS-R002.
func TestStatWalCollectorNoServerSideDelta(t *testing.T) {
	q := pgqueries.ByID("pg_stat_wal_v1")
	if q == nil {
		t.Fatal("pg_stat_wal_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	if strings.Contains(sql, "lag(") || strings.Contains(sql, "lag (") {
		t.Error("pg_stat_wal_v1 must not use LAG() — deltas computed analyzer-side")
	}
}
