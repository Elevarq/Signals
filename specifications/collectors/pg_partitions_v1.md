# pg_partitions_v1 — Collector Specification

## Purpose

Partition strategy and parent/child relationships for all user-schema
partitioned tables. Provides partition key definition and per-child
bound expressions for partition topology analysis.

## Catalog source

- pg_partitioned_table (partition strategy)
- pg_class (parent and child relation identity)
- pg_namespace (schema names)
- pg_inherits (parent → child relationship)
- pg_get_partkeydef() (partition key expression)
- pg_get_expr(relpartbound, oid) (child bound expression)

## Output columns

| Column | Type | Description |
|---|---|---|
| parent_schema | text | Schema of the partitioned parent |
| parent_name | text | Partitioned parent table name |
| partition_strategy | char | r=range, l=list, h=hash |
| partition_key | text | Partition key expression |
| child_schema | text | Schema of the child partition (empty if no children) |
| child_name | text | Child partition name (empty if no children) |
| child_bounds | text | Partition bound expression (empty if no children) |

## Design notes

Parents with no children yet produce one row with empty child columns.
This ensures newly partitioned tables appear in the output even before
any partitions are attached.

The query uses LEFT JOIN pg_inherits + child pg_class to handle the
no-children case.

## Schema filter

Excludes pg_catalog, information_schema, pg_toast, pg_temp_%,
pg_toast_temp_%. Applied to the parent schema.

## Invariants

- Deterministic ordering: ORDER BY parent_schema, parent_name,
  child_schema, child_name
- Empty result serializes as []
- Stable output column order (explicit SELECT, no SELECT *)
- Read-only query, passes linter

## Configuration

- Category: schema
- Cadence: 24h (CadenceDaily)
- Retention: RetentionMedium
- Min PG version: 10 (declarative partitioning)
- Enabled by default: yes

## Sensitivity

Low. Partition strategy, key expressions, and bound values are
structural metadata.

## Analyzer use cases

- FI-R015 enrichment (partitioned parent context)
- Partition count monitoring
- Uneven partition detection
- Schema documentation
