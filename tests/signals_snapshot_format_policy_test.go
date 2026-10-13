package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/export"
	"github.com/elevarq/signals/snapshot"
)

// Anti-drift guards for the snapshot/output-format compatibility & deprecation
// policy (#503, part of the upgrade-lifecycle umbrella #500). The policy in
// docs/compatibility/snapshot-format-policy.md documents the ALREADY-versioned,
// self-describing export format (snapshot.SchemaVersion,
// export.CollectorStatusSchemaVersion, export.ManifestVersion + per-file
// SHA-256). These tests pin the written contract to the live constants so the
// policy can never silently drift from the emitted bytes.

func policyDoc(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "compatibility", "snapshot-format-policy.md"))
	if err != nil {
		t.Fatalf("read docs/compatibility/snapshot-format-policy.md: %v", err)
	}
	return string(data)
}

// TestSnapshotFormatPolicy_PinsLiveVersionConstants fails if any emitted
// version identifier changes without the policy document being updated to
// match. This is the drift guard: a constant bump forces a policy edit.
func TestSnapshotFormatPolicy_PinsLiveVersionConstants(t *testing.T) {
	body := policyDoc(t)

	cases := []struct {
		what  string
		value string
	}{
		{"snapshot.SchemaVersion", snapshot.SchemaVersion},
		{"export.CollectorStatusSchemaVersion", export.CollectorStatusSchemaVersion},
		{"export.ManifestVersion", fmt.Sprintf("%d", export.ManifestVersion)},
	}
	for _, c := range cases {
		if !strings.Contains(body, c.value) {
			t.Errorf("snapshot-format-policy.md does not reference the live %s value %q; "+
				"update the policy when the format constant changes", c.what, c.value)
		}
	}
}

// TestSnapshotFormatPolicy_StatesRequiredContract pins the parts of the policy
// that are load-bearing promises: the version-evolution rule, the emission &
// support window, the integrity guarantee, the no-secrets invariant, and the
// explicit non-goals (no runtime negotiation, no new version fields).
func TestSnapshotFormatPolicy_StatesRequiredContract(t *testing.T) {
	body := policyDoc(t)

	required := []string{
		// Version semantics.
		"Additive",
		"breaking change bumps the major",
		"unknown fields and unknown files",
		// Emission & support window (the customer-facing promise).
		"two minor release cycles",
		// Integrity + no-secrets.
		"SHA-256",
		"manifest.json",
		"INV-SIGNALS-07",
		// Non-goals.
		"No runtime producer/consumer negotiation",
		"No new version fields",
		// Producer-side framing + cross-repo consumer reference (by issue only).
		"producer",
		"Analyzer#3763",
	}
	var missing []string
	for _, s := range required {
		if !strings.Contains(body, s) {
			missing = append(missing, s)
		}
	}
	if len(missing) > 0 {
		t.Errorf("snapshot-format-policy.md is missing required contract statements: %s",
			strings.Join(missing, ", "))
	}
}
