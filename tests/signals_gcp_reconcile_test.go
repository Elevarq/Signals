package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// #732 (audit A7b) — the GCP startup reconcile must be idempotent across a
// reboot / script re-run: reuse the already-minted operator token (never rotate
// it out from under the running daemon) and reconcile the container BY NAME
// (docker start if it exists) rather than an unconditional `docker run` that
// fails on the name clash. These tests drive the real
// deploy/gcp/scripts/reconcile-collector.sh with a mock `docker` on PATH.
//
// Spec: specifications/gcp-startup-idempotency.md (GSI-R001..R003).

// mockDockerDir writes a fake `docker` that records its invocations to logFile
// and simulates container existence via a marker file. `docker inspect` exits 0
// only after a `docker run` has created the marker.
func mockDockerDir(t *testing.T, logFile, marker string) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/usr/bin/env bash
echo "$@" >> "` + logFile + `"
case "$1" in
  inspect) [ -f "` + marker + `" ] && exit 0 || exit 1 ;;
  run)     : > "` + marker + `"; exit 0 ;;
  start)   exit 0 ;;
  *)       exit 0 ;;
esac
`
	p := filepath.Join(dir, "docker")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatalf("write mock docker: %v", err)
	}
	return dir
}

func runReconcile(t *testing.T, dockerDir, tokenFile string) {
	t.Helper()
	scriptPath := filepath.Join(repoRoot(t), "deploy", "gcp", "scripts", "reconcile-collector.sh")
	cmd := exec.Command("bash", scriptPath)
	cmd.Env = append(os.Environ(),
		"PATH="+dockerDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"IMAGE_URI=example.com/signals:test",
		"TOKEN_FILE="+tokenFile,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("reconcile-collector.sh failed: %v\n%s", err, out)
	}
}

// GSI-R001/R002 — first run creates the container and mints a token; a second
// run (container present) reconciles via `docker start`, does NOT `docker run`
// again, and REUSES the same token.
func TestGCPReconcile_IdempotentAcrossRerun(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl not on PATH; reconcile script needs it to mint a token")
	}
	work := t.TempDir()
	logFile := filepath.Join(work, "docker.log")
	marker := filepath.Join(work, "container.marker")
	tokenFile := filepath.Join(work, "signals-api-token")
	dockerDir := mockDockerDir(t, logFile, marker)

	// First run: no container, no token.
	runReconcile(t, dockerDir, tokenFile)
	token1, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatalf("token not minted on first run: %v", err)
	}
	log1, _ := os.ReadFile(logFile)
	if !strings.Contains(string(log1), "run -d --name signals") {
		t.Fatalf("first run must `docker run` the container; log:\n%s", log1)
	}

	// Second run: container now exists (marker set), token file present.
	_ = os.WriteFile(logFile, nil, 0o644) // reset log to inspect only the 2nd run
	runReconcile(t, dockerDir, tokenFile)
	log2, _ := os.ReadFile(logFile)
	if strings.Contains(string(log2), "run -d --name signals") {
		t.Errorf("second run must NOT `docker run` again (name clash); log:\n%s", log2)
	}
	if !strings.Contains(string(log2), "start signals") {
		t.Errorf("second run must reconcile via `docker start signals`; log:\n%s", log2)
	}

	// Token must be reused, not rotated.
	token2, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatalf("token file missing after second run: %v", err)
	}
	if string(token1) != string(token2) {
		t.Error("operator token was rotated on re-run; it must be reused so access to the running daemon is not orphaned (#732)")
	}
}

// GSI-R003 — the terraform startup script embeds the shared reconcile script
// (single source of truth) rather than an unconditional inline `docker run`.
func TestGCPReconcile_TerraformEmbedsSharedScript(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "gcp", "terraform", "main.tf"))
	if err != nil {
		t.Fatalf("read gcp terraform: %v", err)
	}
	if !strings.Contains(string(data), "reconcile-collector.sh") {
		t.Error("gcp terraform must embed deploy/gcp/scripts/reconcile-collector.sh (#732)")
	}
}
