# connection_utilization_v1 — Collector Specification

## Purpose

Scalar snapshot of connection utilization: total client backends,
counts by state, configured `max_connections`, and percentage in
use. Fast narrow view of connection-pool pressure.

## Catalog source

- `pg_stat_activity` filtered to `backend_type = 'client backend'`
- `pg_settings['max_connections']`

## Output columns

One row.

| Column | Type | Description |
|---|---|---|
| total_connections | bigint | Client backend count |
| active | bigint | `state = 'active'` |
| idle | bigint | `state = 'idle'` |
| idle_in_txn | bigint | `state = 'idle in transaction'` |
| idle_in_txn_aborted | bigint | `state = 'idle in transaction (aborted)'` |
| max_connections | int | Cluster GUC value |
| pct_used | numeric | `round(total / max_connections * 100, 2)` |

## Scope filter

`WHERE backend_type = 'client backend'` — excludes walsenders,
autovacuum workers, background workers.

## Invariants

- Exactly one row per sample.
- `pct_used` is a non-negative numeric.
- Read-only, passes linter.

## Failure Conditions

- FC-01: `max_connections` unreadable (never happens on standard
  catalog) → `max_connections` NULL, `pct_used` NULL; row still
  emitted.

## Configuration

- Category: activity
- Cadence: 5m (Cadence5m)
- Retention: RetentionShort
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Low. Aggregates only.

## Relationship to `pg_stat_activity_summary_v1`

Both collectors emit total/active/idle/IIT counts. This one uniquely
adds `max_connections` and `pct_used`; the summary adds age
distributions, wait-event breakdown, and backend-type breakdown.
At a later pass, consider merging this collector's
`max_connections` / `pct_used` fields into the summary and retiring
this collector. For now, retain both.

## Analyzer requirements unblocked

- Capacity alerts — `pct_used` near `max_connections`.
- Context for `query-latency-regression` (connection saturation
  as a confounder).
