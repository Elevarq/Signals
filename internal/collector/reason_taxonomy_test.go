package collector

import (
	"testing"

	"github.com/elevarq/signals/internal/metrics"
	"github.com/elevarq/signals/internal/pgqueries"
)

func inSet(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// TestClassifyRunErrorReasonsAreCanonical (#441) pins classifyRunError's
// outputs to metrics.CollectorFailedReasons, so the metric label enum, the
// classifier, and the docs cannot drift apart.
func TestClassifyRunErrorReasonsAreCanonical(t *testing.T) {
	cases := []struct{ msg, want string }{
		{"permission denied for table pg_statistic_ext_data", "permission_denied"},
		{"ERROR (SQLSTATE 42501)", "permission_denied"},
		{`relation "x" does not exist (SQLSTATE 42P01)`, "object_missing"},
		{"function y does not exist (SQLSTATE 42883)", "object_missing"},
		{"context deadline exceeded", "timeout"},
		{"statement timeout", "timeout"},
		{"some other failure", "execution_error"},
	}
	for _, c := range cases {
		got := classifyRunError(c.msg)
		if got != c.want {
			t.Errorf("classifyRunError(%q) = %q, want %q", c.msg, got, c.want)
		}
		if !inSet(metrics.CollectorFailedReasons, got) {
			t.Errorf("classifyRunError produced %q which is not in metrics.CollectorFailedReasons", got)
		}
	}
}

// TestSkipReasonConstsAreCanonical (#441) pins the runtime skip-reason
// constants to metrics.CollectorSkippedReasons.
func TestSkipReasonConstsAreCanonical(t *testing.T) {
	for _, r := range []string{
		reasonBudgetExhausted,
		reasonPrivilegeOwnerOnly,
		reasonPrivilegeRestricted,
		reasonPrivilegeColumnFiltered,
	} {
		if !inSet(metrics.CollectorSkippedReasons, r) {
			t.Errorf("skip reason const %q is not in metrics.CollectorSkippedReasons", r)
		}
	}
}

// TestColumnPrivilegeDegradeReasonsAreCanonical (#458) closes the loop the
// const test cannot: the run's reason is set from the collector's
// ColumnPrivilegeDegradeReason FIELD, not the status const, so a typo in a
// QueryDef would slip past. Assert every collector that declares one uses
// the canonical value and pairs it with a probe.
func TestColumnPrivilegeDegradeReasonsAreCanonical(t *testing.T) {
	var checked int
	for _, q := range pgqueries.All() {
		if q.ColumnPrivilegeDegradeReason == "" {
			continue
		}
		checked++
		if q.ColumnPrivilegeDegradeReason != reasonPrivilegeColumnFiltered {
			t.Errorf("collector %q ColumnPrivilegeDegradeReason=%q, want %q",
				q.ID, q.ColumnPrivilegeDegradeReason, reasonPrivilegeColumnFiltered)
		}
		if !inSet(metrics.CollectorSkippedReasons, q.ColumnPrivilegeDegradeReason) {
			t.Errorf("collector %q reason %q not in metrics.CollectorSkippedReasons", q.ID, q.ColumnPrivilegeDegradeReason)
		}
		if q.ColumnPrivilegeProbeSQL == "" {
			t.Errorf("collector %q declares a column-privilege degrade reason but no probe SQL — a bare 0-row result must be probed, never assumed privilege-filtered", q.ID)
		}
	}
	if checked == 0 {
		t.Error("no collector declares ColumnPrivilegeDegradeReason — expected pg_stats_v1 (#458)")
	}
}
