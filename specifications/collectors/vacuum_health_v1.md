# vacuum_health_v1 — Collector Specification

## Purpose

High-signal synthesis over `pg_stat_user_tables` focused on the
three autovacuum-relevant pathologies: accumulated dead tuples,
never/recently-unvacuumed tables, and XID freeze-age headroom.
Emits only tables where at least one signal fires, so the output
stays actionable even on large schemas. Includes `reloptions` for
detecting per-table autovacuum overrides.

## Catalog source

- `pg_stat_user_tables` joined with `pg_class`

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | Schema |
| table_name | text | Table name |
| n_live_tup | bigint | Live-row estimate |
| n_dead_tup | bigint | Dead-row estimate |
| dead_pct | numeric | `round(n_dead_tup / (n_live_tup + n_dead_tup) * 100, 2)`, or 0 when empty |
| last_vacuum | timestamptz | Last manual VACUUM |
| last_autovacuum | timestamptz | Last autovacuum |
| last_analyze | timestamptz | Last manual ANALYZE |
| last_autoanalyze | timestamptz | Last autoanalyze |
| vacuum_count | bigint | Manual VACUUMs (cumulative) |
| autovacuum_count | bigint | Autovacuums (cumulative) |
| xid_age | bigint | `age(relfrozenxid)` |
| reloptions | text | Flattened `array_to_string(reloptions, ', ')` |

## Scope filter

- `n_dead_tup > 0` OR
- `last_autovacuum IS NULL` OR
- `age(relfrozenxid) > 500_000_000` (80% of default freeze threshold
  of 200M, chosen as an early-warning band)

LIMIT 50, ordered by `n_dead_tup DESC`.

## Invariants

- Deterministic ordering: `n_dead_tup DESC` (primary), then
  implementation-defined for ties.
- Read-only, passes linter.
- `dead_pct` is server-computed; analyzer does not recompute.

## Failure Conditions

- FC-01: Permission denied → standard collector error path.

## Configuration

- Category: tables
- Cadence: 15m (Cadence15m)
- Retention: RetentionMedium
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot (derivations are point-in-time)
- Enabled by default: yes

## Sensitivity

Low.

## Analyzer requirements unblocked

- `autovacuum-lag` — primary evidence.
- `table-bloat-risk` — dead-tuple ratio.
- `xid-wraparound-risk` — relation-level freeze age.
- `object-parameter-drift` — `reloptions` for per-table autovacuum
  overrides.
