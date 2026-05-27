# temp_io_pressure_v1 — Collector Specification

## Purpose

Per-database temp-file usage: count and bytes written to temp files
since last `stats_reset`. Narrow view focused on `work_mem`
exhaustion — the primary cause of spilled sorts, hashes, and
materializations.

## Catalog source

- `pg_stat_database`

## Output columns

| Column | Type | Description |
|---|---|---|
| database_name | text | `datname` |
| temp_files | bigint | Count of temp files created (cumulative) |
| temp_bytes | bigint | Temp bytes written (cumulative) |
| stats_reset | timestamptz | Last reset |

## Scope filter

- `WHERE datname IS NOT NULL`
- `AND (temp_files > 0 OR temp_bytes > 0)` — suppresses clean
  databases

Ordering: `temp_bytes DESC NULLS LAST`.

## Invariants

- Deterministic ordering by temp-byte pressure.
- Stable output column order.
- Read-only, passes linter.

## Failure Conditions

- FC-01: Counter reset without `stats_reset` advance → per
  `delta-semantics.md` FC-DS-01.

## Configuration

- Category: server
- Cadence: 15m (Cadence15m)
- Retention: RetentionMedium
- Min PG version: 10
- Requires extension: none
- Semantics: cumulative (see `delta-semantics.md`)
- Enabled by default: yes

## Sensitivity

Low.

## Analyzer requirements unblocked

- `query-latency-regression` — temp-file growth as a secondary
  signal of regression (wrong plan spilling to disk).
- Work-mem sizing advice — complements query-level evidence from
  `pg_stat_statements_v1`.
