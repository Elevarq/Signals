# long_running_txns_v1 — Collector Specification

## Purpose

Enumerate active (non-idle) transactions that have been open for
more than 5 minutes. Long-running transactions hold snapshots and
locks, block vacuum, and extend WAL retention — the focused narrow
watch for this class of problem.

## Catalog source

- `pg_stat_activity`

## Output columns

| Column | Type | Description |
|---|---|---|
| pid | int | Backend PID |
| usename | text | Connected user |
| application_name | text | Application label |
| client_addr | inet | Client address |
| state | text | Backend state |
| wait_event_type | text | Wait-event category |
| wait_event | text | Specific wait event |
| txn_age_seconds | double precision | `EXTRACT(EPOCH FROM (now() - xact_start))` |
| query_snippet | text | `LEFT(query, 200)` — first 200 chars of current query |

## Scope filter

- `xact_start IS NOT NULL`
- `state != 'idle'`
- `now() - xact_start > interval '5 minutes'`

## Invariants

- Deterministic ordering: `ORDER BY xact_start ASC` (oldest first).
- Stable output column order.
- Read-only, passes linter.
- Only returns non-idle sessions — idle-in-transaction is covered by
  `idle_in_txn_offenders_v1`.

## Failure Conditions

- FC-01: No long-running transactions → empty result. Not an error.
- FC-02: Role lacks `pg_monitor` → may see only own sessions; see
  cross-reference caveat in `pg_stat_activity_v1`.

## Configuration

- Category: activity
- Cadence: 5m (Cadence5m)
- Retention: RetentionShort
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Medium. `query_snippet` may embed literals; deployment-boundary
assumption applies.

## Analyzer requirements unblocked

- `xid-wraparound-risk` — long-running txns hold `xmin`.
- `autovacuum-lag` — long-running txns block vacuum.
- Reporting for contention investigation.
