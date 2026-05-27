# pg_statio_user_indexes_v1 — Collector Specification

## Purpose

Per-index block read/hit counters. Complements
`pg_statio_user_tables_v1` by attributing index-side I/O to specific
indexes rather than the aggregated table-level row. Needed to rank
which indexes are actually hot versus merely present.

## Catalog source

- `pg_statio_user_indexes`

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | Schema |
| relname | text | Table name |
| indexrelname | text | Index name |
| idx_blks_read | bigint | Disk blocks read from this index (cumulative) |
| idx_blks_hit | bigint | Cache hits against this index (cumulative) |

## Scope filter

`pg_statio_user_indexes` already excludes system schemas. No
additional filter.

## Invariants

- Deterministic ordering: `ORDER BY schemaname, relname, indexrelname`.
- Stable output column order.
- Read-only query, passes linter.

## Failure Conditions

- FC-01: Permission denied → standard collector error path.
- FC-02: Counter decrease without reset advance → per
  `delta-semantics.md`. Reset is inferred from
  `pg_stat_database_v1.stats_reset`.

## Configuration

- Category: io
- Cadence: 15m (Cadence15m) — default on unset cadence
- Retention: RetentionMedium
- Min PG version: 10
- Requires extension: none
- Semantics: cumulative (see `delta-semantics.md`)
- Enabled by default: yes

## Sensitivity

Low.

## Analyzer requirements unblocked

- `index-usage-change` — finer-grained attribution than
  `pg_stat_user_indexes.idx_scan` alone.
- `missing-index-candidate` — corroborative; heavy index read
  pressure on a composite vs. a missing leading column is evidence.
- `io-cost-calibration` — index contribution to physical-read rate.
