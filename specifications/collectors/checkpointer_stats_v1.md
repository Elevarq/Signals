# checkpointer_stats_v1 — Collector Specification

## Purpose

Checkpointer activity on PostgreSQL 17+: counts of timed and
requested checkpoints, restartpoint progress, write and sync time,
buffers written. PG 17 split these fields out of `pg_stat_bgwriter`
into a dedicated view. On PG ≤ 16 this collector is filtered out
by the `MinPGVersion` gate; equivalent evidence is available from
`bgwriter_stats_v1`.

## Catalog source

- `pg_stat_checkpointer` (introduced in PostgreSQL 17)

## Query shape

Uses `SELECT *` for forward compatibility with PG 18+ additions to
this view.

## Output columns

Dynamic. Canonical columns on PG 17:

| Column | Type | Description |
|---|---|---|
| num_timed | bigint | Scheduled checkpoints (cumulative) |
| num_requested | bigint | Requested checkpoints (cumulative) |
| restartpoints_timed | bigint | Scheduled restartpoints (standby) |
| restartpoints_req | bigint | Requested restartpoints |
| restartpoints_done | bigint | Completed restartpoints |
| write_time | double precision | Checkpoint write time, ms |
| sync_time | double precision | Checkpoint sync time, ms |
| buffers_written | bigint | Buffers written during checkpoints / restartpoints |
| stats_reset | timestamptz | Last reset |

## Scope filter

Single-row view. No filter.

## Invariants

- Exactly one row per sample.
- Column set is whatever the target's `pg_stat_checkpointer`
  exposes.
- Read-only, passes linter.

## Failure Conditions

- FC-01: PG < 17 → filtered via `MinPGVersion`. Absence expected;
  equivalent evidence in `bgwriter_stats_v1`.
- FC-02: Permission denied → standard collector error path.
- FC-03: Counter decrease without `stats_reset` advance → per
  `delta-semantics.md`.

## Configuration

- Category: server
- Cadence: 15m (Cadence15m)
- Retention: RetentionMedium
- Min PG version: 17
- Requires extension: none
- Semantics: cumulative (see `delta-semantics.md`)
- Enabled by default: yes

## Sensitivity

Low.

## Analyzer requirements unblocked

- `checkpoint-pressure` — primary evidence on PG 17+.
- `wal-retention-risk` — restartpoint progress on standbys.
