package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/metrics"
)

// #138 drift gate — the failure taxonomy declared in
// docs/observability/operational-readiness.md MUST match the
// Go enum 1:1. Failing this test means either:
//   - a new code was added to one side but not the other, OR
//   - a wire string was changed.
//
// Either way the operator-facing contract has drifted.
func TestFailureTaxonomy_GoMatchesDoc(t *testing.T) {
	docPath := filepath.Join("..", "docs", "observability", "operational-readiness.md")
	raw, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read %s: %v", docPath, err)
	}
	doc := string(raw)

	// Every Go enum value MUST appear in the doc as a backticked
	// code (`target_unreachable`, etc.).
	for _, code := range metrics.AllFailureReasonCodes {
		needle := "`" + string(code) + "`"
		if !strings.Contains(doc, needle) {
			t.Errorf("failure code %q is in the Go enum but not documented in %s", code, docPath)
		}
	}

	// Spot the docs's own enum table for any backticked
	// failure-code-shaped string the Go enum doesn't carry. The
	// taxonomy's wire strings are lower_snake_case so we only
	// scan for those in the failure-category section.
	startMarker := "## Failure categories"
	endMarker := "## Prometheus metrics"
	startIdx := strings.Index(doc, startMarker)
	endIdx := strings.Index(doc, endMarker)
	if startIdx < 0 || endIdx < 0 || endIdx <= startIdx {
		t.Fatalf("doc anchors missing — has the file been restructured?")
	}
	section := doc[startIdx:endIdx]

	// Collect every backticked lower_snake_case token in the
	// failure-category section. Compare against the enum.
	knownGo := make(map[string]bool)
	for _, code := range metrics.AllFailureReasonCodes {
		knownGo[string(code)] = true
	}
	for _, line := range strings.Split(section, "\n") {
		// Only scan TABLE ROWS — the closed enum table is the
		// authoritative list. Other backticked tokens in prose
		// (e.g. `reason_code`, `pg_monitor`, `query_timeout` the
		// config field name) match the wire-string shape but
		// aren't enum values.
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "| `") {
			continue
		}
		// First backticked token on the row IS the enum value.
		rest := trimmed[3:] // skip `| ` and the opening backtick we already matched
		close := strings.Index(rest, "`")
		if close < 0 {
			continue
		}
		token := rest[:close]
		if isFailureCodeShape(token) && !knownGo[token] {
			t.Errorf("doc references failure code %q that is not in the Go enum", token)
		}
	}
}

// isFailureCodeShape reports whether s looks like a wire string
// in the taxonomy: lower_snake_case, at least one underscore,
// only [a-z_] characters. Avoids false positives on backticked
// English ("target_unreachable") vs ("the doctor command").
func isFailureCodeShape(s string) bool {
	if !strings.Contains(s, "_") {
		return false
	}
	if len(s) < 3 {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && r != '_' {
			return false
		}
	}
	return true
}

// #138 — every enum value's wire string is exactly the constant value
// (no map indirection / no normalisation lossy round-trip).
func TestFailureTaxonomy_WireStringsMatchConsts(t *testing.T) {
	expected := map[metrics.FailureReasonCode]string{
		metrics.ReasonTargetUnreachable:             "target_unreachable",
		metrics.ReasonTargetTLSInvalid:              "target_tls_invalid",
		metrics.ReasonAuthFailed:                    "auth_failed",
		metrics.ReasonRoleInsufficient:              "role_insufficient",
		metrics.ReasonCollectorPGVersionUnsupported: "collector_pg_version_unsupported",
		metrics.ReasonCollectorExtensionMissing:     "collector_extension_missing",
		metrics.ReasonCollectorQueryTimeout:         "collector_query_timeout",
		metrics.ReasonCollectorCircuitOpen:          "collector_circuit_open",
		metrics.ReasonStorageWriteFailed:            "storage_write_failed",
		metrics.ReasonStorageBusy:                   "storage_busy",
		metrics.ReasonConfigInvalid:                 "config_invalid",
		metrics.ReasonUnknown:                       "unknown",
	}
	for got, want := range expected {
		if string(got) != want {
			t.Errorf("FailureReasonCode value drift: got %q, want %q", got, want)
		}
	}
	if len(expected) != len(metrics.AllFailureReasonCodes) {
		t.Errorf("test pinned %d codes; AllFailureReasonCodes has %d — sync the test",
			len(expected), len(metrics.AllFailureReasonCodes))
	}
}

// #138 — IsValid round-trip.
func TestFailureTaxonomy_IsValid(t *testing.T) {
	for _, code := range metrics.AllFailureReasonCodes {
		if !code.IsValid() {
			t.Errorf("code %q is in AllFailureReasonCodes but IsValid() == false", code)
		}
	}
	if metrics.FailureReasonCode("future_unknown_code").IsValid() {
		t.Errorf("unknown wire-string accepted by IsValid")
	}
}
