# pg_stats_extended_v1 — Collector Specification

## Purpose

Optional histogram-based planner statistics for advanced PostgreSQL
analysis. Provides most_common_vals, most_common_freqs, and
histogram_bounds per column — the data samples that pg_stats_v1
deliberately excludes.

## Status

**Active.** Implemented in
`internal/pgqueries/catalog_schema.go`. Classified
`HighSensitivity = true` on the **skip-path** (no `SensitiveColumns`
declared) via the registry. Under R075 v2 (revised 2026-05) the
daemon-wide gate `HighSensitivityEnabled` defaults to **true**
(collect-everything default) so the collector **runs by default**.
An operator who opts out via
`signals.high_sensitivity_collectors_enabled: false` drops the
collector from the eligible set — it then appears in
`collector_status.json` as `status=skipped, reason=config_disabled`
(EA-R001). The collector is on the skip-path because its row (MCV
and histogram values) IS the sensitive payload; nothing meaningful
would remain after redacting those columns. Configuration plumbing
— the `signals.collect_histograms` / `ARQ_SIGNALS_COLLECT_HISTOGRAMS`
shape described below — is provided by the existing
high-sensitivity surface.

## Relationship to pg_stats_v1

pg_stats_v1 collects numerical summaries (n_distinct, correlation,
null_frac, avg_width) that are always safe to collect. This collector
extends that with the sampled-value columns that pg_stats_v1
intentionally excludes.

Both collectors read from the same catalog view (pg_stats) but serve
different purposes:

| Collector | Content | Sensitivity | Default |
|---|---|---|---|
| pg_stats_v1 | Numerical summaries | Low | Enabled |
| pg_stats_extended_v1 | Data samples | High | **Disabled** |

## Catalog source

- pg_stats (system view over pg_statistic)

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | Schema name |
| tablename | text | Table name |
| attname | text | Column name |
| most_common_vals | text | Most frequent values (anyarray cast to text) |
| most_common_freqs | text | Frequencies of most common values |
| histogram_bounds | text | Histogram bucket boundaries (anyarray cast to text) |

## Excluded columns

The following pg_stats columns are excluded even from this extended
collector because they apply only to array/composite types and add
disproportionate volume:

- most_common_elems
- most_common_elem_freqs
- elem_count_histogram

## Schema filter

Same as pg_stats_v1: excludes pg_catalog, information_schema,
pg_toast, pg_temp_%, pg_toast_temp_%.

## Invariants

- Deterministic ordering: ORDER BY schemaname, tablename, attname
- Empty result serializes as []
- Stable output column order (explicit SELECT, no SELECT *)
- Read-only query, passes linter
- Runs by default under R075 v2 (collect-everything default);
  skip-path classification means an operator opt-out via
  `signals.high_sensitivity_collectors_enabled: false` drops the
  collector entirely.
- When gated off, collector_status reports `reason=config_disabled`
  (EA-R001) so the operator's coverage gap is never silent.

## Configuration

- Category: schema
- Cadence: 24h (CadenceDaily)
- Retention: RetentionShort (sampled values should not persist long)
- Min PG version: 10
- **Enabled by default: yes** (R075 v2: collect-everything default).
  Operators opt out with
  `signals.high_sensitivity_collectors_enabled: false` (or
  `ARQ_SIGNALS_HIGH_SENSITIVITY_COLLECTORS_ENABLED=false`), which
  skips this collector entirely (skip-path).

## Sensitivity

**High.** Output contains actual data values sampled by the planner.
For example:

- `most_common_vals` for an `email` column would contain real email
  addresses
- `histogram_bounds` for a `salary` column would reveal salary
  distribution boundaries

### Mitigations

1. **Skip-path under operator privacy opt-out.** Runs by default
   (R075 v2: collect-everything default); an operator who needs to
   keep sampled values out of the artifact sets
   `signals.high_sensitivity_collectors_enabled: false` to drop the
   collector entirely (skip-path classification).
2. **All data stays local.** Elevarq Signals stores collected data only
   in the local SQLite database and export ZIPs. No external
   transmission occurs.
3. **Short retention.** The RetentionShort class means sampled values
   are cleaned up quickly (not retained for long-term analysis).
4. **Operator consent.** Enabling this collector is an explicit
   administrative decision, documented in configuration.

### When to enable

Enable this collector when:

- You need column-type exhaustion detection (integer PK approaching
  max value) and cannot determine high-water marks from sequences
  alone
- You want planner-quality-of-estimate analysis using real
  distribution data
- Your environment permits collection of sampled data values from
  PostgreSQL statistics

### When NOT to enable

Do not enable if:

- Your security policy prohibits collection of data samples from
  production databases
- Columns contain PII, PHI, or other regulated data that must not
  appear in diagnostic artifacts
- You are unsure whether sampled values in diagnostic data would
  violate your compliance requirements

## Analyzer use cases

When this collector is available, the Elevarq Analyzer can:

- **SE-GAP-01 (close):** Detect column-type exhaustion by parsing
  histogram_bounds to find high-water marks for integer columns,
  even when no sequence is associated
- **Planner quality:** Compare estimated vs. actual selectivity using
  real distribution data
- **Data skew detection:** Identify columns with extreme skew from
  most_common_vals/freqs ratios

## SQL query (planned)

```sql
SELECT
    schemaname,
    tablename,
    attname,
    most_common_vals::text,
    most_common_freqs::text,
    histogram_bounds::text
FROM pg_stats
WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
  AND schemaname NOT LIKE 'pg_temp_%'
  AND schemaname NOT LIKE 'pg_toast_temp_%'
ORDER BY schemaname, tablename, attname
```
