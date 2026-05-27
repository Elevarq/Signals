# Extension-Absent Emission — Cross-Cutting Specification

Feature: Uniform Reporting of Absent or Gated Collectors
Version: 1.0
Type: behavioral
Status: ACTIVE

---

## Purpose

Several collectors depend on optional extensions
(`pg_stat_statements`, `vector`, `pg_buffercache`, etc.) or on a
minimum PostgreSQL major version (`pg_stat_io_v1` requires PG ≥ 16).
When a precondition fails, the collector is gated out before query
execution rather than left to fail at runtime. This specification
defines the canonical channel through which downstream consumers
(notably the Arq Analyzer's evidence-completeness model) learn
**why** a collector produced no data.

## Channel: collector_status.json

Every export ZIP carries `collector_status.json`. It records, for
the snapshot window covered by this export, exactly one entry per
**registered** collector — including those that did not run.

A representative entry for a gated collector:

```json
{
  "id": "pg_stat_statements_v1",
  "attempted": false,
  "status": "skipped",
  "reason": "extension_missing",
  "detail": "pg_stat_statements extension is not installed",
  "row_count": 0,
  "duration_ms": 0,
  "collected_at": ""
}
```

`status` is one of `success | partial | skipped | failed`; `reason`
is the structured cause when status is not `success`. The full
schema is defined in Appendix A
(`features/arq-signals/appendix-a-api-contract.md` —
"Collector status schema") and `specifications/collector_status.md`.

`query_results.ndjson` carries only real query rows. A gated
collector contributes zero rows there. The two files are
complementary, not redundant: `query_results.ndjson` is data;
`collector_status.json` is metadata about what ran and why.

## Requirements

- **EA-R001 (canonical channel)**: For every registered collector
  whose precondition fails — `extension_missing`,
  `version_unsupported`, or `config_disabled` — the implementation
  MUST emit exactly one entry in `collector_status.json` with
  `attempted = false`, `status = "skipped"`, and the corresponding
  `reason` value. No row is written to `query_results.ndjson` for a
  gated collector.

- **EA-R002 (status file ubiquity)**: `collector_status.json` MUST
  be present in every export ZIP regardless of whether any
  collectors are gated. Consumers can treat the file as guaranteed
  and fall through to `extension_inventory_v1` only as a
  cross-check when more detail is needed (e.g. to learn which other
  extensions are installed alongside the gated one).

- **EA-R003 (collector spec reference)**: A collector spec whose
  query depends on an extension or a minimum PG version MUST
  reference this specification in its **Failure Conditions**
  section and document the reason value(s) it can produce.

- **EA-R004 (analyzer contract)**: The Arq Analyzer's
  `EvidenceCompleteness` model MUST consume `collector_status.json`
  and surface the per-collector `reason` so detectors can
  distinguish:

  - `ExtensionUnavailable` — `reason = "extension_missing"`
  - `VersionUnsupported`   — `reason = "version_unsupported"`
  - `ConfigDisabled`       — `reason = "config_disabled"`
  - `CollectorFailed`      — `status = "failed"` (any
    runtime-failure reason: `execution_error`,
    `permission_denied`, `timeout`, `savepoint_rollback`)

  Existing `available bool` consumers continue to function: a
  gated or failed collector is `available = false` regardless of
  reason.

## Invariants

- **INV-EA-01**: For every registered collector, exactly one entry
  exists in `collector_status.json` per collection run. The status
  file is the complete enumeration of "which collectors did the
  registry know about, and what happened to each". `query_results.
  ndjson` contains only real query rows — no metadata or status
  markers.

- **INV-EA-02**: `collector_status.json` is schema-versioned via
  its own `schema_version` field, independently of the export's
  top-level `metadata.json` version. New `reason` values are
  additive within a major schema version; consumers MUST treat an
  unknown `reason` as `CollectorFailed` (conservative fallback)
  rather than panic.

## Reason taxonomy

The `reason` field on a non-success entry MUST be one of:

| Reason | Status | When |
|---|---|---|
| `version_unsupported` | `skipped` | PG major version below the collector's minimum |
| `extension_missing`   | `skipped` | Required extension not installed in the target database |
| `config_disabled`     | `skipped` | Collector explicitly disabled via configuration |
| `execution_error`     | `failed`  | Query parse/plan/execute error returned by PG |
| `permission_denied`   | `failed`  | Role lacks the privileges the query needs |
| `timeout`             | `failed`  | Cancelled by the per-query timeout |
| `savepoint_rollback`  | `failed`  | Per-query savepoint had to be rolled back |

`skipped` reasons are evaluated **before** query execution
(`internal/pgqueries/registry.go::GatedIDsByReason`). `failed`
reasons are recorded **after** execution attempts. This split is
important: skipped collectors carry `attempted = false` and zero
duration; failed collectors carry `attempted = true` and the real
duration.

## Non-functional requirements

- **NFR-EA-01**: Gating at registry-time MUST NOT add a per-query
  round-trip. Extension and version detection happens once, at
  collection setup (`detectExtensions` + version probe), and the
  results feed `GatedIDsByReason` for all registered collectors.

- **NFR-EA-02**: `collector_status.json` size is bounded by the
  number of registered collectors (≤ ~200 entries even on a
  hypothetical large registry); it does not grow with row count or
  snapshot duration.

## Acceptance rules

- For a target without `pg_stat_statements` installed, an
  integration test confirms `collector_status.json` carries a
  `pg_stat_statements_v1` entry with
  `status = "skipped" AND reason = "extension_missing"` AND zero
  rows for that collector in `query_results.ndjson`.

- A test asserts that every collector with a `RequiresExtension`
  or `MinPGVersion` declaration appears in `collector_status.json`
  on a target where the precondition fails — even if no SQL ever
  ran on its behalf.

- The Arq Analyzer's coverage report exposes the four states
  enumerated in EA-R004 (`ExtensionUnavailable`,
  `VersionUnsupported`, `ConfigDisabled`, `CollectorFailed`) plus
  `CollectorOK`, distinguishable by the `reason` field on
  `Completeness` entries.

## Implementing files

- `internal/pgqueries/registry.go` — `GatedIDsByReason` returns
  collectors split by gating reason.
- `internal/collector/collector.go` — converts gated IDs into
  `QueryRun` rows with `status = "skipped"` and the corresponding
  reason.
- `internal/collector/status.go` — defines the
  `CollectorStatusFile` and `CollectorStatus` types and the
  `schema_version` constant.
- `internal/export/export.go` — writes `collector_status.json` into
  every export ZIP unconditionally (INV-SIGNALS-11).
