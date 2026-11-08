// Package dryrun723 hosts a single INTENTIONALLY FAILING test used to validate
// the poc/singlestore branch-protection gate for Elevarq/Signals#723 (F17a).
//
// It exists only to prove that a red required check (the aggregate `ci-gate`
// context) blocks a merge to the active POC integration branch. This file is a
// throwaway on the `723-protection-dryrun` branch — it ships no product
// behavior and is deleted once the blocked-merge evidence is captured. It must
// never be merged.
package dryrun723

import "testing"

// TestDryRun723_ProtectionGateMustBlockMerge always fails on purpose. With the
// branch-protection policy applied (required context: ci-gate, strict,
// enforce-admins), this failing check must make the PR un-mergeable — that is
// the evidence for #723's third acceptance item.
func TestDryRun723_ProtectionGateMustBlockMerge(t *testing.T) {
	t.Fatal("intentional dry-run failure (#723 / F17a): a failing required check must block merge to poc/singlestore; this throwaway branch is deleted after evidence capture")
}
