# pg_stats_v1 — Collector Specification

## Purpose

Column-level planner statistics for cardinality and correlation
analysis. Provides n_distinct and correlation per column, needed for
the FK cardinality check (FI-R012) and correlation-based stale
statistics detection (FI-R052).

## Catalog source

- pg_stats (system view over pg_statistic)

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | Schema name |
| tablename | text | Table name |
| attname | text | Column name |
| n_distinct | real | Distinct value estimate (negative=fraction, positive=count) |
| correlation | real | Physical/logical sort alignment [-1.0, 1.0] |
| null_frac | real | Fraction of null values [0.0, 1.0] |
| avg_width | int | Average column width in bytes |

## Excluded columns

The following pg_stats columns are deliberately excluded because they
contain actual data samples and can be very large:

- most_common_vals
- most_common_freqs
- histogram_bounds
- most_common_elems
- most_common_elem_freqs
- elem_count_histogram

## Schema filter

Excludes pg_catalog, information_schema, pg_toast, pg_temp_%,
pg_toast_temp_%.

## Invariants

- Deterministic ordering: ORDER BY schemaname, tablename, attname
- Empty result serializes as []
- Stable output column order (explicit SELECT, no SELECT *)
- Read-only query, passes linter
- No data samples in output

## Configuration

- Category: schema
- Cadence: 24h (CadenceDaily)
- Retention: RetentionMedium
- Min PG version: 10
- Enabled by default: yes

## Sensitivity

Low. n_distinct and correlation are numerical summaries, not data
values. null_frac reveals proportion of nulls but not which rows.

## Analyzer requirements unblocked

- FI-R012: FK column cardinality check (n_distinct)
- FI-R052: Correlation check for range queries (correlation)
  Currently PENDING — becomes implementable with this collector.

## Privilege boundary: column-filtered zero rows (#458)

`pg_stats` filters every row by `has_column_privilege(role, table, col,
'select')`. A least-privilege monitoring role (`pg_monitor` /
`pg_read_all_stats`) with no table/column `SELECT` therefore reads **zero
rows** — silently, with no error, indistinguishable on row count alone from
a genuinely empty database.

To make the grant boundary diagnosable rather than a silent empty success,
this collector declares `ColumnPrivilegeDegradeReason =
privilege_column_filtered` and a `ColumnPrivilegeProbeSQL` that counts
analyzed user tables (`pg_stat_user_tables` where `last_analyze` or
`last_autoanalyze` is set). When a run returns 0 rows AND the probe returns
> 0 (the database HAS analyzed tables, so `pg_statistic` holds rows a
fully-privileged role would see), the run is recorded
`status=skipped, reason=privilege_column_filtered` — not a silent empty
success — and a warn-once advises the optional grant. The probe runs inside
its own savepoint; any probe error is treated as "cannot confirm" and the
run stays a (legitimate) empty success, so a genuinely-empty database is
never mislabelled.

Collecting per-column statistics is an **optional customer choice**: grant
the monitoring role `SELECT` on the tables (or specific columns) — no
superuser. Without it the collector correctly degrades and R035
(`stats.create_statistics_candidate.v1`) stays dormant, now visibly so.

Distinct from `OwnerOnlyDegrade` / `PrivilegedViewDegrade`, which classify a
hard 42501 **error**; `pg_stats` never errors — it silently filters rows.
