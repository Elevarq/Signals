# index_bloat_estimate_v1 — Collector Specification

## Status

ACTIVE

## Purpose

Index bloat is the second half of the canonical "drop X to recover
Y MiB" recommendation. R104 / `bloat_estimate_v1` covers tables;
this collector covers indexes using the same statistical approach
so operators on managed PG (RDS / Aurora / Cloud SQL / AlloyDB /
Azure Flexible Server) — where the exact-path `pgstattuple` /
`pgstatindex` extension is unavailable — get a workable index-
bloat surface out of the box.

Pairs naturally with R103 / `index_health_summary_v1`: a bloated
index that's also tagged `unused` is the highest-priority drop
candidate the analyzer can surface.

## Catalog source

`pg_index` ⋈ `pg_class` (relation + index) ⋈ `pg_namespace` ⋈
`pg_attribute` (for indexed column names) ⋈ `pg_stats`
(aggregated `SUM(avg_width)` over the indexed columns).
`current_setting('block_size')` provides the page size.

## Output columns

One row per non-system index (`relkind = 'i'` for ordinary
indexes; `relkind = 'I'` partitioned-index parents are included).

| Column | Type | Description |
|---|---|---|
| `schemaname` | text | Index schema. |
| `tablename` | text | Underlying table name. |
| `indexname` | text | Index name. |
| `index_oid` | oid | `pg_index.indexrelid`. |
| `relkind` | char | `i` (ordinary index) or `I` (partitioned index parent). |
| `actual_size_bytes` | bigint | `pg_relation_size(index_oid)`. |
| `expected_size_bytes` | bigint | Statistical estimate (formula below). NULL when stats are missing or all indexed columns are expressions. |
| `bloat_bytes` | bigint | `GREATEST(actual_size_bytes - expected_size_bytes, 0)`. 0 when estimate is NULL or estimate exceeds actual. |
| `bloat_ratio` | numeric(5,3) | `bloat_bytes / NULLIF(actual_size_bytes, 0)`, range `0.000 … 1.000`. NULL when estimate is NULL or index is empty. |
| `reltuples` | bigint | `pg_class.reltuples` for the **index** (planner estimate, refreshed by ANALYZE / REINDEX). |
| `is_unique` | boolean | `pg_index.indisunique`. Unique indexes can't be dropped — REINDEX is the only knob. |
| `is_primary` | boolean | `pg_index.indisprimary`. Same constraint as unique. |
| `stats_missing` | boolean | TRUE when the index has no resolvable column widths (expression-only index, or underlying table never analyzed). |

### Estimation formula

```text
expected_size_bytes ≈
    CEIL(
        index_reltuples * (avg_indexed_width + INDEX_TUPLE_HDR + ITEM_PTR)
        / GREATEST(block_size - PAGE_HDR, 1)
    ) * block_size

where:
    avg_indexed_width  = SUM(pg_stats.avg_width)  over key columns
    INDEX_TUPLE_HDR    = 8   bytes — IndexTupleData header
    ITEM_PTR           = 4   bytes — per-tuple ItemIdData (line pointer)
    PAGE_HDR           = 24  bytes — PageHeaderData
    block_size         = current_setting('block_size')::numeric
```

The formula is the canonical PG-wiki derivation for index bloat,
simplified for modern PG. Constants differ from
`bloat_estimate_v1` because index tuples carry a smaller header
(no transaction visibility fields — that's the heap's job) and
have a per-tuple line-pointer slot on every page.

The estimate is **deliberately** statistical, not exact.
Operators who need precision install `pgstattuple` and use a
future `index_bloat_exact_v1` collector (out of scope; gated via
EA-R001 when added).

## Scope filter

- `pg_index.indexrelid` resolves a `pg_class` row with
  `relkind IN ('i', 'I')`.
- System schemas excluded (INV-SIGNALS-12):
  `pg_catalog`, `information_schema`, `pg_toast`, `pg_temp_%`,
  `pg_toast_temp_%`.

Column width is summed across `pg_index.indkey[ord]` where
`ord <= indnkeyatts` (so INCLUDE columns are **not** counted —
they don't contribute to key search width).

## Invariants

- One row per surviving index — no aggregation.
- `bloat_ratio ∈ [0.000, 1.000]` or NULL — floored at 0.
- `stats_missing = TRUE` ↔ `expected_size_bytes IS NULL` ↔
  `bloat_ratio IS NULL`. The three move together.
- Expression indexes (`pg_index.indkey[i] = 0`) cannot be sized
  by the formula — those columns' widths aren't in `pg_stats`.
  Mixed indexes (some keys resolve, some are expressions) treat
  the expression positions as unknown and emit
  `stats_missing = TRUE` to avoid biasing the estimate downward.
- Partitioned-index parents (`relkind = 'I'`) have
  `pg_relation_size = 0` (storage lives in leaf partitions) and
  surface with `bloat_bytes = 0`, `bloat_ratio = NULL`. Documented
  behaviour, not a bug.
- Read-only — single SELECT against catalog + stats views.
- Passes the linter.
- No `pgstattuple` / `pgstatindex` dependency.

## Failure Conditions

- **FC-01**: Underlying table has never been ANALYZED →
  `pg_stats` has no rows for it → `stats_missing = TRUE`,
  estimate NULL. Surfaced for the analyzer to recommend ANALYZE.
- **FC-02**: Index has all key columns as expressions →
  `stats_missing = TRUE`. Documented limitation; a future
  variant could parse `pg_index.indexprs` widths but is out of
  scope for v1.
- **FC-03**: Empty index (`reltuples = 0`) → estimate = 0,
  `bloat_bytes = 0`, `bloat_ratio = NULL`. No false positives.
- **FC-04**: Query timeout on a catalog with tens of thousands
  of indexes → standard collector-timeout path.

## Configuration

- Category: `indexes`
- Cadence: `Cadence6h`
- Retention: `RetentionMedium`
- Timeout: 30 seconds
- Min PG version: none (works on every Signals-supported major,
  PG 14+ via the daemon's window)
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes
- Sensitivity: `medium` — same surface as
  `index_health_summary_v1` and `pg_indexes_v1`: schema names,
  table names, index names. No values, no SQL text.

## Sensitivity

Medium. Identical surface to existing index-shape collectors.

## Analyzer requirements unblocked

- **Direct "REINDEX X to recover Y MiB"** recommendation: a row
  with `bloat_bytes > 100 MiB` is actionable verbatim.
- **Bloat-growth on unique / primary indexes**: those can't be
  dropped, so REINDEX is the only knob. Cross-snapshot
  `bloat_bytes` delta tells operators when to schedule
  `REINDEX INDEX CONCURRENTLY`.
- **Drop-candidate prioritisation** when combined with R103:
  `is_unique = false` AND `idx_scan = 0` (from
  `index_health_summary_v1.health_findings` containing `unused`)
  AND `bloat_bytes > 100 MiB` → drop, do not REINDEX.
- **Recovery-target sizing**: schema-level sums.

## Known constraints

- **Statistical, not exact.** Index tuple layout varies by index
  type (btree vs gin vs gist vs brin); the formula is calibrated
  for btree and accepts ~20 % drift on the other access methods.
  Operators who need precision use `pgstatindex`.
- **Expression indexes** are out of scope for v1 — the formula
  needs `avg_width` per key column, which expressions don't have
  in `pg_stats`. Filed limitation, not a defect.
- **INCLUDE columns** (covering indexes, PG 11+) are
  deliberately excluded from the width sum — they extend the
  tuple footprint, but the formula uses key columns only to
  match the PG-wiki convention. Result: a small under-estimate
  on covering indexes. Documented; acceptable for the v1 surface.

## Out of scope

- Exact bloat via `pgstattuple` / `pgstatindex` — separate
  collector, EA-R001-gated.
- Per-access-method (gin / gist / brin / hash) per-method
  formulas — v1 calibrates for btree and accepts drift on the
  rarer types.
- Expression-index width estimation — needs `indexprs` parsing
  or per-expression `EXPLAIN`-based width sampling; out of
  scope for v1.
- Configurable thresholds for "large bloat" — analyzer-side
  decision; this collector is a passive pass-through
  (INV-SIGNALS-01).
