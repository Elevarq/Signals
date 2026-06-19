# pg_stat_database_v1 — Collector Specification

## Purpose

Per-database activity counters: transactions, cache hits/misses,
temp-file usage, deadlocks. Baseline evidence for cache
effectiveness (`blks_hit` / `blks_read`), contention (`deadlocks`),
spill pressure (`temp_files`, `temp_bytes`). Acts as the fallback
I/O signal on PostgreSQL < 16 where `pg_stat_io` is unavailable.

## Catalog source

- `pg_stat_database`

## Output columns

| Column | Type | Description |
|---|---|---|
| datid | oid | Database OID |
| datname | text | Database name |
| numbackends | int | Current connected backends |
| xact_commit | bigint | Committed transactions (cumulative) |
| xact_rollback | bigint | Rolled-back transactions (cumulative) |
| blks_read | bigint | Disk blocks read (cumulative) |
| blks_hit | bigint | Blocks found in cache (cumulative) |
| tup_returned | bigint | Rows returned by queries |
| tup_fetched | bigint | Rows fetched by index scans |
| tup_inserted | bigint | Rows inserted |
| tup_updated | bigint | Rows updated |
| tup_deleted | bigint | Rows deleted |
| conflicts | bigint | Recovery conflicts |
| temp_files | bigint | Temp files created |
| temp_bytes | bigint | Temp bytes written |
| deadlocks | bigint | Deadlocks detected |
| blk_read_time | double precision | Time reading blocks, ms |
| blk_write_time | double precision | Time writing blocks, ms |
| session_time | double precision | Time spent by sessions, ms (PG 14+; NULL on < 14) |
| active_time | double precision | Time executing SQL, ms (PG 14+; NULL on < 14) |
| idle_in_transaction_time | double precision | Time idle-in-transaction, ms (PG 14+; NULL on < 14) |
| sessions | bigint | Sessions established, cumulative (PG 14+; NULL on < 14) |
| sessions_abandoned | bigint | Sessions lost to client disconnect (PG 14+; NULL on < 14) |
| sessions_fatal | bigint | Sessions ended by fatal error (PG 14+; NULL on < 14) |
| sessions_killed | bigint | Sessions terminated by operator (PG 14+; NULL on < 14) |
| stats_reset | timestamptz | Last reset |

The seven session/timing fields are emitted in canonical position
across all majors: real values on PG 14+ (per-major SQL override), typed
`NULL` stubs on PG 10–13 (default SQL). Consumers see a stable column
set regardless of server version — the same union-schema discipline
`pg_stat_io_v1` / `pg_stat_wal_v1` use across the PG 18 boundary.

## Scope filter

`WHERE datname IS NOT NULL` — excludes rows with NULL `datname`
(shared-catalog aggregate rows). Template databases are included
by `datname` but flagged via `datistemplate` downstream from
`pg_database_v1`.

## Invariants

- Deterministic ordering: `ORDER BY datname`.
- Stable output column order.
- Read-only query, passes linter.

## Failure Conditions

- FC-01: Permission denied → standard collector error path.
- FC-02: Counter decrease without `stats_reset` advance → per
  `delta-semantics.md` FC-DS-01.

## Configuration

- Category: database
- Cadence: 15m (Cadence15m)
- Retention: RetentionMedium
- Min PG version: 10
- Requires extension: none
- Semantics: cumulative (see `delta-semantics.md`)
- Enabled by default: yes

## Sensitivity

Low. Per-database aggregates.

## Analyzer requirements unblocked

- `io-cost-calibration` — fallback path for PG < 16.
- `query-concentration-risk` — temp-file / deadlock pressure as
  secondary evidence.
- Generic DB-health coverage.

## Known gap vs aspirational spec

The PG 14+ session/timing fields (`session_time`, `active_time`,
`idle_in_transaction_time`, `sessions`, `sessions_abandoned`,
`sessions_fatal`, `sessions_killed`) are now emitted — real on PG 14+,
NULL stubs on PG 10–13 — per the output-columns table above
(Elevarq/Signals#210). They unblock the connection-churn detector,
which reads Δ`sessions` across snapshots as the
connection-establishment rate, and let `io-cost-calibration`
cross-validate against `active_time` for session-weighted cost
normalization.

Remaining gap: PG 12+ `checksum_failures` / `checksum_last_failure` are
still not emitted. Additive when a consumer needs them; no detector
depends on them yet.
