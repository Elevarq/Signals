# pg_stat_wal_v1 — Collector Specification

## Purpose

WAL generation and write activity: records, full-page images, bytes,
write/sync times, buffer-full stalls. Feeds the
`wal-retention-risk`, `checkpoint-pressure`, and replication-lag
analyses. On PG < 14 this collector is unavailable; the analyzer
infers WAL activity from `pg_stat_statements_v1.wal_bytes` instead.

## Catalog source

- `pg_stat_wal` (introduced in PostgreSQL 14)

## Output columns

| Column | Type | Description |
|---|---|---|
| wal_records | bigint | WAL records generated (cumulative) |
| wal_fpi | bigint | Full-page images (cumulative) |
| wal_bytes | numeric | WAL bytes (cumulative) |
| wal_buffers_full | bigint | Times WAL buffers were full, forcing writes |
| wal_write | bigint | WAL writes (cumulative) |
| wal_sync | bigint | WAL syncs (cumulative) |
| wal_write_time | double precision | Time writing WAL, ms |
| wal_sync_time | double precision | Time syncing WAL, ms |
| stats_reset | timestamptz | Last reset |

## Scope filter

Single-row output (`pg_stat_wal` is cluster-wide).

## Invariants

- Exactly one row per sample.
- Stable output column order.
- Read-only, passes linter.

## Failure Conditions

- FC-01: PG < 14 → filtered via `MinPGVersion`. Absence is expected.
- FC-02: Permission denied → standard collector error path.
- FC-03: Counter decrease without `stats_reset` advance → per
  `delta-semantics.md`.

## Configuration

- Category: runtime
- Cadence: 15m (Cadence15m)
- Retention: RetentionMedium
- Min PG version: 14
- Requires extension: none
- Semantics: cumulative (see `delta-semantics.md`)
- Enabled by default: yes

## Sensitivity

Low. Cluster-wide aggregates.

## Analyzer requirements unblocked

- `wal-retention-risk` — WAL generation rate × replication-slot WAL
  retention.
- `checkpoint-pressure` — WAL volume correlates with checkpoint
  frequency; combined with `pg_stat_checkpointer_v1` or
  `pg_stat_bgwriter_v1`.
- `replication-slot-retention` — lag projection.
