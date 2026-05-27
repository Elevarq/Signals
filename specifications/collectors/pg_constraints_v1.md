# pg_constraints_v1 — Collector Specification

## Purpose

Constraint inventory for all user-schema constraints. Emits one row
per constrained column, enabling multi-column constraint analysis.
Primary consumer: the Arq first-impression missing-FK-index detector.

## Catalog source

- pg_constraint
- pg_class
- pg_namespace
- pg_attribute (joined via unnest(conkey))
- pg_stat_user_tables (for n_live_tup)
- Referenced table via confrelid

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | Schema of the constrained table |
| relname | text | Constrained table name |
| conname | text | Constraint name |
| contype | char | Constraint type: p/f/u/c/x |
| condef | text | Human-readable constraint definition |
| column_name | text | Constrained column name |
| column_position | int | 1-based position in the constraint |
| relkind | char | Table kind: r/p/t/m |
| n_live_tup | bigint | Live tuple count from pg_stat_user_tables |
| confrelname | text | Referenced table (FK only; empty otherwise) |
| confschemaname | text | Referenced schema (FK only; empty otherwise) |

## Multi-column design

One row per constrained column. A composite FK on (a, b, c) produces
three rows with the same conname and column_position 1, 2, 3.

## Schema filter

Excludes pg_catalog, information_schema, pg_toast, pg_temp_%,
pg_toast_temp_%.

## Invariants

- Deterministic ordering: ORDER BY schemaname, relname, conname, column_position
- Empty result serializes as []
- Stable output column order (explicit SELECT, no SELECT *)
- Read-only query, passes linter

## Configuration

- Category: schema
- Cadence: 24h (CadenceDaily)
- Retention: RetentionMedium
- Min PG version: 10
- Enabled by default: yes

## Sensitivity

Low. Constraint definitions are structural metadata.

## Analyzer requirements unblocked

- FI-R010 through FI-R016: Category 1 missing-FK-index detector
- The detector reads ev.Raw["pg_constraint_v1"]
