# idle_in_txn_offenders_v1 — Collector Specification

## Purpose

Enumerate backends in `idle in transaction` or
`idle in transaction (aborted)` state. These sessions hold locks,
block vacuum, retain XIDs, and consume connection slots without
doing useful work — the canonical "app forgot to COMMIT" pathology.

## Catalog source

- `pg_stat_activity`

## Output columns

| Column | Type | Description |
|---|---|---|
| pid | int | Backend PID |
| usename | text | Connected user |
| application_name | text | Application label |
| client_addr | inet | Client address |
| state | text | `idle in transaction` or `idle in transaction (aborted)` |
| txn_age_seconds | double precision | `EXTRACT(EPOCH FROM (now() - xact_start))` |
| state_age_seconds | double precision | `EXTRACT(EPOCH FROM (now() - state_change))` |
| query_snippet | text | `LEFT(query, 200)` — last query before the idle state |

## Scope filter

- `state IN ('idle in transaction', 'idle in transaction (aborted)')`
- `pid != pg_backend_pid()` (exclude collector's own session)

## Invariants

- Deterministic ordering: `ORDER BY xact_start ASC NULLS LAST`
  (oldest first).
- Stable output column order.
- Read-only, passes linter.

## Failure Conditions

- FC-01: No idle-in-txn sessions → empty result. Not an error.
- FC-02: Role lacks `pg_monitor` → may only see own sessions;
  cross-reference `pg_role_capabilities_v1` (future).

## Configuration

- Category: activity
- Cadence: 5m (Cadence5m)
- Retention: RetentionShort
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Medium. Query snippet may embed literals; deployment-boundary
assumption applies.

## Analyzer requirements unblocked

- `autovacuum-lag` — IIT sessions block vacuum.
- `xid-wraparound-risk` — IIT sessions retain XIDs.
- Reporting: application misbehavior investigation.
