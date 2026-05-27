# Diagnostic Pack 2 — Server Survival Plan

## Overlap analysis against 21 existing collectors

| Target | Existing coverage | Assessment |
|--------|------------------|------------|
| Replication slots risk | NONE | NEW — critical for WAL disk exhaustion |
| Replication status/lag | NONE | NEW — critical for HA environments |
| Checkpointer stats (PG 17+) | bgwriter_stats_v1 covers pre-17 | NEW — complements bgwriter for PG 17+ |
| Vacuum health synthesis | pg_stat_user_tables_v1 provides raw stats | NEW — adds high-signal dead tuple pressure, overdue vacuum, disabled autovacuum |
| Idle-in-transaction offenders | pg_stat_activity_v1 has raw sessions, connection_utilization_v1 has counts | NEW — focused actionable list |
| Temp I/O pressure | pg_stat_database_v1 has temp_files/temp_bytes | PARTIAL — database-level already captured; add nothing unless per-query adds value |
| Database sizes | server_identity_v1 has current db size only | NEW — all databases |
| Largest relations | pg_stat_user_tables_v1 has row counts but no sizes | NEW — top-N by disk size |

## Selected collectors (8 new)

| # | Collector ID | Source | Category | Cadence | Constraints | Operator value |
|---|-------------|--------|----------|---------|-------------|----------------|
| 1 | `replication_slots_risk_v1` | pg_replication_slots | replication | 5m | Graceful-skip: empty result when no slots exist | Detects WAL retention risk from stale/inactive slots |
| 2 | `replication_status_v1` | pg_stat_replication | replication | 5m | Graceful-skip: empty when no replicas | Replication lag and sync state |
| 3 | `checkpointer_stats_v1` | pg_stat_checkpointer | server | 15m | PG 17+ only; graceful-skip on older versions | Checkpoint timing after PG 17 split |
| 4 | `vacuum_health_v1` | pg_stat_user_tables + pg_class | tables | 15m | None | High-signal vacuum risk: dead tuples, overdue vacuum, disabled autovacuum |
| 5 | `idle_in_txn_offenders_v1` | pg_stat_activity | activity | 5m | None | Actionable list of idle-in-transaction backends |
| 6 | `database_sizes_v1` | pg_database | server | 1h | None | All database sizes for growth triage |
| 7 | `largest_relations_v1` | pg_stat_user_tables | tables | 1h | None | Top 30 relations by total disk size |
| 8 | `temp_io_pressure_v1` | pg_stat_database | server | 15m | None | Per-database temp file usage |

## Intentionally deferred

- **Multixact-specific risk**: Already covered by `mxid_age()` in
  `wraparound_db_level_v1` and `wraparound_rel_level_v1`. A separate
  collector would be redundant.
- **Per-query temp I/O**: Raw pg_stat_statements already captured;
  analyzer can derive per-query temp metrics.
- **Vacuum progress**: `pg_stat_progress_vacuum` is real-time only and
  typically empty; low signal-to-noise for periodic collection.

## Graceful-skip behavior

| Collector | When absent | Behavior |
|-----------|------------|----------|
| replication_slots_risk_v1 | No slots configured | Empty rowset (0 rows) |
| replication_status_v1 | No replicas connected | Empty rowset (0 rows) |
| checkpointer_stats_v1 | PG < 17 | Skipped by MinPGVersion filter |
| vacuum_health_v1 | Empty database | Empty rowset (0 rows) |
