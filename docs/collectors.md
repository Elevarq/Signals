# Collector Inventory

Elevarq Signals includes 99 read-only diagnostic collectors. All
queries execute inside `READ ONLY` transactions with savepoint
isolation. Collectors requiring unavailable extensions or
unsupported PostgreSQL versions are silently skipped and surface
in `collector_status.json` with a `reason` field
(`extension_missing` / `version_unsupported` / `config_disabled`)
per the EA-R001 contract.

Every query is visible in
[`internal/pgqueries/`](../internal/pgqueries/) — the canonical
registry is the `Register(QueryDef{...})` calls in that
directory.

## Baseline collectors

Core PostgreSQL statistics from built-in views.

| Query ID | PostgreSQL source | Cadence | Notes |
|----------|-------------------|---------|-------|
| `pg_version_v1` | `version()` | 6h | Server version string |
| `pg_settings_v1` | `pg_settings` | 6h | All runtime parameters |
| `pg_stat_activity_v1` | `pg_stat_activity` | 5m | Active sessions |
| `pg_stat_database_v1` | `pg_stat_database` | 15m | Database-level counters |
| `pg_stat_user_tables_v1` | `pg_stat_user_tables` | 15m | Table scan/tuple stats |
| `pg_stat_user_indexes_v1` | `pg_stat_user_indexes` | 15m | Index usage stats |
| `pg_statio_user_tables_v1` | `pg_statio_user_tables` | 15m | Table I/O stats |
| `pg_statio_user_indexes_v1` | `pg_statio_user_indexes` | 15m | Index I/O stats |
| `pg_stat_statements_v1` | `pg_stat_statements` | 15m | Query statistics (requires extension, dynamic columns) |

## Wraparound risk collectors

Transaction ID and multixact age monitoring.

| Query ID | PostgreSQL source | Cadence | Notes |
|----------|-------------------|---------|-------|
| `wraparound_db_level_v1` | `pg_database` | 24h | Transaction ID age by database |
| `wraparound_rel_level_v1` | `pg_class` | 24h | Transaction ID age by table (top 200) |
| `wraparound_blockers_v1` | `pg_stat_activity` | 5m | Long-running transactions blocking wraparound |

## Diagnostic Pack 1

Operational health, security, and planner diagnostics.

| Query ID | PostgreSQL source | Cadence | Notes |
|----------|-------------------|---------|-------|
| `server_identity_v1` | `version()`, uptime, db size | 6h | Server version, uptime, database context |
| `extension_inventory_v1` | `pg_available_extensions` | 6h | Installed extensions with versions |
| `bgwriter_stats_v1` | `pg_stat_bgwriter` | 15m | Checkpoint and background writer health |
| `long_running_txns_v1` | `pg_stat_activity` | 5m | Transactions older than 5 minutes |
| `blocking_locks_v1` | `pg_stat_activity` | 5m | Lock-blocking chains with wait durations |
| `login_roles_v1` | `pg_roles` | 6h | Login roles with privilege flags (no password hashes) |
| `connection_utilization_v1` | `pg_stat_activity` | 5m | Connection counts vs max_connections |
| `planner_stats_staleness_v1` | `pg_stat_user_tables` + `pg_class` | 1h | Estimate drift and modifications since analyze |
| `pgss_reset_check_v1` | `pg_stat_statements_info` | 1h | Statistics reset timestamp (requires extension, PG 14+) |

## Server Survival Pack

Collectors focused on conditions that can severely degrade or bring
down a PostgreSQL server.

| Query ID | PostgreSQL source | Cadence | Notes |
|----------|-------------------|---------|-------|
| `replication_slots_risk_v1` | `pg_replication_slots` | 5m | Stale/inactive slots, retained WAL (empty when no slots) |
| `replication_status_v1` | `pg_stat_replication` | 5m | Replica lag and sync state (empty when standalone) |
| `pg_stat_replication_slots_v1` | `pg_stat_replication_slots` | 5m | Logical slot spill/stream/total counters (PG 14+, empty when no logical slots) |
| `checkpointer_stats_v1` | `pg_stat_checkpointer` | 15m | Checkpoint stats (PG 17+ only, complements bgwriter) |
| `vacuum_health_v1` | `pg_stat_user_tables` + `pg_class` | 15m | Dead tuple pressure, overdue vacuum, XID freeze age |
| `idle_in_txn_offenders_v1` | `pg_stat_activity` | 5m | Idle-in-transaction backends holding locks |
| `database_sizes_v1` | `pg_database` | 1h | All database sizes for growth monitoring |
| `largest_relations_v1` | `pg_stat_user_tables` | 1h | Top 30 relations by disk size |
| `temp_io_pressure_v1` | `pg_stat_database` | 15m | Per-database temp file usage |

## Cluster identity & per-database configuration

Collectors that capture identity and configuration overrides so
downstream consumers can disambiguate same-named databases across
clusters and reason about effective per-role / per-database
defaults.

| Query ID | PostgreSQL source | Cadence | Notes |
|----------|-------------------|---------|-------|
| `cluster_identity_v1` | `inet_server_addr()` / `inet_server_port()` / `pg_is_in_recovery()` / `pg_last_wal_*_lsn()` / `pg_postmaster_start_time()` + `cluster_name`, `TimeZone` GUCs | 6h | Network + cluster fingerprint, distinct from `server_identity_v1`. NULL inet fields on unix-socket connections. NULL last_wal_* on primaries. |
| `pg_db_role_settings_v1` | `pg_db_role_setting` + `pg_database` + `pg_roles` | 24h | `ALTER DATABASE … SET …` / `ALTER ROLE … SET …` / `ALTER ROLE … IN DATABASE … SET …` defaults. Scope classified as `database` / `role` / `role_in_database` / `global`. |

## Index hygiene

| Query ID | PostgreSQL source | Cadence | Notes |
|----------|-------------------|---------|-------|
| `index_health_summary_v1` | `pg_index` + `pg_class` + `pg_namespace` + `pg_attribute` + `pg_stat_user_indexes` | 6h | One row per non-system index with derived `health_findings` array (`unused`, `large_unused`, `invalid`, `not_ready`, `redundant`, `duplicate`) plus `duplicate_of` / `redundant_with` pointers |

## Bloat estimates

Statistical estimates that run on every PG — no `pgstattuple` /
`pgstatindex` required. Designed for managed-PG services (RDS,
Aurora, Cloud SQL, AlloyDB, Azure Flex) where the exact-path
extensions are unavailable.

| Query ID | PostgreSQL source | Cadence | Notes |
|----------|-------------------|---------|-------|
| `bloat_estimate_v1` | `pg_class` + `pg_namespace` + `pg_stats` + `pg_stat_user_tables` | 6h | One row per non-system `relkind ∈ {r,m,p}` table; emits `bloat_bytes`, `bloat_ratio`, `stats_missing` |
| `index_bloat_estimate_v1` | `pg_index` + `pg_class` + `pg_namespace` + `pg_attribute` + `pg_stats` | 6h | One row per non-system `relkind ∈ {i,I}` index; width sum bounded by `indnkeyatts` (INCLUDE columns excluded); expression-key indexes emit `stats_missing = TRUE` |

## In-flight operation progress

`pg_stat_progress_*` family — empty rowsets are normal (no
operation running). Every member is gated to PG 14+.

| Query ID | PostgreSQL source | Cadence | Notes |
|----------|-------------------|---------|-------|
| `pg_stat_progress_vacuum_v1` | `pg_stat_progress_vacuum` | 5m | Per-(auto)vacuum operation; PG 17 byte-denominated columns normalised |
| `pg_stat_progress_analyze_v1` | `pg_stat_progress_analyze` | 5m | Per-ANALYZE operation |
| `pg_stat_progress_create_index_v1` | `pg_stat_progress_create_index` | 5m | Per-CREATE INDEX / REINDEX, incl. CONCURRENTLY |
| `pg_stat_progress_cluster_v1` | `pg_stat_progress_cluster` | 5m | Per-CLUSTER and VACUUM FULL |
| `pg_stat_progress_basebackup_v1` | `pg_stat_progress_basebackup` | 5m | Per-active basebackup, byte + tablespace progress |
| `pg_stat_progress_copy_v1` | `pg_stat_progress_copy` | 5m | Per-COPY operation; PG 17 `tuples_skipped` normalised |

## Foreign Data Wrapper inventory

| Query ID | PostgreSQL source | Cadence | Notes |
|----------|-------------------|---------|-------|
| `fdw_wrappers_v1` | `pg_foreign_data_wrapper` | 6h | Installed FDWs |
| `fdw_servers_v1` | `pg_foreign_server` | 6h | FDW server definitions |
| `fdw_user_mappings_v1` | `pg_user_mapping` | 6h | Per-role server mappings |
| `fdw_foreign_tables_v1` | `pg_foreign_table` | 6h | Foreign table definitions |

## I/O and WAL statistics

| Query ID | PostgreSQL source | Cadence | Notes |
|----------|-------------------|---------|-------|
| `pg_stat_io_v1` | `pg_stat_io` | 15m | Per (backend_type, object, context) physical I/O counters (PG 16+); PG 18 column-shape changes normalised |
| `pg_stat_wal_v1` | `pg_stat_wal` | 15m | Cluster-wide WAL generation, write, sync counters (PG 14+); PG 18 column-shape changes normalised |

## Schema model

Daily-cadence collectors documenting the structural surface of
the database. System schemas (`pg_catalog`, `information_schema`,
`pg_toast`, `pg_temp_*`, `pg_toast_temp_*`) are uniformly
excluded.

| Query ID | PostgreSQL source | Cadence | Notes |
|----------|-------------------|---------|-------|
| `pg_schemas_v1` | `pg_namespace` | 24h | User-schema inventory |
| `pg_columns_v1` | `information_schema.columns` | 24h | Column types, nullability, defaults |
| `pg_constraints_v1` | `pg_constraint` | 24h | PK / FK / CHECK / UNIQUE / EXCLUSION definitions |
| `pg_indexes_v1` | `pg_indexes` | 24h | Index definitions with `indexdef` source |
| `pg_partitions_v1` | `pg_inherits` + `pg_class` | 24h | Partition inheritance topology |
| `pg_sequences_v1` | `pg_sequences` | 24h | Sequence inventory |
| `pg_views_v1` | `pg_views` | 24h | View identity (definitions in a separate collector) |
| `pg_matviews_v1` | `pg_matviews` | 24h | Materialised view identity |
| `pg_triggers_v1` | `pg_trigger` + `pg_class` | 24h | Trigger inventory (definitions in a separate collector) |
| `pg_functions_v1` | `pg_proc` + `pg_namespace` | 24h | User function inventory (PG 11+) |
| `pg_stats_v1` | `pg_stats` | 24h | Column-level planner stats (`n_distinct`, `correlation`); MCV / histogram payloads deliberately excluded |
| `pg_stats_extended_v1` | `pg_statistic_ext` | 24h | Multi-column extended statistics |
| `pg_vector_columns_v1` | `pg_attribute` + `pg_type` | 24h | `vector` columns inventory (requires `vector` extension, PG 14+) |

## DDL definitions

DDL bodies for the user-defined objects above. Separate from the
identity collectors so daily structural snapshots stay small even
on schemas with very large function bodies.

| Query ID | PostgreSQL source | Cadence | Notes |
|----------|-------------------|---------|-------|
| `pg_functions_definitions_v1` | `pg_get_functiondef(...)` | 24h | Function bodies (PG 11+) |
| `pg_views_definitions_v1` | `pg_get_viewdef(...)` | 24h | View bodies |
| `pg_matviews_definitions_v1` | `pg_get_viewdef(...)` | 24h | Materialised-view bodies |
| `pg_triggers_definitions_v1` | `pg_get_triggerdef(...)` | 24h | Trigger bodies |

## Storage placement

| Query ID | PostgreSQL source | Cadence | Notes |
|----------|-------------------|---------|-------|
| `pg_tablespaces_v1` | `pg_tablespace` | 6h | Tablespace inventory with sizes |
| `pg_class_storage_v1` | `pg_class` | 6h | Per-relation storage (`relpersistence`, `relam`, `reltablespace`, `reloptions`) |
| `pg_attribute_storage_v1` | `pg_attribute` | 6h | Per-attribute storage hints (`attstorage`, `attcompression`, PG 14+) |

## Activity summaries

Lower-cardinality summaries derived from `pg_stat_activity` /
`pg_locks` so the analyzer doesn't have to re-aggregate the
per-pid rows for every cycle.

| Query ID | PostgreSQL source | Cadence | Notes |
|----------|-------------------|---------|-------|
| `pg_stat_activity_summary_v1` | `pg_stat_activity` | 5m | Per (state, wait_event_type, database) counts |
| `pg_locks_summary_v1` | `pg_locks` | 5m | Per (mode, granted) lock counts |

## Function statistics and configuration

| Query ID | PostgreSQL source | Cadence | Notes |
|----------|-------------------|---------|-------|
| `pg_stat_user_functions_v1` | `pg_stat_user_functions` | 1h | Per-function call / time stats (requires `track_functions != none`) |
| `pg_proc_config_v1` | `pg_proc.proconfig` | 24h | Per-function GUC overrides (`SET foo = bar` attached at `CREATE FUNCTION`) |

## Security capabilities

| Query ID | PostgreSQL source | Cadence | Notes |
|----------|-------------------|---------|-------|
| `pg_role_capabilities_v1` | `pg_roles` + `has_*_privilege(...)` | 6h | Role privilege fingerprint — what the connected role can do (privilege bits only, no password material) |

## Two-phase commit

| Query ID | PostgreSQL source | Cadence | Notes |
|----------|-------------------|---------|-------|
| `pg_prepared_xacts_v1` | `pg_prepared_xacts` | 1h | Prepared (2PC) transactions with server-computed age; orphaned 2PC holds back xmin and blocks vacuum |

## TimescaleDB (Tiger Data)

Collected only when the `timescaledb` extension is installed
(R114); on plain PostgreSQL every member is skipped with
`reason=extension_missing`. All members except the detection
collector additionally require TimescaleDB ≥ 2.14
(`reason=version_unsupported` below that, R115). Sources are the
documented PUBLIC-readable `timescaledb_information` views and two
catalog-priced helper functions — internal `_timescaledb_*`
catalogs and the exact (per-chunk-locking) size functions are
deliberately not queried. Design:
[`timescaledb-collectors-design.md`](timescaledb-collectors-design.md);
permissions: [`postgres-role.md`](postgres-role.md).

| Query ID | TimescaleDB source | Cadence | Notes |
|----------|--------------------|---------|-------|
| `timescaledb_extension_v1` | `pg_extension` + `pg_settings` + existence probes | 6h | Version, edition (`timescaledb.license`), telemetry level, capability flags (feature-detected via `to_regclass`) |
| `timescaledb_hypertables_v1` | `timescaledb_information.hypertables` | 6h | Hypertable inventory (dynamic columns — `primary_dimension` on ≥ 2.20) |
| `timescaledb_dimensions_v1` | `timescaledb_information.dimensions` | 24h | Time/space partitioning dimensions, chunk intervals |
| `timescaledb_chunks_v1` | `timescaledb_information.chunks` | 6h | Per-chunk rows, newest created first, capped at 5000 |
| `timescaledb_chunk_summary_v1` | aggregate over chunks view | 6h | Complete per-hypertable rollup (count, compressed count, range/creation bounds) — makes the chunk cap detectable |
| `timescaledb_hypertable_sizes_v1` | `hypertable_approximate_detailed_size()` | 1h | Approximate table/index/toast/total bytes (monitoring-priced; no per-chunk locks) |
| `timescaledb_compression_settings_v1` | `timescaledb_information.hypertable_compression_settings` | 24h | segmentby/orderby settings (pre-rename view name, valid 2.14→2.27) |
| `timescaledb_compression_stats_v1` | `hypertable_compression_stats()` | 1h | Before/after compression bytes per hypertable (recorded at compression time) |
| `timescaledb_continuous_aggregates_v1` | `timescaledb_information.continuous_aggregates` | 6h | Cagg inventory; `view_definition` is high-sensitivity (redact path) |
| `timescaledb_jobs_v1` | `timescaledb_information.jobs` | 1h | All automation jobs incl. retention/compression/refresh policies (`proc_name` + `config`) |
| `timescaledb_job_stats_v1` | `timescaledb_information.job_stats` | 15m | Per-job run counters and last-run status — visible for all jobs |
| `timescaledb_job_errors_v1` | `timescaledb_information.job_errors` | 1h | Failed runs, newest first, capped at 1000; rows visible only with job-owner/db-owner membership (zero rows is the expected least-privilege state); `err_message` is high-sensitivity (redact path) |

## Version and extension behavior

- Collectors with `MinPGVersion` are excluded on older PostgreSQL
  versions (e.g. `checkpointer_stats_v1` requires PG 17+)
- Collectors with `RequiresExtension` are excluded when the extension
  is not installed (e.g. `pg_stat_statements_v1`)
- `pg_stat_statements_v1` uses a wildcard projection (`SELECT s.*`)
  for cross-version compatibility — column names may differ between
  PG versions. Signals does not rank or limit these rows; Analyzer
  owns top-N workload selection.
- `pg_stat_statements_v1` self-filters (R106). Rows are scoped to
  the connected database via `pg_database.datname =
  current_database()`, and rows attributable to Signals' own
  sessions are suppressed via a `NOT EXISTS` correlated subquery
  against `pg_stat_activity` where `application_name = 'arq-signals'`
  (matched on `userid` / `dbid`). The Signals connection sets
  `application_name = 'arq-signals'` in its startup parameters
  (sourced from the `collector.AppName` constant) so the filter
  works from the very first statement on every session. If a
  non-Signals application sets its own `application_name` to
  `arq-signals`, its rows are suppressed — that is operator
  misconfiguration, not a Signals defect.

### Self-filter limits (operator note)

The `pg_stat_statements_v1` self-filter is **best-effort**, not a
security boundary:

- The filter relies on PostgreSQL `application_name = 'arq-signals'`.
  `application_name` is a client-set startup parameter and can be
  spoofed by any session. Suppression therefore identifies *intent*,
  not identity. This is fine for the filter's purpose (keep
  monitoring noise out of workload analysis), but do not treat the
  absence of a row as proof that no Signals session executed the
  statement.
- The `NOT EXISTS` join uses `pg_stat_activity`. If the configured
  role is missing `pg_monitor` membership (see `docs/postgres-role.md`),
  PostgreSQL hides other users' rows in `pg_stat_activity`, which can
  reduce filter accuracy across Signals workers or daemons. Grant
  `pg_monitor` to get full filter coverage.
- These limits affect **data quality and operator troubleshooting
  only**. They do not grant any access or privilege to a session that
  spoofs the application name — `pg_stat_statements` carries no
  secrets and the read-only safety model is unchanged.
- Replication collectors return empty results on standalone instances
