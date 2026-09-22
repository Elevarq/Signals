package tests

import (
	"os"
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/metrics"
	"github.com/elevarq/signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// Issue #93 / R079 — guard against documentation drift on the
// `signals_collection_failures_total` reason label set.
//
// The metrics consumer guide promises a specific reason enum.
// `metrics.CollectionFailureReasons` is the constant source of truth.
// classifyCollectionFailure() in internal/collector emits exactly
// these values. If any of the three sources disagrees, this test
// fails before the docs ship.
// ---------------------------------------------------------------------------

func TestMetricsDoc_FailureReasonsMatchConstant(t *testing.T) {
	data, err := os.ReadFile("../docs/metrics-consumer-guide.md")
	if err != nil {
		t.Fatalf("read consumer guide: %v", err)
	}
	body := string(data)

	for _, reason := range metrics.CollectionFailureReasons {
		if !strings.Contains(body, reason) {
			t.Errorf("docs/metrics-consumer-guide.md missing reason %q from CollectionFailureReasons", reason)
		}
	}
}

// #441 — the collectors_failed / collectors_skipped reason enums must
// match their constants AND carry no phantom (a documented value the code
// never emits, e.g. the removed `savepoint_rollback`). The consumer guide
// rows are the operator contract; drift breaks alert rules silently.
func TestMetricsDoc_CollectorReasonsMatchConstants(t *testing.T) {
	data, err := os.ReadFile("../docs/metrics-consumer-guide.md")
	if err != nil {
		t.Fatalf("read consumer guide: %v", err)
	}
	body := string(data)

	failedRow := tableRowContaining(body, "signals_collectors_failed_total")
	skippedRow := tableRowContaining(body, "signals_collectors_skipped_total")
	if failedRow == "" || skippedRow == "" {
		t.Fatal("could not locate the collectors_failed / collectors_skipped rows in the consumer guide")
	}

	assertRowReasonsEqual(t, "collectors_failed", failedRow, metrics.CollectorFailedReasons)
	assertRowReasonsEqual(t, "collectors_skipped", skippedRow, metrics.CollectorSkippedReasons)

	// The eligibility-gate reason constants must be a subset of the
	// documented skipped enum — they are emitted as skip reasons.
	for _, gate := range []string{
		pgqueries.GateReasonVersionUnsupported,
		pgqueries.GateReasonExtensionMissing,
		pgqueries.GateReasonConfigDisabled,
	} {
		if !sliceContains(metrics.CollectorSkippedReasons, gate) {
			t.Errorf("pgqueries gate reason %q is not in metrics.CollectorSkippedReasons", gate)
		}
	}
}

// tableRowContaining returns the first markdown table row that mentions
// needle, or "" if none.
func tableRowContaining(body, needle string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "|") && strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

// assertRowReasonsEqual checks that the reason-shaped backticked tokens in
// a doc row equal want exactly — no missing values (alerts on them would be
// empty) and no phantom values (documented but never emitted).
func assertRowReasonsEqual(t *testing.T, label, row string, want []string) {
	t.Helper()
	// The row's backticked tokens are the metric name, the label columns
	// (`target`, `reason`), and the reason values. Keep only reason values:
	// lowercase [a-z_], not the metric name, not a label-column name.
	notAReason := map[string]bool{"target": true, "reason": true}
	got := map[string]bool{}
	for i, tok := range strings.Split(row, "`") {
		if i%2 == 0 { // even indices are outside backticks
			continue
		}
		if strings.HasPrefix(tok, "signals_") || notAReason[tok] || len(tok) < 3 {
			continue
		}
		reasonLike := true
		for _, r := range tok {
			if (r < 'a' || r > 'z') && r != '_' {
				reasonLike = false
				break
			}
		}
		if reasonLike {
			got[tok] = true
		}
	}
	wantSet := map[string]bool{}
	for _, r := range want {
		wantSet[r] = true
		if !got[r] {
			t.Errorf("%s: reason %q is in the constant but missing from the doc row", label, r)
		}
	}
	for g := range got {
		if !wantSet[g] {
			t.Errorf("%s: doc row lists phantom reason %q not in the constant (code never emits it)", label, g)
		}
	}
}

func sliceContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// Belt-and-braces: the constant must not silently grow / shrink
// without an explicit code review. Capping bounds future additions
// to deliberate ones.
func TestMetricsDoc_FailureReasonsBounded(t *testing.T) {
	const maxAllowed = 10
	if len(metrics.CollectionFailureReasons) > maxAllowed {
		t.Errorf("CollectionFailureReasons has grown to %d entries (max %d) — review and bump the bound deliberately",
			len(metrics.CollectionFailureReasons), maxAllowed)
	}
	if len(metrics.CollectionFailureReasons) == 0 {
		t.Error("CollectionFailureReasons must not be empty")
	}
}
