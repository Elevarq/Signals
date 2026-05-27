# index_health_summary_v1 — Collector Specification

## Status

ACTIVE

## Purpose

PostgreSQL surfaces enough raw catalog (`pg_index`,
`pg_stat_user_indexes`, `pg_attribute`) to derive the four
canonical index-hygiene findings, but every analyzer that wants
them reinvents the SQL. This collector centralises the derivation
into a single Signals-side summary so the analyzer ingests
already-classified rows and operators get the same view via
`arqctl` / Workbench.

Index hygiene is the highest-leverage "improve your PostgreSQL
database" recommendation a tool can make — every cluster has
`DROP INDEX … to recover N% disk` findings waiting to be surfaced.

## Catalog source

`pg_index` ⋈ `pg_class` (relation + index) ⋈ `pg_namespace` ⋈
`pg_attribute` (for column names) ⋈ `pg_stat_user_indexes`
(LEFT JOIN — system relations don't appear in `_user_` views).

## Output columns

One row per non-system index in the connected database.

| Column | Type | Description |
|---|---|---|
| `schemaname` | text | Index schema. |
| `tablename` | text | Owning relation name. |
| `indexname` | text | Index name. |
| `index_oid` | oid | `pg_index.indexrelid`. |
| `size_bytes` | bigint | `pg_relation_size(indexrelid)`. |
| `idx_scan` | bigint | Cumulative scan count from `pg_stat_user_indexes`. NULL if the index has no stats entry. |
| `idx_tup_read` | bigint | Cumulative tuple-read count. NULL when no stats entry. |
| `is_unique` | boolean | `pg_index.indisunique`. |
| `is_primary` | boolean | `pg_index.indisprimary`. |
| `is_valid` | boolean | `pg_index.indisvalid`. |
| `is_ready` | boolean | `pg_index.indisready`. |
| `column_set` | text[] | Column names in index-definition order. NULL for fully-expression indexes (`indkey[i] = 0` for all columns); partial when some columns are expressions. |
| `duplicate_of` | text | When `duplicate` is tagged: name of the lower-OID index on the same table with identical `column_set`. NULL otherwise. |
| `redundant_with` | text | When `redundant` is tagged: name of a larger index on the same table whose `column_set` strictly extends this one as a left-prefix. NULL otherwise. |
| `health_findings` | text[] | Subset of `{unused, large_unused, invalid, not_ready, redundant, duplicate}`. Empty array means "healthy". |

### Classification rules

- **`unused`**: `idx_scan = 0` AND NOT `is_unique` AND NOT
  `is_primary`. Unique / primary indexes have correctness duties
  beyond scan counts and are never tagged unused.
- **`large_unused`**: `unused` AND `size_bytes > 104_857_600`
  (100 MiB). v1 threshold is a built-in constant; a configurable
  threshold is a follow-up.
- **`invalid`**: `is_valid = false`.
- **`not_ready`**: `is_ready = false`.
- **`duplicate`**: there exists another index on the same
  `(schemaname, tablename)` with an identical non-NULL
  `column_set` and a lower `index_oid`. The lower-OID canonical
  index is itself **not** tagged `duplicate` — only the followers
  are. `duplicate_of` carries the canonical's name.
- **`redundant`**: there exists another index on the same
  `(schemaname, tablename)` whose `column_set` strictly extends
  this one as a left-prefix (i.e. this index's columns are
  `larger.column_set[1 : length(this.column_set)]`). `redundant_with`
  carries the larger index's name.

Multiple tags allowed. An unused 500 MiB index produces
`{unused, large_unused}`.

## Scope filter

System schemas are excluded:

- `pg_catalog`
- `information_schema`
- `pg_toast`
- `pg_temp_%`
- `pg_toast_temp_%`

This honours INV-SIGNALS-12 (system-schema exclusion).

## Invariants

- One row per surviving index — no aggregation across indexes.
- `health_findings` is **always** an array, never NULL. Empty
  array means "no findings".
- `duplicate_of` and `redundant_with` always reference a name
  that exists in the result set's `(schemaname, tablename)`
  group — no dangling pointers.
- `column_set` ordering matches the index definition, not
  alphabetical.
- Read-only — single `SELECT` against catalog + stats views, no
  joins outside `pg_catalog`'s purview.
- Passes the linter.

## Failure Conditions

- **FC-01**: System index in `pg_catalog` has
  `indisvalid = false` — the collector excludes system schemas
  from its scope so this surfaces only when the operator stored
  user data inside `pg_catalog` (unsupported). Not a collector
  error.
- **FC-02**: Query exceeds `Timeout: 30s` on a catalog with tens
  of thousands of indexes → standard collector-timeout path. The
  row count is naturally bounded by the database's index count;
  catalogs with > 10k user indexes are pathological.

## Configuration

- Category: `indexes`
- Cadence: `Cadence6h`
- Retention: `RetentionMedium`
- Timeout: 30 seconds
- Min PG version: none (works on every Signals-supported major,
  PG 14+ via the daemon's window).
- Requires extension: none
- Semantics: snapshot (point-in-time view of every index +
  derived findings)
- Enabled by default: yes
- Sensitivity: `medium` — index names, table names, and column
  names are part of the schema model. Already exposed by
  `pg_indexes_v1` and `pg_stats_v1`. No values, no query text.

## Sensitivity

Medium — schema identifiers are emitted; no values, no SQL text,
no PII. Column names and table names mirror the existing
`pg_indexes_v1` and `pg_stats_v1` collectors. Operators who run
those collectors today already accept this surface.

## Analyzer requirements unblocked

- **Direct "drop X to recover Y MiB" recommendation**: row
  carrying `large_unused` tag is actionable verbatim.
- **Quarter-over-quarter trend**: `idx_scan = 0` across N
  consecutive 6h cycles → high-confidence drop candidate.
- **Statistics-rot detection**: `idx_scan` flat while owning
  table sees heavy `seq_scan` traffic indicates the planner
  isn't using the index (statistics or query-pattern shift).
- **Schema-cleanup batches**: duplicate / redundant findings
  cluster by table; analyzer can group them into a single
  recommendation per table.

## Operator value (standalone)

A future `arqctl doctor` check (C7-style) can surface the top N
`large_unused` and `duplicate` findings without the analyzer in
the loop. Out of scope for R103 itself — covered as a follow-up
in the issue tracker.

## Known constraints

- **Expression indexes**: an index column on an expression has
  `pg_index.indkey[i] = 0` — there's no `pg_attribute` row to
  resolve to a column name. The collector emits NULL for
  `column_set` when **every** indexed column is an expression;
  for mixed indexes the array carries the resolvable names and
  the expression positions are absent. Duplicate / redundant
  detection then never fires on those indexes (NULL never
  matches NULL). This is documented behaviour, not a bug — full
  expression-index analysis would need
  `pg_get_indexdef(indexrelid, k, true)` per column and is
  out of scope for v1.
- **Per-column `pg_relation_size` cost**: every index calls
  `pg_relation_size(indexrelid)` once. Bounded by index count;
  catalogs in the typical operator-managed range (< 5k indexes)
  complete in well under the 30-second budget.

## Out of scope

- Per-index access-pattern stats (`pg_statio_user_indexes`) —
  separate collector already exists.
- `pg_stat_user_indexes` cross-snapshot delta — analyzer-side.
- Partial-index predicate analysis — would require parsing the
  predicate, out of scope for v1.
- Configurable `large_unused` threshold — built-in 100 MiB for
  v1, configurable in a follow-up.
