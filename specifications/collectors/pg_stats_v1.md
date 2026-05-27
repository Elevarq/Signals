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
