# Diagnostic Pack 1 — Implementation Plan

## Selected diagnostics and overlap analysis

| # | Diagnostic | Status | Notes |
|---|-----------|--------|-------|
| 1 | Server version/identity | PARTIALLY COVERED | `pg_version_v1` captures `version()` only. Add uptime + database context. |
| 2 | Critical settings check | ALREADY COVERED | `pg_settings_v1` captures all settings. Analyzer can filter. |
| 3 | Extension inventory | NEW | `pg_available_extensions` with installed versions. |
| 4 | Checkpoint & bgwriter health | NEW | `pg_stat_bgwriter` — single-row view. |
| 5 | Long-running transactions | PARTIALLY COVERED | `wraparound_blockers_v1` is similar but scoped to freeze blockers. Add dedicated long-tx collector. |
| 6 | Locks & blocking | NEW | `pg_blocking_pids()` join — PG 9.6+. |
| 7 | Dangerous login roles | NEW | `pg_roles` with rolcanlogin and privilege flags. |
| 8 | Query workload ranking by total exec time | ANALYZER | Raw `pg_stat_statements_v1` (SELECT *) is captured; top-N selection belongs in Analyzer. |
| 9 | Query workload ranking by mean exec time | ANALYZER | Same raw data. |
| 10 | Query workload ranking by I/O | ANALYZER | Same raw data. |
| 11 | Query workload ranking by calls | ANALYZER | Same raw data. |
| 12 | Connection utilization | NEW | Aggregate from `pg_stat_activity`. |
| 13 | Tables at risk of XID wraparound | ALREADY COVERED | `wraparound_rel_level_v1` already captures this. |
| 14 | Table planner metadata / stats staleness | NEW | Joins pg_stat_user_tables + pg_class for estimate drift. |
| 15 | pg_stat_statements reset check | NEW | `pg_stat_statements_info` — PG 14+. |

## Collectors to implement (7 new)

| Collector ID | Source | Category | Cadence | Extension | MinPG |
|-------------|--------|----------|---------|-----------|-------|
| `server_identity_v1` | version(), uptime, db context | server | 6h | — | 14 |
| `extension_inventory_v1` | pg_available_extensions | server | 6h | — | 14 |
| `bgwriter_stats_v1` | pg_stat_bgwriter | server | 15m | — | 14 |
| `long_running_txns_v1` | pg_stat_activity | activity | 5m | — | 14 |
| `blocking_locks_v1` | pg_stat_activity + pg_blocking_pids | activity | 5m | — | 14 |
| `login_roles_v1` | pg_roles | security | 6h | — | 14 |
| `connection_utilization_v1` | pg_stat_activity | activity | 5m | — | 14 |
| `planner_stats_staleness_v1` | pg_stat_user_tables + pg_class | tables | 1h | — | 14 |
| `pgss_reset_check_v1` | pg_stat_statements_info | extensions | 1h | pg_stat_statements | 14 |

## Not implementing (already covered by existing collectors)

- Critical settings → `pg_settings_v1`
- Query workload ranking by total/mean/IO/calls → Analyzer over raw `pg_stat_statements_v1`
- XID wraparound tables → `wraparound_rel_level_v1`

## Naming convention

`<descriptive_name>_v1` — consistent with existing catalog.

## Snapshot/export impact

None — new collectors use the same NDJSON result format. No schema
changes needed.
