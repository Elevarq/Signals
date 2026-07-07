# Query-Run Status in Snapshot Export — Behavioral Specification

Spec version: 1.0
Status: ACTIVE
Issue: [Elevarq/Signals#250](https://github.com/Elevarq/Signals/issues/250)

## Purpose

The daemon persists an explicit per-run classification — `status`
(`success` / `failed` / `skipped`) and `reason` (e.g.
`privilege_owner_only`, `permission_denied`, `config_disabled`) — in
the `query_runs` table (migration `004_query_run_status.sql`), and
R116 ([`owner_only_privilege_degradation.md`](owner_only_privilege_degradation.md))
relies on that classification to distinguish an expected privilege
boundary from a genuine fault.

The snapshot export did not carry these columns: `query_runs.ndjson`
rows serialized only the pre-004 fields, so an R116 owner-only skip
arrived at ZIP consumers as a bare
`error: "… permission denied … (SQLSTATE 42501)"` row —
indistinguishable from a real collector failure. Every consumer (the
Analyzer's `evidence_coverage` classification, downstream tooling) had
to re-derive the skip from the SQLSTATE plus its own copy of the
owner-only collector list, re-implementing R116 instead of reading it.

This spec extends the `query_runs.ndjson` row schema so the persisted
classification survives the snapshot boundary. It complements
`owner_only_privilege_degradation.md` NFR-02, which deliberately
scoped the export schema out of that change.

## Interfaces

### Inputs

- Persisted `query_runs` rows in the resolved export scope
  (R084/R085), each carrying `status` and `reason`. Population is
  guaranteed: migration 004 backfills pre-existing rows, and the
  insert layer (`insertRunsAndResults`) normalizes an empty status
  from `error` at write time.

### Outputs

- One NDJSON row per run in `query_runs.ndjson`, now including the
  `status` and `reason` fields alongside the existing nine fields
  (`id`, `target_id`, `snapshot_id`, `query_id`, `collected_at`,
  `pg_version`, `duration_ms`, `row_count`, `error`).

## Behaviors

- **Given** a run recorded `success`, **when** it is exported,
  **then** its row carries `status: "success"` and `reason: ""`.
- **Given** an R116 owner-only skip
  (`skipped` / `privilege_owner_only`, with the driver error text
  preserved in `error`), **when** it is exported, **then** its row
  carries `status: "skipped"`, `reason: "privilege_owner_only"`, and
  the unchanged `error` text.
- **Given** a genuine failure (`failed` / e.g. `permission_denied`),
  **when** it is exported, **then** its row carries
  `status: "failed"` and the recorded reason.

## Rules

- **R118 — Export carries run classification.** Every
  `query_runs.ndjson` row shall include `status` and `reason`,
  emitted **verbatim** from the persisted columns. The export layer
  never remaps, synthesizes, or drops a classification.

## Invariants

- **INV-01** — A consumer can distinguish `skipped` from `failed`
  from the row alone — no SQLSTATE parsing, no knowledge of which
  collectors are owner-only.
- **INV-02** — The change is additive: all nine pre-existing fields
  keep their names, types, and semantics. In particular `error`
  retains the underlying driver text where one was recorded, even for
  R116 skips (it is diagnostic detail; the classification lives in
  `status`/`reason`).
- **INV-03** — Export-side only: no change to what the daemon
  persists, collects, or logs.

## Failure conditions

- **FC-01** — A row whose persisted `status` holds an unexpected
  value is exported verbatim, never coerced to a known value — the
  export reports what the store recorded.

## Constraints

- `status` is one of `success`, `failed`, `skipped` for all rows
  written by any daemon ≥ migration 004; `reason` is empty for
  `success` rows.

## Non-Functional Requirements

- **NFR-01** — No measurable export-size or latency impact beyond the
  two added JSON fields per row.
- **NFR-02** — Backward compatible for consumers: existing fields are
  untouched, so pre-R118 readers continue to work; post-R118 readers
  may rely on `status`/`reason` being present.
