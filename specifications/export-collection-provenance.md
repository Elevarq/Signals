# Export Collection-Time Policy Provenance

## Status

ACTIVE

## Issue

Elevarq/Signals#729 (audit A3 from the code review).

## Purpose

`metadata.json` stamped the CURRENT exporter's `unsafe_mode` and
`high_sensitivity_collectors_enabled` onto exports that can contain historical
snapshots collected under a DIFFERENT policy. A later "false" flag could then
accompany retained sensitive text, falsely implying the included history was
collected without high-sensitivity content or had been scrubbed. This
specification requires the export to describe its included evidence honestly:
the collection-time policy is captured per snapshot and the export reports an
aggregate that is uniform only when every packaged snapshot agrees, and
otherwise "mixed" or "unknown" — never false certainty.

This is a provenance-honesty correction, not a claim that any opt-out ever
promised retroactive deletion.

## Scope

In scope:

- Migration `009_snapshot_collection_policy.sql`: nullable per-snapshot
  `collect_unsafe_mode` / `collect_high_sensitivity` columns (tri-state;
  NULL = unknown, no backfill).
- Capturing the collection-time policy on each snapshot at insert
  (`internal/collector`).
- Reading the tri-state through the snapshot store (`internal/db`).
- Export: per-snapshot policy in `snapshots.ndjson`, and an aggregated
  `collection_policy` object in `metadata.json`.
- Documented opt-out semantics.

Out of scope:

- Retroactively filtering or deleting historical content on opt-out (explicitly
  NOT done — opt-out suppresses future collection only).
- Changing the existing top-level `unsafe_mode` /
  `high_sensitivity_collectors_enabled` keys (retained for compatibility; their
  meaning is clarified, not altered).

## Interfaces

- Snapshot (store): `CollectUnsafeMode *bool`, `CollectHighSensitivity *bool` —
  the policy in effect when the snapshot was collected; nil = unknown.
- `snapshots.ndjson` per row: `collect_unsafe_mode`, `collect_high_sensitivity`
  (JSON `true` / `false` / `null`).
- `metadata.json` `collection_policy` object:
  - `unsafe_mode`, `high_sensitivity_collectors_enabled`: one of `"true"`,
    `"false"`, `"mixed"`, `"unknown"` — aggregated over the packaged snapshots.
  - `semantics`: a string documenting that opt-out suppresses future collection
    only and never rewrites history.
  - The pre-existing top-level `unsafe_mode` /
    `high_sensitivity_collectors_enabled` keys continue to describe the current
    export process (the builder), used by the forensic unsafe-reasons block.

## Behaviors

### B1 — Uniform recorded policy is reported exactly

Given every packaged snapshot was collected with the same known policy value,
When the export metadata is built, Then `collection_policy.<flag>` is that value
(`"true"` or `"false"`).

### B2 — Disagreeing history is "mixed"

Given packaged snapshots recorded different values for a flag, When the metadata
is built, Then `collection_policy.<flag>` is `"mixed"` — even if the exporter's
own flag is `false`.

### B3 — Unknown history is "unknown" (never invented)

Given every packaged snapshot has an unknown (NULL / pre-#729) value for a flag,
When the metadata is built, Then `collection_policy.<flag>` is `"unknown"`, and
no true/false is fabricated.

### B4 — Recorded + unknown is "mixed"

Given at least one packaged snapshot has a recorded value and at least one is
unknown, When the metadata is built, Then `collection_policy.<flag>` is
`"mixed"`, so the export never advertises certainty over partly-unknown content.

### B5 — Per-snapshot policy in snapshots.ndjson

Given a multi-cycle export, When `snapshots.ndjson` is written, Then each row
carries `collect_unsafe_mode` / `collect_high_sensitivity` as the collection-time
policy of THAT snapshot (true/false/null), so consumers attribute provenance per
cycle rather than from a single top-level flag.

### B6 — Upgrade does not invent historical policy

Given a store with pre-#729 snapshots, When migration 009 is applied, Then the
new policy columns are NULL for those rows (unknown), with no backfilled value.

## Rules

- **CTP-R001**: Each snapshot MUST record the unsafe-role and
  high-sensitivity policy in effect at its own collection time, immutably.
- **CTP-R002**: The policy columns MUST be tri-state — a pre-#729 snapshot (no
  recorded value) reads as unknown (nil / NULL); migration 009 MUST NOT invent a
  value for historical rows.
- **CTP-R003**: `metadata.json` MUST include a `collection_policy` aggregate
  derived from the packaged snapshots: `"true"`/`"false"` only when every
  packaged snapshot agrees, `"unknown"` only when all are unknown, otherwise
  `"mixed"`. It MUST NOT report a uniform value when history disagrees or is
  partly unknown.
- **CTP-R004**: `snapshots.ndjson` MUST carry each snapshot's collection-time
  policy (true/false/null) so provenance is attributable per cycle.
- **CTP-R005**: The export MUST document that opt-out suppresses FUTURE
  collection only and does not retroactively scrub or filter history; the
  existing top-level `unsafe_mode` / `high_sensitivity_collectors_enabled` keys
  describe the current export process, not the included history.

## Invariants

- **INV-CTP-01**: A `collection_policy.<flag>` of `"true"` or `"false"` means
  EVERY packaged snapshot was provably collected with that policy — a current
  exporter flag can never be presented as a property of mixed/unknown history.
- **INV-CTP-02**: Unknown collection-time policy is represented explicitly and
  is never replaced by a fabricated true/false on upgrade or export.

## Failure conditions

| Trigger | System response |
|---------|-----------------|
| Packaged snapshots disagree on a flag | `collection_policy.<flag>` = `"mixed"` (CTP-R003). |
| All packaged snapshots unknown for a flag | `collection_policy.<flag>` = `"unknown"` (CTP-R003). |
| Recorded value coexists with unknown | `collection_policy.<flag>` = `"mixed"` (CTP-R003/INV-CTP-01). |

## Constraints

- Additive, backward-compatible: nullable columns, new JSON keys; existing keys
  unchanged. No PostgreSQL write, no new network.

## NFR overrides

None. This strengthens provenance honesty (privacy/compliance posture); it
weakens no existing guarantee.

## Rollback / recovery

The columns are additive and nullable; the new JSON keys are additive. Reverting
the reader code leaves the columns inert. No runtime impact on collection.
