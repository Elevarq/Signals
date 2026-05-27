package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// pg_stat_replication_slots_v1 — logical replication slot health
// (spill / stream / total counters from pg_stat_replication_slots, PG 14+).
//
// Specification: specifications/collectors/pg_stat_replication_slots_v1.md
// ---------------------------------------------------------------------------

func TestReplicationSlotsHealthCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_stat_replication_slots_v1")
	if q == nil {
		t.Fatal("pg_stat_replication_slots_v1 is not registered")
	}
	if q.Category != "replication" {
		t.Errorf("category: got %q, want %q", q.Category, "replication")
	}
}

func TestReplicationSlotsHealthCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_stat_replication_slots_v1")
	if q == nil {
		t.Fatal("pg_stat_replication_slots_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_stat_replication_slots_v1 failed linter: %v", err)
	}
}

func TestReplicationSlotsHealthCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_stat_replication_slots_v1")
	if q == nil {
		t.Fatal("pg_stat_replication_slots_v1 not registered")
	}
	if q.Cadence != pgqueries.Cadence5m {
		t.Errorf("cadence: got %v, want Cadence5m", q.Cadence)
	}
}

func TestReplicationSlotsHealthCollectorRetention(t *testing.T) {
	q := pgqueries.ByID("pg_stat_replication_slots_v1")
	if q == nil {
		t.Fatal("pg_stat_replication_slots_v1 not registered")
	}
	if q.RetentionClass != pgqueries.RetentionShort {
		t.Errorf("retention: got %q, want RetentionShort", q.RetentionClass)
	}
}

func TestReplicationSlotsHealthCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_stat_replication_slots_v1")
	if q == nil {
		t.Fatal("pg_stat_replication_slots_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

func TestReplicationSlotsHealthCollectorMinPGVersion(t *testing.T) {
	q := pgqueries.ByID("pg_stat_replication_slots_v1")
	if q == nil {
		t.Fatal("pg_stat_replication_slots_v1 not registered")
	}
	if q.MinPGVersion != 14 {
		t.Errorf("MinPGVersion: got %d, want 14", q.MinPGVersion)
	}
}

// TC-RSLOTS-04 / FC-25: excluded on PG 13 and below.
func TestReplicationSlotsHealthCollectorExcludedOnPG13(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 13,
		Extensions:     []string{},
	})
	for _, q := range filtered {
		if q.ID == "pg_stat_replication_slots_v1" {
			t.Error("pg_stat_replication_slots_v1 must be excluded on PG 13")
		}
	}
}

func TestReplicationSlotsHealthCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_stat_replication_slots_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_stat_replication_slots_v1 must be included on PG 14+")
	}
}

func TestReplicationSlotsHealthCollectorIncludedOnPG17(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 17,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_stat_replication_slots_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_stat_replication_slots_v1 must be included on PG 17")
	}
}

func TestReplicationSlotsHealthCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_stat_replication_slots_v1")
	if q == nil {
		t.Fatal("pg_stat_replication_slots_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_stat_replication_slots_v1 must not use SELECT * — column projection must be explicit")
	}
}

func TestReplicationSlotsHealthCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_stat_replication_slots_v1")
	if q == nil {
		t.Fatal("pg_stat_replication_slots_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_stat_replication_slots_v1 must have ORDER BY for deterministic output")
	}
}

func TestReplicationSlotsHealthCollectorUsesStatView(t *testing.T) {
	q := pgqueries.ByID("pg_stat_replication_slots_v1")
	if q == nil {
		t.Fatal("pg_stat_replication_slots_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_stat_replication_slots") {
		t.Error("pg_stat_replication_slots_v1 must query pg_stat_replication_slots")
	}
}

func TestReplicationSlotsHealthCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_stat_replication_slots_v1")
	if q == nil {
		t.Fatal("pg_stat_replication_slots_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"slot_name",
		"spill_txns", "spill_count", "spill_bytes",
		"stream_txns", "stream_count", "stream_bytes",
		"total_txns", "total_bytes",
		"stats_reset",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_stat_replication_slots_v1 must include column %q", col)
		}
	}
}
