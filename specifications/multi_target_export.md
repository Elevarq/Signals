# Multi-Target Export Correctness — Specification

## Status

ACTIVE

## Purpose

When a target-specific export is requested (TargetID > 0), all
artifacts in the ZIP must be scoped to that target only. This ensures
the Elevarq Analyzer receives a consistent, single-target bundle.

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
- MTE-R010 (#720 / F09 — canonical-instant time-range comparison):
  The `since` / `until` export selectors MUST select by the **instant**
  they denote, not by their textual form. `collected_at` is stored as
  canonical UTC second-resolution RFC3339 (`...Z`); the store MUST
  normalize each bound to that same canonical form before comparing, so
  two RFC3339 strings that denote the **same instant** (e.g.
  `2026-11-07T11:00:00+01:00` and `2026-11-07T10:00:00Z`) select
  identical evidence. Bounds are **inclusive** with correct whole-second
  precision against second-resolution storage: a `since` lower bound
  rounds **up** to the next whole second when it carries sub-second
  precision (so a stored second strictly before the requested instant is
  not wrongly included), and an `until` upper bound truncates **down** to
  the whole second (so an exactly-equal whole-second stored timestamp is
  included even when `until` is expressed with fractional seconds). An
  empty bound is unconstrained; an unparseable bound is left unchanged
  (the API rejects invalid RFC3339 before the store is reached).

## Invariants

- INV-MTE-01: A target-scoped export never contains data from a
  different target.
- INV-MTE-02: Export output is deterministic for the same target
  and time range.
- INV-MTE-03 (#720): Time-range selection depends only on the instants
  the bounds denote — never on timezone-offset spelling or sub-second
  text that sorts differently from the equivalent canonical UTC form.
