package collector

import (
	"testing"

	"github.com/elevarq/signals/internal/metrics"
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
	} {
		if !inSet(metrics.CollectorSkippedReasons, r) {
			t.Errorf("skip reason const %q is not in metrics.CollectorSkippedReasons", r)
		}
	}
}
