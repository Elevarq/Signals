package tests

import (
	"strings"
	"testing"
)

// #718 (F12) — the collector is a single-writer SQLite store, so the chart must
// use the Recreate deployment strategy (stop-before-start) rather than the
// Kubernetes default RollingUpdate, which would surge a second pod against the
// same ReadWriteOnce store.
//
// Spec: specifications/singleton-store-upgrade.md (STO-R001..R002).

// STO-R001 — the rendered Deployment pins strategy.type: Recreate.
func TestHelm_DeploymentUsesRecreateStrategy(t *testing.T) {
	out := renderHelm(t)

	// Isolate the Deployment document so we don't match a strategy field in
	// another manifest.
	dep := extractManifest(t, out, "kind: Deployment")
	if !strings.Contains(dep, "strategy:") {
		t.Fatal("Deployment has no strategy block; singleton SQLite store requires Recreate (#718)")
	}
	if !strings.Contains(dep, "type: Recreate") {
		t.Errorf("Deployment strategy must be Recreate for the single-writer store; got:\n%s", dep)
	}
	// Defensive: the default RollingUpdate must not be what ships.
	if strings.Contains(dep, "type: RollingUpdate") {
		t.Error("Deployment uses RollingUpdate; a surge pod would share the single-writer SQLite store (#718)")
	}
}

// extractManifest returns the single YAML document in out that contains marker,
// split on the `---` document separator.
func extractManifest(t *testing.T, out, marker string) string {
	t.Helper()
	for _, doc := range strings.Split(out, "\n---") {
		if strings.Contains(doc, marker) {
			return doc
		}
	}
	t.Fatalf("no manifest containing %q in render", marker)
	return ""
}
