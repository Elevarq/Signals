# Acceptance — Export Collection-Time Policy Provenance (#729)

Spec: `export-collection-provenance.md`. Tests:
`tests/signals_export_collection_provenance_test.go` (metadata aggregate +
per-snapshot ndjson) and `internal/db/collection_policy_test.go` (tri-state
persistence + no invented backfill on upgrade).

```yaml
- id: AC-CTP-01            # normal — uniform recorded policy reported exactly
  ref: [CTP-R003, B1, INV-CTP-01]
  given: every packaged snapshot collected with the same known policy
  when: the export metadata is built
  then: collection_policy.<flag> equals that value ("true"/"false")
  test: TestCTP_UniformPolicyReported

- id: AC-CTP-02            # failure — disagreeing history cannot read as clean
  ref: [CTP-R003, B2, INV-CTP-01]
  given: snapshots collected under different high-sensitivity policy
  when: the metadata is built (exporter flag false)
  then: collection_policy.high_sensitivity_collectors_enabled == "mixed"
  test: TestCTP_MixedHistoryNotFalselyClean

- id: AC-CTP-03            # boundary — unknown legacy is not invented
  ref: [CTP-R002, CTP-R003, B3, INV-CTP-02]
  given: a snapshot with unknown (NULL / pre-#729) collection-time policy
  when: the metadata is built
  then: collection_policy.<flag> == "unknown" (never a fabricated true/false)
  test: TestCTP_UnknownLegacyNotInvented

- id: AC-CTP-04            # boundary — recorded + unknown is mixed
  ref: [CTP-R003, B4, INV-CTP-01]
  given: one snapshot with a recorded value and one unknown
  when: the metadata is built
  then: collection_policy.<flag> == "mixed"
  test: TestCTP_KnownPlusUnknownIsMixed

- id: AC-CTP-05            # normal — per-snapshot policy in snapshots.ndjson
  ref: [CTP-R004, B5]
  given: a multi-cycle forensic export with differing per-cycle policy
  when: snapshots.ndjson is written
  then: each row carries its collection-time policy (true/false/null)
  test: TestCTP_SnapshotNdjsonCarriesPerCyclePolicy

- id: AC-CTP-06            # boundary — upgrade does not invent historical policy
  ref: [CTP-R002, B6, INV-CTP-02]
  given: a pre-#729 store with existing snapshots
  when: migration 009 is applied
  then: the policy columns are NULL (unknown) for those rows, no backfill
  test: TestMigration009_DoesNotInventLegacyPolicy

- id: AC-CTP-07            # normal — concrete policy persists round-trip
  ref: [CTP-R001]
  given: a snapshot inserted with a concrete collection-time policy
  when: it is read back from the store
  then: the tri-state values match exactly (not unknown)
  test: TestInsertSnapshot_PersistsCollectionPolicy
```
