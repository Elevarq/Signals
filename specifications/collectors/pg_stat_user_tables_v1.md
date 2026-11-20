# pg_stat_user_tables_v1 — Collector Specification

## Purpose

Per-table activity counters and vacuum/analyze lifecycle timestamps.
The always-present table-level surface for autovacuum, bloat, and
access-pattern detectors. Note: analyzer-side, this collector's
output is exposed via the typed field `ev.Tables` (always
initialized, never missing).

## Catalog source

- `pg_stat_user_tables`

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | Schema |
| relname | text | Table name |
| seq_scan | bigint | Sequential scans (cumulative) |
| seq_tup_read | bigint | Rows returned by seqscans |
| idx_scan | bigint | Index scans (cumulative) |
| idx_tup_fetch | bigint | Rows fetched via indexes |
| n_tup_ins | bigint | Rows inserted |
| n_tup_upd | bigint | Rows updated |
| n_tup_del | bigint | Rows deleted |
| n_tup_hot_upd | bigint | HOT updates |
| n_live_tup | bigint | Live-row estimate |
| n_dead_tup | bigint | Dead-row estimate |
| last_vacuum | timestamptz | Last manual VACUUM |
| last_autovacuum | timestamptz | Last autovacuum |
| last_analyze | timestamptz | Last manual ANALYZE |
| last_autoanalyze | timestamptz | Last autoanalyze |
| vacuum_count | bigint | Manual VACUUMs (cumulative) |
| autovacuum_count | bigint | Autovacuums (cumulative) |
| analyze_count | bigint | Manual ANALYZEs |
| autoanalyze_count | bigint | Autoanalyzes |
| stats_reset | timestamptz | Per-object time these statistics were last reset. PG19+ only (capability-gated on the actual server major); NULL on PG14-18 (column absent) and whenever the server reports it NULL (never reset). See `specifications/pg19-per-object-stats-reset.md` (#817). |

## Scope filter

`pg_stat_user_tables` excludes system schemas. No additional filter.

## Invariants

- Deterministic ordering: `ORDER BY schemaname, relname`.
- Stable output column order; `stats_reset` is appended last so the
  pre-existing identity and column order are unchanged across majors
  (#817, INV-SR-01).
- Read-only, passes linter.

## Failure Conditions

- FC-01: Permission denied → standard collector error path.
- FC-02: Counter decrease between samples → per
  `delta-semantics.md`. On PG19+ a per-object reset is directly
  identifiable via this view's own `stats_reset` column (#817); on
  PG14-18 the column is a typed NULL and reset is inferred via
  `pg_stat_database_v1.stats_reset`.

## Configuration

- Category: tables
- Cadence: default (Cadence1h; configured zero-value → default)
- Retention: RetentionMedium
- Min PG version: 10
- Requires extension: none
- Semantics: mixed — cumulative counters + state timestamps
- Enabled by default: yes

## Sensitivity

Low.

## Analyzer requirements unblocked

- `autovacuum-lag` — dead-tuple pressure, last-autovacuum staleness.
- `analyze-staleness` — last-analyze staleness.
- `missing-index-candidate` — seq-scan vs idx-scan ratio.
- `table-bloat-risk` — dead-tuple ratio.
- `object-parameter-drift` — HOT-update ratio for fillfactor advice.
