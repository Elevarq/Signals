# planner_stats_staleness_v1 — Collector Specification

## Purpose

Identify tables where the planner's row-count estimate
(`pg_class.reltuples`) has drifted significantly from the actual
live-row count (`pg_stat_user_tables.n_live_tup`), and where
modifications have accumulated since the last ANALYZE. Core
evidence for `analyze-staleness` detector.

## Catalog source

- `pg_stat_user_tables` joined with `pg_class`

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | Schema |
| table_name | text | Table name |
| estimated_rows | bigint | `pg_class.reltuples` |
| estimated_pages | int | `pg_class.relpages` |
| actual_live_rows | bigint | `pg_stat_user_tables.n_live_tup` |
| modifications_since_analyze | bigint | `n_mod_since_analyze` |
| last_analyze | timestamptz | Manual ANALYZE timestamp |
| last_autoanalyze | timestamptz | Autoanalyze timestamp |
| estimate_drift_pct | numeric | `round(abs(reltuples - n_live_tup) / GREATEST(reltuples, n_live_tup) * 100, 2)` |

## Scope filter

Implicit via join to `pg_stat_user_tables` (user schemas only).

## Invariants

- Deterministic ordering: `ORDER BY n_mod_since_analyze DESC NULLS LAST`.
- Stable output column order.
- Read-only, passes linter.
- `estimate_drift_pct` is server-computed; analyzer does not
  recompute.

## Failure Conditions

- FC-01: Division-by-zero guarded server-side via `NULLIF(GREATEST
  (reltuples, n_live_tup), 0)` — drift emits NULL for empty
  tables. Not an error.
- FC-02: Permission denied → standard collector error path.

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

## Analyzer requirements unblocked

- `analyze-staleness` — primary evidence.
- `missing-index-candidate` — stale stats affect cost estimates.
