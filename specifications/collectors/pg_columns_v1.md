# pg_columns_v1 — Collector Specification

## Purpose

Column inventory with data types for all user-schema tables, views,
materialized views, and foreign tables. Uses PostgreSQL-native catalog
tables (pg_attribute, pg_type, pg_attrdef) with format_type() for
human-readable type names.

## Catalog source

- pg_attribute (column metadata)
- pg_class (relation identity and kind)
- pg_namespace (schema name)
- pg_attrdef (default existence check)
- format_type(atttypid, atttypmod) for type name with modifiers

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | Schema name (pg_namespace.nspname) |
| relname | text | Table/view name (pg_class.relname) |
| attname | text | Column name |
| attnum | int | 1-based column position |
| typname | text | Full type with modifiers (e.g., "numeric(10,2)") |
| is_nullable | bool | true if column allows NULL |
| has_default | bool | true if column has a default expression |
| attlen | int | Storage size in bytes (-1 for variable-length) |

## Excluded data

- Default expression text (pg_get_expr(adbin, adrelid)) — may
  contain sensitive literal values. Only the boolean has_default
  is emitted.
- System columns (attnum <= 0)
- Dropped columns (attisdropped = true)

## Relation kinds included

- r: regular table
- p: partitioned table
- v: view
- m: materialized view
- f: foreign table

## Schema filter

Excludes pg_catalog, information_schema, pg_toast, pg_temp_%,
pg_toast_temp_%.

## Invariants

- Deterministic ordering: ORDER BY schemaname, relname, attnum
- Empty result serializes as []
- Stable output column order (explicit SELECT, no SELECT *)
- Read-only query, passes linter
- No default expression text in output

## Configuration

- Category: schema
- Cadence: 24h (CadenceDaily)
- Retention: RetentionMedium
- Min PG version: 10
- Enabled by default: yes

## Sensitivity

Low. Column names and data types are structural metadata. has_default
is a boolean, not the expression itself.

## Analyzer use cases

- Schema-aware LLM reasoning (column types inform query analysis)
- Cross-snapshot drift detection (column added/removed/type-changed)
- Schema documentation
