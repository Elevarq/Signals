# Multi-Target Export Correctness — Specification

## Purpose

When a target-specific export is requested (TargetID > 0), all
artifacts in the ZIP must be scoped to that target only. This ensures
the Arq Analyzer receives a consistent, single-target bundle.

## Requirements

### Query run/result filtering

- MTE-R001: When Options.TargetID > 0, query_runs.ndjson MUST
  include only runs for that target.
- MTE-R002: When Options.TargetID > 0, query_results.ndjson MUST
  include only results for runs belonging to that target.
- MTE-R003: When Options.TargetID == 0 (all targets), current
  behavior is preserved — all runs and results are included.

### Target-scoped collector status

- MTE-R004: When Options.TargetID > 0, collector_status.json MUST
  contain only statuses for the exported target.
- MTE-R005: collector_status.json MUST include a target_name field
  at the file level identifying which target it covers.
- MTE-R006: Different targets may have different statuses for the
  same collector (e.g., pg_functions_v1 succeeds on PG 17, skipped
  on PG 10). The export scoped to each target reflects its own
  status independently.
- MTE-R007: When Options.TargetID == 0, collector_status.json
  behavior is unchanged (instance-level or omitted).

### Backward compatibility

- MTE-R008: The ZIP file structure does not change — same filenames,
  same JSON shapes. Only the contents are filtered by target.
- MTE-R009: Existing callers that do not set TargetID continue to
  work without modification.

## Invariants

- INV-MTE-01: A target-scoped export never contains data from a
  different target.
- INV-MTE-02: Export output is deterministic for the same target
  and time range.
