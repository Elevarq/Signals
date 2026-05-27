# pg_statistic_ext_v1 — Collector Specification

## Purpose

Inventory of multi-column extended-statistics objects
(`CREATE STATISTICS …`) on the target. A downstream
CREATE STATISTICS advisor needs to know which
extended-stats objects already exist so it never recommends a
duplicate.

The collector emits **catalog metadata only** — names, target
tables, attribute numbers, and statistic kinds. It does NOT
read `pg_statistic_ext_data` (the actual sampled per-stat
data), which has owner-only visibility post-PG12.

## Relationship to other stats collectors

| Collector | Source | Scope | Purpose |
|---|---|---|---|
| `pg_stats_v1` | `pg_stats` view | per-column summaries | n_distinct, correlation, null_frac, avg_width |
| `pg_attribute_storage_v1` | `pg_attribute` | per-column config | storage mode, compression, **attstattarget** |
| `pg_stats_extended_v1` | `pg_stats` view (high-sens) | per-column samples | MCV, histograms |
| `pg_statistic_ext_v1` | `pg_statistic_ext` catalog | per-table objects | which CREATE STATISTICS exist |

The four collectors cover different cross-sections of the
statistics surface; none overlaps.

## Catalog source

- `pg_statistic_ext` joined with `pg_class` and `pg_namespace`.

## Output columns

| Column | Type | Description |
|---|---|---|
| stat_schema | text | Schema the statistics object lives in. |
| stat_name | text | The `stxname` (operator-supplied object name, or PG-auto-generated). |
| table_schema | text | Schema of the target relation. |
| table_name | text | The relation the stats object is attached to. |
| attnums | int[] | `stxkeys` — array of attribute numbers the object covers. |
| kinds | text[] | `stxkind` — array of single-char codes for which statistic kinds were declared: `d` (functional dependencies), `f` (MCV list, PG12+), `m` (ndistinct, PG10+), `e` (expression, PG14+). |

## Scope filter

- The **target** relation's schema must NOT be in
  `pg_catalog`, `information_schema`, `pg_toast`,
  `pg_temp_*`, `pg_toast_temp_*`.
- The statistics object's own schema is NOT filtered — an
  operator may keep their stats objects in a schema they
  control while the target table sits elsewhere.

## Invariants

- Deterministic ordering:
  `ORDER BY table_schema, table_name, stat_name`.
- Stable output column order.
- Read-only query.

## Failure conditions

- FC-01: Permission denied on `pg_statistic_ext` → standard
  collector error path. With `pg_monitor` membership (or any
  role with basic catalog access) this should not occur.
- FC-02: Empty rowset (no extended-statistics objects defined
  on the target) → success with zero rows. Most customers
  fall in this state; the empty-but-present-collector signal
  itself is information the analyzer uses.

## Configuration

- Category: schema
- Cadence: 24h (CadenceDaily)
- Retention: RetentionMedium
- Min PG version: 14 (the `e` kind was added then; PG ≥ 10
  has `d` and `m`, ≥ 12 has `f` — the PG14 gate keeps the
  collector definition uniform with `pg_attribute_storage_v1`
  and `pg_columns_v1`. On older PGs `e` would simply not
  appear in any row's `kinds` array.)
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Low. The metadata exposed here is the DDL the operator
already wrote — names + target tables + kind codes. The
actual sampled values (`pg_statistic_ext_data.stxdndistinct`,
`.stxddependencies`, etc.) are NOT collected; those have
owner-only visibility post-PG12 and would require either
superuser or function-level GRANT to read.

`pg_monitor` membership is sufficient for the query as
written. No additional grants required.

## Analyzer requirements unblocked

- `stats.create_statistics_candidate.v1` —
  CREATE STATISTICS advisor uses this collector as its
  "what already exists" guard: never recommend an object
  with the same `(table, attnums)` as one already present.
