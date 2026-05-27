# largest_relations_v1 — Collector Specification

## Purpose

Top 30 user tables by total relation size (main + indexes + TOAST)
for storage triage. Complements per-table metrics by surfacing
"which tables drive the storage footprint" at a glance.

## Catalog source

- `pg_stat_user_tables` joined with size functions

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | Schema |
| table_name | text | Table name |
| total_size_bytes | bigint | `pg_total_relation_size(relid)` — main + indexes + TOAST |
| table_size_bytes | bigint | `pg_relation_size(relid)` — main only |
| indexes_size_bytes | bigint | `pg_indexes_size(relid)` |
| n_live_tup | bigint | Live-row estimate |
| n_dead_tup | bigint | Dead-row estimate |

## Scope filter

User schemas via `pg_stat_user_tables`. LIMIT 30 ordered by
`pg_total_relation_size(relid) DESC`.

## Invariants

- Deterministic ordering: total size descending.
- Stable output column order.
- Read-only, passes linter.

## Failure Conditions

- FC-01: On very large schemas, per-fork `lseek` behind
  `pg_total_relation_size()` can be slow. Bounded by the
  collector's 10-second timeout.
- FC-02: Relation dropped between catalog read and size call →
  size returns NULL or errors. Query does not use `LEFT JOIN
  LATERAL` guards, so a dropped relation mid-flight could raise —
  accepted for simplicity; retry on next cadence.

## Configuration

- Category: tables
- Cadence: 1h (Cadence1h)
- Retention: RetentionMedium
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Low.

## Relationship to `pg_class_storage_v1`

`pg_class_storage_v1` (proposed, new collector) is the full
storage-accounting surface: every user relation, with main / TOAST
/ TOAST-index size, reloptions, tablespace. `largest_relations_v1`
(this collector) is the narrow top-30 for quick triage.

Both can coexist: `largest_relations_v1` is cheap enough to run
hourly and catches growth, while `pg_class_storage_v1` is a full
daily snapshot.

## Analyzer requirements unblocked

- Storage-capacity reporting.
- `table-bloat-risk` — combined with `dead_tup` for prioritized
  bloat candidates.
