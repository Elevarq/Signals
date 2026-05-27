# pg_views_v1 — Collector Specification

## Purpose

View inventory for all user-schema views. Provides view identity and
ownership. Definition text is available in definition mode but
excluded by default for safety.

## Catalog source

- pg_views (system view)

## Definition modes

| Mode | Output | Default |
|---|---|---|
| inventory | schemaname, viewname, viewowner | yes |
| definition | adds: definition (view SQL text) | no |
| hash_only | adds: definition_hash (SHA-256, computed by Arq Signals runtime) | no |

v1 registers the inventory-mode query. Definition and hash_only
modes are future configuration options — the Arq Signals runtime
will compute hashes application-side per the finalized plan.

## Output columns (inventory mode)

| Column | Type | Description |
|---|---|---|
| schemaname | text | Schema containing the view |
| viewname | text | View name |
| viewowner | text | View owner role name |

## Output columns (definition mode, adds)

| Column | Type | Description |
|---|---|---|
| definition | text | View SQL from pg_get_viewdef() |

Note on definition stability: deparsed SQL from pg_get_viewdef() is
suitable for drift detection within the same PostgreSQL major version.
It is not guaranteed to be semantically stable across PG versions due
to changes in the deparser's whitespace, quoting, and formatting.

## Schema filter

Excludes pg_catalog, information_schema, pg_toast, pg_temp_%,
pg_toast_temp_%.

## Invariants

- Deterministic ordering: ORDER BY schemaname, viewname
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

Low (inventory mode). Moderate (definition mode — view SQL reveals
table relationships and join logic). Inventory mode is the default.

## Analyzer use cases

- Schema documentation
- View dependency tracking
- Cross-snapshot drift detection (view added/removed/owner-changed)
