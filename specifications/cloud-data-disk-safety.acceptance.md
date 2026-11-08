# Acceptance — Cloud Data Disk Mount Safety (#713)

Spec: `cloud-data-disk-safety.md`. Tests:
`tests/signals_prepare_data_disk_test.go` (behavioral — drives the shared
prepare-data-disk.sh with mock fs tools; structural — every affected template
is safe).

```yaml
- id: AC-CDD-01            # normal — a provably-blank device is formatted
  ref: [CDD-R002, B1, INV-CDD-01]
  given: a resolved device whose probe reports blank (blkid -p exit 2)
  when: prepare-data-disk.sh runs
  then: the device is formatted and mounted
  test: TestPrepareDisk_FormatsBlankDevice

- id: AC-CDD-02            # boundary — an existing filesystem is preserved
  ref: [CDD-R003, B3, INV-CDD-01]
  given: a device whose probe reports an existing filesystem (exit 0)
  when: prepare-data-disk.sh runs
  then: it is NOT reformatted; it is mounted
  test: TestPrepareDisk_PreservesExistingFilesystem

- id: AC-CDD-03            # failure — a probe error fails closed, never formats
  ref: [CDD-R002, B1, INV-CDD-02]
  given: a device whose probe errors (exit not 0/2)
  when: prepare-data-disk.sh runs
  then: it fails closed and does not format
  test: TestPrepareDisk_ProbeErrorFailsClosed

- id: AC-CDD-04            # failure — a missing device fails closed (no scan-pick)
  ref: [CDD-R001, B2, INV-CDD-02]
  given: an intended device that never attaches
  when: prepare-data-disk.sh runs with a short deadline
  then: it fails closed; no device is formatted
  test: TestPrepareDisk_MissingDeviceFailsClosed

- id: AC-CDD-05            # failure — an ambiguous identity fails closed
  ref: [CDD-R001, B2, INV-CDD-02]
  given: an identity matching more than one device
  when: prepare-data-disk.sh runs
  then: it fails closed with an ambiguity diagnostic
  test: TestPrepareDisk_AmbiguousIdentityFailsClosed

- id: AC-CDD-06            # structural — no affected template is unsafe
  ref: [CDD-R004]
  given: the AWS/Azure/GCP terraform, bicep, CFN, and imagebuilder templates
  when: each is inspected
  then: none uses `if ! blkid ... mkfs`; each embeds the shared script or a
        blkid -p probe gate
  test: TestCloudTemplates_NoUnsafeDiskPattern
```
