package tests

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// #733 (audit A8) — the CI Helm/Kubernetes gate must verify each pinned tool
// tarball (kube-linter, conftest) against an expected SHA-256 before
// extraction, and fail closed on any mismatch, missing download, malformed
// pin, or absent member. These tests drive the real
// scripts/install-pinned-tool.sh against local fixtures (file:// URLs, no
// network) and assert the structural pinning in ci.yml.
//
// Spec: specifications/ci-tool-pinning.md (SCT-R001..R004).

// makeTarGz writes a gzip-compressed tar containing one member and returns the
// tarball path plus its lowercase hex SHA-256.
func makeTarGz(t *testing.T, dir, member string, content []byte) (path, digest string) {
	t.Helper()
	path = filepath.Join(dir, "fixture.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: member, Mode: 0o755, Size: int64(len(content))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture back: %v", err)
	}
	sum := sha256.Sum256(raw)
	return path, hex.EncodeToString(sum[:])
}

func runInstaller(t *testing.T, url, digest, member, dest string) (string, error) {
	t.Helper()
	script := filepath.Join(repoRoot(t), "scripts", "install-pinned-tool.sh")
	cmd := exec.Command("bash", script, url, digest, member, dest)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// AC-SCT-01 — a matching digest extracts the member and exits 0.
func TestCIPinnedTool_MatchingDigestExtracts(t *testing.T) {
	src := t.TempDir()
	tarball, digest := makeTarGz(t, src, "kube-linter", []byte("#!/bin/sh\necho hi\n"))
	dest := t.TempDir()

	out, err := runInstaller(t, "file://"+tarball, digest, "kube-linter", dest)
	if err != nil {
		t.Fatalf("matching digest should succeed, got err=%v out=%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(dest, "kube-linter")); statErr != nil {
		t.Fatalf("member not extracted on verified install: %v", statErr)
	}
}

// AC-SCT-02 — altered bytes (wrong digest) fail closed before extraction.
func TestCIPinnedTool_AlteredBytesRejected(t *testing.T) {
	src := t.TempDir()
	// Build with one content, pin its digest, then rebuild with different
	// bytes at the same path — the pin no longer matches (tamper/replace).
	_, pinned := makeTarGz(t, src, "conftest", []byte("original trusted bytes\n"))
	tarball, altered := makeTarGz(t, src, "conftest", []byte("REPLACED malicious bytes\n"))
	if pinned == altered {
		t.Fatal("fixture bytes did not change; test is vacuous")
	}
	dest := t.TempDir()

	out, err := runInstaller(t, "file://"+tarball, pinned, "conftest", dest)
	if err == nil {
		t.Fatalf("altered bytes must be rejected, but installer exited 0; out=%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(dest, "conftest")); statErr == nil {
		t.Fatal("member was extracted despite digest mismatch (not fail-closed)")
	}
}

// AC-SCT-03 — a missing download fails closed.
func TestCIPinnedTool_MissingDownloadRejected(t *testing.T) {
	dest := t.TempDir()
	missing := filepath.Join(t.TempDir(), "does-not-exist.tar.gz")
	// Any 64-hex digest; the download fails before verification matters.
	digest := strings.Repeat("a", 64)

	out, err := runInstaller(t, "file://"+missing, digest, "kube-linter", dest)
	if err == nil {
		t.Fatalf("missing download must fail closed, got exit 0; out=%s", out)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatal("something was extracted on a failed download")
	}
}

// AC-SCT-04 — a malformed pin fails closed before any download.
func TestCIPinnedTool_MalformedDigestRejected(t *testing.T) {
	src := t.TempDir()
	tarball, _ := makeTarGz(t, src, "kube-linter", []byte("x"))
	dest := t.TempDir()

	for _, bad := range []string{"deadbeef", "", "ZZZZ" + strings.Repeat("a", 60)} {
		out, err := runInstaller(t, "file://"+tarball, bad, "kube-linter", dest)
		if err == nil {
			t.Fatalf("malformed digest %q must be rejected, got exit 0; out=%s", bad, out)
		}
	}
	if _, statErr := os.Stat(filepath.Join(dest, "kube-linter")); statErr == nil {
		t.Fatal("member extracted despite malformed pin")
	}
}

// AC-SCT-05 — a verified tarball missing the requested member fails closed.
func TestCIPinnedTool_MissingMemberRejected(t *testing.T) {
	src := t.TempDir()
	// Tarball contains "some-other-file", but we ask for "kube-linter".
	tarball, digest := makeTarGz(t, src, "some-other-file", []byte("x"))
	dest := t.TempDir()

	out, err := runInstaller(t, "file://"+tarball, digest, "kube-linter", dest)
	if err == nil {
		t.Fatalf("missing member must fail closed, got exit 0; out=%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(dest, "kube-linter")); statErr == nil {
		t.Fatal("kube-linter present despite being absent from the tarball")
	}
}

// AC-SCT-06 — ci.yml installs both tools through the verifier with a pinned
// 64-hex digest, and never pipes a download straight into tar for them.
func TestCIPinnedTool_WorkflowPinsBothTools(t *testing.T) {
	ciPath := filepath.Join(repoRoot(t), ".github", "workflows", "ci.yml")
	data, err := os.ReadFile(ciPath)
	if err != nil {
		t.Fatalf("read ci.yml: %v", err)
	}
	ci := string(data)

	if strings.Count(ci, "scripts/install-pinned-tool.sh") < 2 {
		t.Error("ci.yml must install kube-linter and conftest via install-pinned-tool.sh")
	}
	for _, pin := range []string{
		"1a6d8419b11971372971fdbc22682b684ebfb7cf1c39591662d1b6ca736c41df", // kube-linter v0.8.3
		"284231908e4cf66d156e5577c3eee2f67d9f2edf2521fa2309e0df1969f871e5", // conftest 0.62.0
	} {
		if !strings.Contains(ci, pin) {
			t.Errorf("ci.yml missing pinned digest %s", pin)
		}
	}
	// No unverified "download | tar" for these specific tools.
	for _, line := range strings.Split(ci, "\n") {
		l := strings.TrimSpace(line)
		if strings.Contains(l, "tar -xz") && strings.Contains(l, "|") &&
			(strings.Contains(l, "kube-linter") || strings.Contains(l, "conftest")) {
			t.Errorf("ci.yml still pipes an unverified download into tar: %q", l)
		}
	}
}
