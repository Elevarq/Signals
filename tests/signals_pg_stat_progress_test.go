package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// pg_stat_progress_* family — in-flight operation collectors.
//
// Specification: specifications/collectors/pg_stat_progress_family_v1.md
// Acceptance:    specifications/collectors/pg_stat_progress_family_v1.acceptance.md
// ---------------------------------------------------------------------------

var progressFamilyIDs = []string{
	"pg_stat_progress_vacuum_v1",
	"pg_stat_progress_analyze_v1",
	"pg_stat_progress_create_index_v1",
	"pg_stat_progress_cluster_v1",
	"pg_stat_progress_basebackup_v1",
	"pg_stat_progress_copy_v1",
}

// TC-PROG-01.
func TestProgressFamilyAllRegistered(t *testing.T) {
	for _, id := range progressFamilyIDs {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Errorf("%s is not registered", id)
			continue
		}
		if q.Category != "progress" {
			t.Errorf("%s: category got %q, want %q", id, q.Category, "progress")
		}
	}
}

// TC-PROG-02.
func TestProgressFamilyConsistentConfig(t *testing.T) {
	for _, id := range progressFamilyIDs {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Fatalf("%s not registered", id)
		}
		if q.Cadence != pgqueries.Cadence5m {
			t.Errorf("%s: cadence got %v, want Cadence5m", id, q.Cadence)
		}
		if q.RetentionClass != pgqueries.RetentionShort {
			t.Errorf("%s: retention got %q, want RetentionShort", id, q.RetentionClass)
		}
		if q.ResultKind != pgqueries.ResultRowset {
			t.Errorf("%s: ResultKind got %q, want rowset", id, q.ResultKind)
		}
		if q.MinPGVersion != 14 {
			t.Errorf("%s: MinPGVersion got %d, want 14", id, q.MinPGVersion)
		}
	}
}

// TC-PROG-03.
func TestProgressFamilyPassesLinter(t *testing.T) {
	for _, id := range progressFamilyIDs {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Fatalf("%s not registered", id)
		}
		if err := pgqueries.LintQuery(q.SQL); err != nil {
			t.Errorf("%s failed linter: %v", id, err)
		}
	}
}

// TC-PROG-04 / FC-01: family excluded on PG 13.
func TestProgressFamilyExcludedOnPG13(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 13,
		Extensions:     []string{},
	})
	allowed := make(map[string]bool)
	for _, q := range filtered {
		allowed[q.ID] = true
	}
	for _, id := range progressFamilyIDs {
		if allowed[id] {
			t.Errorf("%s must be excluded on PG 13", id)
		}
	}
}

// TC-PROG-05: family present on every supported major from 14+.
func TestProgressFamilyIncludedOnSupportedMajors(t *testing.T) {
	for _, major := range []int{14, 15, 16, 17, 18} {
		filtered := pgqueries.Filter(pgqueries.FilterParams{
			PGMajorVersion: major,
			Extensions:     []string{},
		})
		present := make(map[string]bool)
		for _, q := range filtered {
			present[q.ID] = true
		}
		for _, id := range progressFamilyIDs {
			if !present[id] {
				t.Errorf("%s must be included on PG %d", id, major)
			}
		}
	}
}

// TC-PROG-06.
func TestProgressFamilyNoSelectStar(t *testing.T) {
	for _, id := range progressFamilyIDs {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Fatalf("%s not registered", id)
		}
		if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
			t.Errorf("%s must not use SELECT *", id)
		}
	}
}

// TC-PROG-07.
func TestProgressFamilyHasOrderBy(t *testing.T) {
	for _, id := range progressFamilyIDs {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Fatalf("%s not registered", id)
		}
		if !containsCI(q.SQL, "ORDER BY") {
			t.Errorf("%s must have ORDER BY for deterministic output", id)
		}
	}
}

// resolveVacuumSQL returns the effective SQL for
// pg_stat_progress_vacuum_v1 on the given PG major (base or
// override, whichever applies).
func resolveVacuumSQL(t *testing.T, major int) string {
	t.Helper()
	for _, q := range pgqueries.Filter(pgqueries.FilterParams{PGMajorVersion: major}) {
		if q.ID == "pg_stat_progress_vacuum_v1" {
			return q.SQL
		}
	}
	t.Fatalf("pg_stat_progress_vacuum_v1 not present on PG %d", major)
	return ""
}

// Regression: pg_stat_progress_vacuum SQL must NOT reference
// indrelid — that column never existed in any PG major and
// shipped briefly in an earlier override due to a memory error
// found during the v0.5.0 cross-major smoke test. The linter
// can't validate column existence, so we pin this explicitly.
func TestProgressVacuumNoIndrelidReference(t *testing.T) {
	for _, major := range []int{14, 15, 16, 17, 18} {
		sql := resolveVacuumSQL(t, major)
		if containsCI(sql, "indrelid") {
			t.Errorf("pg_stat_progress_vacuum_v1 resolved SQL for PG %d references indrelid — that column does not exist in any PG major", major)
		}
	}
}

// Regression: PG 17 / 18 overrides for pg_stat_progress_vacuum
// must populate the real columns added on those majors
// (indexes_total, indexes_processed; PG 18 also adds delay_time).
// A missed override would silently emit NULL stubs and look
// identical in linter-based tests, so we pin the real column
// references here.
func TestProgressVacuumOverridesPopulateNewColumns(t *testing.T) {
	pg17 := resolveVacuumSQL(t, 17)
	for _, col := range []string{"max_dead_tuple_bytes", "dead_tuple_bytes", "num_dead_item_ids", "indexes_total", "indexes_processed"} {
		if !containsCI(pg17, col) {
			t.Errorf("PG 17 SQL for pg_stat_progress_vacuum_v1 must reference real column %q", col)
		}
	}
	pg18 := resolveVacuumSQL(t, 18)
	for _, col := range []string{"max_dead_tuple_bytes", "dead_tuple_bytes", "num_dead_item_ids", "indexes_total", "indexes_processed", "delay_time"} {
		if !containsCI(pg18, col) {
			t.Errorf("PG 18 SQL for pg_stat_progress_vacuum_v1 must reference real column %q", col)
		}
	}
}

// Per-collector view-binding: each family member must query its
// matching upstream view (catches a copy/paste mistake during
// registration).
func TestProgressFamilyQueriesMatchingView(t *testing.T) {
	for _, id := range progressFamilyIDs {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Fatalf("%s not registered", id)
		}
		// id pattern: "pg_stat_progress_<thing>_v1" -> view "pg_stat_progress_<thing>"
		view := strings.TrimSuffix(id, "_v1")
		if !containsCI(q.SQL, view) {
			t.Errorf("%s must query %s", id, view)
		}
	}
}
