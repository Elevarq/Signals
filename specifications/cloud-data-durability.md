# Cloud Data Durability — `/data` Survives Compute Replacement

## Status

ACTIVE

## Issue

Elevarq/Signals#501 (part of #500, P0). Blocks the Signals upgrade-test
issue (#504).

## Purpose

Elevarq Signals keeps all of its durable state — instance identity
(`meta.instance_id`), collection history (`snapshots`, `query_runs`,
`query_results`, `events`), cadence / incremental watermarks (the
per-collector last-run times derived from `query_runs`), and circuit
state — in a single SQLite store at `/data/signals.db`. The collector
is designed to resume incrementally from that store: a replacement
collector that re-attaches the same `/data` continues the same identity
and cadence instead of re-collecting from zero.

For that design to hold in production, `/data` must outlive the compute
instance. The normal upgrade for every VM/AMI deploy mode is
**instance / AMI replacement** — Terraform's `user_data_replace_on_change`,
a CloudFormation stack update that replaces the instance, an Image Builder
re-bake, or a new VM from a new image. If `/data` lives on the instance's
own root/boot disk (or in a Docker **named volume**, which is itself a
directory on the root disk), that replacement destroys the entire store:
history, identity, and watermarks are lost and the replacement collector
initializes an empty database and re-collects from zero.

This specification defines the invariant that a supported production
deploy places `/data` on a durable resource whose lifecycle is
independent of the compute instance, and the per-provider rules that
realize it. It does **not** change what Signals stores (that is out of
scope, per #501's non-goals) — only *where* it is stored and how that
storage survives an upgrade.

## Scope

In scope:

- The six cloud IaC artifacts that run the collector on a VM/AMI:
  - `deploy/aws/terraform/main.tf`
  - `deploy/aws/cloudformation/signals-rds-iam.yaml`
  - `deploy/aws/imagebuilder/signals-collector-component.yaml`
  - `deploy/azure/terraform/main.tf`
  - `deploy/azure/bicep/main.bicep`
  - `deploy/gcp/terraform/main.tf`
- The Helm chart's persistence posture (`deploy/helm/signals`), where the
  production default is already a durable PVC and the eval fallback is
  `emptyDir`.
- The container / Compose named-volume preservation guidance (documentation
  only).

Out of scope (explicit non-goals, #501):

- Changing the store schema or what Signals collects.
- The store-schema-version work (#502), the upgrade CI harness (#504), and
  docs beyond what this change requires (#505).
- Credential storage: credentials remain external and are **never** moved
  into `signals.db` (reaffirms INV-SIGNALS-07).

## Definitions

- **Compute instance** — the EC2 instance / Azure VM / GCE instance (or the
  AMI it boots from) that runs the collector container.
- **Durable data resource** — a provider-native, independently-lifecycled
  block-storage resource: an AWS EBS volume, an Azure managed data disk, or
  a GCP persistent disk, attached to the compute instance but owned and
  destroyed on its own schedule, never as a side effect of replacing the
  instance.
- **Instance / AMI replacement** — the supported upgrade: the compute
  instance (or its backing image) is destroyed and recreated. In Terraform
  this is `user_data_replace_on_change` or any `-replace`; in CloudFormation
  a replacement update; for Image Builder a re-bake + relaunch.
- **Root / boot disk** — the compute instance's own OS disk
  (`root_block_device` / `/dev/xvda` / `os_disk` / `boot_disk`). Its
  lifecycle is bound to the instance; a Docker **named volume** lives on it.

## Behaviors

### B1 — Production deploy places `/data` on a durable resource

Given a supported production VM/AMI deploy (AWS Terraform / CloudFormation /
Image Builder, Azure Terraform / Bicep, GCP Terraform), When the module is
applied, Then `/data` is backed by a durable data resource (EBS volume /
Azure managed data disk / GCP persistent disk) that is a distinct resource
from the root/boot disk.

### B2 — State survives instance / AMI replacement

Given a collector that has run at least one cycle (so `meta.instance_id`,
`snapshots`/`query_runs` rows, and per-collector last-run watermarks exist),
When the compute instance is replaced (new instance / new AMI), Then the
durable data resource is re-attached to the replacement, `/data/signals.db`
is the same store, and the replacement collector continues with the same
`instance_id`, the prior history intact, and the prior watermarks — the
next cycle collects incrementally, not from an empty store.

### B3 — Re-attach, never re-initialize when data exists

Given a durable data resource that already contains a filesystem with
`signals.db`, When the replacement instance boots and mounts it, Then the
mount step preserves the existing filesystem (it does **not** `mkfs`) and
the collector opens the existing store. A `mkfs` is performed **only** when
the durable resource is empty (first provisioning).

### B4 — Persistent storage is retained when compute is replaced

Given a replacement of the compute instance, When the IaC deletes/recreates
the instance resource, Then the durable data resource is **not** deleted:
its deletion policy is retain (Terraform `prevent_destroy` / CloudFormation
`DeletionPolicy: Retain` + `UpdateReplacePolicy: Retain` / an independently
-lifecycled Azure disk / GCP disk). Destroying or replacing compute never
destroys the store.

### B5 — Credentials remain external

Given any deploy mode, When state is persisted to `/data/signals.db`, Then
no credential (DB password, API token, cloud secret) is written into
`signals.db`. Credentials stay in their external, by-reference sources
(IAM / managed identity / service account tokens minted at connect time;
the API token in a root-only env-file outside `/data`). Reaffirms
INV-SIGNALS-07.

### B6 — Helm production default is durable; eval mode is fenced

Given the Helm chart, When `persistence.enabled` is its default (`true`),
Then `/data` is a PVC (durable across pod replacement). When
`persistence.enabled: false`, Then `/data` is an `emptyDir` whose
non-durable, evaluation-only semantics are explicit in `values.yaml`,
`NOTES.txt`, and the template — it must never read as production
upgrade-safe.

## Rules

- **R-DUR-01**: Each of the six VM/AMI IaC artifacts MUST declare a durable
  data resource (EBS volume / Azure managed data disk / GCP persistent
  disk), distinct from the root/boot disk, that backs `/data`. For the
  container-running files, the collector's `-v signals-data:/data` mount
  MUST resolve to a path on that attached durable resource, not to a Docker
  named volume on the root disk.
- **R-DUR-02**: The durable data resource MUST be attached at a stable
  device identifier (Terraform `device_name` / CloudFormation `Device` /
  Azure `lun` / GCP `device_name` or attach block) so the boot-time mount
  logic can find it deterministically across replacements.
- **R-DUR-03**: The boot-time mount logic MUST be format-if-empty /
  preserve-if-populated: it creates a filesystem **only** when the device
  has none, and otherwise mounts the existing filesystem read-write at
  `/data`. It MUST NOT unconditionally `mkfs`.
- **R-DUR-04**: `/data` MUST be owned such that the non-root container user
  (uid 10001, group 10001 — the `signals` user baked into the image) can
  read and write `signals.db`. On first format the mount logic sets that
  ownership; on re-attach it preserves the existing ownership.
- **R-DUR-05**: The durable data resource MUST carry a retain-on-replace /
  retain-on-delete policy so that replacing or deleting the compute instance
  does not delete it: Terraform `lifecycle { prevent_destroy = true }` on
  the volume/disk resource (and the volume is a separate resource from the
  instance, so `user_data_replace_on_change` replaces only compute);
  CloudFormation `DeletionPolicy: Retain` **and** `UpdateReplacePolicy:
  Retain` on the `AWS::EC2::Volume`.
- **R-DUR-06**: The durable data resource MUST be encrypted at rest
  (EBS `encrypted = true` / Azure managed disk default platform encryption /
  GCP persistent disk default encryption).
- **R-DUR-07**: The AWS Image Builder component MUST mount the durable data
  volume at `/data` (via the same stable-device, format-if-empty systemd
  mount the Terraform/CloudFormation paths use) rather than relying on a
  Docker named volume on the root disk, and MUST document that the baked AMI
  re-attaches the existing data volume on instance replacement. The
  collector `docker run` MAY keep `-v signals-data:/data` only if
  `signals-data` resolves to the mounted durable path.
- **R-DUR-08**: Every IaC artifact that changed MUST keep credentials
  external — no rule here introduces any credential value into `signals.db`
  or onto the `/data` volume (reaffirms INV-SIGNALS-07 / the existing
  INV-AMI-04 token handling).
- **R-DUR-09**: The Helm chart MUST default `persistence.enabled: true`
  (durable PVC) and MUST label the `persistence.enabled: false` `emptyDir`
  fallback as evaluation-only / non-durable in `values.yaml`, `NOTES.txt`,
  and the `pvc.yaml`/`deployment.yaml` templates.
- **R-DUR-10**: For an existing install whose `signals.db` currently sits on
  the root/boot disk, the transition to a durable data resource MUST be safe:
  it MUST NOT silently initialize an empty store while an old store exists.
  Where a mode cannot migrate automatically and safely, it MUST fail closed
  and surface an explicit operator runbook (see "Existing-install
  transition"). Reaffirmed by B3.

## Invariants

- **INV-DUR-01**: In a supported production deploy, `/data` lives on a
  durable resource whose lifecycle is independent of the compute instance.
  No supported production path puts `signals.db` solely on the root/boot
  disk or in a Docker named volume on the root disk.
- **INV-DUR-02**: Instance identity (`meta.instance_id`), collection history
  (`snapshots` / `query_runs` / `query_results` / `events`), and cadence /
  incremental watermarks survive instance / AMI replacement. A replacement
  collector re-attaches the existing store and continues from prior state.
- **INV-DUR-03**: Boot-time mount is format-if-empty / preserve-if-populated.
  A populated durable resource is never reformatted; an empty one is
  formatted exactly once.
- **INV-DUR-04**: Credentials never move into `signals.db` or onto the
  `/data` volume as a consequence of this durability work (reaffirms
  INV-SIGNALS-07).
- **INV-DUR-05**: The durable data resource is retained when compute is
  replaced or deleted; persistent storage is never destroyed merely because
  the compute instance is destroyed (retain deletion policy).

## Failure conditions

| Trigger | System response |
|---------|-----------------|
| A supported VM/AMI artifact backs `/data` only with the root/boot disk or a root-disk Docker named volume | Structural guard `TestDataDurability_*` fails CI (regression on R-DUR-01). |
| A durable data resource lacks a retain policy | Structural guard fails (regression on R-DUR-05). |
| A durable data resource is unencrypted | Structural guard fails (regression on R-DUR-06). |
| Boot-time mount would `mkfs` an already-populated device | Not shipped — the mount logic is format-if-empty (R-DUR-03); the guard asserts the format-if-empty idiom is present. |
| An existing root-disk install cannot migrate safely in a given mode | Fail closed; emit the operator runbook; never initialize empty over an existing store (R-DUR-10). |

## Constraints

- IaC cannot be fully provisioned in CI (no cloud credentials, no instance
  replacement). Verification is therefore **structural / template-level**:
  deterministic tests assert each artifact declares the durable resource,
  the retain policy, encryption, the stable attachment, and the `/data`
  mount, and that no supported production path leaves `signals.db` solely on
  the root/boot disk. A store-level continuity test (populate → carry the
  store to a fresh collector → identity/history/watermark intact → next
  cycle continues) models the *replacement* at the store layer.
- `terraform validate` / `helm template` remain green (the existing
  `terraform-validate.yml` and `kube-lint` gates).

## NFR overrides

None. Standard Elevarq security/compliance baseline applies (encryption at
rest is mandated by R-DUR-06; credentials stay external by INV-DUR-04).

## Backup / snapshot note

The durable data resource supports provider-native point-in-time backup
independent of compute: EBS snapshots, Azure managed-disk snapshots, GCP
persistent-disk snapshots. Operators SHOULD schedule snapshots of the data
resource as the backup of record for collection history; this spec does not
mandate a schedule but documents that the resource is snapshot-capable and
that snapshots, not the root disk, are the recovery source.

## Rollback / recovery

Per #501: if a pre-fix install had `/data` on an ephemeral disk, the first
application of this fix seeds a fresh store on the new durable resource —
pre-fix history / identity cannot be recovered (it was already lost on the
prior replacement). This is documented in the per-provider README so
operators do not expect recovery of data that an ephemeral disk already
discarded.
