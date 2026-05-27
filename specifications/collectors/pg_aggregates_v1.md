# pg_aggregates_v1 — Collector Specification

## Purpose

User-defined aggregate inventory. A query calling a user-defined
aggregate cannot be analysed accurately unless it is recorded. Extension
aggregates arrive via `CREATE EXTENSION`; this collector emits only
**non-extension-owned** user aggregates.

## Catalog source

- `pg_aggregate` joined with `pg_proc` (the aggregate + its support
  functions) and `pg_namespace`.

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | Aggregate schema |
| aggname | text | Aggregate name |
| identity_args | text | `pg_get_function_identity_arguments` (arg list) |
| state_type | text | Transition state type (`aggtranstype`) |
| sfunc | text | State transition function, schema-qualified |
| finalfunc | text | Final function, schema-qualified; null when none |
| combinefunc | text | Combine function, schema-qualified; null when none |
| initcond | text | Initial state value (`agginitval`); null when none |
| aggkind | text | `n` normal, `o` ordered-set, `h` hypothetical-set |

Enough for `CREATE AGGREGATE <schema>.<name>(<args>) (SFUNC, STYPE, …)`.
Support functions are provided by `pg_functions_definitions_v1`
(functions are captured first).

## Scope filter

- Excludes system schemas — built-in aggregates live in `pg_catalog`.
- Excludes extension-owned aggregates (`pg_depend` `deptype = 'e'` on the
  aggregate's `pg_proc` entry).

## Invariants

- Deterministic ordering: `ORDER BY schemaname, aggname, identity_args`.
- Empty result serializes as `[]`; explicit column order; read-only;
  passes the linter.

## Failure Conditions

- FC-01: Permission denied → standard collector error path.

## Configuration

- Category: schema · Cadence: 24h · Retention: RetentionMedium ·
  Requires extension: none · Semantics: snapshot · Enabled by default: yes

## Sensitivity

Normal — aggregate metadata + support-function names are structure, not
source text.

## Downstream use

- Queries calling user-defined aggregates can be analysed accurately.
  Audit: Elevarq/Arq-Signals#212.
