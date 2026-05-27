# pg_stat_io_v1 — Collector Specification

## Purpose

Per-(backend_type, io_object, io_context) physical I/O counts. The
primary input for the analyzer's I/O cost calibration: measured
physical reads, read time, and cache reuse by access pattern. Enables
evidence-grounded recommendations for `random_page_cost`,
`seq_page_cost`, `effective_io_concurrency`, and `effective_cache_size`.

## Catalog source

- `pg_stat_io` (introduced in PostgreSQL 16)

## Output columns

| Column | Type | Description |
|---|---|---|
| backend_type | text | Process kind (client backend, autovacuum worker, checkpointer, ...) |
| object | text | I/O object (`relation`, `temp relation`) |
| context | text | I/O context (`normal`, `vacuum`, `bulkread`, `bulkwrite`) |
| reads | bigint | Physical block reads (cumulative) |
| read_time | double precision | Time spent reading, ms (cumulative) |
| writes | bigint | Physical block writes (cumulative) |
| write_time | double precision | Time spent writing, ms (cumulative) |
| writebacks | bigint | Blocks marked for writeback |
| writeback_time | double precision | Writeback time, ms |
| extends | bigint | Relation extension calls |
| extend_time | double precision | Extension time, ms |
| op_bytes | bigint | Block size in bytes for this row |
| hits | bigint | Shared-buffer hits |
| evictions | bigint | Shared-buffer evictions |
| reuses | bigint | Strategy-buffer reuses |
| fsyncs | bigint | Backend-issued fsyncs |
| fsync_time | double precision | fsync time, ms |
| stats_reset | timestamptz | When the view was last reset |

## Scope filter

No schema filter. All `(backend_type, object, context)` tuples with a
non-NULL `stats_reset` are emitted. Rows with all-NULL counters
(placeholder combinations the kernel never executes) are excluded.

## Invariants

- Deterministic ordering: `ORDER BY backend_type, object, context`.
- Stable output column order (explicit `SELECT`, no `SELECT *`).
- Read-only query, passes linter.
- `op_bytes` is emitted so the analyzer can convert block counts to
  bytes without assuming the default `BLCKSZ`.

## Failure Conditions

- FC-01: PostgreSQL < 16 → the query is gated out at the pgqueries
  layer via `MinPGVersion`. `collector_status.json` carries one
  entry with `status = "skipped"` and
  `reason = "version_unsupported"`
  (`specifications/extension-absent-emission.md`, EA-R001). No
  rows in `query_results.ndjson` for this collector.
- FC-02: Permission denied (role lacks `pg_monitor`) → standard
  collector failure path (SAVEPOINT rollback). `collector_status.
  json` carries `status = "failed"`,
  `reason = "permission_denied"`.

## Configuration

- Category: io
- Cadence: 15m (Cadence15m)
- Retention: RetentionMedium
- Min PG version: 16
- Requires extension: none
- Semantics: cumulative (see `delta-semantics.md`)
- Enabled by default: yes

## Sensitivity

Low. Aggregated counters, no query text, no per-relation data.

## Analyzer requirements unblocked

- `io-cost-calibration` detector — primary evidence source.
- `checkpoint-pressure` detector — checkpointer-backend row supplies
  direct write/fsync pressure without inference.
- `toast-planner-blindspot` — combined with `pg_statio_user_tables_v1`
  to partition I/O by relation category.
