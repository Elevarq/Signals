# pg_class_storage_v1 — Collector Specification

## Purpose

Per-relation storage accounting, including TOAST heap and TOAST
index, plus relation-level options (`reloptions`). Provides the
physical-size ground truth that the planner is blind to for TOAST
and the `reloptions` surface that `object-parameter-drift` compares
against.

## Catalog source

- `pg_class` joined with `pg_namespace`, `pg_tablespace`
- Size functions: `pg_relation_size()`, `pg_total_relation_size()`,
  `pg_indexes_size()`

## Output columns

| Column | Type | Description |
|---|---|---|
| relid | oid | Relation OID |
| schemaname | text | Schema |
| relname | text | Relation name |
| relkind | char | `r` (table), `i` (index), `m` (matview), `p` (partitioned), `t` (TOAST) |
| relpersistence | char | `p`=permanent, `u`=unlogged, `t`=temp |
| relispartition | boolean | True if this is a partition child |
| relhasindex | boolean | Has at least one index |
| reltuples | real | Planner's row-count estimate |
| relpages | int | Planner's page count for main relation |
| relallvisible | int | Visibility-map page count |
| relfrozenxid | xid | Frozen XID |
| relminmxid | xid | Frozen MXID |
| reltoastrelid | oid | TOAST relation OID, 0 if none |
| has_toast | boolean | Convenience derivation (`reltoastrelid <> 0`) |
| toast_pages | int | TOAST heap page count (NULL if no TOAST) |
| toast_relpages_index | int | TOAST index page count (NULL if no TOAST) |
| main_bytes | bigint | `pg_relation_size(relid, 'main')` |
| toast_bytes | bigint | TOAST heap + index bytes (NULL if no TOAST) |
| indexes_bytes | bigint | Sum of regular index bytes |
| total_bytes | bigint | `pg_total_relation_size(relid)` |
| reloptions | text[] | `pg_class.reloptions` raw |
| tablespace | text | Tablespace name, or `pg_default` |

## Scope filter

- `relkind IN ('r','m','p')` (tables, materialized views, partitioned
  tables).
- Excludes `pg_catalog`, `information_schema`, `pg_toast`,
  `pg_temp_%`, `pg_toast_temp_%`.
- Indexes are covered by `pg_indexes_v1` and a future
  `pg_class_index_storage_v1` if index reloptions justify a
  dedicated collector.

## Invariants

- Deterministic ordering: `ORDER BY schemaname, relname`.
- Stable output column order.
- Read-only query, passes linter.
- `has_toast` is derivable from `reltoastrelid` but emitted explicitly
  so the analyzer does not need to special-case OID zero.

## Failure Conditions

- FC-01: Relation dropped between catalog scan and size function call
  → size function returns NULL or zero. Query uses `WHERE` with
  `pg_class` snapshot and `LEFT JOIN LATERAL` around sizes so
  dropped relations are silently skipped rather than erroring.
- FC-02: On very large databases (tens of thousands of relations),
  `pg_total_relation_size()` can be slow due to per-fork lseeks.
  Budgeted by the collector's `Timeout` (spec below); if exceeded,
  the query is killed per the standard collector safety path.

## Configuration

- Category: configuration
- Cadence: 24h (CadenceDaily)
- Retention: RetentionMedium
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Timeout: 60s (override of default to accommodate size-function cost)
- Enabled by default: yes

## Sensitivity

Low. Structural metadata.

## Analyzer requirements unblocked

- `toast-planner-blindspot` — main-vs-TOAST size ratio.
- `object-parameter-drift` — current `reloptions` per relation.
- `table-bloat-risk` — combines with dead-tuple counts for precise
  bloat estimation.
- `io-cost-calibration` — relation-weighted mix for the calibration.
