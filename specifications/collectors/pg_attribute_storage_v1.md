# pg_attribute_storage_v1 — Collector Specification

## Purpose

Per-column storage configuration: `attstorage` (PLAIN / EXTERNAL /
EXTENDED / MAIN), `attcompression` (PG14+), average stored width.
Needed to identify TOAST-able columns, candidates for
`SET STORAGE EXTERNAL` on high-entropy data (vectors, encrypted
payloads), and candidates for `SET COMPRESSION lz4` on compressible
columns.

## Catalog source

- `pg_attribute` joined with `pg_class`, `pg_namespace`, `pg_type`,
  and `pg_stats` (for `avg_width`)

## Output columns

| Column | Type | Description |
|---|---|---|
| relid | oid | Relation OID |
| schemaname | text | Schema |
| relname | text | Relation name |
| attnum | smallint | Column ordinal |
| attname | text | Column name |
| atttypid | oid | Type OID |
| atttypname | text | Type name (e.g. `text`, `jsonb`, `vector`) |
| attstorage | char | `p` (PLAIN), `e` (EXTERNAL), `x` (EXTENDED), `m` (MAIN) |
| attcompression | char | `\0`, `p` (pglz), `l` (lz4); PG14+ |
| atttypmod | int | Type-specific modifier (e.g. vector dimension) |
| attnotnull | boolean | NOT NULL constraint |
| attstattarget | int | Per-column statistics target override. `-1` (PG ≤ 17) or `NULL` (PG ≥ 18) means the column uses `default_statistics_target`; non-negative values are operator-applied overrides set via `ALTER TABLE ... ALTER COLUMN ... SET STATISTICS N`. |
| avg_width | int | Average stored width in bytes (from pg_stats; NULL if no stats) |

## Scope filter

- `attnum > 0` (excludes system columns `xmin`, `ctid`, etc.)
- `attisdropped = false`
- Excludes `pg_catalog`, `information_schema`, `pg_toast`,
  `pg_temp_%`, `pg_toast_temp_%`.

## Invariants

- Deterministic ordering: `ORDER BY schemaname, relname, attnum`.
- Stable output column order.
- Read-only query, passes linter.
- `attcompression` emitted as NULL on PG < 14 (field does not exist).

## Failure Conditions

- FC-01: Role cannot read `pg_stats` for some relations (owner-only
  visibility) → `avg_width` serialized as NULL for those rows; does
  not fail the collector.
- FC-02: Permission denied on `pg_attribute` (unusual) → standard
  collector error path.

## Configuration

- Category: configuration
- Cadence: 24h (CadenceDaily)
- Retention: RetentionMedium
- Min PG version: 14 (required by `attcompression`)
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Low. Column definitions are visible to any connected role with
`USAGE` on the schema.

## Analyzer requirements unblocked

- `toast-planner-blindspot` — attribute-level identification of the
  high-entropy columns driving TOAST pressure.
- `vector-column-storage` overlay — combined with `pg_vector_columns_v1`.
- Storage-parameter advice — candidates for `SET COMPRESSION lz4` or
  `SET STORAGE EXTERNAL`.
- `stats.target_too_low.v1` — recommend
  `ALTER COLUMN ... SET STATISTICS N` when `attstattarget` indicates
  the column uses the cluster-wide default and per-column distribution
  suggests the default sample size is too small.
