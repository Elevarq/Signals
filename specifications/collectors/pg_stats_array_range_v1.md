# pg_stats_array_range_v1 — Collector Specification

## Purpose

Per-element and per-range planner statistics for GIN/GIST cost-path
fidelity in the analyzer's embedded planner. Provides the
`pg_stats` slot kinds that `pg_stats_extended_v1` explicitly
excludes:

- `stakind=4` MCELEM — per-element MCV (arrays, tsvector, jsonb)
- `stakind=6` RANGE_LENGTH_HISTOGRAM — float8 length histogram for range types
- `stakind=7` RANGE_BOUNDS_HISTOGRAM — array-of-ranges bounds histogram

Tracking: [Elevarq/Arq-Signals#128](https://github.com/Elevarq/Arq-Signals/issues/128).

## Status

**Active.** Implemented in
`internal/pgqueries/catalog_schema.go`. Gated by TWO conditions:

1. **`HighSensitivityEnabled`** — the daemon-wide high-sensitivity
   safety floor (same as `pg_stats_extended_v1`). When false, this
   collector never runs.
2. **`CollectArrayRangeHistograms`** — a NEW per-collector opt-in
   layered on top of (1). Set both to true to enable.

The two-flag design is deliberate: array / tsvector / range MCV
data is materially MORE sensitive than the regular MCV columns
covered by `pg_stats_extended_v1` (tsvector tokens are real words
from indexed documents; range bounds reveal booking / timestamp
distributions). Operators who want regular histograms but not
this finer-grained data set `HighSensitivityCollectorsEnabled: true`
+ `CollectArrayRangeHistograms: false`.

## Relationship to pg_stats_v1 / pg_stats_extended_v1

| Collector | Content | Sensitivity | Default | Opt-in |
|---|---|---|---|---|
| `pg_stats_v1` | Numerical summaries | Low | Enabled | n/a |
| `pg_stats_extended_v1` | Standard MCV + histogram_bounds | High | Disabled | `HighSensitivityEnabled` |
| **`pg_stats_array_range_v1`** (this) | Per-element MCV + range histograms | **High+** | **Disabled** | `HighSensitivityEnabled` AND `CollectArrayRangeHistograms` |

All three read from `pg_stats`. The slot kinds for #128's collector
populate only for columns whose type permits them (`anyarray`,
`tsvector`, `jsonb`, range types) — most other columns return
empty for every slot.

## Catalog source

- `pg_stats` (system view over `pg_statistic`)

## Output columns

| Column | Type | Description |
|---|---|---|
| `schemaname` | text | Schema name |
| `tablename` | text | Table name |
| `attname` | text | Column name |
| `most_common_elems` | text | Per-element MCV (`anyarray` cast to text). `stakind=4` MCELEM. |
| `most_common_elem_freqs` | text | Frequencies of MCELEM (`real[]` cast to text) |
| `range_length_histogram` | text | `float8[]` length histogram for range types. `stakind=6`. PG 14+. |
| `range_bounds_histogram` | text | Array-of-ranges bounds histogram. `stakind=7`. PG 14+. |

`elem_count_histogram` is OUT OF SCOPE — PG's planner does not
consume it for GIN/GIST cost paths, and it duplicates information
already in `most_common_elem_freqs`.

## Schema filter

Same as `pg_stats_v1` / `pg_stats_extended_v1`: excludes
`pg_catalog`, `information_schema`, `pg_toast`, `pg_temp_%`,
`pg_toast_temp_%`.

## Invariants

- Deterministic ordering: `ORDER BY schemaname, tablename, attname`
- Empty result serializes as `[]`
- Stable output column order (explicit `SELECT`, no `SELECT *`)
- Read-only query, passes the existing collector linter, no
  superuser requirement
- Disabled by default — requires BOTH `HighSensitivityEnabled` and
  `CollectArrayRangeHistograms`
- When disabled, `collector_status` reports
  `reason=config_disabled` (same shape as `pg_stats_extended_v1`)

## Configuration

- Category: schema
- Cadence: `CadenceDaily` (24h)
- Retention: `RetentionShort` (sampled values should not persist)
- Min PG version: 14 (the `range_*_histogram` columns are PG 14+
  on `pg_stats`)
- **Enabled by default: NO**
- Config key: `signals.collect_array_range_histograms: true`
- Env override: `ARQ_SIGNALS_COLLECT_ARRAY_RANGE_HISTOGRAMS=true`

## Sensitivity

**High+.** Output contains actual indexed content values:

- `most_common_elems` for a tsvector column over documents reveals
  the top tokens — real words from the indexed text.
- `most_common_elems` for a `text[]` column reveals real customer
  values (tags, categories, list entries).
- `range_bounds_histogram` for a booking-period column reveals
  the distribution of actual booking dates.

### Mitigations

Same defence in depth as `pg_stats_extended_v1`, plus the
per-collector opt-in:

1. **Disabled by default.**
2. **Two-gate opt-in** (daemon-wide high-sensitivity floor AND
   per-collector flag).
3. **Local storage only** (SQLite + export ZIPs; never external
   transmission).
4. **Short retention** (RetentionShort class).
5. **Explicit operator opt-in** documented in configuration.

## Analyzer-side consumption

The analyzer consumes the four output columns to account for
array / range element statistics (`stakind=4 / 6 / 7` slots in
`pg_statistic`) when reasoning about planner cost paths for GIN / GiST
index advice.

No further analyzer change is needed for this collector to take
effect on analyzer runs.

## Failure conditions

- PG major < 14 → collector skipped with
  `reason=pg_version_unsupported`
- `HighSensitivityEnabled=false` → skipped with
  `reason=config_disabled` (per the daemon-wide floor)
- `CollectArrayRangeHistograms=false` → skipped with
  `reason=config_disabled` (per the per-collector flag)
- Read error → `reason=collector_query_timeout` or
  `reason=role_insufficient` per the closed taxonomy in
  `docs/observability/operational-readiness.md`

## Non-functional requirements

- **Performance**: query reads the `pg_stats` system view; cost is
  proportional to the number of columns with array / range type
  that have been ANALYZE'd. Bounded by the daily cadence.
- **Compatibility**: PG 14, 15, 16, 17, 18. PG 19 inherits the PG
  18 catalog (experimental).
- **Security**: read-only; no `pg_monitor` membership required;
  same role permission floor as `pg_stats_v1`.

## SQL query

```sql
SELECT
    schemaname,
    tablename,
    attname,
    most_common_elems::text       AS most_common_elems,
    most_common_elem_freqs::text  AS most_common_elem_freqs,
    range_length_histogram::text  AS range_length_histogram,
    range_bounds_histogram::text  AS range_bounds_histogram
FROM pg_stats
WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
  AND schemaname NOT LIKE 'pg_temp_%'
  AND schemaname NOT LIKE 'pg_toast_temp_%'
ORDER BY schemaname, tablename, attname
```

## Acceptance criteria

- [x] Spec at `specifications/collectors/pg_stats_array_range_v1.md`
- [x] Collector registered under `HighSensitivity=true` and the new
  config flag
- [x] When disabled: `collector_status` reports
  `reason=config_disabled`
- [x] When enabled on an empty database: emits an empty array
  (deterministic ordering by schema/table/column)
- [x] Read-only query, passes the collector linter
- [x] Sensitivity registered as `high` in the catalogue
- [x] Documented in the user-facing config reference

## References

- [Elevarq/Arq-Signals#128](https://github.com/Elevarq/Arq-Signals/issues/128) — this collector
- `pg_stats_extended_v1.md` — sibling collector for standard MCV / histogram_bounds
