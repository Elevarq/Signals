package collector

import (
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// R108 / INV-SIGNALS-19: when a target's per-cycle budget elapses, every
// remaining due collector must be recorded as a skipped run so the
// status inventory is complete.

func TestBudgetSkippedRunsRecordsEveryRemainingDueCollector(t *testing.T) {
	remaining := []pgqueries.QueryDef{
		{ID: "a_v1"},
		{ID: "b_v1"},
		{ID: "c_v1"},
	}
	i := 0
	newID := func() string {
		i++
		return "run-" + string(rune('0'+i))
	}

	runs := budgetSkippedRuns(remaining, 7, "snap-x", "2026-05-27T10:00:00Z", "PostgreSQL 18.0", newID)

	if len(runs) != len(remaining) {
		t.Fatalf("got %d runs, want %d (one per remaining due collector)", len(runs), len(remaining))
	}
	for idx, r := range runs {
		if r.QueryID != remaining[idx].ID {
			t.Errorf("run[%d].QueryID = %q, want %q", idx, r.QueryID, remaining[idx].ID)
		}
		if r.Status != "skipped" {
			t.Errorf("run[%d].Status = %q, want skipped", idx, r.Status)
		}
		if r.Reason != reasonBudgetExhausted {
			t.Errorf("run[%d].Reason = %q, want %q", idx, r.Reason, reasonBudgetExhausted)
		}
		if r.TargetID != 7 {
			t.Errorf("run[%d].TargetID = %d, want 7", idx, r.TargetID)
		}
		if r.SnapshotID != "snap-x" {
			t.Errorf("run[%d].SnapshotID = %q, want snap-x", idx, r.SnapshotID)
		}
		if r.CollectedAt != "2026-05-27T10:00:00Z" {
			t.Errorf("run[%d].CollectedAt = %q", idx, r.CollectedAt)
		}
		if r.PGVersion != "PostgreSQL 18.0" {
			t.Errorf("run[%d].PGVersion = %q", idx, r.PGVersion)
		}
		if r.ID == "" || r.CreatedAt == "" {
			t.Errorf("run[%d] missing ID/CreatedAt: %+v", idx, r)
		}
	}
}

func TestBudgetSkippedRunsEmpty(t *testing.T) {
	runs := budgetSkippedRuns(nil, 1, "s", "t", "v", func() string { return "x" })
	if len(runs) != 0 {
		t.Errorf("empty remaining should produce no runs, got %d", len(runs))
	}
}

func TestCycleStatus(t *testing.T) {
	cases := []struct {
		name            string
		err             bool
		failed          int
		budgetExhausted int
		want            string
	}{
		{"clean", false, 0, 0, "success"},
		{"failure short-circuits", true, 0, 0, "failed"},
		{"failure wins over partial", true, 2, 3, "failed"},
		{"failed collectors -> partial", false, 1, 0, "partial"},
		{"budget skips -> partial", false, 0, 2, "partial"},
		{"both -> partial", false, 1, 2, "partial"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.err {
				err = errSentinel
			}
			if got := cycleStatus(err, tc.failed, tc.budgetExhausted); got != tc.want {
				t.Errorf("cycleStatus(err=%v, failed=%d, budget=%d) = %q, want %q",
					tc.err, tc.failed, tc.budgetExhausted, got, tc.want)
			}
		})
	}
}

var errSentinel = errSentinelType("boom")

type errSentinelType string

func (e errSentinelType) Error() string { return string(e) }
