package tests

import (
	"sort"
	"testing"
	"time"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// TestFilterExcludesByExtension verifies that Filter with no extensions
// excludes queries requiring an extension (e.g. pg_stat_statements_v1).
// Traces: ARQ-SIGNALS-R014 / TC-SIG-020
func TestFilterExcludesByExtension(t *testing.T) {
	result := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 16,
		Extensions:     nil,
	})

	for _, q := range result {
		if q.ID == "pg_stat_statements_v1" {
			t.Fatal("expected pg_stat_statements_v1 to be excluded when no extensions are provided")
		}
	}
}

// TestFilterIncludesWithExtension verifies that Filter includes
// pg_stat_statements_v1 when that extension is listed.
// Traces: ARQ-SIGNALS-R015 / TC-SIG-021
func TestFilterIncludesWithExtension(t *testing.T) {
	result := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 16,
		Extensions:     []string{"pg_stat_statements"},
	})

	found := false
	for _, q := range result {
		if q.ID == "pg_stat_statements_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected pg_stat_statements_v1 to be included when extension is provided")
	}
}

// TestFilterResultSorted verifies that Filter output is sorted by ID.
// Traces: ARQ-SIGNALS-R014 / TC-SIG-020
func TestFilterResultSorted(t *testing.T) {
	result := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 16,
		Extensions:     []string{"pg_stat_statements"},
	})

	ids := make([]string, len(result))
	for i, q := range result {
		ids[i] = q.ID
	}
	if !sort.StringsAreSorted(ids) {
		t.Errorf("Filter result is not sorted by ID: %v", ids)
	}
}

// TestCadenceDuration verifies that all named Cadence constants map to
// the expected time.Duration values.
// Traces: ARQ-SIGNALS-R014 / TC-SIG-020
func TestCadenceDuration(t *testing.T) {
	cases := []struct {
		name     string
		cadence  pgqueries.Cadence
		expected time.Duration
	}{
		{"5m", pgqueries.Cadence5m, 5 * time.Minute},
		{"15m", pgqueries.Cadence15m, 15 * time.Minute},
		{"1h", pgqueries.Cadence1h, 1 * time.Hour},
		{"6h", pgqueries.Cadence6h, 6 * time.Hour},
		{"daily", pgqueries.CadenceDaily, 24 * time.Hour},
		{"weekly", pgqueries.CadenceWeekly, 7 * 24 * time.Hour},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cadence.Duration()
			if got != tc.expected {
				t.Errorf("Cadence %s: expected %v, got %v", tc.name, tc.expected, got)
			}
		})
	}
}

// TestSelectDueBasic verifies that SelectDue correctly identifies queries
// whose cadence interval has elapsed.
// Traces: ARQ-SIGNALS-R014 / TC-SIG-020
func TestSelectDueBasic(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	queries := []pgqueries.QueryDef{
		{ID: "q_5m", Cadence: pgqueries.Cadence5m, SQL: "SELECT 1"},
		{ID: "q_1h", Cadence: pgqueries.Cadence1h, SQL: "SELECT 2"},
		{ID: "q_daily", Cadence: pgqueries.CadenceDaily, SQL: "SELECT 3"},
	}

	lastRuns := pgqueries.LastRunMap{
		"q_5m": now.Add(-10 * time.Minute), // 10 min ago, > 5m => due
		"q_1h": now.Add(-30 * time.Minute), // 30 min ago, < 1h => not due
		// q_daily: never run => due
	}

	due := pgqueries.SelectDue(now, queries, lastRuns)

	dueIDs := make(map[string]bool)
	for _, q := range due {
		dueIDs[q.ID] = true
	}

	if !dueIDs["q_5m"] {
		t.Error("q_5m should be due (10 min > 5 min cadence)")
	}
	if dueIDs["q_1h"] {
		t.Error("q_1h should NOT be due (30 min < 1h cadence)")
	}
	if !dueIDs["q_daily"] {
		t.Error("q_daily should be due (never run)")
	}
}
