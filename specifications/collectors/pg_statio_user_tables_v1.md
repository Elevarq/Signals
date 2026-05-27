# pg_statio_user_tables_v1 — Collector Specification

## Purpose

Per-table split of block-read pressure across **heap**, **index**,
**TOAST heap**, and **TOAST index**. The only catalog surface that
distinguishes TOAST I/O from main-relation I/O. Primary evidence for
the TOAST planner blind-spot detector and the I/O cost calibration's
relation-mix estimate.

## Catalog source

- `pg_statio_user_tables`

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | Schema |
| relname | text | Table name |
| heap_blks_read | bigint | Disk blocks read from heap (cumulative) |
| heap_blks_hit | bigint | Cache hits against heap (cumulative) |
| idx_blks_read | bigint | Disk blocks read from indexes |
| idx_blks_hit | bigint | Cache hits against indexes |
| toast_blks_read | bigint | Disk blocks read from TOAST heap |
| toast_blks_hit | bigint | Cache hits against TOAST heap |
| tidx_blks_read | bigint | Disk blocks read from TOAST index |
| tidx_blks_hit | bigint | Cache hits against TOAST index |

## Scope filter

`pg_statio_user_tables` already excludes system schemas. No
additional filter.

## Invariants

- Deterministic ordering: `ORDER BY schemaname, relname`.
- Stable output column order.
- Read-only query, passes linter.
- TOAST columns may be NULL for tables with no TOAST relation — the
  analyzer distinguishes "no TOAST" from "TOAST present but unread".

## Failure Conditions

- FC-01: Permission denied → standard collector error path.
- FC-02: Counter decrease without reset advance → per
  `delta-semantics.md`. `pg_statio_*` exposes no `stats_reset`
  column; the analyzer infers reset via
  `pg_stat_database_v1.stats_reset` for the same database.

## Configuration

- Category: io
- Cadence: 15m (Cadence15m) — the default on unset cadence
- Retention: RetentionMedium
- Min PG version: 10
- Requires extension: none
- Semantics: cumulative (see `delta-semantics.md`)
- Enabled by default: yes

## Sensitivity

Low. Structural metadata + aggregate counters.

## Analyzer requirements unblocked

- `toast-planner-blindspot` — derived metric
  `toast_amplification = (Δtoast_blks_read + Δtidx_blks_read) /
  max(1, Δheap_blks_read)`.
- `io-cost-calibration` — partitions per-relation I/O pressure.
- `table-bloat-risk`, `index-bloat-risk` — corroborates bloat
  findings with measured read pressure.
