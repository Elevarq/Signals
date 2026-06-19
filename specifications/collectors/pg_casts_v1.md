# pg_casts_v1 — Collector Specification

## Purpose

User-defined cast inventory. A query relying on a user-defined
`CREATE CAST` (`x::t`, `CAST(…)`, or an implicit/assignment cast) cannot
be analysed accurately unless the cast is recorded. Most casts are
built-in or extension-provided; this collector emits only
**user-defined, non-extension-owned** casts.

## Catalog source

- `pg_cast` joined with `pg_type` (source + target) and `pg_proc` (cast
  function, when method = function).

## Output columns

| Column | Type | Description |
|---|---|---|
| source_schema | text | Source type schema |
| source_type | text | Source type name |
| target_schema | text | Target type schema |
| target_type | text | Target type name |
| cast_impl | text | `<schema>.<func>` (function casts), or `inout` / `binary` |
| castcontext | char | `e` explicit, `a` assignment, `i` implicit |

Enough for `CREATE CAST (source AS target) WITH FUNCTION … | WITHOUT
FUNCTION | WITH INOUT` + the context. The cast function is provided
by `pg_functions_definitions_v1` (functions are captured first).

## Scope filter

- Casts have **no schema**, so built-ins are excluded by OID
  (`pg_cast.oid >= 16384`, FirstNormalObjectId — built-in casts have
  low OIDs).
- Excludes extension-owned casts (`pg_depend` `deptype = 'e'`).

## Invariants

- Deterministic ordering: `ORDER BY source_schema, source_type,
  target_schema, target_type`.
- Empty result serializes as `[]`; explicit column order; read-only;
  passes the linter.

## Failure Conditions

- FC-01: Permission denied → standard collector error path.

## Configuration

- Category: schema · Cadence: 24h · Retention: RetentionMedium ·
  Requires extension: none · Semantics: snapshot · Enabled by default: yes

## Sensitivity

Normal — cast metadata + the cast-function name are structure, not source
text.

## Downstream use

- Queries relying on user-defined casts can be analysed accurately.
  Audit: Elevarq/Signals#212.
