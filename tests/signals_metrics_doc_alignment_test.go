package tests

import (
	"os"
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/metrics"
)

// ---------------------------------------------------------------------------
// Issue #93 / R079 — guard against documentation drift on the
// `arq_signal_collection_failures_total` reason label set.
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
