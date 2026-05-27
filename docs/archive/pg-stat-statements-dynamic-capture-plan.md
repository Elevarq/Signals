# pg_stat_statements Dynamic Capture — Discovery

## Current state

The `pg_stat_statements_v1` collector used a fixed column list:
```sql
SELECT userid, dbid, queryid, calls,
total_exec_time, min_exec_time, max_exec_time, mean_exec_time,
stddev_exec_time, rows, shared_blks_hit, shared_blks_read,
shared_blks_dirtied, shared_blks_written, local_blks_hit,
local_blks_read, local_blks_dirtied, local_blks_written,
temp_blks_read, temp_blks_written, blk_read_time, blk_write_time
FROM pg_stat_statements
```

## Problem

PostgreSQL 17 (pg_stat_statements 1.11) renamed several columns:
- `blk_read_time` → `shared_blk_read_time`
- `blk_write_time` → `shared_blk_write_time`

The fixed column list fails on PG 17+ with:
`ERROR: column "blk_read_time" does not exist (SQLSTATE 42703)`

This error aborts the transaction. Before the savepoint fix, it
cascaded to abort all subsequent queries. After the savepoint fix,
the pg_stat_statements query still fails but other queries succeed.

## Key insight

The result scanning path (`queryToMaps`) already uses dynamic column
discovery via `pgx FieldDescriptions()`. Each row is serialized as a
`map[string]any` using the actual returned column names. The NDJSON
encoder writes whatever keys are in the map.

**The only fixed-schema dependency is the SQL SELECT column list.**

## Fix

Replace the explicit column list with raw `SELECT *`. Do not add
`ORDER BY` or `LIMIT`; Analyzer owns workload ranking and top-N
selection.

This captures whatever columns the installed pg_stat_statements
version exposes, including new columns added in future versions.

## Impact assessment

- **Snapshot format**: Unchanged — NDJSON already preserves dynamic
  column names
- **Export format**: Unchanged — query_results.ndjson carries the
  dynamic payload
- **Linter**: `SELECT *` passes the linter (starts with SELECT, no
  dangerous keywords)
- **Safety model**: Unchanged — query still runs in READ ONLY
  transaction with savepoints
- **Other collectors**: No change needed — their column lists are
  stable across PG 14+
