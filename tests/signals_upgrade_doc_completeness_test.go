package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Anti-drift guards for the Signals upgrade documentation (#505, part of the
// upgrade-lifecycle umbrella #500). The docs in docs/upgrading.md document
// ALREADY-IMPLEMENTED behavior (#501 /data durability, #502 forward-only /
// fail-closed schema policy, #504 populated-upgrade gate). These tests pin the
// doc to that reality so a future edit cannot silently reintroduce the drift
// this issue fixed or drop a supported mode.

// TestUpgradeDoc_CoversEverySupportedMode asserts docs/upgrading.md exists and
// names every supported deploy mode plus the per-mode sections the issue
// requires (backup, verification, recovery) and the governing policy.
func TestUpgradeDoc_CoversEverySupportedMode(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "upgrading.md"))
	if err != nil {
		t.Fatalf("read docs/upgrading.md: %v", err)
	}
	body := string(data)

	required := []string{
		// Deploy modes (issue scope).
		"Docker Compose",
		"Helm / EKS",
		"single-instance",
		"Air-gapped",
		// Per-mode procedure elements.
		"Backup",
		"verification",
		"Recovery",
		// The implemented policy that must be stated (#502).
		"forward-only",
		"SchemaNewerError",
		"internal/db.Migrate()",
		// The preserved-state contract (#501/#504).
		"instance_id",
		"watermark",
		"/data/signals.db",
	}
	var missing []string
	for _, s := range required {
		if !strings.Contains(body, s) {
			missing = append(missing, s)
		}
	}
	if len(missing) > 0 {
		t.Errorf("docs/upgrading.md is missing required coverage: %s",
			strings.Join(missing, ", "))
	}
}

// TestUpgradeDoc_NoStaleMigrateSymbol pins the exact doc-drift this issue
// fixed: the migration entry point is internal/db.Migrate(), never the stale
// `internal/store::migrate` symbol that kubernetes-production.md cited. Guard
// the whole docs/ tree so the stale symbol cannot reappear anywhere.
func TestUpgradeDoc_NoStaleMigrateSymbol(t *testing.T) {
	root := repoRoot(t)
	var offenders []string
	err := filepath.Walk(filepath.Join(root, "docs"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(data), "internal/store::migrate") {
			rel, _ := filepath.Rel(root, path)
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk docs/: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("stale migration symbol `internal/store::migrate` found in %s; "+
			"the real symbol is internal/db.Migrate()", strings.Join(offenders, ", "))
	}
}
