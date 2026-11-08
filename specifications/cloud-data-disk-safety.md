# Cloud Data Disk Mount Safety

## Status

ACTIVE

## Issue

Elevarq/Signals#713 (audit F07 from the code review).

## Purpose

The VM bootstrap scripts (AWS / Azure / GCP) mounted the durable `/data`
volume unsafely: when the intended device was not yet attached they fell back
to SCANNING block devices and picking an arbitrary non-root disk, and they
treated ANY `blkid` failure — including a probe/I-O error — as "no filesystem,
format it". That could format an unrelated disk (e.g. an Azure resource/scratch
disk), use nondurable storage, or destroy data on a transient probe error.
Additionally the AWS data volume's availability zone was derived from the
replaceable instance, making the retained volume a replacement dependency.

This specification requires the bootstraps to resolve the intended volume by
provider identity, wait bounded for it, and format only when it is provably
blank — failing closed on absence, ambiguity, or probe errors.

## Scope

In scope:

- `deploy/common/scripts/prepare-data-disk.sh` — shared, tested safe
  resolve/wait/verify/format/mount logic, embedded by the Terraform bootstraps
  (AWS / Azure / GCP).
- Inline safe logic (same probe gate + bounded wait + fail-closed) in the
  generic templates that cannot pin a volume id: `deploy/azure/bicep/main.bicep`,
  `deploy/aws/cloudformation/signals-rds-iam.yaml`,
  `deploy/aws/imagebuilder/signals-collector-component.yaml`.
- AWS: source the data volume's AZ from the subnet, not the instance, to break
  the replacement dependency (and the dependency cycle introduced by resolving
  the volume by id in user_data).

Out of scope:

- Live cloud reproduction of delayed-attachment / probe-failure (deferred to the
  cloud smoke automation, #212).

## Interfaces

- `prepare-data-disk.sh` — inputs via positional args or env
  (`DATA_DISK_IDENTITY`, `DATA_MOUNT`, `DATA_OWNER`, `DATA_WAIT_SECONDS`).
  Resolves by identity, waits bounded, formats only on a confirmed-blank probe,
  mounts, verifies the mount source, chowns.
- Provider identity paths: AWS `nvme-Amazon_Elastic_Block_Store_<volid>`,
  Azure `/dev/disk/azure/scsi1/lun<lun>`, GCP
  `/dev/disk/by-id/google-<device_name>`.

## Behaviors

### B1 — Format only a provably-blank device

Given a resolved device, When a low-level probe (`blkid -p`) reports it is blank
(exit 2), Then it is formatted; When the probe reports an existing
filesystem/signature (exit 0), Then it is preserved; When the probe errors (any
other exit), Then the bootstrap FAILS CLOSED and never formats.

### B2 — Resolve by identity; fail closed on absence/ambiguity

Given the intended device identity, When it does not attach within the deadline,
or when it matches more than one device, Then the bootstrap fails closed — it
never scans and picks an arbitrary disk.

### B3 — Existing data preserved on re-attach

Given a re-attached populated volume, When the bootstrap runs, Then the existing
filesystem is detected (probe exit 0) and preserved; `/data` is mounted
read-write without reformatting.

### B4 — AWS volume AZ independent of the instance

Given the AWS deployment, When the data volume is created, Then its availability
zone is sourced from the subnet, not from the replaceable instance — so the
retained volume is not a replacement dependency and there is no dependency
cycle.

## Rules

- **CDD-R001**: The bootstrap MUST resolve the data device by its intended
  provider identity, wait a bounded time for attachment, and FAIL CLOSED on
  absence or ambiguity — never scan-and-pick an arbitrary disk.
- **CDD-R002**: The format decision MUST use a low-level probe: format only on a
  confirmed-blank result; on a probe ERROR the bootstrap MUST fail closed and
  never format.
- **CDD-R003**: An existing filesystem MUST be preserved (never reformatted) on
  re-attach.
- **CDD-R004**: No affected template may retain the unsafe patterns (format on a
  plain `blkid` failure; scan-and-pick the last matching device). Every template
  embeds the shared script or uses the equivalent inline probe gate.
- **CDD-R005**: The AWS data volume's availability zone MUST be sourced
  independently of the replaceable instance (from the subnet).

## Invariants

- **INV-CDD-01**: An existing data filesystem is never destroyed by the
  bootstrap — formatting happens only on a confirmed-blank device.
- **INV-CDD-02**: Absence, ambiguity, and probe errors fail closed rather than
  acting on an unverified device.

## Failure conditions

| Trigger | System response |
|---------|-----------------|
| Intended device not attached within deadline | fail closed (CDD-R001). |
| Identity matches more than one device | fail closed, "ambiguous" diagnostic (CDD-R001). |
| `blkid` probe error (exit not 0/2) | fail closed, never format (CDD-R002). |
| Existing filesystem (probe exit 0) | preserved, mounted, not reformatted (CDD-R003). |

## Constraints

- The shared script is dependency-free beyond bash, blkid, mkfs, mount,
  findmnt, chown. The behavioral test drives it with mock fs tools against
  regular-file fixtures (no real block devices, no cloud). Terraform validates.

## NFR overrides

None. Strengthens durability/data-safety on bootstrap; weakens nothing.

## Rollback / recovery

The shared script + embeds and the inline gates are additive safety. Reverting
restores the prior (unsafe) scan-and-format behaviour. The AWS AZ change is a
plan-level dependency fix with no data impact.
