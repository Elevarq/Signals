# pg_stat_activity_summary_v1 — Collector Specification

## Purpose

Aggregated session-state counters from `pg_stat_activity`: counts by
state, oldest-transaction age, idle-in-transaction pressure,
long-running query counts. Companion to the narrow existing
collectors that watch specific activity conditions:
`long_running_txns_v1`, `idle_in_txn_offenders_v1`,
`connection_utilization_v1`, `blocking_locks_v1`.

This collector deliberately exposes no query text and no per-session
identity — it is the "overview dashboard" view.

## Catalog source

- `pg_stat_activity` — aggregated

## Output columns

One row.

| Column | Type | Description |
|---|---|---|
| total_backends | int | Client-backend count (excludes collector's own session, walsenders, autovacuum workers for this count) |
| active_count | int | `state = 'active'` |
| idle_count | int | `state = 'idle'` |
| idle_in_transaction_count | int | `state = 'idle in transaction'` |
| idle_in_transaction_aborted_count | int | `state = 'idle in transaction (aborted)'` |
| fastpath_count | int | `state = 'fastpath function call'` |
| disabled_count | int | `state = 'disabled'` |
| waiting_count | int | `wait_event IS NOT NULL AND state = 'active'` |
| oldest_xact_age_seconds | bigint | `EXTRACT(EPOCH FROM now() - min(xact_start))`, NULL if no open txn |
| oldest_query_age_seconds | bigint | Oldest `now() - query_start` among active backends |
| oldest_backend_xmin_age_xids | bigint | `age(min(backend_xmin))` |
| active_gt_1min | int | Active queries > 1 minute |
| active_gt_5min | int | Active queries > 5 minutes |
| active_gt_1h | int | Active queries > 1 hour |
| long_idle_in_txn_count | int | Idle-in-txn sessions with state duration > 1 minute |
| by_backend_type | jsonb | Counts grouped by `backend_type` |
| by_wait_event_type | jsonb | Counts grouped by `wait_event_type` among waiting backends |

## Scope filter

Excludes `pid = pg_backend_pid()` from the state-count dimensions.

## Invariants

- Exactly one row per sample.
- `by_backend_type` and `by_wait_event_type` are JSON objects
  (possibly `{}`); never NULL.
- No query text, user names, application names, client addresses, or
  session PIDs in the output.

## Failure Conditions

- FC-01: Permission denied → standard collector error path (the
  enforced role `pg_monitor` has read access, so this is not
  expected in practice).

## Configuration

- Category: activity
- Cadence: 5m (Cadence5m)
- Retention: RetentionShort
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Low. No per-session information.

## Relationship to existing activity collectors

| Collector | Scope | This collector's role |
|---|---|---|
| `long_running_txns_v1` | Per-session list of txns > 5 min | Summary adds counts at 1min / 5min / 1h thresholds |
| `idle_in_txn_offenders_v1` | Per-session list of IIT sessions | Summary adds IIT count + long-IIT count |
| `connection_utilization_v1` | Scalar: total / active / idle / IIT / % used | Summary reproduces these counts plus per-backend-type breakdown |
| `blocking_locks_v1` | Per-(blocker, blocked) pairs | Orthogonal — different evidence |

The narrow collectors emit details at every sample even when the
situation is quiet. This summary collector is the "what is happening
right now" one-glance row that the reporting layer uses before
drilling into per-session detail.

**Overlap with `connection_utilization_v1`:** partial — both emit
total/active/idle/IIT counts. `connection_utilization_v1` additionally
reports `max_connections` and `pct_used`; this collector additionally
reports age distributions, wait-event breakdown, and backend-type
breakdown. Retain both for now; at a later pass, consider merging
`connection_utilization_v1` into this collector's row.

## Analyzer requirements unblocked

- `autovacuum-lag` — IIT and long-txn pressure as vacuum blockers.
- `xid-wraparound-risk` — `oldest_backend_xmin_age_xids` is the
  client-side contribution to wraparound risk.
- Generic health reporting.
