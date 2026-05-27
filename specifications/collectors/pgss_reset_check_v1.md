# pgss_reset_check_v1 — Collector Specification

## Purpose

Track when `pg_stat_statements` was last reset, so the analyzer can
compute meaningful deltas for `pg_stat_statements_v1`. Without this
signal, a reset between samples would silently invalidate window
statistics.

## Catalog source

- `pg_stat_statements_info` (extension, PG 14+)

## Query

```
SELECT
    stats_reset,
    EXTRACT(EPOCH FROM (now() - stats_reset)) AS seconds_since_reset
FROM pg_stat_statements_info
```

## Output columns

One row.

| Column | Type | Description |
|---|---|---|
| stats_reset | timestamptz | When `pg_stat_statements` was last reset |
| seconds_since_reset | double precision | `now() - stats_reset` in seconds |

## Scope filter

Single-row view.

## Invariants

- Exactly one row when both extension and view are available.
- Read-only, passes linter.

## Failure Conditions

- FC-01: Extension absent → collector filtered out at pgqueries
  layer via `RequiresExtension: pg_stat_statements`.
- FC-02: Extension present but PG < 14 →
  `pg_stat_statements_info` is unavailable. Filtered out via
  `MinPGVersion: 14`.
- FC-03: Permission denied → standard collector error path.

## Configuration

- Category: extensions
- Cadence: 1h (Cadence1h)
- Retention: RetentionMedium
- Min PG version: 14
- Requires extension: pg_stat_statements
- Semantics: snapshot (the `stats_reset` timestamp itself)
- Enabled by default: yes

## Sensitivity

Low.

## Analyzer requirements unblocked

- Companion to `pg_stat_statements_v1` — reset detection for
  delta-semantics when the analyzer computes per-queryid deltas
  across samples.
