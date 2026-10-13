package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// #480 — scripts/cut-release.sh must run every pre-tag gate and refuse (with
// a clear message, non-zero exit) on any failure, so a release can't ship
// broken. These tests drive the script in a throwaway git repo with a local
// bare remote, using --dry-run so nothing is actually tagged or pushed.

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// setupReleaseRepo builds a temp repo whose Chart.yaml/values.yaml/CHANGELOG
// are all consistent at version, committed, and pushed to a local bare origin
// so HEAD == origin/main.
func setupReleaseRepo(t *testing.T, version string) string {
	t.Helper()
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")

	mkdir := func(p string) {
		if err := os.MkdirAll(filepath.Join(repo, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, body string) {
		if err := os.WriteFile(filepath.Join(repo, p), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mkdir("deploy/helm/signals")
	write("deploy/helm/signals/Chart.yaml",
		"apiVersion: v2\nname: signals\nversion: "+version+"\nappVersion: \""+version+"\"\n")
	write("deploy/helm/signals/values.yaml",
		"image:\n  repository: ghcr.io/elevarq/signals\n  tag: \""+version+"\"\n  pullPolicy: IfNotPresent\n")
	write("CHANGELOG.md",
		"# Changelog\n\n## [Unreleased]\n\n## ["+version+"] - 2026-10-13\n\n### Added\n- thing\n")

	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "release "+version)

	bare := t.TempDir()
	git(t, bare, "init", "-q", "--bare")
	git(t, repo, "remote", "add", "origin", bare)
	git(t, repo, "push", "-q", "-u", "origin", "main")
	return repo
}

func runCutRelease(t *testing.T, repo string, args ...string) (string, error) {
	t.Helper()
	script := filepath.Join(repoRoot(t), "scripts", "cut-release.sh")
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestCutRelease_PassesAllGatesOnDryRun(t *testing.T) {
	repo := setupReleaseRepo(t, "1.2.3")
	out, err := runCutRelease(t, repo, "v1.2.3", "--dry-run")
	if err != nil {
		t.Fatalf("expected all gates to pass, got error: %v\n%s", err, out)
	}
	for _, want := range []string{"tag v1.2.3", "version lockstep", "CHANGELOG entry found", "--dry-run"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "push") && !strings.Contains(out, "would") {
		t.Errorf("dry-run must not push:\n%s", out)
	}
}

func TestCutRelease_RejectsBadTagFormat(t *testing.T) {
	repo := setupReleaseRepo(t, "1.2.3")
	for _, bad := range []string{"1.2.3", "v1.2", "v1.2.3-rc1", "v1.2.3-beta.1"} {
		out, err := runCutRelease(t, repo, bad, "--dry-run")
		if err == nil {
			t.Errorf("tag %q should be rejected:\n%s", bad, out)
		}
		if !strings.Contains(out, "not a release tag") {
			t.Errorf("tag %q: expected format message, got:\n%s", bad, out)
		}
	}
}

func TestCutRelease_RejectsImageTagDrift(t *testing.T) {
	repo := setupReleaseRepo(t, "1.2.3")
	// Reproduce the v1.5.1 drift: values.yaml image.tag lags Chart.yaml.
	values := filepath.Join(repo, "deploy/helm/signals/values.yaml")
	if err := os.WriteFile(values,
		[]byte("image:\n  repository: ghcr.io/elevarq/signals\n  tag: \"1.2.2\"\n  pullPolicy: IfNotPresent\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "commit", "-aqm", "drift")
	git(t, repo, "push", "-q", "origin", "main")

	out, err := runCutRelease(t, repo, "v1.2.3", "--dry-run")
	if err == nil {
		t.Fatalf("image.tag drift should fail the lockstep gate:\n%s", out)
	}
	if !strings.Contains(out, "image.tag=1.2.2") || !strings.Contains(out, "lockstep") {
		t.Errorf("expected lockstep failure naming image.tag, got:\n%s", out)
	}
}

func TestCutRelease_RejectsMissingChangelog(t *testing.T) {
	repo := setupReleaseRepo(t, "1.2.3")
	cl := filepath.Join(repo, "CHANGELOG.md")
	if err := os.WriteFile(cl, []byte("# Changelog\n\n## [Unreleased]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "commit", "-aqm", "drop changelog entry")
	git(t, repo, "push", "-q", "origin", "main")

	out, err := runCutRelease(t, repo, "v1.2.3", "--dry-run")
	if err == nil {
		t.Fatalf("missing CHANGELOG entry should fail:\n%s", out)
	}
	if !strings.Contains(out, "no '## [1.2.3]' section") {
		t.Errorf("expected CHANGELOG gate message, got:\n%s", out)
	}
}

func TestCutRelease_RejectsDirtyWorktree(t *testing.T) {
	repo := setupReleaseRepo(t, "1.2.3")
	if err := os.WriteFile(filepath.Join(repo, "CHANGELOG.md"),
		[]byte("# Changelog\n\n## [Unreleased]\n\n## [1.2.3] - 2026-10-13\n\n### Added\n- uncommitted edit\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runCutRelease(t, repo, "v1.2.3", "--dry-run")
	if err == nil {
		t.Fatalf("dirty worktree should fail:\n%s", out)
	}
	if !strings.Contains(out, "not clean") {
		t.Errorf("expected dirty-worktree message, got:\n%s", out)
	}
}

func TestCutRelease_RejectsExistingTag(t *testing.T) {
	repo := setupReleaseRepo(t, "1.2.3")
	git(t, repo, "tag", "-a", "v1.2.3", "-m", "pre-existing")
	out, err := runCutRelease(t, repo, "v1.2.3", "--dry-run")
	if err == nil {
		t.Fatalf("existing tag should fail:\n%s", out)
	}
	if !strings.Contains(out, "already exists") {
		t.Errorf("expected existing-tag message, got:\n%s", out)
	}
}
