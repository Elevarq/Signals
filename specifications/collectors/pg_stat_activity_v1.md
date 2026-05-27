# pg_stat_activity_v1 — Collector Specification

## Purpose

Per-session activity snapshot: backend identity, client address,
current wait event, state, and current query (truncated). Provides
the raw session list used by reporting paths that need to show
"what is this PID doing right now". Does **not** aggregate — for the
aggregated overview, see `pg_stat_activity_summary_v1`; for narrow
watches, see `long_running_txns_v1`, `idle_in_txn_offenders_v1`.

## Catalog source

- `pg_stat_activity`

## Output columns

| Column | Type | Description |
|---|---|---|
| pid | int | Backend process ID |
| datname | text | Database name |
| usename | text | Connected user |
| application_name | text | Application label |
| client_addr | inet | Client address |
| backend_start | timestamptz | When the backend was started |
| xact_start | timestamptz | Start of current transaction |
| query_start | timestamptz | Start of current query |
| state_change | timestamptz | Last state transition |
| wait_event_type | text | Wait-event category |
| wait_event | text | Specific wait event |
| state | text | `active`, `idle`, `idle in transaction`, ... |
| backend_type | text | `client backend`, `autovacuum worker`, ... |

## Scope filter

`WHERE pid != pg_backend_pid()` — excludes the collector's own
session.

## Invariants

- Deterministic ordering: `ORDER BY pid`.
- Stable output column order.
- Read-only, passes linter.

## Failure Conditions

- FC-01: Role lacks `pg_monitor` / `pg_read_all_stats` → the view
  returns only the role's own sessions. Analyzer should cross-check
  `pg_role_capabilities_v1` (future) before drawing conclusions
  from this output.

## Configuration

- Category: activity
- Cadence: 5m (Cadence5m)
- Retention: RetentionShort
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Medium. `client_addr`, `usename`, `application_name` are identity
signals. Query text is **not** emitted by this collector. By
deployment-boundary assumption the snapshot remains on site.

## Analyzer requirements unblocked

- Detailed session context when a detector needs to identify a
  specific backend involved in a finding.
