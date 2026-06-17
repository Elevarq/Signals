# bloat_estimate_v1 — Collector Specification

## Status

ACTIVE

## Purpose

Table bloat is the most-requested PostgreSQL operational insight
and the one every analyzer surfaces. The accurate path requires
the `pgstattuple` extension, which is **not** installed on most
managed-PG services (RDS / Aurora / Cloud SQL / AlloyDB / Azure
Flexible Server). The pragmatic path is a statistical estimate
derived from `pg_class.reltuples`, `pg_stats.avg_width`, and the
fixed page-header overhead — close enough to answer
"is this table 50 % bloat or 5 %?" with zero extension
dependencies.

Signals already collects the raw inputs (`pg_class_storage_v1`,
`pg_stats_v1`, `pg_stat_user_tables_v1`); this collector
centralises the canonical derivation so consumers ingest the
operator-facing answer instead of re-deriving the formula in
every downstream tool.

## Catalog source

`pg_class` ⋈ `pg_namespace` ⋈ `pg_stats` (aggregated per relation
for `SUM(avg_width)`) ⋈ `pg_stat_user_tables` (LEFT JOIN —
unanalyzed tables don't appear). `current_setting('block_size')`
provides the page size (handles non-default 4 KB / 16 KB
compile-time configurations).

## Output columns

One row per non-system, table-shaped relation (`relkind IN ('r',
'm', 'p')` — ordinary tables, materialised views, partitioned
parents).

| Column | Type | Description |
|---|---|---|
| `schemaname` | text | Relation schema. |
| `tablename` | text | Relation name. |
| `table_oid` | oid | `pg_class.oid`. |
| `relkind` | char | `r` (table), `m` (matview), `p` (partitioned table). |
| `actual_size_bytes` | bigint | `pg_relation_size(table_oid)`. |
| `expected_size_bytes` | bigint | Statistical estimate (see formula below). NULL when stats are missing. |
| `bloat_bytes` | bigint | `GREATEST(actual_size_bytes - expected_size_bytes, 0)`. 0 when estimate is NULL or estimate exceeds actual. |
| `bloat_ratio` | numeric(5,3) | `bloat_bytes / NULLIF(actual_size_bytes, 0)`, range `0.000 … 1.000`. NULL when stats are missing or table is empty. |
| `reltuples` | bigint | `pg_class.reltuples` (planner estimate, refreshed by ANALYZE). |
| `n_live_tup` | bigint | From `pg_stat_user_tables`; NULL when no stats row. |
| `n_dead_tup` | bigint | From `pg_stat_user_tables`; NULL when no stats row. Independent signal that corroborates the estimate. |
| `last_autovacuum` | timestamptz | When the last autovacuum ran. NULL when never. |
| `stats_missing` | boolean | TRUE when `pg_stats` has no rows for this relation (never analyzed → no estimate possible). |

### Estimation formula

```text
expected_size_bytes ≈
    CEIL(
        reltuples * (TUPLE_HDR + NULL_BMP + SUM(avg_width) + ALIGN_PAD)
        / GREATEST(block_size - PAGE_HDR, 1)
    ) * block_size

where:
    TUPLE_HDR  = 23   bytes — HeapTupleHeader fixed overhead
    NULL_BMP   = 4    bytes — average null-bitmap footprint
    ALIGN_PAD  = 8    bytes — average MAXALIGN padding
    PAGE_HDR   = 24   bytes — PageHeaderData
    block_size = current_setting('block_size')::numeric
```

This is the canonical formula from the
[PG wiki "Show Database Bloat"](https://wiki.postgresql.org/wiki/Show_database_bloat)
query (and its derivatives in check_postgres, pgBadger, etc.) —
simplified to the parts that matter on modern PG and pinned to
`current_setting('block_size')` so non-standard 4 KB / 16 KB
clusters get the right denominator.

The formula is **deliberately** statistical, not exact. Operators
who need exact tuple-level accounting install `pgstattuple` and
use a future `bloat_exact_v1` collector (out of scope here).

## Scope filter

System schemas are excluded:

- `pg_catalog`
- `information_schema`
- `pg_toast`
- `pg_temp_%`
- `pg_toast_temp_%`

Non-table relkinds (`v` views, `i` indexes, `S` sequences,
`f` foreign tables, `c` composite types, `t` TOAST tables) are
excluded — `relkind IN ('r','m','p')` is the only positive
filter. Partitioned-parent rows (`relkind = 'p'`) have
`pg_relation_size` of 0 (storage lives in children); they appear
in output with `actual_size_bytes = 0`, `bloat_bytes = 0`,
`bloat_ratio = NULL`. This is documented behaviour, not a bug —
downstream consumers filter partitioned parents if they want
leaf-only bloat.

## Invariants

- One row per qualifying relation — no aggregation.
- `bloat_ratio ∈ [0.000, 1.000]` or NULL. Floored at 0 when the
  estimate exceeds actual (over-estimation does not produce
  negative bloat).
- `stats_missing = TRUE` ↔ `expected_size_bytes IS NULL` ↔
  `bloat_ratio IS NULL`. The three values move together.
- Read-only — single SELECT against catalog + stats views.
- Passes the linter.
- No dependency on the `pgstattuple` extension.

## Failure Conditions

- **FC-01**: Relation has never been ANALYZED (no `pg_stats`
  rows) → `stats_missing = TRUE`, estimate columns NULL,
  `bloat_bytes = 0`. Not a collector error; the analyzer surfaces
  it as "ANALYZE this table to enable bloat estimation".
- **FC-02**: Empty relation (`reltuples = 0`) → estimate = 0,
  `bloat_bytes = 0`, `bloat_ratio = NULL`. No false positives.
- **FC-03**: Query timeout on a catalog with tens of thousands of
  tables → standard collector-timeout path; row count is
  naturally bounded.
- **FC-04**: Estimate diverges from `pgstattuple` reality by more
  than ~20 % on some workloads (variable column widths,
  fillfactor != 100, heavily-toasted rows). Documented limitation;
  the analyzer should weight `n_dead_tup` and `last_autovacuum`
  alongside the estimate, not treat the ratio as gospel.

## Configuration

- Category: `tables`
- Cadence: `Cadence6h`
- Retention: `RetentionMedium`
- Timeout: 30 seconds
- Min PG version: none (works on every Signals-supported major;
  the formula uses columns stable across PG 14..18)
- Requires extension: none
- Semantics: snapshot (point-in-time view of every table +
  derived estimate)
- Enabled by default: yes
- Sensitivity: `medium` — schema / table names; same surface as
  `largest_relations_v1` and `pg_class_storage_v1` which
  operators already accept.

## Sensitivity

Medium — schema identifiers and integer counters. No values, no
SQL text, no PII.

## Analyzer requirements unblocked

- **Direct "VACUUM FULL / pg_repack X to recover Y MiB" recommendation**:
  a row with `bloat_bytes > 100 MiB` is actionable verbatim. The
  analyzer pairs it with `last_autovacuum` to recommend
  autovacuum tuning vs a one-shot pg_repack.
- **Bloat-growth trend**: cross-snapshot `bloat_bytes` delta
  identifies tables that bloat fast — typically autovacuum is
  starved by long-running transactions (correlate with
  `idle_in_txn_offenders_v1`) or autovacuum scale-factor is too
  loose.
- **Top-N triage**: sort by `bloat_bytes DESC` to land the
  operator's "drop the most-bloated five" action list.
- **Recovery-target sizing**: `bloat_bytes` summed across a
  schema feeds capacity-recovery estimates.

## Operator value (standalone)

A future `signalsctl doctor` check (C8-style) can surface the top N
bloated tables without analyzer integration. Out of scope for
R104 itself — covered as a follow-up.

## Known constraints

- **Statistical, not exact.** The formula assumes uniform row
  layout and standard fillfactor. Tables with heavy TOAST
  pressure, very wide variable-width columns, or non-default
  fillfactor will see ratios drift from `pgstattuple`'s exact
  answer. The spec acknowledges this and points operators who
  need precision at a future `bloat_exact_v1` collector
  (`pgstattuple` extension required).
- **Partitioned parents** report 0 bytes for both actual and
  expected (storage lives in children). Downstream consumers
  filter `relkind = 'p'` if they want leaf-only bloat.
- **Materialised views** are included (`relkind = 'm'`) because
  they bloat exactly like ordinary tables and operators expect
  to see them in this report.

## Out of scope

- Per-index bloat. A sibling `index_bloat_estimate_v1` collector
  is filed as a follow-up — same statistical approach but
  against `pg_index` + `pg_stats` per indexed column. Keeping it
  out of R104 keeps the v1 PR reviewable and lets the index
  variant land independently.
- Exact bloat via `pgstattuple`. A future `bloat_exact_v1`
  collector, gated by extension presence via the standard
  EA-R001 channel, will provide it. Operators on managed PG who
  can't install `pgstattuple` keep this collector as their only
  bloat surface.
- Configurable thresholds for "large bloat" — analyzer-side
  decision; this collector is a passive pass-through
  (INV-SIGNALS-01).
