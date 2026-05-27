# pg_rules_v1 — Collector Specification

## Purpose

Rewrite-rule inventory. A `CREATE RULE` rewrites
SELECT/INSERT/UPDATE/DELETE on a relation, so a table that carries rules
plans those statements differently. Without the rules recorded, DML on
that relation cannot be analysed accurately. Rules are uncommon (mostly
superseded by triggers/views) but DML-specific.

## Catalog source

- `pg_rules` (system view).

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | Schema of the rule's table |
| tablename | text | Relation the rule rewrites |
| rulename | text | Rule name |
| definition | text | `CREATE RULE …` text (`pg_get_ruledef`) |

`definition` is produced at runtime by `pg_get_ruledef` — it is result
data, never query text, so it cannot trip the SQL safety linter.

## Scope filter

- Excludes system schemas.
- Excludes the implicit `_RETURN` view-SELECT rule (that comes with the
  view definition).

## Invariants

- Deterministic ordering: `ORDER BY schemaname, tablename, rulename`.
- Empty result serializes as `[]`; explicit column order; read-only;
  passes the linter.

## Failure Conditions

- FC-01: Permission denied → standard collector error path.

## Configuration

- Category: schema · Cadence: 24h · Retention: RetentionMedium ·
  Requires extension: none · Semantics: snapshot · **Enabled by default: no**

## Sensitivity

**HighSensitivity.** The rule `definition` is arbitrary SQL (the action),
the same class as view / function / trigger definition text. Gated off by
default (R075); enabled via the daemon flag or an R098 per-target profile.

## Downstream use

- DML on rule-bearing relations can be analysed accurately.
  Audit: Elevarq/Arq-Signals#212.
