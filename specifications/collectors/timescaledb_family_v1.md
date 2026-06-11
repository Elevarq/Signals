# timescaledb_family_v1 — Collector Family Specification

Status: ACTIVE (implemented in `internal/pgqueries/catalog_timescaledb.go`,
issue #73)
Rules: R114 (collector family), R115 (extension-version gating)
Design note: `docs/timescaledb-collectors-design.md`
Issue: #73

## Purpose

Detect TimescaleDB (Tiger Data) on a monitored target and collect its
catalog, statistics, policy, and background-job metadata: hypertables,
dimensions, chunks, compression/columnstore state, continuous
aggregates, retention/compression/refresh policies, and job execution
health. Feeds the future TS-R001..TS-R009 Analyzer rule family
(follow-up issue; no Analyzer rules ship with this collector family).

## Catalog sources

Documented, PUBLIC-readable surfaces only:

- `pg_catalog.pg_extension`, `pg_catalog.pg_settings` (detection)
- `timescaledb_information.{hypertables,dimensions,chunks,
  hypertable_compression_settings,continuous_aggregates,jobs,
  job_stats,job_errors}`
- API functions `hypertable_approximate_detailed_size(regclass)` and
  `hypertable_compression_stats(regclass)` (catalog-priced; no table
  ACL checks; introduced 2.14.0)

Explicitly NOT used (rationale in the design note § 3):
`_timescaledb_catalog.*` / `_timescaledb_internal.*` (internal,
unstable), `timescaledb_experimental.policies` (deprecated upstream),
`timescaledb_information.job_history` (owner-filtered, redundant),
`data_nodes` (multi-node removed in 2.14), exact size functions
`hypertable_size` / `hypertable_detailed_size` / `chunks_detailed_size`
(O(chunks) AccessShareLocks per call).

## Family members

Common `QueryDef` properties: `Category: "timescaledb"`,
`RequiresExtension: "timescaledb"`, `MinPGVersion: 14`,
`ResultKind: ResultRowset`, SELECT/WITH-only SQL passing the
registration linter. All except `timescaledb_extension_v1` carry
`RequiresExtensionMinVersion: "2.14"` (R115).

| ID | Source | Cadence | Retention | Timeout | Sensitivity |
|---|---|---|---|---|---|
| `timescaledb_extension_v1` | pg_extension + pg_settings + existence probes | 6h | Long | 5s | low |
| `timescaledb_hypertables_v1` | hypertables view (`SELECT *`) | 6h | Medium | 15s | low |
| `timescaledb_dimensions_v1` | dimensions view (`SELECT *`) | 24h | Medium | 15s | low |
| `timescaledb_chunks_v1` | chunks view, newest-created-first (`chunk_creation_time DESC`), LIMIT 5000 | 6h | Medium | 30s | low |
| `timescaledb_chunk_summary_v1` | per-hypertable aggregate over chunks view | 6h | Medium | 30s | low |
| `timescaledb_hypertable_sizes_v1` | hypertables × LATERAL `hypertable_approximate_detailed_size` | 1h | Medium | 30s | low |
| `timescaledb_compression_settings_v1` | hypertable_compression_settings view (`SELECT *`) | 24h | Medium | 15s | low |
| `timescaledb_compression_stats_v1` | hypertables × LATERAL `hypertable_compression_stats` | 1h | Medium | 30s | low |
| `timescaledb_continuous_aggregates_v1` | continuous_aggregates view (`SELECT *`) | 6h | Medium | 15s | HighSensitivity, redact path: `SensitiveColumns: ["view_definition"]` |
| `timescaledb_jobs_v1` | jobs view (`SELECT *`) | 1h | Medium | 15s | low |
| `timescaledb_job_stats_v1` | job_stats view (`SELECT *`) | 15m | Short | 15s | low |
| `timescaledb_job_errors_v1` | job_errors view (`SELECT *`), newest-first, LIMIT 1000 | 1h | Medium | 15s | HighSensitivity, redact path: `SensitiveColumns: ["err_message"]` |

## Output columns

Version-variant views are captured with dynamic columns
(`SELECT *`, R037 precedent) because the column sets drift across
TimescaleDB versions (`hypertables.primary_dimension{,_type}` ≥ 2.20;
`continuous_aggregates.finalized` ≤ 2.24; settings `index` column
≥ 2.22). Consumers treat per-version columns as optional; NDJSON
preserves whatever the connected version exposes.

Fixed projections:

### timescaledb_extension_v1 (exactly one row)

| Column | Type | Description |
|---|---|---|
| `extversion` | text | installed TimescaleDB version (provenance for the family) |
| `extension_schema` | text | `extnamespace::regnamespace` — API function schema |
| `license` | text | `timescaledb.license` GUC: `timescale` / `apache`; NULL if unreadable |
| `telemetry_level` | text | `timescaledb.telemetry_level` GUC; NULL if unreadable |
| `has_information_views` | boolean | `to_regclass('timescaledb_information.hypertables') IS NOT NULL` |
| `has_job_history` | boolean | probe ⇒ 2.15+ |
| `has_columnstore_aliases` | boolean | probe ⇒ 2.18+ (columnstore rename present) |
| `has_experimental_policies` | boolean | upstream-removal tripwire |
| `has_functions_schema` | boolean | `_timescaledb_functions` present ⇒ 2.11+ |
| `bgw_job_in_catalog` | boolean | `_timescaledb_catalog.bgw_job` present ⇒ 2.25+ |

These booleans are the **capability flags** for the Analyzer.

### timescaledb_chunk_summary_v1 (one row per hypertable)

| Column | Type | Description |
|---|---|---|
| `hypertable_schema` / `hypertable_name` | text | identity |
| `chunk_count` | bigint | total chunks (authoritative even when `timescaledb_chunks_v1` truncates) |
| `compressed_chunk_count` | bigint | chunks with `is_compressed` |
| `oldest_range_start` / `newest_range_end` | timestamptz | time coverage (NULL for integer dimensions) |
| `oldest_range_start_integer` / `newest_range_end_integer` | bigint | coverage for integer-typed dimensions (NULL for time dimensions) |
| `oldest_chunk_created_at` / `newest_chunk_created_at` | timestamptz | creation-time bounds |

### timescaledb_hypertable_sizes_v1 (one row per hypertable)

`hypertable_schema`, `hypertable_name`, `table_bytes`, `index_bytes`,
`toast_bytes`, `total_bytes` from
`hypertable_approximate_detailed_size(format('%I.%I', schema, name)::regclass)`.

### timescaledb_compression_stats_v1 (one row per hypertable)

`hypertable_schema`, `hypertable_name`, `total_chunks`,
`number_compressed_chunks`,
`before_compression_total_bytes`, `after_compression_total_bytes`
(+ the table/index/toast splits) from
`hypertable_compression_stats(...)`. Byte columns are NULL when
nothing is compressed; sizes are recorded at compression time and not
updated by later inserts (upstream-documented accuracy caveat).
Compression ratio is derivable (`before/after`) — per INV-SIGNALS-01
the collector emits evidence, not the derived ratio.

## Invariants

- Deterministic ordering on every rowset (`ORDER BY` on the natural
  identity columns; INV-SIGNALS-10).
- Read-only: SELECT/WITH only; passes the linter; runs inside the
  session/transaction read-only posture (R013/R017/R021). No
  TimescaleDB mutating API (`add_job`, `compress_chunk`,
  `convert_to_columnstore`, `drop_chunks`,
  `refresh_continuous_aggregate`, `timescaledb_pre_restore`, …) is
  ever called; `get_telemetry_report()` is not called (expensive).
- The family is inert on plain PostgreSQL: gated out before any SQL
  executes (`extension_missing`), surfaced per EA-R001
  (INV-SIGNALS-24).
- Bounded output: `timescaledb_chunks_v1` ≤ 5000 rows, ordered
  `chunk_creation_time DESC` so the cap is uniformly newest-first
  across time- and integer-dimension hypertables (range-based
  ordering would sort every integer-dimension chunk after all
  time-dimension chunks and starve them out of the cap), with
  truncation always detectable via
  `timescaledb_chunk_summary_v1` counts, and
  `timescaledb_job_errors_v1` ≤ 1000 rows (newest-first — the
  backing table is per-execution, so a crash-looping job can
  accumulate rows far faster than the monthly retention job prunes);
  all other members are bounded by hypertable/job/dimension
  cardinality.
- No raw application SQL beyond the two redact-path columns
  (`view_definition`, `err_message`).
- No stored OIDs for Timescale objects: the information views expose
  schema+name identity only; regclass casts appear solely as function
  arguments, wrapped in `to_regclass()` so a hypertable dropped
  between the information-view read and the cast degrades to a NULL
  argument (verified on 2.27.2: `hypertable_compression_stats` is
  STRICT and emits no row; `hypertable_approximate_detailed_size`
  is not strict and emits one all-NULL row — in both cases the
  LEFT JOIN preserves the hypertable row with NULL metrics) instead
  of failing the collector. Note: the upstream
  `hypertable_compression_settings.hypertable` column is a regclass
  rendered per the session `search_path`; the collector session uses
  the server-default search_path, so rendering is stable per target.

## Failure conditions

- FC-TSDB-01: **TimescaleDB not installed** → family ineligible via
  `RequiresExtension`; `collector_status.json` records each member
  `status=skipped, reason=extension_missing` (EA-R001). Normal state.
- FC-TSDB-02: **TimescaleDB < 2.14** → all members except
  `timescaledb_extension_v1` ineligible via
  `RequiresExtensionMinVersion` (R115);
  `status=skipped, reason=version_unsupported`. Detection still runs.
- FC-TSDB-03: **View or function missing at execution time** (future
  upstream removal; extension installed in a schema outside the
  collector role's `search_path` breaking unqualified function
  calls) → query fails inside its savepoint (R038), snapshot
  continues; classified `reason=object_missing` (SQLSTATE
  42P01/42883, R115).
- FC-TSDB-04: **Permission denied** (not expected; family surfaces
  are PUBLIC-readable) → standard `reason=permission_denied` path;
  snapshot continues.
- FC-TSDB-05: **`job_errors` empty for a least-privilege role** →
  zero rows with `status=success`. Partial by design: the view is
  security-barrier-filtered to job-owner/db-owner membership.
  Documented in `docs/postgres-role.md`; consumers cross-check
  `timescaledb_job_stats_v1.total_failures` (visible for all jobs).
- FC-TSDB-06: **Apache-2 edition** → TSL surfaces (compression,
  caggs, policies) are empty/zero; collectors succeed;
  `license=apache` capability flag disambiguates.
- FC-TSDB-07: **Empty TimescaleDB database** (extension installed,
  no hypertables) → empty rowsets, `status=success`. Normal state.
- FC-TSDB-08: **High-sensitivity opt-out**
  (`high_sensitivity_collectors_enabled: false`) → the two
  redact-path members keep running with `view_definition` /
  `err_message` NULL-ed per row (R075 revised).

## Configuration

No new configuration. The family participates in the existing
gates: R075 sensitivity opt-out (redact path), R098 per-target
profiles, R015 cadences, R012 timeouts. Enabled by default.

## Permissions

Baseline `arq_signals` role (LOGIN + `pg_monitor`,
`docs/postgres-role.md`) is sufficient for every member.
`timescaledb_information.*` is PUBLIC-readable without row filtering;
the size/stats functions perform no table ACL checks. The single
exception is row visibility in `job_errors` (FC-TSDB-05): full
fleet-wide error detail additionally requires membership in the
database-owner role — optional, documented, never required for the
snapshot to succeed.

## Version provenance

Each family result is attributable to the `extversion` +
capability-flag row collected by `timescaledb_extension_v1` in the
same snapshot, plus `metadata.json.pg_version` and the per-collector
`collected_at` in `collector_status.json`.

## Analyzer requirements unblocked (follow-up issue)

TS-R001 excessive chunk count, TS-R002 chunk interval risk, TS-R003
compression opportunity, TS-R004 compression backlog, TS-R005 cagg
refresh lag, TS-R006 retention policy ineffective, TS-R007 background
job failures, TS-R008 hypertable candidate, TS-R009 time-series index
recommendation.
