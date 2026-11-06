package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Elevarq/Signals#675 — scripts/cosign-sign-idempotent.sh must wrap
// `cosign sign --yes <ref>` so that a Rekor transparency-log 409
// ("an equivalent entry already exists") on a re-run / unchanged digest
// is treated as success (the ref is already signed = desired end state),
// while any OTHER cosign failure still fails. These tests drive the
// script with a fake `cosign` on PATH so no real signing or network is
// needed.
//
// Cases:
//
//	(a) fake cosign exits 0                 -> script exits 0
//	(b) fake cosign prints a 409 body, exit 1 -> script exits 0 (tolerated)
//	(c) fake cosign prints unrelated error, exit 1 -> script exits 1

// runCosignWrapper runs the idempotent-sign wrapper with a fake `cosign`
// shimmed onto PATH. fakeCosign is a bash script body (minus the shebang)
// that stands in for the cosign binary.
func runCosignWrapper(t *testing.T, fakeCosign string) (string, error) {
	t.Helper()

	script := filepath.Join(repoRoot(t), "scripts", "cosign-sign-idempotent.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("wrapper script missing: %v", err)
	}

	// A throwaway bin dir holding only our fake cosign, placed first on PATH.
	binDir := t.TempDir()
	fakePath := filepath.Join(binDir, "cosign")
	if err := os.WriteFile(fakePath, []byte("#!/usr/bin/env bash\n"+fakeCosign), 0o755); err != nil {
		t.Fatalf("write fake cosign: %v", err)
	}

	cmd := exec.Command("bash", script, "ghcr.io/elevarq/signals@sha256:deadbeef")
	cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// (a) cosign succeeds -> wrapper succeeds.
func TestCosignIdempotent_SuccessPassesThrough(t *testing.T) {
	out, err := runCosignWrapper(t, "echo 'tlog entry created'\nexit 0\n")
	if err != nil {
		t.Fatalf("expected exit 0 on cosign success, got err=%v\noutput:\n%s", err, out)
	}
}

// (b) cosign fails with a Rekor-409 body -> wrapper tolerates it (exit 0).
func TestCosignIdempotent_Rekor409Tolerated(t *testing.T) {
	// Representative of the real cosign/Rekor 409 message.
	body := `error: signing ghcr.io/elevarq/signals@sha256:deadbeef: ` +
		`uploading to tlog: POST https://rekor.sigstore.dev/api/v1/log/entries: ` +
		`409 Conflict: {"code":409,"message":"An equivalent entry already exists ` +
		`in the transparency log"}` + "\ncreateLogEntryConflict"
	out, err := runCosignWrapper(t, "echo '"+body+"' >&2\nexit 1\n")
	if err != nil {
		t.Fatalf("expected exit 0 on tolerated Rekor 409, got err=%v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "treating as success") {
		t.Fatalf("expected a 'treating as success' line in output, got:\n%s", out)
	}
}

// (c) cosign fails with an unrelated error -> wrapper propagates failure.
func TestCosignIdempotent_OtherFailurePropagates(t *testing.T) {
	out, err := runCosignWrapper(t,
		"echo 'error: getting signer: reading key: no such file or directory' >&2\nexit 1\n")
	if err == nil {
		t.Fatalf("expected non-zero exit on unrelated cosign failure, got success\noutput:\n%s", out)
	}
	if _, ok := err.(*exec.ExitError); !ok {
		t.Fatalf("expected an ExitError, got %T: %v", err, err)
	}
}
