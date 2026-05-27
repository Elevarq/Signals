# wraparound_rel_level_v1 — Collector Specification

## Purpose

Top 200 relations (user tables + TOAST) by XID freeze age, with
size and last-vacuum context. Analyzer-side ranking picks the
final set from this prefiltered population. Primary evidence for
`xid-wraparound-risk` at the relation level.

## Catalog source

- `pg_class` + `pg_namespace` + `pg_stat_all_tables`
- `pg_relation_size()`, `pg_total_relation_size()`
- `age(relfrozenxid)`, `mxid_age(relminmxid)`

## Output columns

| Column | Type | Description |
|---|---|---|
| schema | text | Schema name |
| relname | text | Relation name |
| relkind | char | `r` (table) or `t` (TOAST) |
| oid | oid | Relation OID |
| table_bytes | bigint | `COALESCE(pg_relation_size(oid), 0)` |
| total_bytes | bigint | `COALESCE(pg_total_relation_size(oid), 0)` |
| rel_xid_age | bigint | `age(relfrozenxid)` |
| rel_mxid_age | bigint | `mxid_age(relminmxid)` |
| last_vacuum | timestamptz | Manual VACUUM |
| last_autovacuum | timestamptz | Autovacuum |
| reloptions | text | `array_to_string(reloptions, ', ')` |

## Scope filter

- `relkind IN ('r', 't')` — user tables and TOAST relations.
- Excludes `pg_catalog`, `information_schema`, `pg_toast` namespaces.
- `COALESCE(pg_relation_size(oid), 0) > 0` — excludes empty
  relations (they cannot drive wraparound).

LIMIT 200 ordered by `age(relfrozenxid) DESC`.

## Invariants

- Deterministic ordering by XID age descending.
- Stable output column order.
- Read-only, passes linter.
- `COALESCE` on size functions ensures rows survive permission
  issues on individual relations.

## Failure Conditions

- FC-01: Size functions return NULL when the role lacks SELECT
  privilege and is not `pg_read_all_stats`. Handled via `COALESCE`
  — row emitted with `table_bytes = 0`.
- FC-02: Timeout on very large schemas is bounded by the
  collector's 15-second timeout.

## Configuration

- Category: wraparound
- Cadence: 24h (CadenceDaily)
- Retention: RetentionMedium
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Low.

## Analyzer requirements unblocked

- `xid-wraparound-risk` — relation-level candidates.
- `autovacuum-lag` — corroborative.
