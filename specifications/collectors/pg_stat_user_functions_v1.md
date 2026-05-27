# pg_stat_user_functions_v1 — Collector Specification

## Purpose

Per-function execution counters: call count, total time, self time.
The evidence source for the `function-hint-candidate` detector,
which recommends per-function `ALTER FUNCTION ... SET` overrides for
narrowly-targeted planner-cost tuning.

## Catalog source

- `pg_stat_user_functions`

## Output columns

| Column | Type | Description |
|---|---|---|
| funcid | oid | Function OID |
| schemaname | text | Schema |
| funcname | text | Function name (without argument list) |
| calls | bigint | Invocation count (cumulative) |
| total_time | double precision | Total time in function and callees, ms (cumulative) |
| self_time | double precision | Total time in function body only, ms (cumulative) |

## Scope filter

Excludes `pg_catalog`, `information_schema`. Rows are only populated
when `track_functions` is set to `pl` or `all`; with `track_functions = 'none'`
this view is empty.

## Invariants

- Deterministic ordering: `ORDER BY total_time DESC, funcid ASC`.
- Stable output column order.
- Read-only query, passes linter.

## Failure Conditions

- FC-01: `track_functions = 'none'` → view is empty. Not an error.
  The analyzer correlates with `pg_settings_v1.track_functions` to
  report coverage accurately rather than concluding "no functions
  run."
- FC-02: Permission denied → standard collector error path.

## Configuration

- Category: runtime
- Cadence: 1h (Cadence1h)
- Retention: RetentionMedium
- Min PG version: 10
- Requires extension: none
- Semantics: cumulative (see `delta-semantics.md`)
- Enabled by default: yes

## Sensitivity

Low. Function names are schema metadata. No argument values, no
return values.

## Analyzer requirements unblocked

- `function-hint-candidate` — primary evidence.
- `query-concentration-risk` — when workload concentration is driven
  by a function rather than top-level SQL, this is where it shows up.
