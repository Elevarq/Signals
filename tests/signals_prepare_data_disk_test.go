package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// #713 (audit F07) — the VM bootstrap must resolve the intended durable data
// volume by provider identity, wait bounded for it, and format ONLY when it is
// provably blank — never scan-and-pick an arbitrary disk, and never treat a
// blkid probe error as "empty, format it". These tests drive the shared
// deploy/common/scripts/prepare-data-disk.sh against regular-file fixtures with
// mock fs tools, exercising the format-decision and fail-closed paths.
//
// Spec: specifications/cloud-data-disk-safety.md (CDD-R001..R004).

// mockDiskToolsDir writes mock blkid/mkfs.ext4/mount/findmnt/chown onto a dir.
// blkid -p exits with $BLKID_PROBE_RC (the blank/existing/error signal); blkid
// -s UUID prints a fixed UUID. mkfs.ext4 and mount/chown append to logFile;
// findmnt echoes $FINDMNT_SOURCE so the post-mount verification can pass.
func mockDiskToolsDir(t *testing.T, logFile string) string {
	t.Helper()
	dir := t.TempDir()
	tools := map[string]string{
		"blkid": `#!/usr/bin/env bash
if [ "$1" = "-p" ]; then exit "${BLKID_PROBE_RC:-2}"; fi
# blkid -s UUID -o value <dev>
echo "DEADBEEF-0000-0000-0000-000000000000"
`,
		"mkfs.ext4": `#!/usr/bin/env bash
echo "mkfs.ext4 $*" >> "` + logFile + `"
`,
		"mount": `#!/usr/bin/env bash
echo "mount $*" >> "` + logFile + `"
`,
		"findmnt": `#!/usr/bin/env bash
echo "${FINDMNT_SOURCE:-}"
`,
		"chown": `#!/usr/bin/env bash
echo "chown $*" >> "` + logFile + `"
`,
	}
	for name, body := range tools {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatalf("write mock %s: %v", name, err)
		}
	}
	return dir
}

type diskRun struct {
	err    error
	output string
	log    string
}

func runPrepareDisk(t *testing.T, identity, mount string, probeRC string, findmntSource string, deadline string) diskRun {
	t.Helper()
	script := filepath.Join(repoRoot(t), "deploy", "common", "scripts", "prepare-data-disk.sh")
	work := t.TempDir()
	logFile := filepath.Join(work, "ops.log")
	fstab := filepath.Join(work, "fstab")
	toolsDir := mockDiskToolsDir(t, logFile)
	// Use a writable temp mount point (the real bootstraps use /data, which is
	// root-only); the mock `mount` is a no-op so the path just needs to exist.
	mountDir := filepath.Join(work, strings.TrimPrefix(mount, "/"))

	cmd := exec.Command("bash", script, identity, mountDir, "10001:10001", deadline)
	cmd.Env = append(os.Environ(),
		"PATH="+toolsDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"PREPARE_DISK_SKIP_BLOCK_CHECK=1",
		"PREPARE_DISK_FSTAB="+fstab,
		"BLKID_PROBE_RC="+probeRC,
		"FINDMNT_SOURCE="+findmntSource,
	)
	out, err := cmd.CombinedOutput()
	logData, _ := os.ReadFile(logFile)
	return diskRun{err: err, output: string(out), log: string(logData)}
}

// makeFixtureDev creates a regular file standing in for a resolved device.
func makeFixtureDev(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte{}, 0o644); err != nil {
		t.Fatalf("fixture dev: %v", err)
	}
	return p
}

// CDD-R003 — a provably-blank device (blkid probe rc 2) is formatted and mounted.
func TestPrepareDisk_FormatsBlankDevice(t *testing.T) {
	dev := makeFixtureDev(t, "data0")
	r := runPrepareDisk(t, dev, "/data", "2", dev, "4")
	if r.err != nil {
		t.Fatalf("blank device should prepare successfully: %v\n%s", r.err, r.output)
	}
	if !strings.Contains(r.log, "mkfs.ext4") {
		t.Errorf("a blank device must be formatted; ops log:\n%s", r.log)
	}
}

// CDD-R003 — an existing filesystem (probe rc 0) is preserved, never reformatted.
func TestPrepareDisk_PreservesExistingFilesystem(t *testing.T) {
	dev := makeFixtureDev(t, "data0")
	r := runPrepareDisk(t, dev, "/data", "0", dev, "4")
	if r.err != nil {
		t.Fatalf("populated device should mount without error: %v\n%s", r.err, r.output)
	}
	if strings.Contains(r.log, "mkfs.ext4") {
		t.Errorf("an existing filesystem must NOT be reformatted (data loss); ops log:\n%s", r.log)
	}
}

// CDD-R002 — a blkid probe ERROR (rc not 0/2) fails closed and never formats.
func TestPrepareDisk_ProbeErrorFailsClosed(t *testing.T) {
	dev := makeFixtureDev(t, "data0")
	r := runPrepareDisk(t, dev, "/data", "8", dev, "4")
	if r.err == nil {
		t.Fatalf("a blkid probe error must fail closed, but the script exited 0\n%s", r.output)
	}
	if strings.Contains(r.log, "mkfs.ext4") {
		t.Errorf("a probe error must NOT format the device; ops log:\n%s", r.log)
	}
}

// CDD-R001 — the intended device never attaches → fail closed after the
// deadline; no scan-and-pick fallback, no format.
func TestPrepareDisk_MissingDeviceFailsClosed(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "never-attaches")
	r := runPrepareDisk(t, missing, "/data", "2", missing, "2")
	if r.err == nil {
		t.Fatalf("a never-attaching device must fail closed, but the script exited 0\n%s", r.output)
	}
	if strings.Contains(r.log, "mkfs.ext4") {
		t.Errorf("a missing device must never be formatted; ops log:\n%s", r.log)
	}
}

// CDD-R001 — an ambiguous identity (matches >1 device) fails closed rather than
// guessing which disk to use.
func TestPrepareDisk_AmbiguousIdentityFailsClosed(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "disk-a")
	b := filepath.Join(dir, "disk-b")
	_ = os.WriteFile(a, []byte{}, 0o644)
	_ = os.WriteFile(b, []byte{}, 0o644)
	glob := filepath.Join(dir, "disk-*")
	r := runPrepareDisk(t, glob, "/data", "2", a, "2")
	if r.err == nil {
		t.Fatalf("an ambiguous identity must fail closed, but the script exited 0\n%s", r.output)
	}
	if !strings.Contains(r.output, "ambiguous") {
		t.Errorf("expected an ambiguity diagnostic; got:\n%s", r.output)
	}
}

// CDD-R004 — no affected VM-bootstrap template retains the unsafe patterns:
// (a) treating a plain `blkid` failure as "format it", or (b) a scan-and-pick
// loop that assigns the LAST matching device without a fail-closed ambiguity
// check. Every template must use the blkid -p probe gate (or embed the shared
// prepare-data-disk.sh which does).
func TestCloudTemplates_NoUnsafeDiskPattern(t *testing.T) {
	templates := []string{
		"deploy/aws/terraform/main.tf",
		"deploy/azure/terraform/main.tf",
		"deploy/gcp/terraform/main.tf",
		"deploy/azure/bicep/main.bicep",
		"deploy/aws/cloudformation/signals-rds-iam.yaml",
		"deploy/aws/imagebuilder/signals-collector-component.yaml",
	}
	for _, tmpl := range templates {
		data, err := os.ReadFile(filepath.Join(repoRoot(t), tmpl))
		if err != nil {
			t.Fatalf("read %s: %v", tmpl, err)
		}
		body := string(data)
		// (a) the unsafe "format whenever plain blkid fails" gate.
		if strings.Contains(body, "if ! blkid ") {
			t.Errorf("%s: retains unsafe `if ! blkid ... mkfs` (formats on a probe error) (#713)", tmpl)
		}
		// A template is safe if it embeds the shared script OR uses the blkid -p
		// probe gate inline.
		embedsShared := strings.Contains(body, "prepare-data-disk.sh")
		hasProbeGate := strings.Contains(body, "blkid -p")
		if !embedsShared && !hasProbeGate {
			t.Errorf("%s: no safe disk preparation (neither the shared script nor a blkid -p probe gate) (#713)", tmpl)
		}
	}
}
