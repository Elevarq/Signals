# Feature Specification: Arq Signals

## Purpose

Arq Signals is the open-source PostgreSQL diagnostic signal collector. It
connects to PostgreSQL instances, executes approved read-only SQL collectors,
produces structured snapshots of diagnostic data, and packages them for
transfer or storage. It contains no analysis, scoring, recommendations, or
LLM integration.

## Scope

### In Scope
- PostgreSQL connectivity and credential management
- Versioned SQL query catalog with safety linting
- Cadence-based scheduled collection
- Structured snapshot output (NDJSON + metadata)
- ZIP export for snapshot transfer
- CLI for collection operations
- HTTP API for collection and export endpoints
- Local persistent storage for collected signals
- BSD-3-Clause open-source distribution

### Out of Scope
- Requirement checking or scoring
- Derived statistics computation
- LLM integration or report generation
- Recommendations or root-cause analysis
- Proprietary analysis logic
- Dashboard (analysis views)
- Licensing / feature gating

## Inputs
- PostgreSQL connection parameters (host, port, dbname, user, credentials)
- Configuration file (`signals.yaml`) or environment variables
- Optional: target filters, cadence overrides, export time range

## Outputs
- Structured snapshots stored in local persistent storage
- ZIP export packages containing:
  - `metadata.json` (collector version, timestamp, PG version, target info)
  - `collector_status.json` (execution outcome per collector — see R072)
  - `query_catalog.json` (executed query definitions)
  - `query_runs.ndjson` (execution metadata per query)
  - `query_results.ndjson` (raw result payloads)

## Requirements

### Collection

**ARQ-SIGNALS-R001**: The system shall connect to a PostgreSQL instance using
supplied connection parameters (host, port, dbname, user, and one of:
password, password_file, password_env, pgpass_file).

**ARQ-SIGNALS-R002**: The system shall execute only approved collector queries.
Approval is enforced by a static linter that rejects DDL, DML, dangerous
functions, and multi-statement SQL at registration time. Unapproved queries
shall cause the process to abort at startup.

**ARQ-SIGNALS-R003**: The system shall collect diagnostic data from PostgreSQL
using at minimum the following versioned collectors:
- `pg_version_v1` — server version
- `pg_settings_v1` — runtime configuration
- `pg_stat_activity_v1` — active sessions
- `pg_stat_database_v1` — database-level statistics
- `pg_stat_user_tables_v1` — table statistics
- `pg_stat_user_indexes_v1` — index statistics
- `pg_statio_user_tables_v1` — table I/O statistics
- `pg_statio_user_indexes_v1` — index I/O statistics
- `pg_stat_statements_v1` — query statistics (when extension is installed)

Additional collectors (e.g. wraparound detection) may be registered.

**ARQ-SIGNALS-R004**: The system shall write collected outputs in a structured
snapshot format. Each query result shall be stored as NDJSON (one JSON object
per row, newline-delimited). Payloads exceeding 4096 bytes shall be
gzip-compressed.

**ARQ-SIGNALS-R005**: The system shall include snapshot metadata with each
collection run, including at minimum:
- collection timestamp (RFC3339)
- collector version (semver + commit hash)
- PostgreSQL server version
- target identifier

**ARQ-SIGNALS-R006**: The system shall package snapshots into a ZIP archive for
transfer or storage. The archive shall contain metadata.json,
collector_status.json, query_catalog.json, query_runs.ndjson, and
query_results.ndjson.

### Safety

**ARQ-SIGNALS-R007**: The system shall not perform scoring, recommendations,
root-cause analysis, or LLM interaction. No module in the Arq Signals
codebase shall depend on components that implement these functions.

**ARQ-SIGNALS-R008**: The system shall operate without network calls to
external AI services. No transport for LLM communication shall be present
in the codebase.

**ARQ-SIGNALS-R009**: The system shall be suitable for open-source release
under the BSD-3-Clause license. The repository shall contain no proprietary
analysis logic, no proprietary prompts, no confidential content, and no
credentials.

### Interface

**ARQ-SIGNALS-R010**: The system shall expose a stable CLI with at minimum
the following commands:
- `collect now` — trigger an immediate collection cycle
- `export` — download a snapshot ZIP archive (with optional output path)
- `status` — show collector status and target connectivity
- `version` — print version information

The CLI shall communicate with the running collector via its HTTP API. The
API address and authentication token shall be configurable via flags or
environment variables.

**ARQ-SIGNALS-R011**: The system shall expose an HTTP API on a configurable
address with the endpoints, response schemas, and authentication requirements
defined in Appendix A (API Contract).

**ARQ-SIGNALS-R012**: The system shall use per-query timeouts and a per-target
time budget to prevent slow queries from blocking collection of other targets.
The effective timeout for any single query is the minimum of: the query's own
timeout, the configured query timeout, and the remaining target time budget.

**ARQ-SIGNALS-R013**: All PostgreSQL connections shall be read-only, enforced
by three layers:
1. Static linter rejecting DDL/DML at registration
2. Session-level `default_transaction_read_only=on`
3. Per-query `BEGIN ... READ ONLY` transaction

**ARQ-SIGNALS-R014**: The system shall filter eligible queries by PostgreSQL
major version and installed extensions. Queries requiring unavailable
extensions or unsupported versions shall be silently skipped.

**ARQ-SIGNALS-R015**: The system shall support cadence-based scheduling with
at minimum: 5m, 15m, 1h, 6h, 24h, and 7d intervals. Each query declares its
own cadence. The scheduler shall not catch up missed intervals.

**ARQ-SIGNALS-R016**: Credentials shall never be cached in memory beyond the
scope of a single connection attempt, never written to persistent storage,
and never included in snapshot exports.

### Runtime Safety

**ARQ-SIGNALS-R017**: The system shall validate that the PostgreSQL session
can be placed into a read-only transaction posture before executing any
collector queries. If the session cannot be confirmed as read-only,
collection for that target shall fail with a clear error.

**ARQ-SIGNALS-R018**: The system shall refuse collection when the effective
PostgreSQL role has the superuser attribute (rolsuper = true).

**ARQ-SIGNALS-R019**: The system shall refuse collection when the effective
PostgreSQL role has the replication attribute (rolreplication = true).

**ARQ-SIGNALS-R020**: The system shall refuse collection when the effective
PostgreSQL role has the bypass RLS attribute (rolbypassrls = true).

**ARQ-SIGNALS-R021**: The system shall enforce read-only transaction execution
for every collector run by using BEGIN ... READ ONLY and verifying
session-level default_transaction_read_only is set to on.

**ARQ-SIGNALS-R022**: The system shall set conservative session-local timeouts
for collector execution. At minimum: statement_timeout (from configured query
timeout), lock_timeout (5 seconds), and idle_in_transaction_session_timeout
(from configured target timeout).

**ARQ-SIGNALS-R023**: The system shall distinguish between hard safety failures
(superuser, replication, bypassrls, read-only verification failure) that block
collection, and non-critical hygiene warnings (e.g. role is member of
pg_write_all_data) that are logged but do not block.

**ARQ-SIGNALS-R024**: The system shall not expose database passwords or secrets
in logs, API responses, status output, or exported snapshots. Credential
resolution errors shall be redacted before logging or returning to callers.

**ARQ-SIGNALS-R025**: The system shall provide clear, actionable operator-facing
error messages when safety posture validation fails, including which check
failed and how to remediate.

**ARQ-SIGNALS-R026**: The system shall support an explicit unsafe override via
the ARQ_SIGNALS_ALLOW_UNSAFE_ROLE environment variable (default: false). When
enabled, blocked role attributes are downgraded to warnings. Unsafe mode shall
be recorded in export metadata as unsafe_mode: true with the specific bypassed
checks listed.

### Configuration

**ARQ-SIGNALS-R027**: The system shall support configuration via a YAML file
and/or environment variables, with the schema defined in Appendix B
(Configuration Schema). Environment variables shall take precedence over
file-based values.

**ARQ-SIGNALS-R028**: The system shall search for configuration files in
order: explicit path via CLI flag, then system path `/etc/arq/signals.yaml`,
then local path `./signals.yaml`. The first file found is used.

**ARQ-SIGNALS-R029**: The system shall support configuring a single
PostgreSQL target entirely via environment variables (ARQ_SIGNALS_TARGET_*)
for containerized deployments. See Appendix B for the full variable list.

**ARQ-SIGNALS-R030**: The system shall validate configuration at startup and
reject invalid values (e.g. unparseable durations, empty required fields).
Non-blocking warnings (e.g. weak TLS mode) shall be logged without aborting.

### Collection Cycle Semantics

**ARQ-SIGNALS-R031**: The system shall run collection cycles at a configurable
interval (default: 5 minutes). The first cycle after startup shall force
execution of all eligible queries regardless of cadence, to establish a
baseline.

**ARQ-SIGNALS-R032**: The system shall prevent overlapping collection cycles.
If a cycle is still running when the next interval triggers, the new cycle
shall be skipped with a warning.

**ARQ-SIGNALS-R033**: The system shall collect from multiple targets
concurrently with a configurable maximum parallelism (default: 4). A failure
on one target shall not block or delay collection from other targets.

### Data Integrity

**ARQ-SIGNALS-R034**: If the PostgreSQL read-only transaction fails to commit
after queries have been executed, the system shall not persist query results
or record the collection as successful. The transaction commit result must be
checked and a commit failure must abort the success path for that target.

### Export Metadata

**ARQ-SIGNALS-R035**: The export metadata.json shall contain at minimum the
fields defined in Appendix A, section "Export metadata schema." When unsafe
mode is active, the metadata shall include `unsafe_mode: true` and
`unsafe_reasons` listing the specific bypassed checks. When the export
carries snapshot data, the metadata shall additionally include the
`snapshot_count` and `ingest_mode` fields defined in R086.

### Export Scope

**ARQ-SIGNALS-R084**: The default export scope is the **latest run of
each collector per active target**. An `arqctl export` invocation with
no selector flags shall package, for each target with at least one
recorded collector run, the most recent run of every collector that
has ever run against that target — the row in `query_runs` with the
largest `collected_at` for each distinct `(target_id, query_id)` pair —
together with the snapshots those runs belong to. Targets that have
never produced a recorded run are omitted from the default export.

This rule replaces both the v0.3.x default that aggregated every
snapshot into a single ZIP and the interim "latest completed snapshot
per target" default (superseded 2026-05; see issue #5). The
all-snapshots behavior remains available behind the explicit `--all`
selector (R085); a single point-in-time snapshot remains available
behind `--snapshot-id`.

Rationale: collectors run at different cadences (5m/15m/1h/6h/24h). A
single collection cycle persists a **new** snapshot containing only the
collectors that were *due* that cycle, so the single latest snapshot
per target is **not** a complete current picture — immediately after a
5-minute cycle it carries only the 5m collectors and silently drops the
15m/1h/6h/24h evidence, undermining R072 (completeness). Scoping the
default to the latest run *per collector* restores completeness across
cadences while preserving R084's original intent: one default export =
one analyzer ingest = one analysis cycle. It does not reintroduce the
quadratic-history problem the latest-snapshot default was created to
solve — each collector contributes exactly one (its most recent) run,
not its full history. Full history remains behind `--all`.

**ARQ-SIGNALS-R085**: Operators shall be able to widen or narrow the
export scope via mutually-named selectors. The CLI (`arqctl export`)
and the HTTP API (`GET /export`) shall support, in addition to the
default-latest scope from R084:

| Selector | CLI flag | HTTP query param | Semantics |
|---|---|---|---|
| All snapshots | `--all` | `all=true` | Every row in `snapshots`, oldest to newest. The pre-R084 default behavior, retained for forensics and operator-driven full-history exports. Mutually exclusive with `--snapshot-id`. |
| Single snapshot | `--snapshot-id <id>` | `snapshot_id=<id>` | Exactly the named snapshot row. 404 if the ID is unknown (FC-08). |
| Time range | `--since <RFC3339>`, `--until <RFC3339>` | `since=<RFC3339>`, `until=<RFC3339>` | Snapshots whose `collected_at` falls in the half-open range. Existing R035-era filtering, now first-class. |
| Target filter | `--target-id <int>` | `target_id=<int>` | Restrict any of the above scopes to a single target. Composes with the others (R073 unchanged). |

`--all` and `--snapshot-id` are mutually exclusive; supplying both is
an input error and shall be rejected with a 400 / non-zero exit.

When neither `--all`, `--snapshot-id`, `--since`, nor `--until` is
provided, the export uses the R084 default scope. `--target-id` alone
narrows the R084 default to one target.

**ARQ-SIGNALS-R086**: The export metadata shall name its scope and
intent explicitly via two new fields under R035:

| Field | Type | Semantics |
|---|---|---|
| `snapshot_count` | integer | The number of distinct `snapshots` rows packaged in this ZIP **after R090 filtering** (orphaned `target_id`s are excluded from the default scope but appear under `--all`). Always populated. For the R084 default export this is the number of distinct snapshots the latest-per-collector runs belong to — **at least** the number of active targets, and more when a target's collectors span cadences that last fired in different cycles; for `--snapshot-id` it is 1; for `--all` it is the size of the daemon's store at the moment of export, including any orphaned snapshots. |
| `run_scope` | string | One of `"latest-per-collector"` or `"snapshot"`. `"latest-per-collector"` marks the R084 default export: runs are assembled as the most recent run per `(target_id, query_id)`, so two runs in the same export may carry **different** `collected_at` values. `"snapshot"` marks every selector-scoped export (`--all`, `--snapshot-id`, `--since/--until`), whose runs are exactly those belonging to the selected snapshot rows. Consumers use this to know whether per-run `collected_at` must be read individually (latest-per-collector) or can be taken from the snapshot. A consumer that does not recognise the value, or finds it absent (legacy producer), MUST treat the export as `"snapshot"`. |
| `ingest_mode` | string | One of `"analyze"` or `"history_only"`. Indicates how the consuming Analyzer should process this export. The default `arqctl export` (R084 scope) sets `ingest_mode = "analyze"` — the consumer treats it as a current snapshot and may run full report generation. The backlog-replay flow defined in R087 sets `ingest_mode = "history_only"` for snapshots that pre-date the most recent one in a replay burst. |

`ingest_mode` is advisory metadata, not enforcement. The Analyzer side
of this contract (a sibling specification in the `arq` repository)
chooses what stages to run for each mode. R086 only specifies the
producer's labelling discipline.

For backward compatibility during the transition period (one release
cycle), an export ZIP missing these fields shall be treated by the
Analyzer as `snapshot_count` equal to the number of distinct
`snapshot_id` values observed in `query_runs.ndjson` and
`ingest_mode = "analyze"`.

### Collector Freshness Metadata

**ARQ-SIGNALS-R107**: Because the R084 default export assembles the
latest run *per collector*, runs in one export may carry different
`collected_at` values, and a low-cadence collector (e.g. 24h) may be
present but old. A consumer must be able to judge each collector's
freshness from the export alone, without external state. For every
collector entry in `collector_status.json`, the export shall carry:

| Field | Semantics |
|---|---|
| `collected_at` | RFC3339 timestamp of the specific run this entry describes (per-collector — not a single export-level time). |
| `cadence` | The collector's expected cadence as a duration string (`"5m"`, `"15m"`, `"1h"`, `"6h"`, `"24h"`). |
| `freshness` | One of `fresh`, `stale`, `never_run` (defined below). |

Freshness classification:

- `fresh` — the latest run's age (`now - collected_at`) is at most
  twice the collector's cadence (one full missed cycle of slack to
  avoid flapping at the boundary).
- `stale` — the latest run is older than twice the cadence: at least
  one full cycle was missed.
- `never_run` — the collector is eligible for the target (passes the
  R098/R075 eligibility filter) but has no run in the default scope.

The **target-scoped** default export (`--target-id`) shall additionally
enumerate eligible-but-never-run collectors as `never_run` entries so a
consumer can distinguish "collector ran and is current" from "collector
has never produced data." `never_run` enumeration is limited to
target-scoped exports because the instance-level
`collector_status.json` carries no per-entry target attribution, so a
`never_run` row there would be ambiguous across targets; `collected_at`,
`cadence`, and the `fresh`/`stale` classification are unambiguous and
appear in both. Collectors that are ineligible for a target
(version/extension/profile gated) are not `never_run` — they are
recorded as `skipped` runs every cycle (R072), so a registered
collector with no run at all for a cycled target is one whose cadence
has simply not fired yet.

### Version-Sensitive Collectors

**ARQ-SIGNALS-R037**: For diagnostic views whose schema varies across
PostgreSQL or extension versions (such as pg_stat_statements), the
collector shall capture the complete returned row shape dynamically.
Each row shall be serialized using the actual column names returned by
PostgreSQL at runtime. The collector shall not depend on a fixed column
list or fixed column positions for these views.

**ARQ-SIGNALS-R038**: If a version-sensitive collector query fails (e.g.
due to a missing or renamed column), the failure shall be isolated to
that query. Other collector queries in the same collection cycle shall
continue executing and producing results.

**ARQ-SIGNALS-R039**: Dynamic capture shall not weaken the read-only
safety model, credential handling guarantees, or export format
conventions.

### Diagnostic Pack 1

**ARQ-SIGNALS-R040**: The system shall collect server identity information
including PostgreSQL version number, server uptime, connected database name,
and database size.

**ARQ-SIGNALS-R041**: The system shall collect an inventory of installed
PostgreSQL extensions with their version information.

**ARQ-SIGNALS-R042**: The system shall collect checkpoint and background
writer health statistics from pg_stat_bgwriter.

**ARQ-SIGNALS-R043**: The system shall collect long-running transactions
(older than a configurable threshold) including PID, user, age, and a
truncated query snippet. Query text shall be truncated to prevent
capturing large query bodies.

**ARQ-SIGNALS-R044**: The system shall collect active lock-blocking chains
showing which sessions are blocking other sessions, including wait
durations.

**ARQ-SIGNALS-R045**: The system shall collect an inventory of login-capable
roles with their privilege flags (superuser, createdb, createrole,
replication, bypassrls). The collector shall NOT access password hashes
or the pg_authid table.

**ARQ-SIGNALS-R046**: The system shall collect connection utilization
metrics including total, active, idle, and idle-in-transaction counts
relative to max_connections.

**ARQ-SIGNALS-R047**: The system shall collect planner statistics staleness
indicators including estimated vs actual row counts, modifications since
last analyze, and estimate drift percentage.

**ARQ-SIGNALS-R048**: When pg_stat_statements is installed and
pg_stat_statements_info is available (PG 14+), the system shall collect
the statistics reset timestamp. When pg_stat_statements or
pg_stat_statements_info is unavailable, this collector shall be
gracefully skipped.

### Server Survival Pack

**ARQ-SIGNALS-R049**: The system shall collect replication slot status
including retained WAL size and active/inactive state. When no
replication slots are configured, the collector shall return an empty
result without error.

**ARQ-SIGNALS-R050**: The system shall collect replication status
including connected replicas, lag indicators, and sync state. When no
replicas are connected, the collector shall return an empty result
without error.

**ARQ-SIGNALS-R051**: On PostgreSQL 17 and later, the system shall
collect checkpoint statistics from pg_stat_checkpointer. On earlier
versions, this collector shall be gracefully skipped.

**ARQ-SIGNALS-R052**: The system shall collect a high-signal vacuum
health diagnostic that includes dead tuple percentage, XID freeze age,
autovacuum configuration overrides, and vacuum/analyze recency. This
collector adds operator-oriented context beyond raw table statistics.

**ARQ-SIGNALS-R053**: The system shall collect an actionable list of
backends in idle-in-transaction state, including PID, user, application,
transaction age, and a truncated query snippet.

**ARQ-SIGNALS-R054**: The system shall collect all database sizes for
growth monitoring and disk-risk triage.

**ARQ-SIGNALS-R055**: The system shall collect the largest user relations
by disk size to support storage triage.

**ARQ-SIGNALS-R056**: The system shall collect per-database temporary
file and byte usage to detect work_mem exhaustion pressure.

### Schema Intelligence Pack

**ARQ-SIGNALS-R057**: The system shall collect a constraint inventory
from pg_constraint including constraint type, table, column(s), and
referenced table for foreign keys. Multi-column constraints shall be
unnested with ordinal position.

**ARQ-SIGNALS-R058**: The system shall collect an index definition
inventory from pg_indexes including schema, table, index name, and
the full CREATE INDEX definition text.

**ARQ-SIGNALS-R059**: The system shall collect column-level planner
statistics from pg_stats including n_distinct, correlation, null_frac,
and avg_width. Data sample columns (most_common_vals, histogram_bounds)
shall be excluded.

**ARQ-SIGNALS-R060**: The system shall collect a column inventory from
pg_attribute with type information via format_type(). Default expression
text shall NOT be emitted (security). System columns (attnum <= 0) and
dropped columns shall be excluded.

**ARQ-SIGNALS-R061**: The system shall collect a schema namespace
inventory from pg_namespace with owner information.

**ARQ-SIGNALS-R062**: The system shall collect a view inventory from
pg_views (metadata only, no definition text).

**ARQ-SIGNALS-R063**: The system shall collect view definitions from
pg_get_viewdef in a separate collector from the inventory.

**ARQ-SIGNALS-R064**: The system shall collect a materialized view
inventory including populated and indexed status.

**ARQ-SIGNALS-R065**: The system shall collect materialized view
definitions in a separate collector from the inventory.

**ARQ-SIGNALS-R066**: The system shall collect partition topology
from pg_partitioned_table and pg_inherits including parent-child
relationships and partition bounds.

**ARQ-SIGNALS-R067**: The system shall collect a trigger inventory
from pg_trigger using tgtype bitmask encoding. Internal triggers
(tgisinternal) shall be excluded.

**ARQ-SIGNALS-R068**: The system shall collect trigger definitions
from pg_get_triggerdef in a separate collector.

**ARQ-SIGNALS-R069**: The system shall collect a function/procedure
inventory from pg_proc (PG 11+) including language, kind (function/
procedure/aggregate/window), and volatility. Function bodies shall
NOT be included in the inventory collector.

**ARQ-SIGNALS-R070**: The system shall collect function body
definitions from pg_proc.prosrc in a separate high-sensitivity
collector.

**ARQ-SIGNALS-R071**: The system shall collect a sequence inventory
from pg_sequences including data type, current value, min/max,
increment, and cycle configuration.

### Collector Execution Model

**ARQ-SIGNALS-R072**: The system shall record the execution outcome
of every registered collector for each snapshot cycle. The status
metadata (collector_status.json) shall be included in every export
ZIP alongside metadata.json. The schema is defined in
specifications/collector_status.md. Status values:
- success: query ran and returned results (or legitimate empty)
- partial: query ran with known limitations
- skipped: query was not attempted (version, extension, config)
- failed: query was attempted but produced an error

Reason categories for non-success statuses:
- version_unsupported (skipped)
- extension_missing (skipped)
- config_disabled (skipped)
- budget_exhausted (skipped) — the collector was due and eligible but
  the target's per-cycle time budget elapsed before it was attempted
  (R108)
- execution_error (failed)
- permission_denied (failed)
- timeout (failed)
- savepoint_rollback (failed)

**ARQ-SIGNALS-R108**: When a target's per-cycle time budget elapses
mid-collection, the system shall record a `skipped` run with
`reason=budget_exhausted` for **every** remaining due collector that
did not get a turn, so the status inventory is complete. This applies
at both points the collection loop can stop early on budget: before a
collector is attempted, and after a collector's own query times out
against the exhausted budget.

Consequences:

- The cycle's overall status is `partial` whenever any collector was
  skipped for `budget_exhausted` (in addition to the existing
  `partial`-on-failure rule).
- Recording the skipped inventory must survive the exhausted budget:
  the bookkeeping that persists the cycle (the read transaction's
  commit and the SQLite write) shall not be governed by the elapsed
  per-cycle budget context, so an over-budget cycle still persists its
  complete status inventory rather than discarding the whole cycle.

This closes the gap where an over-budget cycle marked some collectors
successful while leaving the remaining due collectors with no row at
all — a consumer could not distinguish "ran clean" from "never got a
turn" (R072 completeness).

**ARQ-SIGNALS-R109**: A disabled or removed target shall not appear in
the default export or in `arqctl status`. Specifically:

- The default export scope (R084 `GetLatestRunsPerCollector`) and the
  per-target snapshot helpers JOIN `targets` with `t.enabled = 1`, so a
  disabled target contributes no runs or snapshots to the default
  export. `--all` (R085) still surfaces disabled targets' history for
  forensics.
- `arqctl status` (`GET /status`) lists only enabled targets.
- The daemon reconciles the `targets.enabled` column against
  configuration on startup and on every reload: a target present and
  enabled in config becomes `enabled = 1`; a target disabled in config,
  or removed from it entirely, becomes `enabled = 0` (soft-disable).
  Soft-disable never deletes snapshots — disabled targets' history
  remains reachable via `--all`.

This closes the drift where the lazy per-collection `UpsertTarget`
(which runs only for enabled targets) left a previously-enabled
target's row at `enabled = 1` after it was disabled or removed, so its
stale snapshots kept appearing in the default export and `/status`.

**ARQ-SIGNALS-R110**: An export shall observe a consistent state of the
local store across all of its reads. The store issues several
independent reads to compose one export ZIP
(`GetLatestRunsPerCollector` -> `GetSnapshotsByIDs` ->
`GetQueryResultByRunID` per run -> `GetQueryCatalog`), and the daemon
runs a retention `cleanup()` on a timer that deletes `query_runs` and
`snapshots`. Without serialisation, a delete committing between the
export's reads can leave the export referring to rows that were just
removed — most visibly the "missing result payload for successful run"
hard error.

The store therefore serialises **exports against destructive writes**:
exports take a shared read lock; the destructive retention deletes
(`DeleteQueryRunsOlderThanByClass`, `DeleteSnapshotsOlderThan`) take an
exclusive write lock. Consequences:

- Multiple exports run concurrently (shared lock).
- A retention cycle that fires during an export waits for the export
  to complete; an export that starts while retention is running waits
  for retention to finish. Both operations are short-lived in practice.
- Concurrent collection commits are **not** serialised against
  exports: they only **add** rows, so an export reading "old state"
  before a commit remains internally consistent (no tear).

A future revision MAY upgrade this to a per-export SQLite read
transaction (true WAL MVCC snapshot) without changing the externally
observable invariant.

**ARQ-SIGNALS-R073**: The system shall support target-scoped export.
When exporting for a specific target, query_runs, query_results, and
collector_status shall contain only data for that target. The
collector_status shall be synthesized from query_runs for target
exports.

### Deterministic Ordering

**ARQ-SIGNALS-R074**: All collector output shall be deterministically
ordered. Specifically:
- Query catalog entries: ordered by query_id
- Collector status entries: ordered by collector id
- Schema collector results: ordered by ORDER BY clauses in the
  collector SQL (typically schema name, object name)
- Export ZIP file entries: written in a fixed order (metadata,
  collector_status, snapshots, catalog, runs, results)

### Collector Sensitivity

**ARQ-SIGNALS-R075**: The system shall classify as **high-sensitivity**
the collectors that emit application-authored SQL text or live
statement text:

- application-authored SQL definitions:
  `pg_views_definitions_v1`, `pg_matviews_definitions_v1`,
  `pg_triggers_definitions_v1`, `pg_functions_definitions_v1`;
- live `pg_stat_activity` statement text: `long_running_txns_v1`,
  `blocking_locks_v1`, `idle_in_txn_offenders_v1`,
  `wraparound_blockers_v1`.

High-sensitivity collectors run **by default** (collect-everything
default). An operator who prefers privacy over diagnostic richness opts
**out** by setting `signals.high_sensitivity_collectors_enabled: false`
(or `ARQ_SIGNALS_HIGH_SENSITIVITY_COLLECTORS_ENABLED=false`). This is a
one-time startup configuration, not a per-cycle decision. The opt-out
behaves per collector, declared on the `QueryDef` via a list of
sensitive column names (`SensitiveColumns`):

- **Redact** (live `pg_stat_activity` collectors with mixed
  sensitive/non-sensitive columns: `long_running_txns_v1`,
  `blocking_locks_v1`, `idle_in_txn_offenders_v1`,
  `wraparound_blockers_v1`). When `SensitiveColumns` is non-empty, the
  collector **still runs**, but the listed columns are set to `NULL` in
  persisted output. The non-sensitive columns (`pid`, `wait_event`,
  `txn_age_seconds`, `waiting_seconds`, …) survive, preserving the
  collector's diagnostic value (blocking-lock chain shape,
  idle-in-transaction visibility, long-running-tx detection).
- **Skip** (collectors whose row is itself the sensitive payload — DDL
  definitions, sampled-value stats, RLS policies / rewrite rules).
  When `SensitiveColumns` is empty/nil, the collector is dropped from
  the eligible set and recorded `status=skipped, reason=config_disabled`
  in `collector_status.json`. (Pre-2026-05 behavior preserved for
  these.)

Each high-sensitivity collector declares its own branch. The
classifying flag (`HighSensitivity=true`) is unchanged; the
`SensitiveColumns` list distinguishes the two opt-out paths and lets
new collectors choose redaction when they have meaningful non-sensitive
columns.

The per-target R098 `restricted` profile remains stricter than the
daemon-wide opt-out: a restricted profile drops **every** high-
sensitivity collector regardless of `SensitiveColumns` (no redaction
substitute). INV-SENS-01 still holds: per-target profile never widens
eligibility beyond the daemon-wide gate. The toggle is local operator
control over data sensitivity; it is not an exfiltration boundary (Arq
Signals runs inside the operator's own environment).

`metadata.json.high_sensitivity_collectors_enabled` records the
effective state so a consumer or auditor can tell, without parsing the
body, whether live `pg_stat_activity` query text or SQL definitions may
be present in the export.

This default was reversed from opt-in to opt-out in 2026-05 (issue #6):
the live query-text collectors had been default-on without proper
classification, and gating them off by default lost diagnostically
valuable signals (long-running transactions, blocking locks,
idle-in-transaction, wraparound risk). Default-on with classification +
opt-out keeps the signals available while making the sensitivity
explicit and giving operators a clean privacy toggle.

### Configuration Validation

**ARQ-SIGNALS-R076**: The system shall perform strict configuration
validation at startup, before any collection begins. Validation
distinguishes hard errors (abort with actionable message) from
warnings (log and continue). The full taxonomy is defined in
`appendix-b-configuration-schema.md` ("Validation rules"). In
particular: malformed `ARQ_SIGNALS_*` environment variable values
(e.g., non-integer for an integer field) are hard errors, not
silently dropped.

### Persistence

**ARQ-SIGNALS-R036**: The system shall persist collected data locally so that
it survives process restarts. The persistence layer shall support:
- Atomic writes (collection results stored transactionally)
- Retention-based cleanup (data older than configured days is deleted)
- Schema migration (storage schema is versioned and auto-migrated on startup)
- An instance identifier (generated on first run, stable across restarts)

**ARQ-SIGNALS-R077**: A collection cycle's query runs, query results,
and the legacy snapshot row shall be persisted atomically within a
single local-storage transaction. Partial persistence (e.g., legacy
snapshot present without query runs, or vice versa) shall not be
observable to readers or in exports.

The specific storage engine is an implementation choice, but the guarantees
above must be maintained.

### Audit logging and export metadata

**ARQ-SIGNALS-R078**: The system shall emit structured audit events
covering the operationally significant lifecycle moments — startup
configuration validation, per-target collection cycles, and export
requests — and shall extend export metadata with the fields required
to reconstruct the running posture of the daemon at the moment data
was produced. The intent is to support SOC 2 / ISO 27001 readiness:
auditors must be able to reconstruct *what* ran, *when*, *under what
configuration*, and *what was exported*, without learning *which
secrets* were involved.

Audit events are slog records carrying the structured key
`audit_event=<name>` plus typed attributes. Specifically:

- **Startup events**:
  - `audit_event=config_validated`, `status=ok|error`,
    `warnings=N`, `hard_errors=N`.
  - `audit_event=high_sensitivity_collectors`, `enabled=true|false`.
  - `audit_event=targets_loaded`, `enabled=N`, `disabled=N`.
- **Collection events** (per target, per cycle):
  - `audit_event=collection_started`, `target=<name>`.
  - `audit_event=collection_completed`, `target=<name>`,
    `snapshot_id=<id>`, `status=success|partial|failed`,
    `duration_ms=N`, `collectors_total=N`, `collectors_success=N`,
    `collectors_failed=N`, `collectors_skipped=N`.
- **Export events**:
  - `audit_event=export_requested`, `source_ip=<ip>`,
    `target_id=<id-or-empty>`, `since=<value>`, `until=<value>`.
  - `audit_event=export_completed`, `status=success|failed`,
    `duration_ms=N`, `size_bytes=N`, `error_category=<short tag>`
    (only on failure).

Audit events shall **never** include passwords, API tokens, full
connection strings, query result payloads, or any other field that
could exfiltrate secrets or production data. Field names that begin
with `password`, contain `token`, or hold a DSN-like value
(`postgres://`, `host=… password=…`) are explicitly banned from
audit-event attributes.

**Export metadata** (the `metadata.json` member of every export
ZIP) shall include at minimum:

| Field | Purpose |
|-------|---------|
| `arq_signals_version` | Build version of the daemon that produced the export. |
| `schema_version` | Snapshot/export schema version. |
| `generated_at` | Timestamp the export was produced (UTC, RFC 3339). |
| `instance_id` | Stable identifier of the producing daemon instance. |
| `target_name` | Target's logical name when the export is target-scoped; absent otherwise. |
| `high_sensitivity_collectors_enabled` | Whether the high-sensitivity gate (R075) was open at collection time. |
| `collector_status_schema_version` | Version of the `collector_status.json` schema, separate from the top-level snapshot schema. |

These fields make it possible for an auditor to determine whether a
given export contains application-authored SQL definitions (R075)
without having to parse the body of the ZIP, and to align the
export against the daemon version that produced it.

### Operational metrics endpoint

**ARQ-SIGNALS-R079**: The system shall expose an optional Prometheus
`/metrics` endpoint that publishes **operational health metrics
about the Arq Signals daemon itself**. The endpoint shall **never**
expose collected PostgreSQL data, SQL text, query results, view or
function definitions, or any sensitive payload — its scope is
limited to counters, gauges, and histograms describing the daemon's
own behaviour.

The endpoint is **disabled by default**. It is enabled by setting
`signals.metrics_enabled: true` (or the equivalent
`ARQ_SIGNALS_METRICS_ENABLED=true` environment variable). The
serving path defaults to `/metrics` and may be overridden via
`signals.metrics_path` (or `ARQ_SIGNALS_METRICS_PATH`). Setting the
path to `/health` is forbidden — the unauthenticated health endpoint
is reserved for liveness probes.

When enabled, the endpoint is mounted on the same HTTP listener as
the rest of the API and inherits the existing bearer-token auth
contract (R011). This is consistent with the rest of the API surface
and gives operators a single auth surface to manage. Operators that
prefer unauthenticated scraping should bind the listener to
loopback or to an internal network and rely on network-level
controls; the daemon itself does not relax auth on a per-path basis.

The metric set shall be exactly:

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `arq_signal_collection_cycles_total` | counter | `target`, `status` | Per-target collection cycles, labelled `success` / `partial` / `failed`. |
| `arq_signal_collection_failures_total` | counter | `target`, `reason` | Per-target hard failures (`reason` ∈ `connect_error`, `safety_check`, `persistence`, `internal`). |
| `arq_signal_collection_duration_seconds` | histogram | `target`, `status` | Wall-clock duration of each cycle. |
| `arq_signal_collectors_succeeded_total` | counter | `target` | Sum of per-cycle successful collector counts. |
| `arq_signal_collectors_failed_total` | counter | `target`, `reason` | Sum of per-cycle failed collector counts; `reason` is the same enum used in `collector_status.json` (`permission_denied`, `timeout`, `execution_error`). |
| `arq_signal_collectors_skipped_total` | counter | `target`, `reason` | Sum of per-cycle skipped collector counts; `reason` ∈ `config_disabled`, `version_unsupported`, `extension_missing`, `budget_exhausted`. |
| `arq_signal_export_requests_total` | counter | `status` | All export requests, labelled `success` / `failed`. |
| `arq_signal_export_failures_total` | counter | `error_category` | Failed exports, keyed by the same category emitted in audit logs. |
| `arq_signal_export_duration_seconds` | histogram | `status` | Wall-clock duration of each export. |
| `arq_signal_sqlite_persistence_failures_total` | counter | (none) | Count of `InsertCollectionAtomic` failures (R077 rollbacks). |
| `arq_signal_last_successful_collection_timestamp` | gauge | `target` | Unix seconds of the most recent successful collection per target. |
| `arq_signal_high_sensitivity_collectors_enabled` | gauge | (none) | `1` if the R075 gate is open, `0` otherwise. |

Label cardinality is bounded:

- `target` ranges over operator-configured targets (a small fixed
  set per deployment).
- `status`, `reason`, and `error_category` are fixed enums whose
  values are listed in the table.

The following label values are **explicitly forbidden** because they
would create unbounded cardinality or reintroduce sensitive content:
collector / query IDs, database names, host names, user names, file
paths, raw error message bodies, SQL text.

### Version-aware query catalog

**ARQ-SIGNALS-R081**: The system shall determine the connected
target's PostgreSQL major version, installed extensions, current
database, and current user via a single discovery probe at the start
of each collection cycle, before any catalog filtering. The discovery
result drives version-specific SQL selection so collectors continue
to work as PostgreSQL evolves its `pg_stat_*` schema.

The system supports first-class catalogs for PostgreSQL major
versions **14, 15, 16, 17, and 18**. PostgreSQL 19 is treated as
**experimental**: the collector falls back to the highest supported
catalog (PG 18) and logs a warning so operators see the
experimental status. Major versions below 14 are out of scope.

Per-major catalog files (`internal/pgqueries/catalog_pg14.go`
through `catalog_pg19.go`) carry **only the SQL that differs from
the version-agnostic default**. Most collectors share one default
SQL across all supported majors and need no per-version override.
This minimises duplication and keeps the SQL diff visible per major.

The following invariants apply to any version-specific SQL override:

- **Stable logical IDs**: a collector's logical ID
  (`pg_stat_io_v1`, etc.) is the same across all majors. Consumers
  see one ID; only the SQL underneath changes. There is no
  `pg_stat_io_v1_pg18` flavour.
- **Normalized output columns**: when PostgreSQL renames or
  restructures columns between majors, each version's SQL emits the
  same canonical column set (the union of pre- and post-rename
  columns). Columns that don't have a native source on a given
  major are emitted as `NULL` of the appropriate type.
- **No new SELECT *** in version-specific overrides. Any SELECT *
  that exists is a documented R037 dynamic-capture exemption (see
  `pg_stat_statements_v1`) or a pre-existing cross-version
  compromise to be tightened in a follow-up.
- **Safety linter applies equally**: override SQL is lint-checked at
  registration time exactly like the default registry. Override SQL
  cannot weaken R002 (DDL/DML rejection) or R013 (read-only
  enforcement).

When a collector exists in the version-agnostic registry but has no
SQL that's executable on the connected major (because of removed
underlying views or unsupported syntax), the system shall emit a
`status=skipped, reason=version_unsupported` entry in
`collector_status.json` for that collector, the same way `R075`
emits `reason=config_disabled` for high-sensitivity collectors that
the operator did not opt into.

### Control-plane boundary

**ARQ-SIGNALS-R082**: The system shall operate in one of two modes
— **standalone OSS** (Mode A) and **Arq-managed** (Mode B) — and
shall preserve a clear trust boundary between locally-controlled
configuration and externally-driven orchestration.

This rule is **DESIGN-ONLY**. It defines the contract; runtime
behavior lands in phased follow-up work (see "Future implementation
plan" below). No code or operator-visible behavior changes from
R082's introduction.

#### Modes

**Mode A — Standalone OSS.** The default. Targets are configured in
`signals.yaml`; collection is driven locally on the operator's poll
interval. No external service can request collection or narrow the
target set. This is the mode every open-source user runs.

**Mode B — Arq-managed.** Optional. When enabled by local config,
Arq Signal accepts authenticated collection requests from the
commercial Arq control plane. The control plane may **narrow** the
configured target set to a subset (the contracted/licensed
databases) and trigger collection cycles. The target set in
`signals.yaml` remains the authoritative ceiling — Arq cannot
expand it.

#### Trust boundary

- Arq Signal runs inside the customer environment. Default behavior
  has no outbound data egress (R007 / R008 / R009 remain in force).
- Arq may also run inside the customer environment depending on
  the deployment topology; the design does not assume Arq is remote.
- Mode B requests are **authenticated** (Phase 1: bearer token,
  consistent with R011). Stronger auth (mTLS, signed JWTs) is a
  future extension.
- Arq Signal **does not enforce commercial licensing as a security
  boundary**. The collector is open source; an operator who removes
  the license check can do so trivially. The commercial value lives
  in Arq's analysis, recommendations, and reporting — not in
  obscured collector behavior.

#### Target selection

- The list of targets in `signals.yaml` is the **authoritative
  ceiling**. Mode B can only narrow this set, never expand it.
- Mode B requests reference targets by their configured `name`.
  Unknown names are rejected with an explicit reason; an unknown
  name in the request does not cause silent partial success.
- Targets marked `enabled: false` are not collected even when Mode
  B explicitly requests them. Future spec extensions may relax this
  behind an explicit `allow_disabled` flag; R082 does not.

#### Proposed API shape

```
POST /collect/now
Authorization: Bearer <token>
Content-Type: application/json

{
  "targets": ["prod-main", "prod-reporting"],
  "reason": "scheduled_arq_cycle",
  "request_id": "01J5K6T3HW2A4DGCXV5Z6P0M3R"
}
```

| Field | Type | Required | Behaviour |
|---|---|---|---|
| `targets` | string[] | optional | Subset of configured target names. When **absent**, behaviour matches Mode A — collect all enabled targets. When **present and non-empty**, the cycle's effective set is `targets ∩ enabled-configured-targets`. An **empty array** (`"targets": []`) is treated as a client bug and rejected with `400 Bad Request`; collectors are never silently dropped. Backward compatible: empty body / no body retains existing semantics. |
| `reason` | string matching `^[A-Za-z0-9_-]{1,64}$` | optional | Short label surfaced in audit events. Restricted to the same charset as `request_id` so neither field can carry log-injection bytes or unbounded whitespace. Not free-form prose. |
| `request_id` | string matching `^[A-Za-z0-9_-]+$` (≤ 32 chars) | optional | Correlation identifier propagated through to per-target audit events. Restricted to ASCII alphanumerics, `_`, and `-` so audit-log greppability stays predictable. When absent, Arq Signal generates a ULID (which already satisfies the regex). |

Response:

```
202 Accepted
Content-Type: application/json

{
  "request_id": "01J5K…",
  "accepted_targets": ["prod-main", "prod-reporting"],
  "rejected_targets": []
}
```

When `targets` includes any name that is not present in
`signals.yaml`, or any target marked `enabled: false`, or when
`targets` is an empty array, the request returns `400 Bad Request`
with the rejected names + reason. The cycle is **not triggered**;
disabled targets are never silently dropped from the accepted set.

#### Audit requirements

`/collect/now` emits one of three top-level audit events per
request, each carrying the actor and (when supplied) the
correlation id:

| Event | When emitted | Carries |
|---|---|---|
| `collect_now_requested` | request was accepted; cycle was queued. | `actor`, `request_id`, `requested_targets`, `accepted_targets`, optional `reason`. |
| `collect_now_rejected` | request failed validation. The cycle is **not** queued. | `actor`, `error` (one of `invalid_json`, `invalid_request_id`, `invalid_reason`, `empty_targets_array`, `targets_not_collectible`), plus the same target / id / reason fields as far as they were parsed before the rejection. |
| `collect_now_dropped` | request passed validation but the on-demand channel buffer is already full (a previous on-demand request is queued; R032 prevents overlapping cycles). The cycle for **this** request_id will not run. | `actor`, `request_id`, `reason_category=previous_request_pending`. |

Successful cycles also propagate the `request_id` through to the
per-target events:

- `collection_started` — `target`, plus `request_id` when non-empty.
- `collection_completed` — `target`, `snapshot_id`, `status`,
  `duration_ms`, the four `collectors_*` counters, plus
  `request_id` when non-empty.

Field semantics:

- `request_id` — when the caller did not supply one, Arq Signal
  generates a ULID. Always present on `collect_now_requested` and
  `collect_now_dropped`. May be absent on `collect_now_rejected`
  (e.g. an invalid `request_id` field never produces a usable id).
- `requested_targets` — explicit list when the request narrowed the
  cycle, the literal string `all_enabled` when the `targets` field
  was absent. Audit attribute values are bounded; never an
  unbounded label set.
- `accepted_targets` — list of target names actually scheduled.
- `rejected_targets` — list of `{name, reason}` records on
  `collect_now_rejected` for the per-target failure paths. The
  `reason` enum: `unknown_target`, `disabled_target`.
- `reason` — the request's optional `reason` label, surfaced
  verbatim (already charset-validated).
- `actor` — `local_operator` for every Phase 1 / Phase 2 request,
  regardless of request shape. The `arq_control_plane` actor value
  is reserved for Phase 3, where a separate
  `signals.arq_control_plane_token` distinguishes the control-plane
  identity from the operator identity. **Until Phase 3 ships, the
  presence of a `request_id` does not change the actor field** —
  inferring control-plane identity from request shape would let any
  caller forge the audit log.

The R078 audit-attribute denylist remains in force. Secrets, SQL
payloads, raw request bodies, and PG row data are never present
in audit attributes regardless of request shape.

#### Security requirements

- Bearer token on every Mode B request (same token mechanism as
  R011). Stronger auth (mTLS, signed JWTs, separate operator vs
  control-plane tokens) is future work.
- The request can **only narrow** the configured target set. Any
  target name not present in `signals.yaml` is rejected.
- High-sensitivity collectors (R075) remain gated by local config.
  Mode B cannot enable them.
- Existing per-IP rate limiter for invalid auth (R024) applies
  unchanged. **No new rate limiting is introduced by R082.** The
  collector's existing serialization (R032 — `running.TryLock` in
  `runCycle`) already prevents overlapping cycles: a flood of
  accepted Mode B requests collapses into one in-flight cycle plus
  log-level "skipped — previous cycle still running" entries. A
  dedicated rate limit on accepted requests can be added in
  Phase 4+ if abuse patterns appear.

#### Licensing model

| Layer | Owner |
|---|---|
| License validation | Arq |
| Contracted target list / customer entitlement | Arq |
| Analysis, scoring, recommendations | Arq |
| Enterprise workflow + reporting | Arq |
| Local extraction | Arq Signal |
| Local API + this endpoint | Arq Signal |
| Local snapshot export | Arq Signal |
| Operational metrics (R079) | Arq Signal |
| Safe collector execution | Arq Signal |

Arq Signal does **not** validate licenses, check entitlements, or
gate collection on a commercial signal. Operators running Mode A
get the full collector capability — that is by design and matches
the BSD-3-Clause license posture (R009). The commercial value
remains in Arq's analysis layer, not in obscured collector behaviour.

#### Non-goals (R082)

- No license enforcement inside Arq Signal.
- No remote SaaS control plane assumption (Mode B works equally
  well for in-cluster Arq).
- No collector profile selection — that is a separate future spec.
- No per-collector export view — R080 remains separate.
- No status callback channel from Arq Signal back to Arq.
  Mode B is fire-and-forget on the request side; Arq retrieves
  results via the existing `/export` endpoint.
- No per-`request_id` outcome tracking. R082 propagates the
  correlation id through audit events but does not expose a way
  to look up "what happened to request X?" via the API.
  Outcome-by-request_id retrieval is Phase 4+ work.
- No new rate limiting on accepted requests beyond the existing
  collector serialization (see Security requirements above).

#### Future implementation plan

| Phase | Scope | Spec status |
|---|---|---|
| 1 | `POST /collect/now` accepts optional JSON body with `targets` field. Empty array, unknown names, or disabled names → 400 with rejected list. Backward compatible with empty-body POSTs. Audit `actor` remains `local_operator`. | Implemented from R082 directly. |
| 2 | `request_id` (regex `^[A-Za-z0-9_-]+$`, ≤32 chars) + `reason` (≤64 chars) fields. Audit-event extension with `requested_targets` / `accepted_targets` / `rejected_targets`. Correlation id propagated through per-target `collection_started` / `collection_completed` events. Audit `actor` still `local_operator`. | Implemented from R082 directly. |
| 3 | `signals.mode: standalone \| arq_managed` config flag. Separate `signals.arq_control_plane_token` so the operator can distinguish actor identity in audit events. Mode B requires the flag to be set. **First phase in which the audit `actor` field can carry `arq_control_plane`.** | Implemented per R083 (v0.3.1). |
| 4 | Collector profiles, entitlement metadata exchange, per-`request_id` outcome lookup endpoint, status callback channel. Optional rate limiting on accepted Mode B requests if real-world abuse patterns appear. | Out of scope for R082 / R083. Separate spec. |

### Mode B authentication and configuration

**ARQ-SIGNALS-R083**: When the operator opts into Mode B (R082) by
setting `signals.mode: arq_managed`, the system shall accept
authenticated requests from the Arq control plane via a **separate
bearer token** distinct from the local API token. The audit `actor`
field is derived from *which token matched* — never from request
shape — so audit identity cannot be forged by a caller that holds
only the local API token.

This rule is **SHIPPED** (v0.3.1). The dual-token authentication
middleware, startup validation, audit-event actor decoration, and
mode gating are implemented in `internal/config/config.go`,
`internal/api/server.go`, and `internal/safety/audit.go`. Test
coverage is in `tests/signals_r083_managed_mode_test.go`
(TC-SIG-081..092).

#### Config proposal

```yaml
signals:
  # R083: Mode B opt-in. "standalone" (default) keeps Phase 1 /
  # Phase 2 behaviour byte-for-byte. "arq_managed" activates the
  # arq_control_plane_token check.
  mode: standalone

  # R083: Separate bearer token for the Arq control plane.
  # Used ONLY when mode=arq_managed. Supplied via file (preferred)
  # or env var; never as a YAML literal — same posture as
  # api.token (R011).
  arq_control_plane_token_file: /etc/arq/control-plane.token
  # alternative:
  # arq_control_plane_token_env: ARQ_CONTROL_PLANE_TOKEN
```

| Field | Type | Default | Validation |
|---|---|---|---|
| `signals.mode` | enum `standalone` \| `arq_managed` | `standalone` | hard error on any other value |
| `signals.arq_control_plane_token_file` | path | empty | required when `mode: arq_managed`; file is re-read on every authentication attempt to support rotation without restart |
| `signals.arq_control_plane_token_env` | env-var name | empty | mutually exclusive with `_file` |

The token value is **never accepted as a YAML literal** — same
posture as `api.token`. R078's audit-attribute denylist keeps
the token out of any audit record.

Env-var overrides (consistent with R076 / appendix B):

- `ARQ_SIGNALS_MODE` → `signals.mode`
- `ARQ_SIGNALS_ARQ_CONTROL_PLANE_TOKEN_FILE` → file path
- `ARQ_SIGNALS_ARQ_CONTROL_PLANE_TOKEN_ENV` → name of the env var
  carrying the token (indirection mirrors `password_env`)

#### Auth behaviour

The existing bearer-token middleware (R011) is extended to
compare the supplied token to **both** configured tokens in
constant time:

```
Authorization: Bearer <token>
       │
       ├─ matches api.token                   → actor = local_operator
       ├─ matches arq_control_plane_token     → actor = arq_control_plane
       │  (only when mode=arq_managed)
       └─ matches neither                     → 401, rate limiter records failure
```

Once the actor is determined, it is attached to the request
context and surfaced on every audit event the request emits.
The actor never changes mid-request and never depends on request
body shape (R082 invariant carried forward).

In `mode=standalone`, the `arq_control_plane_token` config (if
present) is **ignored at auth time** — only `api.token` is
consulted. A request that would have matched the control-plane
token simply gets a 401, identical to any other unknown token.
This keeps the standalone deployment posture identical to
Phase 1 / Phase 2.

#### Audit behaviour

The Phase 2 actor invariant ("always `local_operator`") relaxes:

| Phase | Audit `actor` source |
|---|---|
| 1 / 2 | always `local_operator` (field exists but always carries this value) |
| 3 | `local_operator` when `api.token` matched; `arq_control_plane` when `arq_control_plane_token` matched, **and only when `mode: arq_managed`** |

Audit events whose `actor` value is now sourced from the auth
match:

- `collect_now_requested` / `collect_now_rejected` /
  `collect_now_dropped` (R082 Phase 2 — these already carry
  `actor`; only the value changes)
- `collection_started` / `collection_completed` when correlated
  by `request_id`
- `export_requested` / `export_completed` (R078) — Phase 3
  extends these to carry `actor` so an auditor can distinguish
  exports triggered by the local operator from those triggered
  by the control plane

A new startup audit event records the active mode and whether a
control-plane token is configured. The token *value* is never
logged — only its configured/not-configured boolean status:

```
audit_event=mode_configured
mode=arq_managed
arq_control_plane_token_configured=true
```

#### Backward compatibility

- `mode: standalone` is the default. A daemon with no
  `signals.mode` setting behaves byte-for-byte like Phase 2.
- `api.token` continues to authorise everything it authorises
  today, in both modes. Operators do not need to migrate.
- The 202 / 400 / 401 response contracts on `/collect/now` and
  `/export` are unchanged.
- Phase 1 / Phase 2 audit-event names and attribute schemas are
  unchanged on the wire — only the `actor` value can now carry
  `arq_control_plane` (and only in Mode B with the control-plane
  token).
- Adding `actor` to `export_requested` / `export_completed` is
  additive: existing parsers that don't read the field continue
  to work.

#### Security failure cases

R076's `ValidateStrict` gains the following hard errors. Each
aborts startup with an actionable message:

| Failure | Cause |
|---|---|
| `signals.mode is "arq_managed" but no control-plane token is configured` | Operator activated Mode B without supplying a token. |
| `signals.arq_control_plane_token is identical to api.token` | The two tokens must be distinct so `actor` is unambiguous. |
| `signals.arq_control_plane_token is shorter than 32 characters` | Same length floor as the auto-generated `api.token`. |
| `signals.arq_control_plane_token_file` does not exist or is unreadable | Symmetric with the existing `api.token_file` handling. |
| `signals.arq_control_plane_token_file` and `_env` both set | Pick one — same posture as multi-credential rejection on targets. |
| `signals.mode` is any value other than `standalone` or `arq_managed` | Typo guard. |

Runtime considerations (not startup errors):

- **Token rotation:** the file is re-read on every authentication
  attempt, so rotating the token does not require restarting the
  daemon.
- **Cross-actor confusion:** a local operator who guesses or
  steals the Arq token would see their requests audited as
  `actor=arq_control_plane`. This is acceptable — token
  compromise of either token is a separate incident class. The
  audit field reflects reality: whoever sent the request had the
  control-plane token. R024's per-IP rate limiter on invalid
  attempts continues to apply. A compromise-response runbook is
  tracked as a separate docs follow-up; R083 itself only commits
  to making rotation possible.
- **Replay protection:** out of scope for R083. The bearer token
  is the only auth surface. Higher-strength auth (mTLS, signed
  JWTs, request-bound nonces) is Phase 4+ work.
- **Network-level attacks:** Mode B does **not** require Arq to
  be remote. The recommended deployment is Arq in-cluster with
  Arq Signal, both processes on a private network. R011's
  loopback-bind guidance still applies.

#### Tests planned

| TC | Coverage |
|---|---|
| TC-SIG-081 | `signals.mode` defaults to `standalone` when unset. |
| TC-SIG-082 | `mode: arq_managed` without a configured control-plane token → startup error from `ValidateStrict`. |
| TC-SIG-083 | `arq_control_plane_token` equal to `api.token` → startup error. |
| TC-SIG-084 | `arq_control_plane_token` shorter than 32 chars → startup error. |
| TC-SIG-085 | Both `_file` and `_env` configured → startup error. |
| TC-SIG-086 | Request with valid `api.token` in any mode → 2xx with `actor=local_operator` in the corresponding audit event. |
| TC-SIG-087 | Request with valid `arq_control_plane_token` in `mode=arq_managed` → 2xx with `actor=arq_control_plane`. |
| TC-SIG-088 | Request with valid `arq_control_plane_token` in `mode=standalone` → 401 (token is ignored, treated as unknown). |
| TC-SIG-089 | Request with unknown token → 401 + rate-limiter records failure (R024 unchanged). |
| TC-SIG-090 | Token rotation: replacing the file's contents and re-issuing a request authenticates against the new value within the same process. |
| TC-SIG-091 | `mode_configured` startup audit event emitted with mode and `arq_control_plane_token_configured` boolean; token value never appears in any audit attribute. |
| TC-SIG-092 | `export_requested` / `export_completed` audit events carry the `actor` field, value derived from the matched token. |

#### Non-goals (R083)

- **No license validation in Arq Signal.** R082's licensing-model
  invariant carries forward.
- **No mTLS / signed JWTs / OIDC.** Higher-strength auth is
  Phase 4+ work.
- **No `mode: arq_managed_only` (refusing the local API token in
  Mode B).** Possible future extension; R083 keeps the local
  token usable in both modes so operators are never locked out
  by an Arq outage.
- **No per-`request_id` outcome lookup endpoint.** Phase 4+.
- **No status callback channel.** Phase 4+.
- **No collector profiles or entitlement metadata.** Separate
  spec.
- **No token-rotation API.** The file-based pattern already
  supports zero-downtime rotation.
- **No token-rotation audit event.** The file re-read is silent
  by design; the audit record on the next request after rotation
  is sufficient observability. Operators that need explicit
  rotation visibility can wrap the rotation in a custom event
  outside Arq Signal.
- **No auth-failure audit events.** Existing 401 responses + R024
  per-IP rate limiter on invalid attempts are unchanged. Adding
  explicit `auth_failed` audit records would be noisy under bot
  scanning and require their own rate-limiting; this is deferred
  to a future audit-completeness pass.

### Backlog replay (DESIGN-ONLY)

**ARQ-SIGNALS-R087**: When the daemon ships pending snapshots upstream
after a delivery outage, it shall send **one ZIP per snapshot, in
ascending `collected_at` order**, never a multi-snapshot bundle. Each
ZIP shall carry:

- The R084 single-snapshot scope (one snapshot row, one target-cycle).
- The `snapshot_count = 1` field from R086.
- The `ingest_mode` field from R086 set as follows:
  - `ingest_mode = "history_only"` for every snapshot in the burst
    **except** the most recent.
  - `ingest_mode = "analyze"` for the most recent snapshot of the
    burst.

This achieves the intended Analyzer-side semantics without the
Signals daemon needing to know about Analyzer stages: older
snapshots restore lifecycle history (the Analyzer ingests them into
its internal SQLite without firing Insight or generating reports);
the most recent triggers a single current analysis. The Analyzer's
contract for honouring `ingest_mode` lives in a sibling specification
in the `arq` repository.

This rule is **DESIGN-ONLY**, consistent with R082's posture. It
fixes the contract so the producer and consumer can be implemented
independently and tested against the same shape; the actual delivery
transport (HTTP push, signed upload, or other) is out of scope per
R088.

**ARQ-SIGNALS-R088**: The delivery transport for upstream snapshot
push (the channel R087 describes) is **out of scope of this
specification slice**. Specifically, this spec does not commit to:

- HTTP POST vs signed-payload upload vs other transports.
- Authentication scheme beyond what R082 / R083 already specify.
- Spool storage shape (whether pending snapshots live in the existing
  `snapshots` table with a new `delivery_state` column, or in a
  separate `pending_deliveries` table, or another structure).
- Retry policy, exponential backoff parameters, or delivery
  observability metrics.

Those concerns belong to the runtime portion of R082 (Mode B
implementation) and shall be specified in their own follow-up. R087
defines only the on-the-wire shape any future transport must produce
(one ZIP per snapshot, ordered, mode-tagged).

### Target identity

**ARQ-SIGNALS-R089**: `UpsertTarget` MUST be **idempotent**.
Repeated calls with the same logical target — identified by the
configured `name` (the table's UNIQUE constraint) — MUST return the
same `targets.id` and MUST NOT cause `snapshots.target_id` to drift
across cycles.

The implementation MUST NOT rely on SQLite's `last_insert_rowid()`
(`sql.Result.LastInsertId()` in Go) after an UPSERT. SQLite's
`INSERT … ON CONFLICT … DO UPDATE` reserves an AUTOINCREMENT id from
`sqlite_sequence` **before** evaluating the conflict; when the DO
UPDATE branch fires, the reserved id is not used by the row but is
still returned by `last_insert_rowid()` and never reset. Treating
that wasted id as the row's id produces orphaned `target_id`
values in `snapshots` — every cycle drifts upward and the targets
table stops being a usable reference.

The reliable signal is `SELECT id FROM targets WHERE name = ?`
issued **after** the UPSERT statement. The fix MUST always run that
SELECT and MUST ignore `LastInsertId()`.

Operational evidence: a daemon running v0.3.x for 17 hours
accumulated 1,337 distinct `snapshots.target_id` values referencing
1 actual `targets` row (1,336 orphans). `arqctl status` reported 1
target; the v0.3.x default export (which then groups by
`snapshots.target_id`) saw 1,337. The two views disagreed because
`snapshots.target_id` was no longer a true reference. R089 closes
this drift.

**ARQ-SIGNALS-R090**: `GetLatestRunsPerCollector` (the R084 default
scope, and the same function narrowed to one target) MUST exclude
`query_runs` rows whose `target_id` does not reference an existing row
in `targets`. The legacy `GetLatestSnapshotsPerTarget` /
`GetLatestSnapshotForTarget` helpers carry the same exclusion for any
path that still uses them. The exclusion is performed at query time via
an explicit `JOIN targets t ON t.id = <table>.target_id` clause.

Rationale: defense in depth. Even with R089 in place going forward,
existing daemon stores carry historical orphans from the v0.3.x
drift. Filtering at export time means the default scope is safe to
ship without a destructive cleanup migration of customer data; the
orphans simply age out via `retention_days`.

The `--all` selector (R085) MUST NOT apply this filter — operators
need to see orphans for forensic / diagnostic exports. Likewise,
`--snapshot-id <id>` references a snapshot directly; if the named
row exists, it is returned regardless of target-row state, so
operators can recover a specific historical capture.

`--target-id <int>` continues to identify a physical row in the
`snapshots.target_id` column. Combined with the default scope it
returns "latest snapshot whose `target_id` matches AND that
target_id is in `targets`"; combined with `--all` it returns
**every** snapshot whose `target_id` matches, including orphan rows
— so operators retain forensic visibility into corruption by
combining `--all --target-id=<orphan>`.

### Minimum snapshot interval

**ARQ-SIGNALS-R091**: The system shall enforce a **minimum interval
between completed snapshots for the same logical target**. The
interval is configured via the new top-level
`signals.min_snapshot_interval` (default: `60s`) and is overridable
via the environment variable `ARQ_SIGNALS_MIN_SNAPSHOT_INTERVAL`
(same time-string format as `poll_interval`).

The logical target key for Beta is `targets.name` (R089). Two
distinct targets (different `name` values) do not block each
other.

When the daemon decides to collect for a target, it queries the
target's most recent completed snapshot (joining `targets` per
R090 so orphan rows are ignored) and computes
`elapsed = now - latest.collected_at`. If `elapsed <
min_snapshot_interval`, the target is **skipped** for that cycle:

- No new `snapshots` row is written.
- No new `query_runs` rows are written.
- No new `query_results` rows are written.
- The PG connection pool is NOT acquired for the skipped target.
- A single audit event `collection_skipped` is emitted with
  structured fields:
  ```
  level=INFO msg="collection skipped — min_snapshot_interval not elapsed"
       target=<name>
       reason_category="min_interval_not_elapsed"
       last_collected_at=<RFC3339>
       elapsed_ms=<int>
       min_interval_ms=<int>
       request_id=<...>   (when set, R082 Phase 2 correlation)
       actor=<...>        (when set)
  ```
- The existing `collection_started` and `collection_completed`
  events are NOT emitted for a skipped collection — those events
  represent actual work; emitting them around a skip would imply
  the cycle ran when it did not.

This rule applies uniformly to:

- Interval-driven cycles (the daemon's `poll_interval` ticker).
- Initial baseline cycle on daemon startup.
- On-demand cycles triggered by `arqctl collect now` and
  `POST /collect/now`.

The intent is to protect the system from rapid repeated collections
that would (a) bloat snapshots/runs/results storage, (b) create
duplicate lifecycle observations downstream of the analyzer, (c)
waste analyzer + Insight work on near-duplicate snapshots, and (d)
mask configuration mistakes (e.g. operator setting
`poll_interval=5s` accidentally) behind a normal-looking export.

A target with no completed snapshots is never skipped by R091 —
the first cycle for any target always runs.

**ARQ-SIGNALS-R092**: An explicit operator override (`--force` on
`arqctl collect now`, `force=true` on `POST /collect/now`)
bypasses R091 for that one cycle. The forced collection is
recorded in audit:

- `collection_started` carries `forced=true`.
- `collection_completed` carries `forced=true`.

This lets an operator force a fresh cycle for diagnostics without
loosening the global default. The override is per-request only —
it does not persist or change the configured interval.

`force=true` does NOT bypass:
- R032 (overlap prevention — only one cycle in flight at a time).
- Any per-query cadence (R015) — `forceAll` continues to govern
  that, independently of R092.
- Safety / role validation (R013, R018-R020).

### Failure conditions

- **FC-10**: Configuration sets `signals.min_snapshot_interval` to
  a non-positive value (zero or negative duration). Treated as a
  hard configuration validation error at startup; the daemon
  refuses to launch with a diagnostic naming the offending value.
  Disabling the protection is not supported in v1.x. (Operators
  who genuinely want every poll interval to result in a collection
  set `poll_interval >= min_snapshot_interval`.)

### Cluster identity

Two same-named databases on different physical PostgreSQL clusters
cannot be distinguished from an exported snapshot ZIP today.
`server_identity_v1` captures `database_name` and `version_num` but
no network-bound or cluster-level fingerprint, and the daemon's
`targets` row (host / port / dbname) is **not embedded** into the
export. A consumer reading a ZIP outside the collecting daemon
cannot tell `app` on host A from `app` on host B.

R093 and R094 close that gap end-to-end: a new collector emits the
cluster fingerprint, and the export contract carries the target's
connection identity into `metadata.json`.

**ARQ-SIGNALS-R093**: The system shall register a `cluster_identity_v1`
collector that emits exactly one row per collection cycle with the
following identity fields: `inet_server_addr`, `inet_server_port`,
`is_in_recovery`, `cluster_name`, `server_timezone`,
`last_wal_receive_lsn`, `last_wal_replay_lsn`,
`postmaster_start_time`.

The collector behavior is governed by
`specifications/collectors/cluster_identity_v1.md` (BEHAVIORAL,
status: ACTIVE).

Degrade-gracefully invariants:

- When the connection is via unix socket, `inet_server_addr` and
  `inet_server_port` are NULL and the collector's `status` is
  `success`.
- Empty-string `cluster_name` is coalesced to NULL.

The `pg_control_system().system_identifier` immutable fingerprint is
deliberately out of scope here — it is gated by `pg_read_all_stats` /
`pg_monitor` membership and the query linter prevents expressing the
standard `has_function_privilege(..., 'EXECUTE')` graceful-fallback
pattern. A future `cluster_system_identifier_v1` collector can be
added if operators need the immutable identifier; the composite key
`(inet_server_addr, inet_server_port, postmaster_start_time)` is
sufficient for v1 disambiguation.

The collector inherits the existing safety model: read-only,
single transaction, no superuser, no writes, no telemetry, no
credential material in output (R013, R018–R020, INV-SIGNALS-05,
INV-SIGNALS-07).

**ARQ-SIGNALS-R094**: Every export ZIP whose `metadata.json` is
anchored to a non-orphan `target_id` shall carry a `target_identity`
block sourced from the daemon's `targets` table for that target.
The block has the shape:

```json
{
  "target_identity": {
    "host": "<string>",
    "port": <integer>,
    "dbname": "<string>",
    "username": "<string>"
  }
}
```

Invariants:

- `target_identity` is **absent** when the snapshot's `target_id`
  does not resolve to a row in `targets` (the R090 orphan case).
- `target_identity` **never** contains password material, secret
  references, or `sslmode` — connection identity only, not
  authentication material (INV-SIGNALS-07).
- For multi-snapshot exports (`--all` or R084 default across N
  targets), `target_identity` MUST be emitted per snapshot in
  `snapshots.ndjson`, not only as a single top-level block. The
  `metadata.json` top-level block carries the identity of the
  scoped target when the export is single-target (R086
  `target_name` is present); otherwise the top-level
  `target_identity` is absent and per-snapshot identity is
  authoritative.
- Backward compatibility: existing consumers that don't read
  `target_identity` continue to work — it is an additive field.

### Failure conditions

- **FC-11**: `cluster_identity_v1` row collection fails mid-query
  (timeout, connection drop) → standard collector error path:
  `status=failed`, `reason=execution_error` or `timeout` in
  `collector_status.json`; no row in `query_results.ndjson`.
  R093's degrade-gracefully invariants apply only to expected
  NULLs (unix-socket inet fields, unset `cluster_name`, primary-side
  LSN), not to connection-level or query-level failures.
- **FC-12**: The daemon cannot resolve the `target_id` for a
  snapshot during export (orphan, per R090). The export
  succeeds; `target_identity` is omitted for that snapshot. The
  consumer treats absence as "identity unknown", not as an error.

### Pre-flight verification

Operator runbooks and CI gates need a single, shell-friendly check
that a deployment is wired up correctly before the daemon starts or
after a config change. Manually exercising every component (config
loader, store path, each target's connectivity, each target's role
safety) is error-prone and inconsistent across operators.

R095 introduces `arqctl doctor` as the canonical pre-flight tool,
promoting the previously informal "before editing arq-signals" check
into engineering-owned coverage.

**ARQ-SIGNALS-R095**: The system shall provide an `arqctl doctor`
subcommand that runs the following operator-facing read-only checks
and reports their union (no short-circuit between independent
checks):

| ID | Name | Behavior |
|----|------|----------|
| C1 | config_valid | Config file is parseable and `ValidateStrict` returns no error. |
| C2 | store_writable | The configured SQLite store directory exists and is writable. |
| C3 | target_reachable | Each enabled target's `host:port` accepts a TCP connection within 3s. |
| C4 | role_safe | Each reachable target's role passes `collector.ValidateRoleSafety` (no superuser / replication / bypassrls — R013, R018–R020). |
| C5 | collector_prerequisites | Each reachable target's enabled collectors are classified as `available` / `extension_missing` / `version_unsupported` / `config_disabled` via the same `pgqueries.GatedIDsByReason` logic the daemon runs at cycle start (EA-R001). |
| C6 | snapshot_freshness | Each enabled target's most recent completed snapshot (from the daemon's SQLite store) is fresher than 2× the configured `poll_interval`. OK below threshold; WARN above; informational WARN when the store can't be read (pre-daemon runs). |

The command behavior is governed by `specifications/doctor.md`
(BEHAVIORAL, status: ACTIVE) and exit code semantics:

- `0` — every check returned OK.
- `1` — one or more checks returned FAIL.
- `2` — usage error (unknown `--check` name, missing required arg).

Doctor MUST honor the existing safety invariants: no writes to PG
(INV-DOC-01), no writes to the SQLite store beyond a write-probe
that is immediately removed (INV-DOC-02), and no credential material
in any output channel (INV-DOC-03 / INV-SIGNALS-07). Dependent
checks (`role_safe` depends on `target_reachable`) emit WARN when
their upstream FAILed, so a single root cause is reported once
rather than amplified across downstream checks.

`--json` emits a stable machine-readable shape (see appendix). The
JSON contract carries `schema_version`, per-check `status` (lower-
case enum: `ok` | `warn` | `fail`), and a summary triple.

### Failure conditions

- **FC-13**: An `arqctl doctor --check=<name>` flag specifies an
  unknown check ID → the command exits 2 with a diagnostic listing
  supported IDs, before any check runs.

### Connection diagnostic

R095 (doctor) addresses the operator question "is the whole
deployment wired up?" by running a battery of checks. The
complementary operator question — "why won't *this* connection
work?" — is poorly served today: the daemon's connect failures
are buried in journald and `arqctl doctor` doesn't surface
classified reasons for one target.

**ARQ-SIGNALS-R096**: The system shall provide an
`arqctl connect test` subcommand that opens a single short-lived
connection attempt per target and classifies the outcome into one
of these categories:

| Category | Meaning |
|----------|---------|
| `ok` | Connection, authentication, and role-safety check succeeded. |
| `dns` | Hostname resolution failed. |
| `tcp` | TCP-layer failure (refused / timeout / unreachable). |
| `tls` | TLS handshake failure. |
| `auth` | PG authentication rejected (SQLSTATE 28P01 / 28000). |
| `startup` | Connected but session-init failed (database does not exist, SQLSTATE 3D000; PG version below the supported floor). |
| `role` | Connected but role validation finds an unsafe attribute (R013, R018–R020). |
| `password_resolve` | Configured secret source unreadable (env var unset, file unreadable, pgpass parse error). |
| `config` | Input parse failure (malformed `--dsn`). |

The subcommand surface is governed by
`specifications/connect-test.md` (BEHAVIORAL, status: ACTIVE).

Invariants:

- Read-only — every connection opens a read-only transaction even
  for the diagnostic `SELECT 1` (INV-SIGNALS-05 reaffirmed).
- Credentials never appear in any output channel; errors pass
  through `collector.RedactError` and DSNs through
  `collector.RedactDSN` (INV-SIGNALS-07 reaffirmed).
- Classification is deterministic — the same underlying error
  always maps to the same category.
- Multi-target output appears in config-declared target order.

Exit codes:

- `0` — every attempt returned `ok`.
- `1` — any non-`ok` outcome.
- `2` — usage error (`<target-name>` and `--dsn` both supplied,
  malformed `--dsn`, unknown target).

### Failure conditions (R096)

- **FC-14**: Both `<target-name>` and `--dsn` supplied → exit 2
  before any connection attempt.
- **FC-15**: `--dsn` missing a required field (`host`, `port`,
  `user`, `dbname`) → exit 2 with the missing field named.
- **FC-16**: Unknown `<target-name>` → exit 2 with the supported
  target list.

### Circuit breaker — per-target backoff + operator pause

R091 enforces a fixed floor between cycles, but does nothing when a
target is in genuine trouble: lock contention, repeated timeouts,
runaway queries. The daemon continues to poll at full cadence and
adds pressure to the exact incident operators need observability for.
And when an operator decides Signals itself is the problem during an
incident, the only existing recourse is to stop the daemon
(killing /status and the API along with it).

R097 introduces a per-target circuit breaker with three states
(`closed`, `open`, `paused`) covering both the auto-backoff and the
manual-override paths.

**ARQ-SIGNALS-R097**: The system shall track per-target circuit
state in memory and gate `collectTarget` invocations on it:

| State | Behaviour |
|-------|-----------|
| `closed` | Normal — cycles run. |
| `open` | Auto-disabled after `fail_threshold` (default 3) consecutive `collectTarget` errors. Skips cycles. Auto-recovers to `closed` after `open_cooldown` (default 5m). |
| `paused` | Manually disabled via `arqctl collect pause` or `POST /collect/pause`. Stays `paused` until explicit `resume`. |

Manual `paused` takes priority over the auto state (INV-CIRC-02).
State is in-memory only — restart resets all targets to `closed`
(INV-CIRC-01). Past pause/resume events live in the audit log so
the operator trail survives the restart.

A skipped cycle (open or paused) writes zero rows (INV-CIRC-03 —
identical posture to R091's INV-SIGNALS-15). The
`collection_skipped` audit event gains two new `reason_category`
values: `circuit_open` and `circuit_paused`.

The circuit gates collection but does NOT gate `--force` for R092
purposes — `--force` bypasses R091 only. To override a paused
target the operator runs `arqctl collect resume` first; this is
deliberate (you can't accidentally collect from a target you
manually paused).

Spec: `specifications/circuit-breaker.md` (BEHAVIORAL).

CLI / API surface:

- `arqctl collect pause [--target=NAME] [--reason="..."]`
- `arqctl collect resume [--target=NAME]`
- `POST /collect/pause` and `POST /collect/resume` (bearer-auth,
  same actor decoration as R083).
- `GET /status` includes `circuit_state` per target plus
  `circuit_paused_reason` / `circuit_paused_at` when paused.
- `/metrics` (R079) exposes `arq_signal_circuit_state{target,state}`
  gauge with one row per (target, state) pair.

Configuration:

```yaml
signals:
  circuit:
    fail_threshold: 3
    open_cooldown: 5m
```

### Failure conditions (R097)

- **FC-17**: `POST /collect/pause` with `reason` > 256 chars →
  `400 Bad Request`.
- **FC-18**: `POST /collect/resume` with an unknown target name
  → `400 Bad Request`. (Pause is more permissive: unknown target
  is a no-op, since operators may pause a target before adding
  it to config.)

### Per-target sensitivity profiles

R075's `signals.high_sensitivity_collectors_enabled` toggle is
daemon-wide — every `*_definitions_v1` collector either runs on
every target or none. That blocks production deployment in mixed
fleets where one database is regulated (view / function / trigger
source text is sensitive) and another isn't.

R098 introduces per-target profiles layered on top of the daemon-
wide gate.

**ARQ-SIGNALS-R098**: Each target's config may optionally include a
`collectors` block:

```yaml
targets:
  - name: pii
    collectors:
      profile: restricted
  - name: analytics
    collectors:
      profile: custom
      exclude:
        - pg_functions_definitions_v1
```

Profile values:

| Profile | Behaviour |
|---------|-----------|
| `default` (or empty) | Inherit daemon-wide. Same as today's behaviour. |
| `restricted` | Drop every `HighSensitivity=true` collector for this target, regardless of the daemon-wide gate. |
| `custom` | Use explicit `include` / `exclude` lists. |

Filter precedence: version/extension gates → daemon-wide
`high_sensitivity_collectors_enabled` → per-target profile.
Profiles NEVER expand eligibility beyond the daemon-wide gate
(INV-SENS-01).

Spec: `specifications/sensitivity-profiles.md` (BEHAVIORAL).

### Failure conditions (R098)

- **FC-19**: `signals.targets[].collectors.profile` outside
  `{empty, default, restricted, custom}` → `ValidateStrict`
  hard error.
- **FC-20**: `profile: custom` with the same collector ID in both
  `include` and `exclude` → `ValidateStrict` hard error.

### Per-class retention

`signals.retention_days` is a single global cutoff: every collector's
output is pruned at the same age. The catalog already declares a
`RetentionClass` per collector (`short` / `medium` / `long`) but
the cleanup logic ignores it — so `pg_settings_v1` (changes once a
quarter) and `pg_stat_activity_v1` (changes every second) age out
together. Operators with constrained disk can't keep a year of GUC
history without also keeping a year of activity samples.

R099 honours the existing class metadata.

**ARQ-SIGNALS-R099**: When `signals.retention` (structured) is set,
the collector prunes `query_runs` per retention class:

```yaml
signals:
  retention:
    short_days: 7
    medium_days: 30
    long_days: 365
```

Each class's day count is independent. A snapshot row is retained
until **every** class's cutoff has passed for its runs — i.e.
the snapshot pruner uses `max(short_days, medium_days, long_days)`
as the envelope.

Backward compatibility: `signals.retention_days` (the legacy flat
field) and `signals.retention` are mutually exclusive (FC-21). A
config that sets only `retention_days` works exactly as it did
before R099 — every class collapses to that value. A config that
sets only `retention` activates the per-class logic. Both together
is a configuration smell that fails fast.

### Failure conditions (R099)

- **FC-21**: `signals.retention_days > 0` AND `signals.retention.*`
  is non-zero in the same config → `ValidateStrict` hard error
  naming both.
- **FC-22**: Any of `signals.retention.{short,medium,long}_days`
  negative → `ValidateStrict` hard error.

### Configuration reload

Adding / removing / modifying a target previously required a full
daemon restart. For 24/7 fleets that's a small availability event
on every change. R100 introduces in-place reload of the runtime-
mutable subset of configuration.

**ARQ-SIGNALS-R100**: The system shall accept config reload via
two equivalent triggers:

- **`SIGHUP`** → daemon re-reads the config file, validates it,
  and (on success) applies the diff.
- **`POST /reload`** → same effect via the API. Bearer-auth +
  actor decoration inherited from R083.

Reload scope in v1:

- Add / remove a target.
- Modify an existing target's connection params or `collectors`
  profile (R098). Pool for that target is closed so the next
  cycle re-dials with the new params.

Reload v1 does NOT update (set-at-construction for now):

- `poll_interval` (ticker keeps its boot value)
- `min_snapshot_interval`, `target_timeout`, `query_timeout`
- `retention` thresholds (R099) — the values in effect at startup
  govern cleanup
- `signals.circuit` thresholds (R097)

A future iteration may extend the scope; the spec is silent on
those fields today.

A reload that fails to load or validate leaves the running state
**untouched** and emits a `config_reload_rejected` audit event.
Validated reloads emit `config_reload_applied`. Per-target
add / remove / modify events fire from `Collector.Reload`:
`config_reload_target_added`, `_removed`, `_modified`.

### Failure conditions (R100)

- **FC-23**: Config file unreadable / unparseable → reload
  rejected, daemon continues with the previous config.
- **FC-24**: New config fails `ValidateStrict` → reload rejected,
  daemon continues with the previous config.

### Logical replication slot health

The existing `replication_slots_risk_v1` collector
(`pg_replication_slots`) surfaces slot identity and retained WAL.
It does not expose the operational health counters that
`pg_stat_replication_slots` (PG 14+) tracks: spill (decoded
transactions that exceeded `logical_decoding_work_mem` and went to
disk), stream (large in-progress transactions), and total bytes
decoded. For shops running logical replication, those counters are
the leading indicator of slot saturation, under-sized
`logical_decoding_work_mem`, and downstream consumer back-pressure.

**ARQ-SIGNALS-R101**: The system shall register a
`pg_stat_replication_slots_v1` collector that emits one row per
logical replication slot from `pg_stat_replication_slots`. The
collector is gated to PG 14+ via `MinPGVersion`; on older majors it
is excluded by the existing R081 catalog dispatch and surfaces in
`collector_status.json` as `skipped, reason=version_unsupported`
(EA-R001).

The collector behavior is governed by
`specifications/collectors/pg_stat_replication_slots_v1.md`
(BEHAVIORAL, status: ACTIVE).

Invariants:

- Empty rowset on instances with no logical slots is the success
  state, not a failure.
- The cumulative counters are passed through as-is. Delta
  computation across snapshots is analyzer-side (INV-SIGNALS-01).
- The collector complements, and does not replace,
  `replication_slots_risk_v1`. Both register independently.

### Failure conditions (R101)

- **FC-25**: PG major version < 14 → collector excluded by the
  `MinPGVersion` gate (R081). `collector_status.json` records
  `status=skipped, reason=version_unsupported` via the existing
  EA-R001 framework.

### In-flight operation visibility (pg_stat_progress_* family)

Existing collectors capture state and outcomes; none surface
operations **in flight right now**. After a snapshot lands the
analyzer can tell that an autovacuum ran or didn't, but not why
it has been stuck for six hours. The PostgreSQL
`pg_stat_progress_*` views close that gap at near-zero cost —
they are plain SELECT-able system views.

**ARQ-SIGNALS-R102**: The system shall register one collector per
`pg_stat_progress_*` view from the supported family:

- `pg_stat_progress_vacuum_v1` (`pg_stat_progress_vacuum`)
- `pg_stat_progress_analyze_v1` (`pg_stat_progress_analyze`)
- `pg_stat_progress_create_index_v1` (`pg_stat_progress_create_index`)
- `pg_stat_progress_cluster_v1` (`pg_stat_progress_cluster`)
- `pg_stat_progress_basebackup_v1` (`pg_stat_progress_basebackup`)
- `pg_stat_progress_copy_v1` (`pg_stat_progress_copy`)

Every family member shares the same configuration: category
`progress`, `Cadence5m`, `RetentionShort`, `ResultRowset`,
`MinPGVersion: 14`, sensitivity `low`.

The collector behavior is governed by
`specifications/collectors/pg_stat_progress_family_v1.md`
(BEHAVIORAL, status: ACTIVE).

Invariants:

- **Empty rowset is the success state.** No in-flight operation
  on this cluster → zero rows → `status = success`. Most cycles
  against a quiet cluster will return zero rows from most family
  members; this is by design.
- **No derived columns.** The collector passes upstream view
  columns through unchanged. Cross-snapshot progress / phase
  comparison is analyzer-side (INV-SIGNALS-01).
- **Stable canonical schema across majors.** Where an upstream
  view's column shape drifted between majors (notably
  `pg_stat_progress_vacuum` on PG 17 — byte-denominated dead-tuple
  accounting — and `pg_stat_progress_copy` on PG 17 — added
  `tuples_skipped`), the canonical SQL emits the union and per-
  major `RegisterOverride` entries populate the right subset. The
  catalog drift allowlist documents the SQL-level divergence.

### Failure conditions (R102)

- **FC-26**: PG major version < 14 → every family member excluded
  by the `MinPGVersion` gate (R081). `collector_status.json`
  records `status=skipped, reason=version_unsupported` per
  EA-R001.

### Index hygiene summary

Signals collects the raw inputs every index-audit needs
(`pg_index`, `pg_stat_user_indexes`, `pg_indexes`,
`pg_attribute`) but each analyzer re-derives the four canonical
findings — unused, invalid, redundant, duplicate. R103
centralises the derivation into a single Signals-side summary so
the analyzer ingests already-classified rows and operators get
the same view via `arqctl` / Workbench.

**ARQ-SIGNALS-R103**: The system shall register an
`index_health_summary_v1` collector that emits one row per
non-system index in the connected database. Each row carries the
index's identity (`schemaname`, `tablename`, `indexname`,
`index_oid`), size and usage counters (`size_bytes`, `idx_scan`,
`idx_tup_read`), correctness flags (`is_unique`, `is_primary`,
`is_valid`, `is_ready`), the ordered `column_set`, the
`duplicate_of` / `redundant_with` references (NULL when the
finding does not apply), and a `health_findings` text-array
populated from the subset `{unused, large_unused, invalid,
not_ready, redundant, duplicate}`.

The collector behavior is governed by
`specifications/collectors/index_health_summary_v1.md`
(BEHAVIORAL, status: ACTIVE).

Invariants:

- One row per non-system index — no aggregation.
- `health_findings` is **always** an array, never NULL. Empty
  array means "no findings".
- System schemas are excluded (INV-SIGNALS-12):
  `pg_catalog`, `information_schema`, `pg_toast`, `pg_temp_%`,
  `pg_toast_temp_%`.
- `duplicate_of` / `redundant_with` always reference a name that
  exists in the result set's `(schemaname, tablename)` group.
- Read-only — single SELECT against catalog + stats views.

The collector inherits the existing safety model: single
transaction, no superuser, no writes, no telemetry, no
credential material (R013, R018–R020, INV-SIGNALS-05,
INV-SIGNALS-07).

### Failure conditions (R103)

- **FC-27**: Query exceeds the collector's 30-second timeout on
  a pathological catalog (> 10k user indexes). Standard
  collector-timeout path; row count is naturally bounded by the
  database's index count.

### Table bloat estimate

Table bloat is the most-requested PG operational insight and the
one every analyzer surfaces. The accurate path requires the
`pgstattuple` extension, which is **not** installed on most
managed-PG services (RDS / Aurora / Cloud SQL / AlloyDB / Azure
Flexible Server). R104 provides the pragmatic statistical
estimate so operators on managed PG get a workable bloat surface
out of the box.

**ARQ-SIGNALS-R104**: The system shall register a
`bloat_estimate_v1` collector that emits one row per non-system,
table-shaped relation (`relkind IN ('r', 'm', 'p')`). Each row
carries the table identity (`schemaname`, `tablename`,
`table_oid`, `relkind`), the actual size
(`pg_relation_size(table_oid)`), a statistical
`expected_size_bytes` derived from `pg_class.reltuples` ×
(`SUM(pg_stats.avg_width)` + fixed tuple/page overhead), the
floored `bloat_bytes = max(actual - expected, 0)` and the
`bloat_ratio` ∈ `[0.000, 1.000]` (or NULL when stats are
missing). Adjacent columns surface `reltuples`, `n_live_tup`,
`n_dead_tup`, `last_autovacuum`, and a `stats_missing` flag.

The estimation formula is canonical:

```text
expected ≈ CEIL(reltuples × (23 + 4 + avg_width_sum + 8)
                / GREATEST(block_size - 24, 1)) × block_size
```

Constants: `TUPLE_HDR=23`, `NULL_BMP=4`, `ALIGN_PAD=8`,
`PAGE_HDR=24`. `block_size` from
`current_setting('block_size')::numeric` so non-default 4 KB /
16 KB compile-time configurations work.

The collector behavior is governed by
`specifications/collectors/bloat_estimate_v1.md` (BEHAVIORAL,
status: ACTIVE).

Invariants:

- `bloat_ratio ∈ [0.000, 1.000]` or NULL — floored at 0 when the
  estimate exceeds actual.
- `stats_missing = TRUE` ↔ `expected_size_bytes IS NULL` ↔
  `bloat_ratio IS NULL` (the three move together).
- No dependency on the `pgstattuple` extension. Works on every
  managed-PG service.
- System schemas excluded (INV-SIGNALS-12).
- Single SELECT, read-only.

The collector inherits the existing safety model: single
transaction, no superuser, no writes, no telemetry (R013,
R018–R020, INV-SIGNALS-05, INV-SIGNALS-07).

### Failure conditions (R104)

- **FC-28**: Relation has never been ANALYZED (no `pg_stats`
  rows) → `stats_missing = TRUE`, estimate columns NULL,
  `bloat_bytes = 0`. Surfaced for the analyzer to recommend
  ANALYZE; not a collector error.
- **FC-29**: Estimate diverges from `pgstattuple` reality by more
  than ~20 % on workloads with variable column widths, non-
  default fillfactor, or heavy TOAST pressure. Documented
  limitation; downstream consumers weight `n_dead_tup` and
  `last_autovacuum` alongside the ratio. A future
  `bloat_exact_v1` collector (`pgstattuple`-gated via EA-R001)
  will provide precision for operators who can install the
  extension.

### Index bloat estimate

Index bloat is the second half of the canonical "drop X to
recover Y MiB" recommendation. R104 covers tables; R105 applies
the same statistical approach to indexes so operators on managed
PG — where `pgstatindex` is unavailable — get a workable index-
bloat surface out of the box. Pairs with R103 to identify
bloated unused indexes as drop candidates.

**ARQ-SIGNALS-R105**: The system shall register an
`index_bloat_estimate_v1` collector that emits one row per
non-system index (`relkind IN ('i', 'I')`). Each row carries the
index identity (`schemaname`, `tablename`, `indexname`,
`index_oid`, `relkind`), the actual size, a statistical
`expected_size_bytes` derived from `pg_class.reltuples` ×
(`SUM(pg_stats.avg_width)` over key columns + fixed index-tuple
overhead), the floored `bloat_bytes`, and the `bloat_ratio` ∈
`[0.000, 1.000]` (or NULL when stats are missing). Adjacent
columns surface `reltuples`, `is_unique`, `is_primary`, and a
`stats_missing` flag.

Index-tuple formula:

```text
expected ≈ CEIL(reltuples × (Σ key_avg_width + 8 + 4)
                / GREATEST(block_size - 24, 1)) × block_size
```

Constants differ from R104: `INDEX_TUPLE_HDR=8`, `ITEM_PTR=4`,
`PAGE_HDR=24`. Index tuples carry a smaller header (no
transaction-visibility fields — those live on the heap) and a
per-tuple line-pointer slot on every page. Width sum is bounded
by `pg_index.indnkeyatts` so INCLUDE columns (PG 11+ covering
indexes) are not counted, matching the PG-wiki convention.

The collector behavior is governed by
`specifications/collectors/index_bloat_estimate_v1.md`
(BEHAVIORAL, status: ACTIVE).

Invariants:

- `bloat_ratio ∈ [0.000, 1.000]` or NULL.
- `stats_missing` ↔ `expected_size_bytes IS NULL` ↔
  `bloat_ratio IS NULL`. Three move together.
- Expression-key indexes (`pg_index.indkey[i] = 0`) cannot be
  sized by the formula and emit `stats_missing = TRUE`.
- Partitioned-index parents (`relkind = 'I'`) have
  `actual_size_bytes = 0` (storage lives in leaf partitions) and
  surface with `bloat_bytes = 0`, `bloat_ratio = NULL`.
- No `pgstattuple` / `pgstatindex` dependency.
- System schemas excluded (INV-SIGNALS-12).

The collector inherits the existing safety model (R013,
R018–R020, INV-SIGNALS-05, INV-SIGNALS-07).

### Failure conditions (R105)

- **FC-30**: Underlying table has never been ANALYZED →
  `stats_missing = TRUE`, estimate NULL. Same handling as FC-28.
- **FC-31**: Index has all key columns as expressions →
  `stats_missing = TRUE`. Mixed indexes (some keys resolve, some
  are expressions) also flag `stats_missing = TRUE` to avoid
  biasing the estimate downward. A future variant could parse
  `pg_index.indexprs` widths.

### pg_stat_statements self-filtering

The `pg_stat_statements_v1` collector reads cumulative query
statistics that the PostgreSQL extension accumulates across every
session. Without filtering, Signals' own probe queries appear in
that output. Two undesirable consequences follow:

1. Customer workload analysis is polluted by Signals' own
   read-only catalog queries — the very rows Analyzer ranks as
   "top queries by total execution time" can shift toward the
   monitoring agent rather than the application workload.
2. Cross-database `pg_stat_statements` rows leak data from
   databases the operator did not intend to expose through the
   configured target. Each `targets[]` entry names a single
   `dbname`; the connection is scoped to that database. The
   statistics view must follow the same scope.

**ARQ-SIGNALS-R106**: All PostgreSQL connections opened by Arq
Signals — collector pool, doctor probe, and conntest probe —
shall set a fixed `application_name` runtime parameter to the
single source-of-truth constant `arq-signals`. The value shall
be set in the connection startup parameters (pgx
`RuntimeParams` / DSN), never via a post-connect `SET`, so
self-filtering works for the very first statement executed on
each new session.

The `pg_stat_statements_v1` collector SQL shall:

- Restrict output to the connected database, equivalent to
  `pg_stat_statements.dbid = (SELECT oid FROM pg_database WHERE
  datname = current_database())`. Cross-database rows are
  excluded.
- Exclude rows whose `userid` × `dbid` × `queryid` was last
  executed by a session whose `application_name = 'arq-signals'`.
  Because `pg_stat_statements` does not carry `application_name`
  directly, the filter is implemented as a `NOT EXISTS`
  correlated subquery against `pg_stat_activity` for the
  `arq-signals` application, joined on `userid` (matching
  `pg_stat_activity.usesysid`) AND `dbid` (matching
  `pg_stat_activity.datid`). The subquery is a conservative
  upper bound: any in-flight Signals session for the same
  user+db suppresses its rows. False positives are bounded to
  the Signals monitoring role and do not affect application
  workload analysis.

The collector shall continue to use `SELECT *` against
`pg_stat_statements` for cross-version column tolerance
(R037 / TC-SIG-044). Field names and the existing collector
contract (`id`, `category`, `result_kind`, `retention_class`,
`requires_extension`) are unchanged.

Invariants:

- **INV-SIGNALS-16**: Every PostgreSQL connection opened by Arq
  Signals reports `application_name = 'arq-signals'` to the
  server. The application name is sourced from a single Go
  constant; no other string literal in the repository sets the
  value.
- **INV-SIGNALS-17**: `pg_stat_statements_v1` rows reflect only
  the connected database. Rows from other databases on the same
  cluster are never collected.
- **INV-SIGNALS-18**: `pg_stat_statements_v1` rows do not
  include statements attributed to the Signals collector
  itself.

The collector inherits the existing safety model (R013,
R018–R020, INV-SIGNALS-05, INV-SIGNALS-07). Self-filtering is a
data-quality requirement, not a safety control.

### Failure conditions (R106)

- **FC-32**: The `pg_stat_statements` extension version pre-dates
  the columns `userid` and `dbid` (PG ≤ 9.3). On those versions
  the collector is already gated by `RequiresExtension`; if the
  installed view shape lacks `userid`/`dbid`, the query fails at
  execution and surfaces through the existing query-failure
  isolation path (TC-SIG-045). No additional handling.
- **FC-33**: Another application sets its own `application_name`
  to `arq-signals`. Their rows are suppressed; this is
  considered operator misconfiguration, not a Signals defect.
  Documented in `docs/collectors.md`.

### Per-collector export view

**ARQ-SIGNALS-R080**: The export ZIP shall optionally include a
per-collector directory of small JSON files alongside the canonical
NDJSON bundle. The intent is human-friendly browsing of a single
collector's most recent output without joining `query_runs.ndjson`
to `query_results.ndjson`. The canonical NDJSON layout (R078
metadata + R072 collector_status + snapshots/runs/results) remains
the consumer contract for downstream tooling — the per-collector
view is **derivative**, not authoritative.

The view is **off by default** to keep the ZIP small. It is enabled
by `signals.export_per_collector_files: true` (or the equivalent
`ARQ_SIGNALS_EXPORT_PER_COLLECTOR_FILES=true` environment variable).

When enabled, the export ZIP contains a `per-collector/` directory
with one JSON file per collector that has at least one entry in
`collector_status.json` for the export's scope:

- File name: `per-collector/<query_id>.json`. The `query_id` is the
  stable logical collector ID (e.g. `pg_stat_database_v1`); it is
  the same value already used in `collector_status.json` and
  `query_runs.ndjson`.
- File content: a JSON object with the latest run's status, run
  metadata (target name, collected_at, duration_ms, row_count, pg
  version), and — for successful runs — the row payload as a JSON
  array. For skipped or failed runs the row payload is omitted and
  the reason / detail are included.
- Latest-run-wins ordering: when the export covers multiple cycles,
  the per-collector file reflects the most recent run for that
  collector within the export's scope.

The per-collector files do not introduce any new field beyond what
already exists in the canonical NDJSON bundle. They are a regrouping
of existing data, never an additional collection or a new label
surface. Audit-event invariants (R078) and the high-sensitivity
gate (R075) are unchanged.

R080 changes export *shape* only; it does **not** change which
targets are collected or exported. Target filtering remains
operator-controlled — the export's scope is determined by
`signals.yaml` (the `targets:` list and the per-target `enabled`
flag) and, for `/export` specifically, by the existing
`?target_id=N` query parameter. R080 does not introduce any
mechanism for an external service to narrow the target set at
request time. Runtime, Arq-driven target filtering — the
control-plane boundary that lets Arq narrow the collected set on a
per-cycle basis — is the subject of a separate spec (R082, future
work) and is intentionally out of scope here.

### Diagnostic DSN assembly

**ARQ-SIGNALS-R111**: The shared diagnostic DSN builder
(`collector.BuildSafeDSN`, used by doctor C4 and `arqctl connect
test` — R095/R096) shall quote every string-valued field per libpq
key=value conventions before assembly: the value is wrapped in
single quotes, with backslash (`\`) and single quote (`'`) each
escaped by a preceding backslash. A field value — including a
resolved password — shall never be able to introduce, override, or
remove any other connection parameter of the assembled DSN.

Given a target whose resolved password (or host, dbname, user,
sslmode) contains DSN metacharacters — whitespace, `'`, `\`, or an
embedded `key=value` sequence — when the diagnostic DSN is
assembled and parsed by the connection library, then every field
shall round-trip to its exact literal value, and the parsed
configuration shall contain no parameters other than those the
builder itself emits. In particular, a hostile password value such
as `x sslmode=disable host=evil` shall not re-target the
connection or downgrade its TLS posture.

The production connection path (`collector.BuildConnConfig`)
already satisfies this by construction (`net/url` escaping); R111
extends the same guarantee to the diagnostic path.

### Failure conditions (R111)

| Trigger | Response |
|---------|----------|
| Field value contains `'`, `\`, whitespace, or embedded `key=value` text | Value is quoted/escaped at assembly; parses back to the literal value; no parameter injection occurs. |
| Assembled DSN fails to parse downstream | The diagnostic attempt fails with an operator-facing config-level error; credentials never appear in the error (R024, INV-SIGNALS-07). |
### Auth lockout scope

**ARQ-SIGNALS-R112**: The per-IP invalid-token rate limiter (R011 /
R024 auth middleware) shall never deny a request that presents a
valid bearer token. Token validity (constant-time comparison against
`api.token` and, in `arq_managed` mode, `arq_control_plane_token`) is
evaluated **before** the limiter's lockout decision. The limiter's
`429 Too Many Requests` response applies only to requests that fail
token validation.

Rationale: the limiter keys on `RemoteAddr` only (it deliberately
ignores `X-Forwarded-For`, which cannot be trusted). Behind NAT, a
reverse proxy that collapses the client address, or a co-located
attacker pod, many clients share one source IP. If the lockout were
evaluated before token validation, an unauthenticated peer could
flood invalid tokens and lock the legitimate operator or Arq control
plane out of pause/resume/reload/export — a denial of service. Gating
only the invalid path preserves the existing brute-force throttle
(an IP over the failure threshold still receives `429` for invalid
attempts) without ever penalising a valid credential.

Given an IP whose invalid-attempt counter is at or past the lockout
threshold, when a request from that IP presents a valid bearer token,
then the request shall authenticate and its failure counter shall be
cleared; when a request from that IP presents an invalid or missing
token, then it shall receive `429`.

### Failure conditions (R112)

| Trigger | Response |
|---------|----------|
| Valid token from a locked-out IP | Authenticated; `recordSuccess` clears the IP's failure counter. |
| Invalid/missing token from an IP over threshold | `429 Too Many Requests`; counter not further incremented. |
| Invalid/missing token from an IP under threshold | `401`; failure recorded. |
### API transport security

**ARQ-SIGNALS-R113**: The HTTP API shall support optional TLS
termination at the daemon. Two new `api` configuration fields,
`tls_cert_file` and `tls_key_file` (env overrides
`ARQ_SIGNALS_API_TLS_CERT_FILE` / `ARQ_SIGNALS_API_TLS_KEY_FILE`),
select the behaviour:

- Neither set → the API is served over plain HTTP (unchanged default;
  safe for the loopback default bind `127.0.0.1:8081`).
- Both set → the API is served over HTTPS using the supplied
  certificate and key, with a minimum protocol version of TLS 1.2.
- Exactly one set → a hard configuration error at load
  (`api.tls_cert_file and api.tls_key_file must both be set or both
  be empty`). TLS is all-or-nothing; a half-configured listener must
  never silently fall back to cleartext.

Rationale: the bearer token gates `pause` / `resume` / `reload` and
the full telemetry `export`. When the listener is bound beyond
loopback — which the container/Helm deployment necessarily does
(`0.0.0.0:<port>`) — an on-path observer on the pod network can
otherwise capture the token and exfiltrate all collected Postgres
telemetry in cleartext. Enabling TLS closes that exposure at the
daemon. In-cluster deployments MAY instead terminate TLS at a service
mesh / sidecar and restrict the listener with a NetworkPolicy; the
deployment chart MUST NOT present a beyond-loopback HTTP listener as a
secure default (see deployment guard below).

The local CLI (`arqctl`, default `--api-addr http://127.0.0.1:8081`)
continues to use plain HTTP against the loopback listener; enabling
daemon TLS is an exposed-listener concern and does not change the
loopback default.

### Failure conditions (R113)

| Trigger | Response |
|---------|----------|
| Only one of `tls_cert_file` / `tls_key_file` set | Hard config error at load; daemon does not start. |
| Both set but cert/key unreadable or invalid at listen | `ServeTLS` returns an error; daemon start fails loudly (no cleartext fallback). |
| Beyond-loopback bind with neither TLS nor a restricting NetworkPolicy (Helm) | Chart emits an explicit insecure-exposure warning; the `0.0.0.0/0` NetworkPolicy placeholder fails chart rendering when the policy is enabled. |

## Invariants

- **INV-SIGNALS-01**: Collector output is passive evidence, not interpretation.
  No collected value shall be annotated with pass/fail status, severity,
  recommendations, or scores.
- **INV-SIGNALS-02**: The SQL query catalog is the single source of truth for
  what data is collected. Adding a collector requires only registering it
  with the catalog at startup.
- **INV-SIGNALS-03**: The snapshot format is a stable contract. Breaking
  changes require a new version suffix (e.g. `_v2`).
- **INV-SIGNALS-04**: No proprietary prompts, scoring models, or analysis
  algorithms shall exist anywhere in the Arq Signals repository.
- **INV-SIGNALS-05**: Collection evidence must only be gathered under
  safety-constrained execution. No collector query shall execute outside a
  verified read-only session.
- **INV-SIGNALS-06**: Unsafe role posture (superuser, replication, bypassrls)
  is a hard failure, not a warning. Warnings are reserved for non-blocking
  hygiene issues.
- **INV-SIGNALS-07**: Credentials must never appear in any exported artifact,
  log line, API response, or stored record.
- **INV-SIGNALS-08**: Collection cycles must not overlap. If the previous
  cycle is still running, the next trigger is skipped.
- **INV-SIGNALS-09**: Transaction commit failure must prevent downstream
  persistence of results and success-path recording.
- **INV-SIGNALS-10**: Collector output ordering must be deterministic.
  The same PostgreSQL state must produce byte-identical collector output.
- **INV-SIGNALS-11**: collector_status.json is always present in exports.
  It is a first-class artifact, not optional metadata.
- **INV-SIGNALS-12**: Schema intelligence collectors exclude system
  schemas (pg_catalog, information_schema, pg_toast, pg_temp_%).
- **INV-SIGNALS-14**: `arqctl status` and the R084 default export
  MUST agree on the active-target set, where "active" means
  `enabled = 1` (R109). The number of targets in the status response
  equals the number of distinct `target_id` values in default-scope
  `snapshots.ndjson`. Disagreement is a regression on R089
  (producer-side drift), R090 (orphan filter), or R109 (enabled
  filter / reconciliation).
- **INV-SIGNALS-15**: A collection skipped by R091 leaves zero
  rows behind. `snapshots`, `query_runs`, and `query_results` row
  counts after a skipped collection equal their pre-skip values.
  The only observable side effect is the `collection_skipped`
  audit event.
- **INV-SIGNALS-13**: Default `arqctl export` (no selector flags) does
  not aggregate cross-cycle history. It carries, per active target,
  exactly the **latest run of each collector** (R084) — at most one run
  per `(target_id, query_id)`, never a collector's full run history.
  The ZIP may therefore reference more than one snapshot per target
  (when that target's collectors last fired in different cycles), but
  the number of runs per collector is bounded at one. Forensic
  full-history exports remain behind the explicit `--all` selector
  (R085).
- **INV-SIGNALS-16**: Every PostgreSQL connection opened by Arq
  Signals reports `application_name = 'arq-signals'` to the
  server. The value originates from a single Go constant; no
  other string literal in the repository sets it.
- **INV-SIGNALS-17**: `pg_stat_statements_v1` rows reflect only
  the connected database (`current_database()`). Rows from other
  databases on the same cluster are never collected.
- **INV-SIGNALS-18**: `pg_stat_statements_v1` rows do not
  include statements attributed to the Signals collector
  itself (`application_name = 'arq-signals'`).
- **INV-SIGNALS-19**: A persisted collection cycle records exactly one
  run per due collector (R108). The number of `query_runs` rows for a
  cycle equals the number of due collectors for that target — no due
  collector is silently absent because the per-cycle budget elapsed
  before it ran. Budget-skipped collectors appear with
  `status=skipped, reason=budget_exhausted`.
- **INV-SIGNALS-20**: `targets.enabled` reflects current configuration
  after startup and after every reload (R109). A target disabled in
  config, or absent from it, has `enabled = 0`; the default export and
  `/status` exclude it. Soft-disabling never deletes its snapshots —
  they remain reachable via `--all`.
- **INV-SIGNALS-21**: No configuration or secret field value can alter
  the set of connection parameters of a DSN it is embedded in (R111).
  Field values are data, never syntax.
- **INV-SIGNALS-22**: A valid bearer token always authenticates,
  independent of the per-IP invalid-attempt counter (R112). The
  lockout limiter can deny only requests that fail token validation;
  it can never deny a request carrying a correct credential.
- **INV-SIGNALS-23**: API TLS is all-or-nothing (R113). The listener
  serves either plain HTTP (no TLS files) or HTTPS (both TLS files);
  a half-configured TLS setup is a hard config error and never
  degrades to cleartext.

## Failure Conditions

- FC-01: Connection failure to PostgreSQL target → log error, skip target,
  continue to next
- FC-02: Query execution timeout → log warning, record error in query_run,
  continue to next query
- FC-03: Linter rejects a query at registration → process aborts
- FC-04: Persistence write failure → log error, retry on next cycle
- FC-05: Export with no data → produce empty ZIP with metadata only
- FC-06: Transaction commit failure → abort success path for that target,
  do not persist results
- FC-07: Role safety check failure → block collection for that target with
  actionable error (unless unsafe override is active)
- FC-08: `--snapshot-id <id>` requested but no `snapshots` row matches →
  HTTP 404 / non-zero `arqctl` exit, with the requested ID echoed in
  the diagnostic. Mutual-exclusion violation (`--all` and
  `--snapshot-id` both set) → HTTP 400 / non-zero exit.
- FC-09: Default export requested when no target has produced a
  completed snapshot yet → empty-data ZIP with `snapshot_count = 0`
  and `ingest_mode = "analyze"`. Existing `metadata.json`,
  `collector_status.json`, and `query_catalog.json` remain present;
  `snapshots.ndjson`, `query_runs.ndjson`, and `query_results.ndjson`
  are zero-length.

## Non-Goals

- Analysis or interpretation of collected data
- User management or authentication beyond API token
- Dashboard UI (analysis concern)
- License enforcement (open source, no gating)
- Report generation of any kind

## Coverage Summary

| Status | Count |
|--------|-------|
| COVERED | 74 |
| PARTIALLY COVERED | 0 |
| UNCOVERED | 3 |

74 requirements (R001-R074) are covered by automated tests. R075-R077
were added in the 2026-04 review cycle and are tracked in
`traceability.md`; their tests land alongside the implementation
commits.
R040-R073 (diagnostic, schema intelligence, and status packs) are
covered by registration, linting, cadence, version-gating, schema
filter, output column, and deterministic ordering tests. Full
behavioral coverage of query execution requires live PostgreSQL.

## Traceability Notes

See [traceability.md](traceability.md) for the requirement-to-test mapping.

## Appendices

- [Appendix A: API Contract](appendix-a-api-contract.md)
- [Appendix B: Configuration Schema](appendix-b-configuration-schema.md)
