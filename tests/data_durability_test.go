// Copyright (c) 2026 Scantr LLC. All rights reserved.
// Elevarq is a trade name of Scantr LLC.
// This file is part of Elevarq Signals. Use is governed by the
// commercial license at LICENSE in the repository root.

// Structural guards for cloud /data durability (Elevarq/Signals#501).
//
// IaC cannot be provisioned in CI (no cloud credentials, no instance
// replacement), so these are deterministic template-level assertions:
// each of the six VM/AMI deploy artifacts must back /data with a durable
// data resource (EBS volume / Azure managed data disk / GCP persistent
// disk) that is a distinct resource from the root/boot disk, carries a
// retain-on-replace policy, is encrypted (where the provider does not
// encrypt by platform default), is attached at a stable device, and is
// mounted at /data format-if-empty — so instance/AMI replacement preserves
// signals.db instead of destroying it.
//
// Spec: specifications/cloud-data-durability.md
// Acceptance: specifications/cloud-data-durability.acceptance.md
// Traces: R-DUR-01..R-DUR-09, INV-DUR-01, INV-DUR-05.
package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// deployPath resolves a path under deploy/ relative to the tests package.
func deployPath(parts ...string) string {
	return filepath.Join(append([]string{"..", "deploy"}, parts...)...)
}

// readDeploy reads a deploy artifact or fails the test.
func readDeploy(t *testing.T, parts ...string) string {
	t.Helper()
	p := deployPath(parts...)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// mustContain asserts src contains every needle; each miss is a distinct,
// rule-referencing failure so the guard explains exactly what regressed.
func mustContain(t *testing.T, name, src string, needles map[string]string) {
	t.Helper()
	for rule, needle := range needles {
		if !strings.Contains(src, needle) {
			t.Errorf("%s: missing %q (%s)", name, needle, rule)
		}
	}
}

// mustMatch asserts src matches the regexp; failure references the rule.
func mustMatch(t *testing.T, name, rule, src, pattern string) {
	t.Helper()
	if !regexp.MustCompile(pattern).MatchString(src) {
		t.Errorf("%s: pattern %q not found (%s)", name, pattern, rule)
	}
}

// --- TC-DUR-01: AWS Terraform -------------------------------------------------

func TestDataDurability_AWSTerraform(t *testing.T) {
	src := readDeploy(t, "aws", "terraform", "main.tf")

	// R-DUR-01: a separate EBS volume resource for /data.
	mustMatch(t, "aws/terraform", "R-DUR-01", src,
		`resource\s+"aws_ebs_volume"\s+"(data|signals_data)"`)
	// R-DUR-06: encrypted at rest.
	mustMatch(t, "aws/terraform", "R-DUR-06", src, `encrypted\s*=\s*true`)
	// R-DUR-05: retained on replace.
	mustMatch(t, "aws/terraform", "R-DUR-05", src, `prevent_destroy\s*=\s*true`)
	// R-DUR-02: attached at a stable device via volume_attachment.
	mustMatch(t, "aws/terraform", "R-DUR-02", src,
		`resource\s+"aws_volume_attachment"`)
	mustMatch(t, "aws/terraform", "R-DUR-02", src, `device_name\s*=`)
	// R-DUR-03: format-if-empty mount idiom in user_data.
	mustMatch(t, "aws/terraform", "R-DUR-03", src,
		`blkid|mkfs[^-]*-t|lsblk`)
	// /data is still the collector's data path.
	mustContain(t, "aws/terraform", src, map[string]string{
		"R-DUR-01": "/data",
	})
}

// --- TC-DUR-02: AWS CloudFormation -------------------------------------------

func TestDataDurability_AWSCloudFormation(t *testing.T) {
	src := readDeploy(t, "aws", "cloudformation", "signals-rds-iam.yaml")

	// R-DUR-01: a separate EBS Volume resource.
	mustMatch(t, "aws/cloudformation", "R-DUR-01", src,
		`Type:\s*AWS::EC2::Volume\b`)
	// R-DUR-06: encrypted.
	mustMatch(t, "aws/cloudformation", "R-DUR-06", src, `Encrypted:\s*true`)
	// R-DUR-05: retain deletion AND replace policy.
	mustMatch(t, "aws/cloudformation", "R-DUR-05", src,
		`DeletionPolicy:\s*Retain`)
	mustMatch(t, "aws/cloudformation", "R-DUR-05", src,
		`UpdateReplacePolicy:\s*Retain`)
	// R-DUR-02: attachment with a fixed Device.
	mustMatch(t, "aws/cloudformation", "R-DUR-02", src,
		`Type:\s*AWS::EC2::VolumeAttachment\b`)
	mustMatch(t, "aws/cloudformation", "R-DUR-02", src, `Device:`)
	// R-DUR-03: format-if-empty mount idiom in UserData.
	mustMatch(t, "aws/cloudformation", "R-DUR-03", src, `blkid|mkfs|lsblk`)
}

// --- TC-DUR-03: AWS Image Builder --------------------------------------------

func TestDataDurability_AWSImageBuilder(t *testing.T) {
	src := readDeploy(t, "aws", "imagebuilder", "signals-collector-component.yaml")

	// R-DUR-07: a systemd mount for /data on the durable device, before the
	// collector unit. The component must stop relying on a root-disk named
	// volume for durable state — it mounts the attached device at /data.
	mustMatch(t, "aws/imagebuilder", "R-DUR-07", src, `data\.mount|/data`)
	// R-DUR-03: format-if-empty idiom present somewhere in the component.
	mustMatch(t, "aws/imagebuilder", "R-DUR-03", src, `blkid|mkfs|lsblk`)
	// R-DUR-07: the unit ordering ensures the mount is up before the
	// collector (RequiresMountsFor or an explicit data mount dependency).
	mustMatch(t, "aws/imagebuilder", "R-DUR-07", src,
		`RequiresMountsFor=/data|data\.mount`)
}

// --- TC-DUR-04: Azure Terraform ----------------------------------------------

func TestDataDurability_AzureTerraform(t *testing.T) {
	src := readDeploy(t, "azure", "terraform", "main.tf")

	// R-DUR-01: a managed data disk resource.
	mustMatch(t, "azure/terraform", "R-DUR-01", src,
		`resource\s+"azurerm_managed_disk"`)
	// R-DUR-05: retained on replace.
	mustMatch(t, "azure/terraform", "R-DUR-05", src, `prevent_destroy\s*=\s*true`)
	// R-DUR-02: attached at a fixed lun via the data-disk attachment.
	mustMatch(t, "azure/terraform", "R-DUR-02", src,
		`resource\s+"azurerm_virtual_machine_data_disk_attachment"`)
	mustMatch(t, "azure/terraform", "R-DUR-02", src, `lun\s*=`)
	// R-DUR-03: format-if-empty mount idiom in cloud-init.
	mustMatch(t, "azure/terraform", "R-DUR-03", src, `blkid|mkfs|lsblk`)
}

// --- TC-DUR-05: Azure Bicep --------------------------------------------------

func TestDataDurability_AzureBicep(t *testing.T) {
	src := readDeploy(t, "azure", "bicep", "main.bicep")

	// R-DUR-01: a managed data disk resource.
	mustMatch(t, "azure/bicep", "R-DUR-01", src,
		`resource\s+\w+\s+'Microsoft\.Compute/disks@`)
	// R-DUR-02: attached at a fixed lun, preserving data (createOption: Attach).
	mustMatch(t, "azure/bicep", "R-DUR-02", src, `lun:`)
	mustMatch(t, "azure/bicep", "R-DUR-02", src, `createOption:\s*'Attach'`)
	mustMatch(t, "azure/bicep", "R-DUR-01", src, `dataDisks:`)
	// R-DUR-03: format-if-empty mount idiom in cloud-init.
	mustMatch(t, "azure/bicep", "R-DUR-03", src, `blkid|mkfs|lsblk`)
}

// --- TC-DUR-06: GCP Terraform ------------------------------------------------

func TestDataDurability_GCPTerraform(t *testing.T) {
	src := readDeploy(t, "gcp", "terraform", "main.tf")

	// R-DUR-01: a persistent disk resource.
	mustMatch(t, "gcp/terraform", "R-DUR-01", src,
		`resource\s+"google_compute_disk"`)
	// R-DUR-05: retained on replace.
	mustMatch(t, "gcp/terraform", "R-DUR-05", src, `prevent_destroy\s*=\s*true`)
	// R-DUR-02: attached_disk block with a stable device_name.
	mustMatch(t, "gcp/terraform", "R-DUR-02", src, `attached_disk\s*\{`)
	mustMatch(t, "gcp/terraform", "R-DUR-02", src, `device_name\s*=`)
	// R-DUR-03: format-if-empty mount idiom in startup-script.
	mustMatch(t, "gcp/terraform", "R-DUR-03", src, `blkid|mkfs|lsblk`)
}

// --- TC-DUR-07a: no supported production path stores signals.db on root only --

// TestDataDurability_NoRootDiskOnlyStore asserts the belt-and-braces invariant
// (INV-DUR-01): every VM/AMI artifact that configures database.path at
// /data/signals.db also declares a durable data resource. A file that keeps
// `-v signals-data:/data` with NO durable volume/disk/mount declared is a
// root-disk-only store — exactly the #501 foot-gun — and fails here.
func TestDataDurability_NoRootDiskOnlyStore(t *testing.T) {
	type artifact struct {
		parts        []string
		durableRegex string // at least one must match to prove a durable resource
	}
	artifacts := []artifact{
		{[]string{"aws", "terraform", "main.tf"}, `aws_ebs_volume`},
		{[]string{"aws", "cloudformation", "signals-rds-iam.yaml"}, `AWS::EC2::Volume\b`},
		{[]string{"aws", "imagebuilder", "signals-collector-component.yaml"}, `data\.mount|RequiresMountsFor=/data`},
		{[]string{"azure", "terraform", "main.tf"}, `azurerm_managed_disk`},
		{[]string{"azure", "bicep", "main.bicep"}, `Microsoft\.Compute/disks@`},
		{[]string{"gcp", "terraform", "main.tf"}, `google_compute_disk`},
	}
	for _, a := range artifacts {
		name := filepath.Join(a.parts...)
		src := readDeploy(t, a.parts...)
		// Only artifacts that actually persist signals.db at /data are in scope.
		if !strings.Contains(src, "/data/signals.db") && !strings.Contains(src, "/data") {
			continue
		}
		if !regexp.MustCompile(a.durableRegex).MatchString(src) {
			t.Errorf("%s: persists /data but declares no durable data resource (/%s/) — root-disk-only store, INV-DUR-01 / R-DUR-01",
				name, a.durableRegex)
		}
	}
}

// --- TC-DUR-07b: Helm eval mode is fenced ------------------------------------

// TestDataDurability_HelmEvalFenced asserts the Helm production default is
// durable (persistence.enabled: true) and the emptyDir fallback is explicitly
// labelled evaluation-only / non-durable in values.yaml and NOTES.txt (R-DUR-09
// / B6). The chart already defaults to a durable PVC; #501 fences the eval path.
func TestDataDurability_HelmEvalFenced(t *testing.T) {
	values := readDeploy(t, "helm", "signals", "values.yaml")
	notes := readDeploy(t, "helm", "signals", "templates", "NOTES.txt")

	// Production default is durable.
	mustMatch(t, "helm values.yaml", "R-DUR-09", values,
		`(?s)persistence:\s*\n\s*enabled:\s*true`)

	// The eval/emptyDir path is explicitly fenced in values.yaml: the comment
	// must name both the eval-only nature and that state is lost on restart.
	lc := strings.ToLower(values)
	if !strings.Contains(lc, "evaluation") && !strings.Contains(lc, "eval-only") && !strings.Contains(lc, "eval only") {
		t.Errorf("helm values.yaml: persistence has no 'evaluation'-only fence for the emptyDir fallback (R-DUR-09)")
	}
	if !strings.Contains(lc, "lost") && !strings.Contains(lc, "not durable") && !strings.Contains(lc, "non-durable") && !strings.Contains(lc, "ephemeral") {
		t.Errorf("helm values.yaml: persistence does not warn state is lost / non-durable when disabled (R-DUR-09)")
	}

	// NOTES.txt renders a loud warning when persistence is disabled.
	lnotes := strings.ToLower(notes)
	if !strings.Contains(lnotes, "persistence") {
		t.Errorf("helm NOTES.txt: no persistence/eval-mode warning for the emptyDir fallback (R-DUR-09)")
	}
}

// --- TC-DUR-09: mount logic sets /data ownership to uid/gid 10001 ------------

// TestDataDurability_MountSetsOwnership asserts the format-if-empty mount logic
// chowns the freshly-formatted /data to the non-root container user baked into
// the image (uid 10001, group 10001), so signals.db is writable (R-DUR-04).
// Accepts the `chown 10001:10001` or `chown 10001.10001` form.
func TestDataDurability_MountSetsOwnership(t *testing.T) {
	artifacts := [][]string{
		{"aws", "terraform", "main.tf"},
		{"aws", "cloudformation", "signals-rds-iam.yaml"},
		{"aws", "imagebuilder", "signals-collector-component.yaml"},
		{"azure", "terraform", "main.tf"},
		{"azure", "bicep", "main.bicep"},
		{"gcp", "terraform", "main.tf"},
	}
	chown := regexp.MustCompile(`chown\s+(-R\s+)?10001[:.]10001`)
	for _, a := range artifacts {
		name := filepath.Join(a...)
		src := readDeploy(t, a...)
		if !chown.MatchString(src) {
			t.Errorf("%s: mount logic does not chown /data to 10001:10001 — the non-root container user cannot write signals.db (R-DUR-04)", name)
		}
	}
}

// --- TC-DUR-10: no credential value is written onto the /data volume ---------

// TestDataDurability_NoCredentialOnDataVolume asserts the IaC-artifact-level
// credential invariant (R-DUR-08 / INV-DUR-04): the durability change does not
// move any secret onto /data. No artifact may write a credential literal into a
// file under /data. The API token stays in /etc/signals/signals.env (or /root),
// which this guard confirms are NOT under /data.
func TestDataDurability_NoCredentialOnDataVolume(t *testing.T) {
	artifacts := [][]string{
		{"aws", "terraform", "main.tf"},
		{"aws", "cloudformation", "signals-rds-iam.yaml"},
		{"aws", "imagebuilder", "signals-collector-component.yaml"},
		{"azure", "terraform", "main.tf"},
		{"azure", "bicep", "main.bicep"},
		{"gcp", "terraform", "main.tf"},
	}
	// A write of a credential-named value into a file under /data: a redirect
	// or tee of SIGNALS_API_TOKEN / password / secret into /data/<something>.
	leak := regexp.MustCompile(`(?i)(SIGNALS_API_TOKEN|password|secret|token)[^\n]*(>|tee)\s*/data/`)
	// Or a token/secret env-file / printf targeting /data.
	leakFile := regexp.MustCompile(`(?i)(printf|echo)[^\n]*(SIGNALS_API_TOKEN|password|secret)[^\n]*>\s*/data/`)
	for _, a := range artifacts {
		name := filepath.Join(a...)
		src := readDeploy(t, a...)
		if leak.MatchString(src) || leakFile.MatchString(src) {
			t.Errorf("%s: writes a credential value onto the /data volume — credentials must stay external (R-DUR-08 / INV-DUR-04 / INV-SIGNALS-07)", name)
		}
		// Positive: the API token file, where present, must NOT be under /data.
		if regexp.MustCompile(`/data/signals\.env`).MatchString(src) {
			t.Errorf("%s: API token env-file placed under /data — must be root-only outside the data volume (R-DUR-08)", name)
		}
	}
}

// --- TC-DUR-11: provider READMEs document the fail-closed transition ---------

// TestDataDurability_TransitionRunbookDocumented asserts each provider README
// carries the existing-install transition runbook + the unrecoverable-history
// rollback note (R-DUR-10). The runbook IS the shipped behavior for the
// "cannot auto-migrate safely" failure condition, so it is a testable artifact.
func TestDataDurability_TransitionRunbookDocumented(t *testing.T) {
	readmes := [][]string{
		{"aws", "README.md"},
		{"azure", "README.md"},
		{"gcp", "README.md"},
	}
	for _, r := range readmes {
		name := filepath.Join(r...)
		src := strings.ToLower(readDeploy(t, r...))
		// (a)+(b): names the durable data volume + instance-replacement preservation.
		if !strings.Contains(src, "data durability") && !strings.Contains(src, "data volume") {
			t.Errorf("%s: no data-durability / data-volume section (R-DUR-10)", name)
		}
		// (c): documents the safe transition for a root-disk install.
		if !strings.Contains(src, "transition") && !strings.Contains(src, "migrat") {
			t.Errorf("%s: no existing-install transition/migration runbook (R-DUR-10)", name)
		}
		// (c): never initialize empty over an existing store (fail-closed intent).
		if !strings.Contains(src, "signals.db") {
			t.Errorf("%s: transition section does not reference signals.db (R-DUR-10)", name)
		}
		// (d): unrecoverable-history rollback note.
		if !strings.Contains(src, "cannot be recovered") && !strings.Contains(src, "unrecoverable") && !strings.Contains(src, "not be recovered") && !strings.Contains(src, "cannot recover") {
			t.Errorf("%s: no pre-fix-history-unrecoverable rollback note (R-DUR-10, #501 rollback)", name)
		}
	}
}
