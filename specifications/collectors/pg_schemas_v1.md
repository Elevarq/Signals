# pg_schemas_v1 — Collector Specification

## Purpose

Schema (namespace) inventory with ownership. Provides the namespace
context for all other schema collectors and enables schema-level
reporting. First collector in Phase 2 of the schema snapshot
foundation.

## Catalog source

- pg_namespace (schema identity)
- pg_roles (schema owner name)

## Output columns

| Column | Type | Description |
|---|---|---|
| nspname | text | Schema name |
| nspowner | text | Owner role name |
| is_default | bool | true when nspname = 'public' |

## Future room

ACL metadata (nspacl) and comments (pg_description) are not included
in v1. These can be added in a future revision without changing the
existing columns.

## Schema filter

Excludes pg_catalog, information_schema, pg_toast, pg_temp_%,
pg_toast_temp_%.

## Invariants

- Deterministic ordering: ORDER BY nspname
- Empty result serializes as []
- Stable output column order (explicit SELECT, no SELECT *)
- Read-only query, passes linter

## Configuration

- Category: schema
- Cadence: 24h (CadenceDaily)
- Retention: RetentionLong
- Min PG version: 10
- Enabled by default: yes

## Sensitivity

Low. Schema names and ownership are structural metadata visible to
any connected role.

## Analyzer use cases

- Namespace context for schema-level reporting
- Schema ownership audit
- Cross-snapshot schema drift (schema added/removed/owner-changed)
