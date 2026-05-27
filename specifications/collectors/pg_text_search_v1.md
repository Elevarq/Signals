# pg_text_search_v1 — Collector Specification

## Purpose

User-defined text-search **configuration** inventory. A query like
`to_tsvector('my_cfg', x)` or `@@ to_tsquery('my_cfg', …)` resolves the
configuration by name at PLAN time; without it recorded, such queries
cannot be analysed accurately. Built-in/extension TS objects are covered
by `CREATE EXTENSION`; this collector emits only **non-extension-owned**
user configurations.

## Catalog source

- `pg_ts_config` joined with `pg_namespace` and `pg_ts_parser` (its
  parser).

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | Configuration schema |
| cfgname | text | Configuration name |
| parser | text | Parser, schema-qualified (`<schema>.<prsname>`) |

Enough for `CREATE TEXT SEARCH CONFIGURATION <schema>.<name> (PARSER = …)`.

## Why configurations only (scope)

Analysis **never executes** a query. `to_tsvector(regconfig, text)`
resolves the configuration by name at planning time; the actual
tokenisation — which needs the configuration's dictionary mappings
(`pg_ts_config_map`), dictionaries, parser internals, and templates — runs
only at **execution**. So the configuration merely needs to **exist** for
the query to plan. Dictionaries, parsers, templates, and config mappings
are a documented follow-up (only needed if analysis ever runs queries).

## Scope filter

- Excludes system schemas — built-in configs live in `pg_catalog`.
- Excludes extension-owned configurations (`pg_depend` `deptype = 'e'`).

## Invariants

- Deterministic ordering: `ORDER BY schemaname, cfgname`.
- Empty result serializes as `[]`; explicit column order; read-only;
  passes the linter.

## Failure Conditions

- FC-01: Permission denied → standard collector error path.

## Configuration

- Category: schema · Cadence: 24h · Retention: RetentionMedium ·
  Requires extension: none · Semantics: snapshot · Enabled by default: yes

## Sensitivity

Normal — configuration + parser names are structure, not sensitive source
text.

## Downstream use

- Queries using `to_tsvector`/`to_tsquery` with a user configuration
  can be analysed accurately. Audit: Elevarq/Arq-Signals#212.
