# pg_stat_statements_v1 — Collector Specification

## Purpose

Normalized query-level performance counters: execution time, call
count, rows, shared-block I/O, WAL generation. Highest-value
runtime signal for detectors — enables
`query-latency-regression`, `query-concentration-risk`,
`function-hint-candidate`, and provides workload evidence for
cost-configuration advice.

## Catalog source

- `pg_stat_statements` (extension)

## Query shape

Uses `SELECT *` for cross-version compatibility. The view's schema
varies across PostgreSQL and extension versions — `blk_read_time`
was renamed to `shared_blk_read_time` in PG 17, several fields were
added in extension versions 1.8 and 1.10, etc. The collector
captures whatever columns the installed version exposes and
serializes them dynamically using actual column names.

No ranking or row limit is applied by Signals. Analyzer owns
workload selection, including any top-N policy by total execution
time, mean execution time, calls, I/O, or another cost model.

## Output columns

Dynamic. Canonical superset across PG 10–17 / extension 1.6–1.11
includes but is not limited to:

| Column | Description |
|---|---|
| queryid | Normalized query fingerprint |
| userid | Executing role OID |
| dbid | Database OID |
| toplevel | True if top-level call (PG14+) |
| query | Normalized query text with `$N` placeholders |
| calls | Execution count (cumulative) |
| total_exec_time | Cumulative execution time, ms |
| min_exec_time, max_exec_time, mean_exec_time, stddev_exec_time | Per-call distribution |
| plans, total_plan_time, mean_plan_time | Plan-time counters (PG13+) |
| rows | Rows returned/affected (cumulative) |
| shared_blks_hit, _read, _dirtied, _written | Shared-buffer I/O |
| local_blks_hit, _read, _dirtied, _written | Local-buffer I/O |
| temp_blks_read, _written | Temp-file blocks |
| blk_read_time, blk_write_time | I/O time, ms (renamed `shared_blk_*` on PG17+) |
| wal_records, wal_fpi, wal_bytes | WAL generation (PG13+) |
| stats_since, minmax_stats_since | Reset-tracking (PG17+) |

## Scope filter

- None. The collector exports the rows exposed by
  `pg_stat_statements` for the connected role. Signals is a data
  collection layer, not an analysis or prioritization layer.

## Query text handling

- Normalized text only: `pg_stat_statements` replaces constants
  with `$N` placeholders for parameterized SQL. Unparameterized
  SQL retains its literals — acceptable under the
  deployment-boundary assumption (data remains on site). No
  collection-time redaction.

## Invariants

- No Analyzer policy is embedded in the collector SQL.
- Column set is whatever the target exposes — never fixed.
- Read-only query, passes linter.
- `queryid` is stable across samples.

## Failure Conditions

- FC-01: Extension not installed → collector is gated out at the
  pgqueries layer via `RequiresExtension: pg_stat_statements`. No
  row is written to `query_results.ndjson` for this collector.
  `collector_status.json` carries one entry with
  `status = "skipped"` and `reason = "extension_missing"` per
  `specifications/extension-absent-emission.md` (EA-R001). The Arq
  Analyzer reads that entry and surfaces the collector as
  `ExtensionUnavailable` (distinct from `CollectorEmpty`). The
  parallel `extension_inventory_v1` collector remains available as
  a cross-check for "which other extensions ARE installed".
- FC-02: Role lacks `pg_read_all_stats` / `pg_monitor` → the query
  returns only rows visible to the current role, which is
  incomplete for workload analysis. Analyzer should cross-reference
  `pg_role_capabilities_v1` (future) before trusting workload
  output.

## Configuration

- Category: extensions
- Cadence: 15m (Cadence15m)
- Retention: RetentionMedium
- Min PG version: 10
- Requires extension: pg_stat_statements
- Semantics: cumulative (see `delta-semantics.md`)
- Enabled by default: yes
- MaxRows: none in Signals

## Sensitivity

Low-to-medium. Query text may embed literals on unparameterized SQL;
by deployment-boundary assumption the snapshot remains within the
customer site.

## Analyzer requirements unblocked

- `query-latency-regression` — primary evidence.
- `query-concentration-risk` — primary evidence.
- `function-hint-candidate` — combined with a future
  `pg_stat_user_functions_v1`, identifies call-site concentration.
- `io-cost-calibration` — per-query cache-hit ratios supplement the
  database-level cache-hit picture.
