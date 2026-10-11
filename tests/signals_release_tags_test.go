package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Anti-drift guard for the Docker Hub distribution contract (#497, #499).
//
// release.yml is the single production-publish path: one multi-arch build
// pushed to BOTH ghcr.io/elevarq/signals and Docker Hub elevarq/signals at
// the same digest. The tag policy — `latest=auto` so only a stable release
// moves `latest`, never a pre-release — is encoded in docker/metadata-action
// config, which no unit test exercises. These assertions pin the load-bearing
// lines so a future edit that (a) reverts to an unconditional `latest`,
// (b) drops a registry, or (c) introduces a publish outside the tag trigger
// fails CI instead of silently shipping.
//
// They are string assertions against the workflow YAML on purpose: the thing
// under guard IS the declarative config, so matching its text is the most
// direct expression of the contract.

func readReleaseWorkflow(t *testing.T) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), ".github", "workflows", "release.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read release.yml: %v", err)
	}
	return string(data)
}

// The publish job must target both registries at the same digest.
func TestRelease_PublishesBothRegistries(t *testing.T) {
	wf := readReleaseWorkflow(t)
	for _, want := range []string{
		"IMAGE_NAME: ghcr.io/elevarq/signals",
		"DOCKERHUB_IMAGE: elevarq/signals",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("release.yml: missing %q — Docker Hub/GHCR dual publish broken (#497)", want)
		}
	}
}

// Docker Hub is a REQUIRED production target and must not silently drop out
// (#499). A stable release expects BOTH registries; the metadata image list
// includes Docker Hub whenever publishing is enabled — NOT gated on a login
// outcome, which was the silent-degrade mechanism that shipped GHCR-only
// "successful" releases for months.
func TestRelease_DockerHubCannotSilentlyDisappear(t *testing.T) {
	wf := readReleaseWorkflow(t)

	// The image list is gated on the explicit publish-mode output, so a
	// failed login can no longer remove Docker Hub from the targets.
	if !strings.Contains(wf, "steps.dockerhub.outputs.enabled == 'true' && env.DOCKERHUB_IMAGE") {
		t.Error("release.yml: Docker Hub image must be included via steps.dockerhub.outputs.enabled, not a login outcome (#499)")
	}
	// The old silent-drop plumbing must be gone entirely.
	for _, forbidden := range []string{
		"dockerhub-login.outcome",
		"dockerhub-check",
		"continue-on-error: true", // the fail-soft directive itself, not prose
	} {
		if strings.Contains(wf, forbidden) {
			t.Errorf("release.yml: found %q — reintroduces the fail-soft/silent-degrade path removed in #499", forbidden)
		}
	}
}

// Missing required Docker Hub credentials must fail the production release,
// not degrade to an apparently-successful GHCR-only run (#499).
func TestRelease_MissingDockerHubCredsFailsRelease(t *testing.T) {
	wf := readReleaseWorkflow(t)
	for _, want := range []string{
		// Hard error + non-zero exit when enabled and creds absent.
		"Docker Hub is a required release target but DOCKERHUB_USERNAME/DOCKERHUB_TOKEN are not set",
		// Positive confirmation the pushed Docker Hub tag matches the digest.
		"Verify Docker Hub publication (same digest as GHCR)",
		"imagetools inspect",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("release.yml: missing %q — a required-but-unpublished Docker Hub target could pass silently (#499)", want)
		}
	}
}

// Disabling Docker Hub must be an explicit, auditable opt-out — distinct from
// an accidental failure (#499).
func TestRelease_DockerHubOptOutIsExplicit(t *testing.T) {
	wf := readReleaseWorkflow(t)
	if !strings.Contains(wf, "vars.DOCKERHUB_PUBLISH") {
		t.Error("release.yml: no explicit DOCKERHUB_PUBLISH opt-out variable — cannot distinguish intentional GHCR-only from failure (#499)")
	}
}

// `latest` must track the newest stable release only. docker/metadata-action
// achieves that with `flavor: latest=auto`; an unconditional `latest`
// (`type=raw,value=latest` or `flavor: latest=true`) would move `latest` on a
// `-rc`/`-beta` tag, which is exactly what we must never do.
func TestRelease_LatestIsStableOnly(t *testing.T) {
	wf := readReleaseWorkflow(t)

	if !strings.Contains(wf, "latest=auto") {
		t.Error("release.yml: `flavor: latest=auto` missing — `latest` would no longer be stable-only (#497)")
	}
	for _, forbidden := range []string{
		"type=raw,value=latest",
		"latest=true",
	} {
		if strings.Contains(wf, forbidden) {
			t.Errorf("release.yml: found %q — this moves `latest` onto pre-releases; use `latest=auto` (#497)", forbidden)
		}
	}
}

// The semver tag set buyers pin against must stay intact.
func TestRelease_SemverTagPatterns(t *testing.T) {
	wf := readReleaseWorkflow(t)
	for _, want := range []string{
		"type=semver,pattern={{version}}",
		"type=semver,pattern={{major}}.{{minor}}",
		"type=semver,pattern={{major}}",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("release.yml: missing tag pattern %q (#497)", want)
		}
	}
}

// A GitHub pre-release tag must be flagged as a pre-release on the Release,
// reinforcing the stable/pre-release split end to end.
func TestRelease_PrereleaseFlaggedFromTag(t *testing.T) {
	wf := readReleaseWorkflow(t)
	if !strings.Contains(wf, "prerelease: ${{ contains(github.ref_name, '-') }}") {
		t.Error("release.yml: pre-release detection from tag name missing (#497)")
	}
}

// Production publish happens on tag push only. Nothing in the repo's
// workflows may push an image or log in to Docker Hub outside release.yml —
// that is what keeps PR / branch / dev builds from ever publishing or
// touching the Docker Hub token.
func TestRelease_NoPublishOutsideTagTrigger(t *testing.T) {
	wf := readReleaseWorkflow(t)
	if !strings.Contains(wf, "tags: ['v*']") {
		t.Error("release.yml: expected tag-only trigger `tags: ['v*']` (#497)")
	}

	dir := filepath.Join(repoRoot(t), ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read workflows dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		body := string(data)

		// Only release.yml may push an image.
		if strings.Contains(body, "push: true") && e.Name() != "release.yml" {
			t.Errorf("%s: has `push: true` — only release.yml may publish images (#497)", e.Name())
		}
		// Only release.yml may authenticate to Docker Hub.
		if strings.Contains(body, "registry: docker.io") && e.Name() != "release.yml" {
			t.Errorf("%s: logs in to docker.io — only release.yml may touch the Docker Hub token (#497)", e.Name())
		}
	}
}
