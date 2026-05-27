# blocking_locks_v1 — Collector Specification

## Purpose

Enumerate currently-blocked sessions together with their blockers.
Resolves blocking chains through `pg_blocking_pids()`, giving a
per-pair (blocked, blocking) row with waiting duration. Operational
first-look collector for lock storms.

## Catalog source

- `pg_stat_activity` self-joined via `pg_blocking_pids()` (PG 9.6+)

## Output columns

| Column | Type | Description |
|---|---|---|
| blocked_pid | int | PID of the waiting session |
| blocked_user | text | Username of the blocked session |
| blocked_query | text | `LEFT(query, 200)` of the blocked session |
| blocking_pid | int | PID of the session holding the blocking lock |
| blocking_user | text | Username of the blocker |
| blocking_query | text | `LEFT(query, 200)` of the blocker |
| wait_event_type | text | Blocked session's wait-event category |
| wait_event | text | Blocked session's specific wait event |
| waiting_seconds | double precision | `EXTRACT(EPOCH FROM (now() - blocked.query_start))` |

## Scope filter

`WHERE cardinality(pg_blocking_pids(blocked.pid)) > 0` — only
emit rows for sessions that actually have a blocker.

## Invariants

- Deterministic ordering: `ORDER BY waiting_seconds DESC`
  (longest-waiting first).
- Stable output column order.
- Read-only, passes linter.

## Failure Conditions

- FC-01: No blocking in progress → empty result. Not an error.
- FC-02: Role lacks `pg_monitor` → can only see own backend; the
  pg_blocking_pids derivation may return incomplete data. Analyzer
  should cross-reference role capability before drawing
  conclusions.

## Configuration

- Category: activity
- Cadence: 5m (Cadence5m)
- Retention: RetentionShort
- Min PG version: 10 (pg_blocking_pids available 9.6+)
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Medium. Query text is truncated to 200 chars but may embed literals
on unparameterized SQL. Deployment-boundary assumption applies.

## Relationship to `pg_locks_summary_v1`

`pg_locks_summary_v1` provides aggregated counts across all lock
modes (both held and waiting). `blocking_locks_v1` is the focused
drill-down when waiting counts > 0: for each waiter, which PID is
holding it up. Use the summary for overview; use this for
investigation.

## Analyzer requirements unblocked

- Contention detail for `query-latency-regression`.
- `autovacuum-lag` — blocking chains that involve vacuum workers.
- Reporting for lock-storm investigation.
