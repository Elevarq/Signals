# pg_collations_v1 — Collector Specification

## Purpose

User-defined collation inventory. A query with `COLLATE <user
collation>`, or a column/index using one, cannot be analysed accurately
unless the collation is recorded. Built-ins (`C`, `POSIX`, `ucs_basic`,
`default`, initdb ICU) live in `pg_catalog`; this collector emits only
**non-extension-owned** user collations.

## Catalog source

- `pg_collation` joined with `pg_namespace`.

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | Collation schema |
| collname | text | Collation name |
| provider | char | `c` libc, `i` icu, `d` default, `b` builtin |
| collcollate | text | LC_COLLATE (libc); null for ICU |
| collctype | text | LC_CTYPE (libc); null for ICU |
| collisdeterministic | bool | Deterministic collation |

Enough for `CREATE COLLATION <schema>.<name> (LOCALE/LC_COLLATE/LC_CTYPE,
PROVIDER, DETERMINISTIC)` for libc collations.

## Version note

The ICU-locale column was renamed across majors (`colliculocale` in PG
15–16 → `colllocale` in PG 17), so it is intentionally **omitted** for
version-stability — selecting it would fail to parse on majors lacking
the column. libc `collcollate`/`collctype` are version-stable. Full
ICU-locale fidelity is a documented follow-up (would need per-major SQL).

## Scope filter

- Excludes system schemas — built-in collations live in `pg_catalog`.
- Excludes extension-owned collations (`pg_depend` `deptype = 'e'`).

## Invariants

- Deterministic ordering: `ORDER BY schemaname, collname`.
- Empty result serializes as `[]`; explicit column order; read-only;
  passes the linter.

## Failure Conditions

- FC-01: Permission denied → standard collector error path.

## Configuration

- Category: schema · Cadence: 24h · Retention: RetentionMedium ·
  Requires extension: none · Semantics: snapshot · Enabled by default: yes

## Sensitivity

Normal — collation metadata (names, locales) is structure, not sensitive
source text.

## Downstream use

- Queries using user collations can be analysed accurately. Audit:
  Elevarq/Signals#212.
