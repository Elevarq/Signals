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
| stats_reset | timestamptz | Per-object time these statistics were last reset. PG19+ only (capability-gated on the actual server major); NULL on PG14-18 (column absent) and whenever the server reports it NULL (never reset). See `specifications/pg19-per-object-stats-reset.md` (#817). |

## Scope filter

`pg_stat_user_indexes` excludes system schemas. No additional
filter.

## Invariants

- Deterministic ordering: `ORDER BY schemaname, relname, indexrelname`.
- Stable output column order; `stats_reset` is appended last so the
  pre-existing identity and column order are unchanged across majors
  (#817, INV-SR-01).
- Read-only, passes linter.

## Failure Conditions

- FC-01: Permission denied → standard collector error path.
- FC-02: Counter decrease between samples → per
  `delta-semantics.md`. On PG19+ a per-object reset is directly
  identifiable via this view's own `stats_reset` column (#817); on
  PG14-18 the column is a typed NULL and reset is inferred via
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
