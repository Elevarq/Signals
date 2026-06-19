# pg_policies_v1 — Collector Specification

## Purpose

Row-level security (RLS) policy inventory. RLS policies add quals to
planned access and change the plans PostgreSQL chooses, so analysis of
RLS-protected tables is inaccurate when the policies are absent from the
snapshot. This collector emits the policies plus the table's RLS-enabled
flags.

Unlike the custom-types gap (Signals#213), a missing policy is a
**fidelity** gap rather than a hard error — hence lower priority.

## Catalog source

- `pg_policies` (system view) joined with `pg_class` / `pg_namespace`
  for the per-table `relrowsecurity` / `relforcerowsecurity` flags.

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | Schema of the policy's table |
| tablename | text | Table the policy applies to |
| policyname | text | Policy name |
| permissive | text | `PERMISSIVE` or `RESTRICTIVE` |
| roles | text | Comma-joined role list (`array_to_string(pg_policies.roles, ', ')`; `public` for PUBLIC) |
| cmd | text | Command the policy covers (`ALL` / `SELECT` / `INSERT` / `UPDATE` / `DELETE`) |
| qual | text | `USING` expression (deparsed) |
| with_check | text | `WITH CHECK` expression (deparsed) |
| rls_enabled | bool | table `relrowsecurity` |
| rls_forced | bool | table `relforcerowsecurity` |

`qual` / `with_check` are deparsed expressions produced at runtime
(column references, not query-text literals), so they cannot trip the
SQL safety linter regardless of content.

## Scope filter

Excludes system schemas (`pg_catalog`, `information_schema`,
`pg_toast`, `pg_temp_%`, `pg_toast_temp_%`).

## Invariants

- Deterministic ordering: `ORDER BY schemaname, tablename, policyname`.
- Empty result serializes as `[]`.
- Stable, explicit output column order (no `SELECT *`).
- Read-only query, passes the safety linter.

## Failure Conditions

- FC-01: Permission denied → standard collector error path.
- FC-02: Role lacks visibility into a policy's `qual`/`with_check`
  (non-owner, non-superuser) → those columns come back NULL; the
  policy row is still emitted with its identity + flags.

## Configuration

- Category: schema
- Cadence: 24h (CadenceDaily)
- Retention: RetentionMedium
- Min PG version: 14 (pg_policies available on all supported majors)
- Requires extension: none
- Semantics: snapshot (structural)
- Enabled by default: no — **HighSensitivity**

## Sensitivity

**HighSensitivity.** Policy `qual` / `with_check` are arbitrary SQL
expressions that can embed column references, literals, and function
calls — the same class as view / function / trigger definition text.
Classified `HighSensitivity = true` on the **skip-path** (no
`SensitiveColumns` declared) — its `qual` / `with_check` columns ARE
the sensitive payload, so redaction would leave a near-empty row.
Runs by default under R075 v2 (collect-everything default); an
operator who opts out via
`signals.high_sensitivity_collectors_enabled: false` (or an R098
`restricted` per-target profile) drops the collector and it appears
in `collector_status.json` as `status=skipped, reason=config_disabled`
(EA-R001).

## Downstream use

- RLS-protected tables can be analysed accurately when their policies
  are present in the snapshot. Audit: Elevarq/Signals#212.
