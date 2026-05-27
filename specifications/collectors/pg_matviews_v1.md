# pg_matviews_v1 — Collector Specification

## Purpose

Materialized view inventory for all user-schema materialized views.
Provides identity, ownership, populated status, and index presence.
Definition text is available in definition mode but excluded by
default. Follows the same inventory/definition/hash_only pattern as
pg_views_v1.

## Catalog source

- pg_matviews (system view)
- pg_class + pg_namespace (for definition mode, pg_get_viewdef join)

## Definition modes

| Mode | Output | Default |
|---|---|---|
| inventory | schemaname, matviewname, matviewowner, ispopulated, hasindexes | yes |
| definition | adds: definition (matview SQL text) | no |
| hash_only | adds: definition_hash (SHA-256, computed by Arq Signals runtime) | no |

v1 registers inventory-mode as pg_matviews_v1 and definition-mode
as pg_matviews_definitions_v1. hash_only is computed application-side.

## Output columns (inventory mode)

| Column | Type | Description |
|---|---|---|
| schemaname | text | Schema containing the materialized view |
| matviewname | text | Materialized view name |
| matviewowner | text | Owner role name |
| ispopulated | bool | true if the matview has been populated |
| hasindexes | bool | true if the matview has indexes |

## Output columns (definition mode, adds)

| Column | Type | Description |
|---|---|---|
| definition | text | Matview SQL from pg_get_viewdef() |

Note on definition stability: deparsed SQL from pg_get_viewdef() is
suitable for drift detection within the same PostgreSQL major version.
It is not guaranteed to be semantically stable across PG versions.

## Schema filter

Excludes pg_catalog, information_schema, pg_toast, pg_temp_%,
pg_toast_temp_%.

## Invariants

- Deterministic ordering: ORDER BY schemaname, matviewname
- Empty result serializes as []
- Stable output column order (explicit SELECT, no SELECT *)
- Read-only query, passes linter

## Configuration

- Category: schema
- Cadence: 24h (CadenceDaily)
- Retention: RetentionMedium
- Min PG version: 10
- Enabled by default: yes (inventory mode)

## Sensitivity

Low (inventory mode). Moderate (definition mode — matview SQL reveals
query logic). Inventory mode is the default.

## Analyzer use cases

- Unpopulated matview detection
- Matview refresh monitoring
- Schema documentation and drift detection
