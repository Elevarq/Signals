# replication_status_v1 — Collector Specification

## Purpose

Live state of connected replicas: sent/write/flush/replay LSNs,
derived lag in bytes, sync state. Complements
`replication_slots_risk_v1` by reporting the active consumer side.

## Catalog source

- `pg_stat_replication`
- `pg_wal_lsn_diff()` — derived lag fields

## Output columns

| Column | Type | Description |
|---|---|---|
| pid | int | Walsender backend PID |
| usename | text | Replication user name |
| application_name | text | As reported by the replica |
| client_addr | inet | Replica network address |
| state | text | `startup`, `catchup`, `streaming`, `backup`, `stopping` |
| sync_state | text | `async`, `potential`, `sync`, `quorum` |
| sent_lsn | pg_lsn | Last LSN sent |
| write_lsn | pg_lsn | Last LSN written on replica |
| flush_lsn | pg_lsn | Last LSN flushed on replica |
| replay_lsn | pg_lsn | Last LSN replayed on replica |
| replay_lag_bytes | bigint | `pg_wal_lsn_diff(sent_lsn, replay_lsn)` |
| write_lag_bytes | bigint | `pg_wal_lsn_diff(sent_lsn, write_lsn)` |
| flush_lag_bytes | bigint | `pg_wal_lsn_diff(write_lsn, flush_lsn)` |

Emitted sort: `replay_lag_bytes DESC NULLS LAST`.

## Scope filter

All rows from `pg_stat_replication`. Empty on primaries with no
connected replicas.

## Invariants

- Deterministic ordering by replay lag.
- Stable output column order.
- Read-only, passes linter.
- Lag fields computed server-side in bytes; analyzer does not
  recompute.

## Failure Conditions

- FC-01: No replicas connected → empty result. Not an error.
- FC-02: Permission denied → standard collector error path
  (readable by `pg_monitor`).

## Configuration

- Category: replication
- Cadence: 5m (Cadence5m)
- Retention: RetentionShort
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Low-to-medium. `client_addr` is a network identifier. By the project
deployment-boundary assumption, the snapshot remains within the
customer site; no redaction at collection time.

## Analyzer requirements unblocked

- Replication-lag coverage for any detector that references
  replicas.
- `wal-retention-risk` — cross-validates slot state when the
  consumer is an active replica.
