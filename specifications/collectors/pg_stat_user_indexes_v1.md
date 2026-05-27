# pg_stat_user_indexes_v1 — Collector Specification

## Purpose

Per-index usage counters: scan count, tuples read, tuples fetched.
Primary evidence for detecting unused or under-utilized indexes.

## Catalog source

- `pg_stat_user_indexes`

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | Schema |
| relname | text | Table name |
| indexrelname | text | Index name |
| idx_scan | bigint | Index scans (cumulative) |
| idx_tup_read | bigint | Index entries read |
| idx_tup_fetch | bigint | Table rows fetched via this index |

## Scope filter

`pg_stat_user_indexes` excludes system schemas. No additional
filter.

## Invariants

- Deterministic ordering: `ORDER BY schemaname, relname, indexrelname`.
- Stable output column order.
- Read-only, passes linter.

## Failure Conditions

- FC-01: Permission denied → standard collector error path.
- FC-02: Counter decrease between samples → per
  `delta-semantics.md`; reset inferred via
  `pg_stat_database_v1.stats_reset`.

## Configuration

- Category: indexes
- Cadence: default (Cadence1h)
- Retention: RetentionMedium
- Min PG version: 10
- Requires extension: none
- Semantics: cumulative (see `delta-semantics.md`)
- Enabled by default: yes

## Sensitivity

Low.

## Analyzer requirements unblocked

- `unused-index` detector — scan count over window.
- `index-usage-change` — scan-rate drift between samples.
- `missing-index-candidate` — corroborative evidence.
