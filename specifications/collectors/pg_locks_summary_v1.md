# pg_locks_summary_v1 — Collector Specification

## Purpose

Aggregated lock state: counts of granted and waiting locks by type
and mode, plus oldest wait duration. Exposes broad lock pressure
without emitting per-relation or per-tuple identity. Companion to
`blocking_locks_v1`, which produces the specific blocker/blocked
pairs for the subset of contention that actually involves blocking.

## Catalog source

- `pg_locks` joined with `pg_stat_activity` for wait durations

## Output columns

One row per `(locktype, mode, granted)` tuple that has at least one
occurrence.

| Column | Type | Description |
|---|---|---|
| locktype | text | `relation`, `transactionid`, `tuple`, `virtualxid`, `object`, `page`, `advisory`, ... |
| mode | text | `AccessShareLock`, `RowExclusiveLock`, `AccessExclusiveLock`, ... |
| granted | boolean | True if held, false if waiting |
| count | int | Number of locks in this bucket |
| max_wait_seconds | bigint | For `granted = false`: longest wait in this bucket. NULL for `granted = true`. |
| distinct_pids | int | Distinct backend PIDs contributing to this row |

## Scope filter

Excludes the collector's own backend (`pid = pg_backend_pid()`).

## Invariants

- Deterministic ordering: `ORDER BY granted DESC, locktype, mode`.
- Stable output column order.
- Read-only, passes linter.
- No per-relation OIDs, no transaction IDs, no tuple identifiers in
  the output.

## Failure Conditions

- FC-01: Permission denied on `pg_locks` (unusual) → standard
  collector error path.
- FC-02: `pg_locks` can be momentarily inconsistent with
  `pg_stat_activity` under high churn; `max_wait_seconds` may be
  NULL for a fraction of waiting rows. Accepted — aggregate remains
  useful.

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

## Relationship to `blocking_locks_v1`

`blocking_locks_v1` (already registered) is the narrow, focused view:
for every backend waiting on a lock, it emits one row with blocker
and blocked PID, user, and query snippets. This is the operationally
critical evidence when a lock storm is in progress.

`pg_locks_summary_v1` (this collector) is the aggregate companion:
it shows the overall lock landscape (granted vs waiting counts per
mode) regardless of whether blocking is occurring. Used for
capacity-style reporting and as background context for other
detectors.

The two are complementary, not duplicative. A reporting path
typically uses `pg_locks_summary_v1` for the overview and drills
into `blocking_locks_v1` when the summary shows waiting counts > 0.

## Analyzer requirements unblocked

- Contention coverage for `query-latency-regression`.
- `autovacuum-lag` — lock contention that blocks autovacuum
  progress.
- Generic health reporting.
