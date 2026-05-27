# pg_proc_config_v1 — Collector Specification

## Purpose

Enumerate functions that have per-function GUC overrides set via
`ALTER FUNCTION ... SET ...` (`pg_proc.proconfig`). Needed so the
`function-hint-candidate` advisor does not recommend settings that
already exist and can flag drift between advised and actual settings.

## Catalog source

- `pg_proc` joined with `pg_namespace`

## Output columns

| Column | Type | Description |
|---|---|---|
| funcid | oid | Function OID |
| schemaname | text | Schema |
| funcname | text | Function name |
| proargtypes_oids | oid[] | Argument type OIDs (for disambiguation) |
| prolang_name | text | Language (`sql`, `plpgsql`, `c`, ...) |
| provolatile | char | `i`=immutable, `s`=stable, `v`=volatile |
| proisstrict | boolean | STRICT marker |
| prosecdef | boolean | SECURITY DEFINER |
| proconfig | text[] | Array of `"name=value"` SET clauses — NULL if none |

## Scope filter

- `proconfig IS NOT NULL` — only functions with at least one SET
  override.
- Excludes `pg_catalog`, `information_schema`.

## Invariants

- Deterministic ordering: `ORDER BY schemaname, funcname, funcid`.
- Stable output column order.
- Read-only query, passes linter.
- `proconfig` is preserved as the raw `text[]` form; parsing into
  key/value pairs is the analyzer's responsibility.

## Failure Conditions

- FC-01: Permission denied on `pg_proc` (rare) → standard collector
  error path.

## Configuration

- Category: configuration
- Cadence: 24h (CadenceDaily)
- Retention: RetentionMedium
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Low. Function definitions and their SET overrides are visible to any
role with `USAGE` on the schema.

## Analyzer requirements unblocked

- `function-hint-candidate` — prevents recommending SETs that are
  already in place; enables drift detection when an advised SET has
  been removed.
- `object-parameter-drift` — function-level surface.
