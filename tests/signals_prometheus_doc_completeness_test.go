package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Anti-drift guard for #167. Every Prometheus metric registered in
// internal/metrics/metrics.go must be documented in docs/prometheus.md's
// metric reference. docs/prometheus.md had fallen two metrics behind the
// code (signals_circuit_state, signals_eligible_collectors); this keeps the
// reference complete as metrics are added.
func TestPrometheusDoc_EveryRegisteredMetricDocumented(t *testing.T) {
	root := repoRoot(t)

	src, err := os.ReadFile(filepath.Join(root, "internal", "metrics", "metrics.go"))
	if err != nil {
		t.Fatalf("read metrics.go: %v", err)
	}
	doc, err := os.ReadFile(filepath.Join(root, "docs", "prometheus.md"))
	if err != nil {
		t.Fatalf("read prometheus.md: %v", err)
	}
	docBody := string(doc)

	re := regexp.MustCompile(`Name:\s*"(signals_[a-z0-9_]+)"`)
	names := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		names[m[1]] = true
	}
	if len(names) == 0 {
		t.Fatal("no signals_* metric names found in metrics.go — the guard regex needs updating")
	}

	var missing []string
	for n := range names {
		if !strings.Contains(docBody, n) {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		t.Errorf("docs/prometheus.md is missing %d registered metric(s) from its reference: %s",
			len(missing), strings.Join(missing, ", "))
	}
}
