# pg_types_v1 — Collector Specification

## Purpose

User-defined type and domain inventory with their DDL definitions. A
table column whose type is a user-defined **enum**, **composite type**,
or **domain** cannot be fully represented in the snapshot unless the
type definition travels with it; without it the dependent table is
dropped from downstream analysis. This collector emits enough to
describe those types (`CREATE TYPE` / `CREATE DOMAIN`) so dependent
tables remain analysable.

## Catalog source

- `pg_type` joined with `pg_namespace`, plus per-kind:
  - enums: `pg_enum` (ordered labels)
  - composites: `pg_attribute` (standalone composite, `pg_class.relkind = 'c'`)
  - domains: `pg_type.typbasetype` / `typnotnull` / `typdefault`, and
    `pg_constraint` (domain CHECK constraints, `contypid`)

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | Schema containing the type |
| typename | text | Type / domain name |
| typtype | char | `e` enum, `c` composite, `d` domain |
| enum_labels | text[] | Enum labels ordered by `enumsortorder` (enum only; else null) |
| composite_columns | text[] | `"<attname> <formatted_type>"` per attribute, ordered by `attnum`, non-dropped (composite only; else null) |
| domain_basetype | text | `format_type(typbasetype, typtypmod)` (domain only; else null) |
| domain_notnull | bool | domain `NOT NULL` flag (domain only; else null) |
| domain_default | text | domain default expression `typdefault` (domain only; else null) |
| domain_constraints | text[] | each domain CHECK via `pg_get_constraintdef` (domain only; else null) |

**Why structured columns rather than a ready DDL string:** the Elevarq
Signals safety linter bans the literal keyword `CREATE`/`ALTER`/… in
collector SQL (it scans the query text, including string literals), so
a server-side-built `CREATE TYPE …` string is not permittable. There is
no `pg_get_typedef()` equivalent of `pg_get_viewdef()`. The collector
therefore emits the structural pieces from which the DDL can be
assembled:

- **enum** → `CREATE TYPE <schema>.<name> AS ENUM (<quoted enum_labels>)`.
- **composite** → `CREATE TYPE <schema>.<name> AS (<composite_columns joined>)`.
- **domain** → `CREATE DOMAIN <schema>.<name> AS <domain_basetype>`
  + ` NOT NULL` when `domain_notnull` + ` DEFAULT <domain_default>` when
  present + each `domain_constraints` element.

Type/enum-label values appear only in the result rows, never in the
query text, so they cannot trip the linter regardless of content.

## Scope filter

- Excludes system schemas (`pg_catalog`, `information_schema`,
  `pg_toast`, `pg_temp_%`, `pg_toast_temp_%`).
- `typtype IN ('e', 'c', 'd')` — base, pseudo, and (auto-generated)
  array types are excluded by construction.
- Composite types that are a table/view **row-type** are excluded:
  only standalone `CREATE TYPE … AS (…)` composites (their backing
  `pg_class.relkind = 'c'`) are emitted.
- Extension-owned types are excluded (a `pg_depend` entry with
  `deptype = 'e'`) — they are recreated by `CREATE EXTENSION`, and
  re-issuing `CREATE TYPE` for them would collide.

## Invariants

- Deterministic ordering: `ORDER BY schemaname, typename`.
- Empty result serializes as `[]`.
- Stable, explicit output column order (no `SELECT *`).
- Read-only query, passes the safety linter.

## Failure Conditions

- FC-01: Permission denied → standard collector error path.

## Configuration

- Category: schema
- Cadence: 24h (CadenceDaily)
- Retention: RetentionMedium
- Min PG version: 14 (all supported majors expose these catalogs
  identically; no per-major variant)
- Requires extension: none
- Semantics: snapshot (structural)
- Enabled by default: yes

## Sensitivity

Normal. The emitted DDL is schema structure — enum labels, composite
attribute names/types, and domain base type + CHECK expressions —
comparable to `pg_constraints_v1.condef` and `pg_columns_v1` (both
normal). It is NOT query-logic source text like view/function bodies
(which are `HighSensitivity`). Operators with stricter requirements
can drop the collector via an R098 per-target profile `exclude`.

## Downstream use

- Tables whose columns use user-defined enums / composites / domains
  can be analysed instead of skipped when these definitions are present
  in the snapshot. Audit: Elevarq/Arq-Signals#212.
