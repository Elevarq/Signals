# Acceptance Tests: Cloud Data Durability

## Feature

`specifications/cloud-data-durability.md` (Elevarq/Signals#501)

## Test harness

IaC cannot be provisioned in CI, so the structural cases (TC-DUR-01..07) are
deterministic template-level assertions in `tests/data_durability_test.go`
(package `tests`, normal `go test` job). The store-continuity case
(TC-DUR-08) models instance/AMI replacement at the store layer in
`tests/signals_data_durability_continuity_test.go`.

## Test Cases

### TC-DUR-01: AWS Terraform backs `/data` with a retained, encrypted EBS volume

**Rule:** R-DUR-01 / R-DUR-02 / R-DUR-05 / R-DUR-06 — Normal

**Scenario:** The AWS Terraform module declares a separate EBS volume for
`/data`, independently lifecycled from the instance.

**Given:**
- `deploy/aws/terraform/main.tf`.

**When:**
- The structural guard parses it.

**Then:**
- A separate `aws_ebs_volume` resource exists with `encrypted = true` and a
  `lifecycle { prevent_destroy = true }` block.
- An `aws_volume_attachment` with a fixed `device_name` attaches it to the
  collector instance.
- Boot-time user_data mounts that device at `/data` format-if-empty, and the
  collector mount for `/data` resolves to that mount.

**Test:** `TestDataDurability_AWSTerraform`

---

### TC-DUR-02: AWS CloudFormation backs `/data` with a retained, encrypted EBS volume

**Rule:** R-DUR-01 / R-DUR-02 / R-DUR-05 / R-DUR-06 — Normal

**Given:**
- `deploy/aws/cloudformation/signals-rds-iam.yaml`.

**Then:**
- An `AWS::EC2::Volume` resource exists with `Encrypted: true`,
  `DeletionPolicy: Retain`, and `UpdateReplacePolicy: Retain`.
- An `AWS::EC2::VolumeAttachment` with a fixed `Device` attaches it to the
  instance.
- UserData mounts that device at `/data` format-if-empty.

**Test:** `TestDataDurability_AWSCloudFormation`

---

### TC-DUR-03: AWS Image Builder mounts the durable `/data` volume (not a root-disk named volume)

**Rule:** R-DUR-03 / R-DUR-04 / R-DUR-07 — Normal

**Given:**
- `deploy/aws/imagebuilder/signals-collector-component.yaml`.

**Then:**
- The component installs a systemd mount/unit that mounts a stable data
  device at `/data` format-if-empty before the collector unit starts.
- The collector's `/data` mount resolves to that path.
- The `docker run` no longer relies on a root-disk Docker named volume for
  durable state (if `-v signals-data:/data` remains, `signals-data` resolves
  to the mounted durable path).

**Test:** `TestDataDurability_AWSImageBuilder`

---

### TC-DUR-04: Azure Terraform backs `/data` with a managed data disk

**Rule:** R-DUR-01 / R-DUR-02 / R-DUR-05 / R-DUR-06 — Normal

**Given:**
- `deploy/azure/terraform/main.tf`.

**Then:**
- An `azurerm_managed_disk` resource exists with a
  `lifecycle { prevent_destroy = true }` block (managed disks are encrypted
  at rest by platform default).
- An `azurerm_virtual_machine_data_disk_attachment` with a fixed `lun`
  attaches it to the collector VM.
- cloud-init mounts the data disk at `/data` format-if-empty.

**Test:** `TestDataDurability_AzureTerraform`

---

### TC-DUR-05: Azure Bicep backs `/data` with a managed data disk

**Rule:** R-DUR-01 / R-DUR-02 / R-DUR-05 — Normal

**Given:**
- `deploy/azure/bicep/main.bicep`.

**Then:**
- A `Microsoft.Compute/disks` data-disk resource exists.
- The VM's `storageProfile.dataDisks` attaches it at a fixed `lun` with
  `createOption: Attach` (preserve, not from-image).
- cloud-init mounts the data disk at `/data` format-if-empty.

**Test:** `TestDataDurability_AzureBicep`

---

### TC-DUR-06: GCP Terraform backs `/data` with a persistent disk

**Rule:** R-DUR-01 / R-DUR-02 / R-DUR-05 / R-DUR-06 — Normal

**Given:**
- `deploy/gcp/terraform/main.tf`.

**Then:**
- A `google_compute_disk` resource exists with a
  `lifecycle { prevent_destroy = true }` block (PDs are encrypted at rest by
  default).
- An `attached_disk` block with a stable `device_name` attaches it to the
  collector instance.
- The startup-script mounts it at `/data` format-if-empty.

**Test:** `TestDataDurability_GCPTerraform`

---

### TC-DUR-07: No supported production path puts `signals.db` solely on the root/boot disk; Helm eval mode is fenced

**Rule:** INV-DUR-01 / R-DUR-09 — Invariant / Boundary

**Scenario:** Belt-and-braces invariant over all six artifacts plus the Helm
chart.

**Given:**
- All six IaC artifacts and `deploy/helm/signals`.

**Then:**
- Each VM/AMI artifact that writes `database.path: /data/signals.db` also
  declares a durable data resource (asserted by TC-DUR-01..06).
- The Helm chart defaults `persistence.enabled: true`.
- `values.yaml`, `NOTES.txt`, and the templates label the
  `persistence.enabled: false` emptyDir path as evaluation-only / non-durable.

**Test:** `TestDataDurability_NoRootDiskOnlyStore`, `TestDataDurability_HelmEvalFenced`

---

### TC-DUR-08: Store identity/history/watermark survive a simulated instance replacement (pre-existing-behavior regression guard)

**Rule:** INV-DUR-02 / INV-DUR-03 / B2 / B3 — regression guard, NOT a red-gate case

**Classification:** This is a **pre-existing-behavior regression guard**, not a
red-before-green pin. #501's non-goals explicitly exclude changing *what*
Signals stores; the store layer (`internal/db`) already preserves identity,
history, and watermarks across a re-open of the same file, so this test is
GREEN before the #501 IaC implementation. It is kept because it is the
store-layer contract the whole durability invariant (INV-DUR-02/03) depends on:
the IaC change is only worth doing *because* re-attaching the same `/data`
makes the store resume. If a future change to the store silently reset identity
on re-open or dropped the watermark, the IaC durability work would be defeated
and this guard would catch it. The red-before-green pins for #501 are the eight
structural guards (TC-DUR-01..07, TC-DUR-09..11), which are RED until the
durable resources exist.

**Scenario:** A collector runs, persisting identity + history + a per-collector
watermark. The compute instance is "replaced": a fresh `db.DB` is opened over
the *same* store file (the durable `/data` carried to the new instance). The
replacement must see the same identity, the same history counts, and the same
watermark — and the next run continues incrementally.

**Given:**
- A SQLite store seeded with `EnsureInstanceID`, snapshots/query_runs for a
  target, and a last-run time (watermark) for a collector.

**When:**
- The store is closed (instance destroyed) and re-opened from the same file
  (durable volume re-attached to the replacement instance).

**Then:**
- `EnsureInstanceID` returns the **same** id (not a new one).
- `CountSnapshots` and the per-target `query_runs` count are unchanged.
- `GetLastRunTimes` returns the prior watermark (the next cycle is incremental,
  not a from-zero re-collect).
- No credential value is present in the store (`meta` has no password/token).

**Test:** `TestDataDurability_StoreSurvivesInstanceReplacement`

---

### TC-DUR-09: Boot-time mount sets `/data` ownership to the non-root container user

**Rule:** R-DUR-04 — Normal (red-gate)

**Scenario:** For `/data` to be writable by the shipped non-root container user
(uid 10001, group 10001), the boot-time mount logic that prepares a freshly
formatted durable device must set ownership to 10001:10001; on re-attach it
preserves the existing ownership (it chowns only the newly-formatted path).

**Given:**
- Each VM/AMI artifact whose boot-time logic formats and mounts the durable
  device: AWS Terraform, AWS CloudFormation, AWS Image Builder, Azure
  Terraform, Azure Bicep, GCP Terraform.

**Then:**
- The mount/format logic in each sets ownership of `/data` to `10001:10001`
  (a `chown 10001:10001 /data` — or `10001.10001` — in the format branch).

**Test:** `TestDataDurability_MountSetsOwnership`

---

### TC-DUR-10: No changed IaC artifact writes a credential value onto the `/data` volume

**Rule:** R-DUR-08 / INV-DUR-04 — Invalid / Invariant (red-gate, IaC-level)

**Scenario:** The durability change must not, as a side effect, move any
credential onto `/data`. This is the IaC-artifact-level counterpart to the
store-layer INV-DUR-04 check in TC-DUR-08.

**Given:**
- All six VM/AMI artifacts.

**Then:**
- No artifact writes a secret/password/token value into a file under `/data`
  (no `> /data/...` or `tee /data/...` of a credential; no `-e SIGNALS_API_TOKEN=`
  / password literal routed into the data volume). The API token continues to
  live in the root-only `/etc/signals/signals.env` (or `/root`), never on
  `/data`.

**Test:** `TestDataDurability_NoCredentialOnDataVolume`

---

### TC-DUR-11: Each provider README documents the fail-closed existing-install transition

**Rule:** R-DUR-10 — Failure / Normal (red-gate, docs-as-behavior)

**Scenario:** An existing install whose `signals.db` sits on the root disk must
transition safely — never silently initialize an empty store over an existing
one. Where a mode cannot migrate automatically and safely, it fails closed and
surfaces an operator runbook. The runbook is the shipped behavior for that
failure condition.

**Given:**
- The per-provider deploy READMEs (`deploy/aws/README.md`,
  `deploy/azure/README.md`, `deploy/gcp/README.md`).

**Then:**
- Each README carries a data-durability / existing-install transition section
  that: (a) names the durable data volume, (b) states that destroying/replacing
  the instance preserves `/data`, (c) documents the safe transition for a
  pre-fix root-disk install (copy `signals.db` to the durable volume; verify
  identity/history before cutover; never initialize empty over an existing
  store), and (d) states the pre-fix-history-is-unrecoverable rollback note.

**Test:** `TestDataDurability_TransitionRunbookDocumented`
