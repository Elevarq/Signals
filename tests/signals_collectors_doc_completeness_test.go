package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/pgqueries"
)

// Anti-drift guard for #166. Every collector registered in pgqueries.All()
// must be documented in docs/collectors.md. The README guard pins the
// collector *count* (99); this pins table *completeness*, so a collector
// cannot be added to the registry without an inventory row — which is exactly
// how docs/collectors.md fell 14 behind the registry.
func TestCollectorsDoc_EveryRegistryIDDocumented(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "collectors.md"))
	if err != nil {
		t.Fatalf("read docs/collectors.md: %v", err)
	}
	body := string(data)

	var missing []string
	for _, q := range pgqueries.All() {
		if !strings.Contains(body, q.ID) {
			missing = append(missing, q.ID)
		}
	}
	if len(missing) > 0 {
		t.Errorf("docs/collectors.md is missing %d registered collector(s) from its inventory: %s",
			len(missing), strings.Join(missing, ", "))
	}
}
