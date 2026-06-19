# pg_operators_v1 — Collector Specification

## Purpose

User-defined operator inventory. A query using a user-defined operator
in a predicate cannot be analysed accurately unless the operator is
recorded in the snapshot. Extension operators arrive via
`CREATE EXTENSION`; this collector emits only **non-extension-owned**
user operators.

## Catalog source

- `pg_operator` joined with `pg_namespace` and `pg_proc` (backing function).

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | Operator schema |
| oprname | text | Operator symbol (e.g. `===`) |
| left_type | text | Left operand type (`format_type`); null for prefix operators |
| right_type | text | Right operand type; null for postfix operators |
| result_type | text | Result type |
| function | text | Backing function, schema-qualified (`oprcode`) |
| oprcanmerge | bool | Mergejoinable |
| oprcanhash | bool | Hashjoinable |

Enough for `CREATE OPERATOR <schema>.<name> (LEFTARG, RIGHTARG, FUNCTION,
…)`. The backing function is provided by `pg_functions_definitions_v1`
(functions are captured first).

## Scope filter

- Excludes system schemas (`pg_catalog`, `information_schema`, `pg_toast`,
  `pg_temp_%`, `pg_toast_temp_%`) — built-in operators live in `pg_catalog`.
- Excludes extension-owned operators (`pg_depend` `deptype = 'e'`).

## Invariants

- Deterministic ordering: `ORDER BY schemaname, oprname, left_type, right_type`.
- Empty result serializes as `[]`.
- Stable explicit column order (no `SELECT *`).
- Read-only query, passes the safety linter.

## Failure Conditions

- FC-01: Permission denied → standard collector error path.

## Configuration

- Category: schema
- Cadence: 24h (CadenceDaily)
- Retention: RetentionMedium
- Requires extension: none
- Semantics: snapshot (structural)
- Enabled by default: yes

## Sensitivity

Normal — operator metadata (names, operand/result types, the backing
function name) is structure, comparable to `pg_constraints_v1`; not
query-logic source text.

## Downstream use

- Queries using user-defined operators can be analysed accurately.
  Operator classes/families are a documented follow-up.
  Audit: Elevarq/Signals#212.
