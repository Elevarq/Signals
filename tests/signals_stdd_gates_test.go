package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// #451 — the STDD CSI gates (traceability, spec-lifecycle, fingerprint) must
// pass on the committed tree AND actually reject the violations they exist to
// catch. The rejection cases copy the real script into a throwaway fixture
// repo (REPO_ROOT resolves to <tmp>/scripts/.. so the script reads the
// fixture, not this repo) and assert a non-zero exit.

func runScript(t *testing.T, dir, script string, args ...string) error {
	t.Helper()
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = dir
	_, err := cmd.CombinedOutput()
	return err
}

func copyScript(t *testing.T, name string) (fixtureRoot, scriptPath string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(repoRoot(t), "scripts", name)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	dst := filepath.Join(root, "scripts", name)
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatal(err)
	}
	return root, dst
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// AC-GATE-01 — all three gates pass on the committed repository tree.
func TestSTDDGates_PassOnCommittedTree(t *testing.T) {
	root := repoRoot(t)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"check-traceability.sh", nil},
		{"check-spec-status.sh", nil},
		{"fingerprint.sh", []string{"--check"}},
	} {
		if err := runScript(t, root, filepath.Join(root, "scripts", tc.name), tc.args...); err != nil {
			t.Errorf("%s must pass on the committed tree: %v", tc.name, err)
		}
	}
}

const matrixHeader = "# Traceability\n\n| Rule ID | Rule Summary | Test ID(s) | Coverage Status | Evidence Type | Notes |\n|---|---|---|---|---|---|\n"

// AC-GATE-02 — an orphan rule (COVERED/BEHAVIORAL, no Test ID) is rejected.
func TestSTDDGates_TraceabilityRejectsOrphan(t *testing.T) {
	root, script := copyScript(t, "check-traceability.sh")
	write(t, filepath.Join(root, "features/signals/traceability.md"),
		matrixHeader+"| R-X-01 | something | | COVERED | BEHAVIORAL | no test |\n")
	if err := runScript(t, root, script); err == nil {
		t.Fatal("traceability gate must reject an orphan COVERED rule with no Test ID")
	}
}

// AC-GATE-05 — PLANNED rows and DOC-evidence rows may omit a Test ID.
func TestSTDDGates_TraceabilityAllowsPlannedAndDoc(t *testing.T) {
	root, script := copyScript(t, "check-traceability.sh")
	write(t, filepath.Join(root, "features/signals/traceability.md"),
		matrixHeader+
			"| R-X-01 | planned | | PLANNED | BEHAVIORAL | later |\n"+
			"| R-X-02 | doc only | | COVERED | DOC | reaffirmation |\n"+
			"| R-X-03 | tested | TestFoo | COVERED | BEHAVIORAL | ok |\n")
	if err := runScript(t, root, script); err != nil {
		t.Fatalf("PLANNED and DOC rows must be allowed to omit a Test ID: %v", err)
	}
}

// AC-GATE-03 — a non-ACTIVE (DRAFT) shipped root spec is rejected.
func TestSTDDGates_SpecStatusRejectsDraft(t *testing.T) {
	root, script := copyScript(t, "check-spec-status.sh")
	write(t, filepath.Join(root, "specifications/foo.md"), "# Foo\n\n## Status\n\nDRAFT\n")
	if err := runScript(t, root, script); err == nil {
		t.Fatal("spec-lifecycle gate must reject a DRAFT shipped spec")
	}
	// A sibling acceptance file is exempt and must not trip the gate.
	write(t, filepath.Join(root, "specifications/bar.md"), "# Bar\n\n## Status\n\nACTIVE\n")
	write(t, filepath.Join(root, "specifications/bar.acceptance.md"), "# Bar acceptance\n")
	// foo.md is still DRAFT, so it still fails — remove it, leave only ACTIVE+acceptance.
	if err := os.Remove(filepath.Join(root, "specifications/foo.md")); err != nil {
		t.Fatal(err)
	}
	if err := runScript(t, root, script); err != nil {
		t.Fatalf("ACTIVE spec + exempt acceptance file must pass: %v", err)
	}
}

// AC-GATE-04 — knowledge-layer drift after the fingerprint is written fails.
func TestSTDDGates_FingerprintDetectsDrift(t *testing.T) {
	root, script := copyScript(t, "fingerprint.sh")
	write(t, filepath.Join(root, "specifications/a.md"), "# A\n\n## Status\n\nACTIVE\n")
	write(t, filepath.Join(root, "features/signals/x.md"), "x\n")
	write(t, filepath.Join(root, "tests/t_test.go"), "package t\n")

	if err := runScript(t, root, script, "--write"); err != nil {
		t.Fatalf("--write should succeed: %v", err)
	}
	if err := runScript(t, root, script, "--check"); err != nil {
		t.Fatalf("--check should pass right after --write: %v", err)
	}
	// Mutate a knowledge-layer file; --check must now fail.
	write(t, filepath.Join(root, "specifications/a.md"), "# A changed\n\n## Status\n\nACTIVE\n")
	if err := runScript(t, root, script, "--check"); err == nil {
		t.Fatal("fingerprint --check must fail after a knowledge-layer change without --write")
	}
}
