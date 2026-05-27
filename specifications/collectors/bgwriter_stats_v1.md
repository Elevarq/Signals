# bgwriter_stats_v1 — Collector Specification

## Purpose

Background-writer and (on PG ≤ 16) checkpointer activity: checkpoint
counts and timings, buffers written by checkpoint/bgwriter/backend,
backend fsyncs, allocation pressure. Primary input for the
`checkpoint-pressure` detector on PG ≤ 16. On PG 17+ the
checkpointer fields move to `pg_stat_checkpointer` — see
`checkpointer_stats_v1`. This collector continues to report the
bgwriter-only fields on all PG versions.

## Catalog source

- `pg_stat_bgwriter`

## Query shape

Uses `SELECT *` for cross-version compatibility. PG 17 split
checkpoint columns out of `pg_stat_bgwriter` into
`pg_stat_checkpointer`, changing this view's schema. The collector
captures whatever columns the installed version exposes and
serializes them dynamically.

## Output columns

Dynamic — whatever `pg_stat_bgwriter` exposes on the target version.
Canonical superset across PG 10–17:

| Column | Type | PG versions | Description |
|---|---|---|---|
| checkpoints_timed | bigint | 10–16 | Scheduled checkpoints (cumulative) |
| checkpoints_req | bigint | 10–16 | Requested checkpoints (cumulative) |
| checkpoint_write_time | double precision | 10–16 | Checkpoint write time, ms |
| checkpoint_sync_time | double precision | 10–16 | Checkpoint sync time, ms |
| buffers_checkpoint | bigint | 10–16 | Buffers written during checkpoints |
| buffers_clean | bigint | all | Buffers written by bgwriter |
| maxwritten_clean | bigint | all | Times bgwriter stopped at max-scan limit |
| buffers_backend | bigint | 10–16 | Buffers written by backends |
| buffers_backend_fsync | bigint | 10–16 | Fsyncs issued by backends |
| buffers_alloc | bigint | all | Buffer allocations (cumulative) |
| stats_reset | timestamptz | all | Last reset |

## Scope filter

Single-row view. No filter.

## Invariants

- Exactly one row per sample.
- Column set is whatever the target's `pg_stat_bgwriter` exposes —
  not a fixed list.
- Read-only, passes linter.

## Failure Conditions

- FC-01: Permission denied → standard collector error path.
- FC-02: Counter decrease without `stats_reset` advance → per
  `delta-semantics.md`.

## Configuration

- Category: server
- Cadence: 15m (Cadence15m)
- Retention: RetentionMedium
- Min PG version: 10
- Requires extension: none
- Semantics: cumulative (see `delta-semantics.md`)
- Enabled by default: yes

## Sensitivity

Low. Cluster-wide aggregates.

## Analyzer requirements unblocked

- `checkpoint-pressure` — primary evidence on PG ≤ 16.
- `io-cost-calibration` — `buffers_backend` and `buffers_alloc` feed
  shared-buffer pressure signals.

## Tests

Existing Go tests cover registration, linter, and filter behavior.
No separate `.acceptance.md` — STDD acceptance rules are embedded
in invariants/failure conditions above.
