# Acceptance Tests: Elevarq Signals

All test cases describe observable behavior and constraints. They are
language-neutral and do not reference specific implementation constructs.

## TC-SIG-001: PostgreSQL Connection

**Linked Rules:** ARQ-SIGNALS-R001
**Scenario:** Connect to a PostgreSQL instance with valid parameters
**Inputs:** host, port, dbname, user, password_file
**Expected Behavior:** Connection succeeds; a simple query returns
without error
**Failure Expectation:** Invalid credentials → connection refused, error
logged

---

## TC-SIG-002: Approved Query Enforcement

**Linked Rules:** ARQ-SIGNALS-R002
**Scenario:** Register a valid SELECT query and a dangerous INSERT query
**Inputs:** A read-only SELECT collector; an INSERT collector
**Expected Behavior:** The SELECT collector registers successfully; the
INSERT collector is rejected and the process aborts at startup
**Notes:** Validates the static linter rejects DDL/DML

---

## TC-SIG-003: Linter Rejects Dangerous Functions

**Linked Rules:** ARQ-SIGNALS-R002, ARQ-SIGNALS-R013
**Scenario:** Register collectors containing pg_terminate_backend,
pg_sleep, etc.
**Inputs:** Collectors with dangerous function calls in their SQL
**Expected Behavior:** Each registration is rejected and the process
aborts

---

## TC-SIG-004: Collector Executes Registered Queries

**Linked Rules:** ARQ-SIGNALS-R003
**Scenario:** Run a collection cycle against a PostgreSQL target
**Inputs:** PostgreSQL target with pg_stat_statements extension installed
**Expected Behavior:** All 9+ registered collectors execute; one
execution record exists per collector; result payloads are stored as
NDJSON

---

## TC-SIG-005: Minimum Query Catalog Coverage

**Linked Rules:** ARQ-SIGNALS-R003
**Scenario:** Verify the query catalog contains all required collectors
**Inputs:** None (introspect the registered catalog)
**Expected Behavior:** The catalog contains at least 9 entries with IDs:
pg_version_v1, pg_settings_v1, pg_stat_activity_v1,
pg_stat_database_v1, pg_stat_user_tables_v1, pg_stat_user_indexes_v1,
pg_statio_user_tables_v1, pg_statio_user_indexes_v1,
pg_stat_statements_v1

---

## TC-SIG-006: NDJSON Encoding

**Linked Rules:** ARQ-SIGNALS-R004
**Scenario:** Encode query results as NDJSON
**Inputs:** 3 result rows with mixed data types
**Expected Behavior:** Output is newline-delimited JSON; each line is a
valid JSON object; compression flag is false for small payloads

---

## TC-SIG-007: NDJSON Compression

**Linked Rules:** ARQ-SIGNALS-R004
**Scenario:** Encode a large payload exceeding 4096 bytes
**Inputs:** 100+ result rows totaling >4KB
**Expected Behavior:** Compression flag is true; decoding returns the
original rows unchanged

---

## TC-SIG-008: Snapshot Metadata Present

**Linked Rules:** ARQ-SIGNALS-R005
**Scenario:** After a collection cycle, inspect the stored metadata
**Inputs:** Completed collection against a PostgreSQL target
**Expected Behavior:** Metadata contains: non-empty collected_at
(RFC3339 format), non-empty pg_version, valid target_id; export
metadata.json contains collector_version in semver format

---

## TC-SIG-009: ZIP Export Structure

**Linked Rules:** ARQ-SIGNALS-R006
**Scenario:** Generate an export ZIP after collection
**Inputs:** At least one completed collection cycle
**Expected Behavior:** ZIP contains: metadata.json, query_catalog.json,
query_runs.ndjson, query_results.ndjson. ZIP does NOT contain:
stats_snapshots, requirement_catalog, reports, environment_profiles

---

## TC-SIG-010: No Analyzer Modules Present

**Linked Rules:** ARQ-SIGNALS-R007
**Scenario:** Inspect the codebase for analysis/scoring/LLM components
**Inputs:** All source files in the repository
**Expected Behavior:** No source file depends on scoring, analysis,
requirement-checking, report generation, or LLM components

---

## TC-SIG-011: No LLM Dependencies

**Linked Rules:** ARQ-SIGNALS-R007, ARQ-SIGNALS-R008
**Scenario:** Search codebase for LLM-related code
**Inputs:** All source files in the repository
**Expected Behavior:** No references to LLM clients, prompt
construction, report generation, or model inference. No LLM-related
module or directory exists.

---

## TC-SIG-012: No Scoring or Recommendations

**Linked Rules:** ARQ-SIGNALS-R007
**Scenario:** Search codebase for scoring/recommendation code
**Inputs:** All source files in the repository
**Expected Behavior:** No references to score computation, grade bands,
requirement definitions, risk rankings, or recommendation text. No
scoring module or directory exists.

---

## TC-SIG-013: No External AI Network Calls

**Linked Rules:** ARQ-SIGNALS-R008
**Scenario:** Inspect all network-related code
**Inputs:** All source files in the repository
**Expected Behavior:** No outbound HTTP client calls to AI services. No
socket connections for LLM. Only network calls are: PostgreSQL
connections and the local HTTP API server.

---

## TC-SIG-014: OSS Readiness Check

**Linked Rules:** ARQ-SIGNALS-R009
**Scenario:** Scan repository for proprietary content
**Inputs:** All files in the repository
**Expected Behavior:** BSD-3-Clause LICENSE file exists. No proprietary
prompts, scoring algorithms, or analysis logic. No credentials, API
keys, or internal endpoints. CONTRIBUTING.md and SECURITY.md exist.

---

## TC-SIG-015: CLI Commands Available

**Linked Rules:** ARQ-SIGNALS-R010
**Scenario:** Run the CLI tool with a help flag
**Inputs:** Built binary
**Expected Behavior:** Help output lists: collect, export, status,
version commands

---

## TC-SIG-016: Health Endpoint

**Linked Rules:** ARQ-SIGNALS-R011
**Scenario:** GET /health on a running Elevarq Signals server
**Inputs:** Running Elevarq Signals process
**Expected Behavior:** Returns HTTP 200 with JSON body containing
"status" and "version" fields. No authentication required.

---

## TC-SIG-017: Status Endpoint

**Linked Rules:** ARQ-SIGNALS-R011
**Scenario:** GET /status with valid bearer token
**Inputs:** Running Elevarq Signals with configured target
**Expected Behavior:** Returns JSON with fields per Appendix A: API
Contract. Response includes target info and collection state. Response
does NOT include secret_type, secret_ref, passwords, or scoring data.

---

## TC-SIG-018: Per-Query Timeout

**Linked Rules:** ARQ-SIGNALS-R012
**Scenario:** Query with 1s timeout against slow-responding target
**Inputs:** Query configured with 1s timeout; target that delays >2s
**Expected Behavior:** Query times out; error recorded in execution
metadata; collection continues with remaining queries

---

## TC-SIG-019: Three-Layer Read-Only Enforcement

**Linked Rules:** ARQ-SIGNALS-R013
**Scenario:** Attempt write operations through the collector connection
**Inputs:** PostgreSQL target with read-only role
**Expected Behavior:** All three layers prevent writes: linter rejects
at registration, session-level read-only blocks at connection,
per-query READ ONLY blocks at transaction

---

## TC-SIG-020: Version and Extension Filtering

**Linked Rules:** ARQ-SIGNALS-R014
**Scenario:** Filter queries for PG 14 without pg_stat_statements
**Inputs:** PostgreSQL major version 14, no pg_stat_statements extension
**Expected Behavior:** pg_stat_statements_v1 excluded (requires
extension); all other queries included

---

## TC-SIG-021: Cadence Scheduling

**Linked Rules:** ARQ-SIGNALS-R015
**Scenario:** Schedule queries with mixed cadences and varied last-run
times
**Inputs:** Queries with 5m, 15m, 1h cadences; varied last execution
timestamps
**Expected Behavior:** Only queries whose cadence has elapsed since last
run are selected. No catch-up for missed intervals.

---

## TC-SIG-022: Credential Safety

**Linked Rules:** ARQ-SIGNALS-R016
**Scenario:** After collection, inspect persistent storage and export
**Inputs:** Completed collection with password_file credential
**Expected Behavior:** No password values in persistent storage tables.
No password values in export ZIP. Password was read from file, used for
connection, then discarded.

---

## TC-SIG-023: Snapshot Format Stability

**Linked Rules:** ARQ-SIGNALS-R004, INV-SIGNALS-03
**Scenario:** Parse snapshot output with a fixed schema contract
**Inputs:** query_results.ndjson from export
**Expected Behavior:** Each line is valid JSON. Each object has
consistent keys matching the PostgreSQL view columns.

---

## TC-SIG-024: Empty Collection Export

**Linked Rules:** ARQ-SIGNALS-R006, FC-05
**Scenario:** Export when no collection data exists
**Inputs:** Fresh system with no snapshots
**Expected Behavior:** ZIP is created with metadata.json only (or with
empty NDJSON files). No error is returned.

---

## TC-SIG-025: Session Read-Only Guard

**Linked Rules:** ARQ-SIGNALS-R017, ARQ-SIGNALS-R021
**Scenario:** Validate session posture before collection
**Inputs:** Connection to PostgreSQL with standard monitoring role
**Expected Behavior:** Session has default_transaction_read_only=on;
transaction opens as READ ONLY

---

## TC-SIG-026: Superuser Role Blocked

**Linked Rules:** ARQ-SIGNALS-R018
**Scenario:** Connect with a superuser role
**Inputs:** Role attributes where rolsuper=true
**Expected Behavior:** Collection fails with error indicating the
superuser attribute was detected

---

## TC-SIG-027: Replication Role Blocked

**Linked Rules:** ARQ-SIGNALS-R019
**Scenario:** Connect with a replication role
**Inputs:** Role attributes where rolreplication=true
**Expected Behavior:** Collection fails with error mentioning
replication attribute

---

## TC-SIG-028: BypassRLS Role Blocked

**Linked Rules:** ARQ-SIGNALS-R020
**Scenario:** Connect with a bypassrls role
**Inputs:** Role attributes where rolbypassrls=true
**Expected Behavior:** Collection fails with error mentioning bypassrls

---

## TC-SIG-029: Session Timeouts Applied

**Linked Rules:** ARQ-SIGNALS-R022
**Scenario:** Verify session-local timeouts are set
**Inputs:** Collector with queryTimeout=10s, targetTimeout=60s
**Expected Behavior:** statement_timeout, lock_timeout (5s), and
idle_in_transaction_session_timeout are set within the collection
transaction

---

## TC-SIG-030: Hard vs Soft Failure Distinction

**Linked Rules:** ARQ-SIGNALS-R023
**Scenario:** Role has pg_write_all_data membership but no
superuser/replication/bypassrls
**Inputs:** rolsuper=false, rolreplication=false, rolbypassrls=false,
but role is member of pg_write_all_data
**Expected Behavior:** Warning logged, collection proceeds

---

## TC-SIG-031: Credential Redaction

**Linked Rules:** ARQ-SIGNALS-R024
**Scenario:** Password resolution error occurs
**Inputs:** Invalid password file path
**Expected Behavior:** Error message does not contain the actual
password. Shown message is generic (e.g. "credential resolution
failed").

---

## TC-SIG-032: Actionable Error Messages

**Linked Rules:** ARQ-SIGNALS-R025
**Scenario:** Collection fails due to superuser role
**Inputs:** Connection where rolsuper=true
**Expected Behavior:** Error message includes: which check failed, the
attribute value, and remediation guidance (e.g. "create a dedicated
monitoring role")

---

## TC-SIG-033: Unsafe Override Enabled

**Linked Rules:** ARQ-SIGNALS-R026
**Scenario:** ARQ_SIGNALS_ALLOW_UNSAFE_ROLE=true with superuser
connection
**Inputs:** Superuser role + override enabled
**Expected Behavior:** Warning logged, collection proceeds, export
metadata includes unsafe_mode=true and lists the specific bypassed
checks

---

## TC-SIG-034: Unsafe Override Disabled by Default

**Linked Rules:** ARQ-SIGNALS-R026
**Scenario:** Superuser connection with no override set
**Inputs:** Superuser role, ARQ_SIGNALS_ALLOW_UNSAFE_ROLE not set
**Expected Behavior:** Collection fails (blocked), same as TC-SIG-026

---

## TC-SIG-035: Multiple Unsafe Attributes

**Linked Rules:** ARQ-SIGNALS-R018, ARQ-SIGNALS-R019, ARQ-SIGNALS-R020
**Scenario:** Role has both superuser and replication attributes
**Inputs:** rolsuper=true, rolreplication=true
**Expected Behavior:** Error message lists all failing attributes

---

## TC-SIG-036: Commit Failure Blocks Downstream Persistence

**Linked Rules:** ARQ-SIGNALS-R034
**Scenario:** PostgreSQL transaction commit fails after queries execute
**Inputs:** Collection queries succeed but transaction commit returns
an error
**Expected Behavior:** The collector returns an error for that target.
No query results, snapshots, or success events are persisted. The
collection is not recorded as successful.

---

## TC-SIG-037: Initial Forced Collection

**Linked Rules:** ARQ-SIGNALS-R031
**Scenario:** System starts for the first time
**Inputs:** Fresh system, no prior collection data
**Expected Behavior:** The first collection cycle executes all eligible
queries regardless of cadence scheduling

---

## TC-SIG-038: Overlap Prevention

**Linked Rules:** ARQ-SIGNALS-R032
**Scenario:** Collection cycle is still running when next interval fires
**Inputs:** Slow target that takes longer than poll interval
**Expected Behavior:** The overlapping cycle is skipped with a warning
log. The running cycle completes normally.

---

## TC-SIG-039: Partial Target Failure

**Linked Rules:** ARQ-SIGNALS-R033
**Scenario:** One of three configured targets is unreachable
**Inputs:** Three targets; one with invalid host
**Expected Behavior:** The unreachable target fails with a logged error.
The other two targets are collected successfully. The cycle completes.

---

## TC-SIG-040: Configuration File Loading

**Linked Rules:** ARQ-SIGNALS-R027, ARQ-SIGNALS-R028
**Scenario:** Load configuration from file and environment
**Inputs:** A signals.yaml file with poll_interval=10m; env var
ARQ_SIGNALS_POLL_INTERVAL=2m
**Expected Behavior:** The effective poll interval is 2m (env var
overrides file)

---

## TC-SIG-041: Status Endpoint Excludes Credentials

**Linked Rules:** ARQ-SIGNALS-R011, ARQ-SIGNALS-R024
**Scenario:** GET /status for a target configured with password_file
**Inputs:** Running system with password_file credential source
**Expected Behavior:** The /status response contains target host, port,
user, dbname. It does NOT contain secret_type, secret_ref, password,
or the path to the password file.

---

## TC-SIG-042: Export Metadata Contains Unsafe Reasons

**Linked Rules:** ARQ-SIGNALS-R035
**Scenario:** Export with unsafe mode active after collecting from a
superuser role
**Inputs:** ARQ_SIGNALS_ALLOW_UNSAFE_ROLE=true, superuser role
**Expected Behavior:** metadata.json contains unsafe_mode=true and
unsafe_reasons listing the specific bypassed check (e.g. "role has
superuser attribute (rolsuper=true)")

---

## TC-SIG-043: Retention Cleanup

**Linked Rules:** ARQ-SIGNALS-R036
**Scenario:** Data older than retention period exists
**Inputs:** Retention configured to 1 day; data collected 2 days ago
**Expected Behavior:** Old data is removed after a collection cycle.
Recent data is preserved.

---

## TC-SIG-044: Dynamic Column Capture for pg_stat_statements

**Linked Rules:** ARQ-SIGNALS-R037
**Scenario:** Collect pg_stat_statements from a PostgreSQL version that
exposes additional or renamed columns
**Inputs:** PostgreSQL instance with pg_stat_statements installed
**Expected Behavior:** The collector captures all columns returned by the
view at runtime. The resulting NDJSON payload uses the actual column
names as JSON keys. No fixed column assumption causes the query to fail.

---

## TC-SIG-045: Query Failure Isolation

**Linked Rules:** ARQ-SIGNALS-R038
**Scenario:** One collector query fails (e.g. column not found) while
others succeed
**Inputs:** A collection cycle where one query returns an error
**Expected Behavior:** The failing query is recorded with an error. All
other queries in the cycle execute successfully and produce results. The
collection cycle completes and persists the successful results.

---

## TC-SIG-046: Dynamic Capture Preserves Safety Model

**Linked Rules:** ARQ-SIGNALS-R039
**Scenario:** Verify that dynamic column capture does not introduce
write operations, credential leaks, or safety regressions
**Inputs:** Collection with dynamic pg_stat_statements query
**Expected Behavior:** The query runs inside a READ ONLY transaction.
The query passes the static linter. No credentials appear in the
output. The collector's safety model is unchanged.

---

## TC-SIG-093: Default Export Scope is Latest-Run-Per-Collector

**Linked Rules:** ARQ-SIGNALS-R084, INV-SIGNALS-13
**Scenario:** Daemon has run multiple collection cycles for one or more
targets, each cycle persisting only the collectors due that cycle.
Operator invokes `signalsctl export` with no selector flags.
**Inputs:**
- Daemon with N completed snapshots for target A and M completed
  snapshots for target B (N, M ≥ 2), where each target runs a single
  collector at one cadence (so latest-run == latest-snapshot for this
  simple case).
- `signalsctl export -o /tmp/out.zip` (no other flags).

**Expected Behavior:**
- `query_runs.ndjson` contains, per target, the latest run of each
  collector (the row with the largest `collected_at` per
  `(target_id, query_id)`); rows from older cycles of the same
  collector are absent.
- `snapshots.ndjson` contains the distinct snapshots those latest runs
  belong to — for this single-collector fixture, exactly two (target
  A's and target B's latest).
- `metadata.json.snapshot_count` equals the number of distinct
  snapshot IDs in `snapshots.ndjson`.
- `metadata.json.run_scope == "latest-per-collector"`.
- `metadata.json.ingest_mode == "analyze"`.
- The ZIP shape (six files, file names) is unchanged from R035.

**Failure Expectation:** A ZIP that includes a non-latest run of any
collector under the default scope is a regression on R084. A
`snapshot_count` that does not match the distinct snapshot IDs in
`snapshots.ndjson` is a regression on R086.

---

## TC-SIG-094: Explicit `--all` Restores Full History

**Linked Rules:** ARQ-SIGNALS-R085, ARQ-SIGNALS-R086
**Scenario:** Operator opts into the pre-R084 behavior for forensics.
**Inputs:**
- Daemon with K total completed snapshots across all targets.
- `signalsctl export --all -o /tmp/out.zip`.

**Expected Behavior:**
- `snapshots.ndjson` contains all K rows in `collected_at` ascending
  order (R074 ordering is preserved).
- `metadata.json.snapshot_count == K`.
- `metadata.json.ingest_mode == "analyze"` (the operator-driven mode;
  history_only is reserved for R087 replay).
- `query_runs.ndjson` and `query_results.ndjson` contain rows for
  all K snapshots.

**Failure Expectation:** A ZIP missing any snapshot when `--all` is
set is a regression. `--all` combined with `--snapshot-id` is an
input error and shall be rejected per FC-08.

---

## TC-SIG-095: `--snapshot-id` Returns Exactly One Snapshot

**Linked Rules:** ARQ-SIGNALS-R085, FC-08
**Scenario:** Operator requests a single named snapshot for replay or
diagnosis.
**Inputs:**
- A known `snapshots.id` value present in the daemon's store.
- `signalsctl export --snapshot-id <id> -o /tmp/out.zip`.

**Expected Behavior:**
- `snapshots.ndjson` contains exactly one row with that `id`.
- `metadata.json.snapshot_count == 1`.
- `metadata.json.ingest_mode == "analyze"` (the operator-driven mode).
- `query_runs.ndjson` and `query_results.ndjson` contain only rows
  whose `snapshot_id` matches.

**Failure Expectation:**
- Unknown `snapshot_id` → HTTP 404 / non-zero `signalsctl` exit, with
  the requested ID echoed in the diagnostic (FC-08).
- Both `--snapshot-id` and `--all` set → HTTP 400 / non-zero exit.

---

## TC-SIG-096: `--since` / `--until` Filter by Time Range

**Linked Rules:** ARQ-SIGNALS-R085
**Scenario:** Operator restricts the export to a time window.
**Inputs:**
- A daemon with snapshots at t0, t1, t2, t3 (t0 < t1 < t2 < t3).
- `signalsctl export --since <t1-RFC3339> --until <t2-RFC3339> -o /tmp/out.zip`.

**Expected Behavior:**
- `snapshots.ndjson` contains only the snapshots whose `collected_at`
  falls in the half-open `[since, until)` window.
- `metadata.json.snapshot_count` matches the actual count.
- Inverted range (since > until) → HTTP 400 / non-zero exit
  (existing post-0.3.1 hardening preserved).
- Malformed RFC3339 in either bound → HTTP 400 / non-zero exit.

**Failure Expectation:** A ZIP that includes a snapshot outside the
range is a regression on R085. A bare `--since` (no `--until`) shall
default `until` to the export request time, not silently widen.

---

## TC-SIG-097: Metadata Carries `snapshot_count` and `ingest_mode`

**Linked Rules:** ARQ-SIGNALS-R035, ARQ-SIGNALS-R086
**Scenario:** Verify the new metadata fields are present and well-typed
across every selector path.
**Inputs:** Run TC-SIG-093, TC-SIG-094, TC-SIG-095, TC-SIG-096 and
inspect each ZIP's `metadata.json`.
**Expected Behavior:** Every ZIP carries:
- `snapshot_count` as an integer ≥ 0.
- `ingest_mode` as the string literal `"analyze"` for any
  operator-driven export. (`"history_only"` only appears in R087
  backlog-replay traffic, which is DESIGN-ONLY for this PR slice
  and not exercised by these tests.)
**Failure Expectation:** Missing fields, non-integer
`snapshot_count`, or `ingest_mode` outside the
`{"analyze","history_only"}` enum is a regression on R086.

---

## TC-SIG-098: Empty-Store Default Export Produces a Well-Formed ZIP

**Linked Rules:** FC-09, ARQ-SIGNALS-R086
**Scenario:** Daemon has not yet produced any completed snapshots
(fresh install, or all targets disabled before any cycle ran).
**Inputs:** `signalsctl export` (no flags) against a daemon whose
`snapshots` table is empty.
**Expected Behavior:**
- ZIP returns 200 with the same six-file layout.
- `metadata.json.snapshot_count == 0`.
- `metadata.json.ingest_mode == "analyze"`.
- `snapshots.ndjson`, `query_runs.ndjson`, and
  `query_results.ndjson` are zero-length.
- `collector_status.json` and `query_catalog.json` are present and
  well-formed.
**Failure Expectation:** A 5xx, an empty body, or a missing required
file is a regression on FC-09. A non-zero `snapshot_count` with
zero `snapshots.ndjson` rows is a regression on R086.

---

## TC-SIG-121: Default Export Includes Lower-Cadence Collectors After a High-Cadence Cycle

**Linked Rules:** ARQ-SIGNALS-R084, ARQ-SIGNALS-R072, INV-SIGNALS-13
**Scenario:** A target's most recent snapshot contains only the
collectors that were due that cycle; a lower-cadence collector last ran
in an earlier snapshot. This is the P0 the latest-snapshot default
dropped (issue #5).
**Inputs:**
- Target A, snapshot S1 (older `collected_at`) with runs for collectors
  `cadence_5m_v1` and `cadence_24h_v1`.
- Target A, snapshot S2 (newer `collected_at`) with a run for
  `cadence_5m_v1` only.
- `signalsctl export` (no flags).

**Expected Behavior:**
- `query_runs.ndjson` contains the latest run of **each** collector:
  `cadence_5m_v1` from S2 and `cadence_24h_v1` from S1.
- `collector_status.json` lists **both** collectors.
- `snapshots.ndjson` contains both S1 and S2 (each is referenced by a
  latest run); `metadata.json.snapshot_count == 2` for the single
  target.
- `metadata.json.run_scope == "latest-per-collector"`.

**Failure Expectation:** An export that omits `cadence_24h_v1` (present
only in S1) because S2 is the latest snapshot is the exact R084
completeness regression issue #5 fixes.

---

## TC-SIG-122: `run_scope` Metadata Marker

**Linked Rules:** ARQ-SIGNALS-R086
**Scenario:** A consumer must know whether runs in an export share a
snapshot timestamp or were assembled latest-per-collector.
**Inputs:** The fixtures above, exported under each scope.
**Expected Behavior:**
- Default (no flags): `metadata.json.run_scope == "latest-per-collector"`.
- `--all`, `--snapshot-id <id>`, and `--since/--until`:
  `metadata.json.run_scope == "snapshot"`.

**Failure Expectation:** A missing `run_scope`, or `"snapshot"` on a
default export (or vice versa), is a regression on R086.

---

## TC-SIG-123: Collector Freshness Metadata

**Linked Rules:** ARQ-SIGNALS-R107, ARQ-SIGNALS-R072
**Scenario:** Consumers must distinguish a fresh collector from a stale
or never-run one from the export alone.
**Inputs:**
- Target A with `cadence_5m_v1` run within the last minute, a
  `cadence_24h_v1` whose latest run is 3 days old, and an eligible
  `cadence_1h_v1` that has never produced a run.
- `signalsctl export --target-id A` (target-scoped — `never_run`
  enumeration requires per-entry target attribution, R107).

**Expected Behavior:**
- Each `collector_status.json` entry carries `collected_at`, `cadence`,
  and `freshness`.
- `cadence_5m_v1` -> `freshness == "fresh"`.
- `cadence_24h_v1` (3 days, > 2x cadence) -> `freshness == "stale"`.
- `cadence_1h_v1` (eligible, no run) -> `freshness == "never_run"` and
  appears as an entry rather than being silently absent.

**Failure Expectation:** A stale low-cadence collector reported as
`fresh`, or an eligible-but-never-run collector silently absent, defeats
the consumer's ability to detect coverage gaps (R107).

---

## TC-SIG-124: Budget Exhaustion Records Remaining Due Collectors

**Linked Rules:** ARQ-SIGNALS-R108, ARQ-SIGNALS-R072, INV-SIGNALS-19
**Scenario:** A target's per-cycle time budget elapses after some, but
not all, due collectors have run.
**Inputs:**
- A cycle with M due collectors whose budget expires after the first N
  (N < M) have been attempted.

**Expected Behavior:**
- The persisted cycle records exactly M `query_runs` rows — one per due
  collector (INV-SIGNALS-19).
- The M-N collectors that never got a turn carry
  `status=skipped, reason=budget_exhausted`.
- The cycle's overall status is `partial`.
- `arq_signal_collectors_skipped_total{reason="budget_exhausted"}`
  increments by M-N.

**Boundary/unit:** the pure helper that builds the skipped runs from
the remaining-due slice produces one `skipped`/`budget_exhausted` run
per remaining collector with the correct `query_id`, `target_id`, and
`snapshot_id`; the status classifier returns `partial` when any
`budget_exhausted` skip occurred and `err == nil`.

**Failure Expectation:** A cycle that marks N collectors successful and
leaves the remaining M-N with no row at all is the R072/INV-SIGNALS-19
completeness regression this rule closes.

---

## TC-SIG-125: Disabled/Removed Targets Excluded from Default Export and Status

**Linked Rules:** ARQ-SIGNALS-R109, INV-SIGNALS-20, INV-SIGNALS-14
**Scenario:** A target that previously collected is later disabled in
config or removed from it. The daemon reloads.
**Inputs:**
- Targets A (enabled) and B, each with collected snapshots; B is then
  disabled (or removed) and a reload occurs.

**Expected Behavior:**
- After reload, `targets.enabled = 0` for B (`ReconcileEnabledTargets`).
- The default export (`GetLatestRunsPerCollector`) contains only A's
  runs/snapshots; B is absent from `snapshots.ndjson`,
  `query_runs.ndjson`, and `collector_status.json`.
- `GET /status` lists only A.
- `signalsctl export --all` still includes B's historical snapshots (no
  deletion).

**Boundary/unit:** `ReconcileEnabledTargets([A])` sets B `enabled = 0`
and A `enabled = 1`; re-enabling B (`ReconcileEnabledTargets([A,B])`)
restores it. `GetLatestRunsPerCollector` returns no rows for an
`enabled = 0` target.

**Failure Expectation:** B appearing as active in the default export or
`/status` after being disabled is the R109 drift this rule closes. B's
snapshots being deleted on disable is a regression (soft-disable only).

---

## TC-SIG-100: UpsertTarget Is Idempotent

**Linked Rules:** ARQ-SIGNALS-R089
**Scenario:** Repeated `UpsertTarget` calls with the same `name` and
config tuple must return the same `targets.id` and must leave the
`targets` table with a single row.
**Inputs:**
- A fresh database with no targets.
- 10 sequential `UpsertTarget("X", host, port, dbname, user, ...)` calls.
**Expected Behavior:**
- `targets` table has exactly 1 row whose `name = "X"`.
- All 10 returned ids are equal to that row's `id`.
- The id matches the literal value of `SELECT id FROM targets WHERE name = 'X'`.
**Failure Expectation:** Any drift in the returned id, or extra
rows in the `targets` table, is a regression on R089.

---

## TC-SIG-101: UpsertTarget Returns Real Table ID Even After Conflict

**Linked Rules:** ARQ-SIGNALS-R089
**Scenario:** After the second and subsequent calls (which trigger
the DO UPDATE branch of the UPSERT), the returned id must still be
the actual `targets.id`, not the wasted reserved AUTOINCREMENT id
that SQLite's `last_insert_rowid()` returns for UPSERT-update.
**Inputs:**
- A fresh database.
- Call `UpsertTarget("X", ...)` once (INSERT branch fires).
- Capture the returned id `id1`.
- Call `UpsertTarget("X", ...)` 5 more times (DO UPDATE branch fires).
**Expected Behavior:**
- All 6 returned ids equal `id1`.
- `SELECT name FROM targets WHERE id = ?` with each returned id
  yields `"X"` (the id is a real foreign-key target).
**Failure Expectation:** A returned id that does not exist in the
`targets` table is the v0.3.x regression that motivated R089.

---

## TC-SIG-102: UpsertTarget Survives Intermediate Inserts on the Same Connection Pool

**Linked Rules:** ARQ-SIGNALS-R089
**Scenario:** The realistic collector flow alternates `UpsertTarget`
with `InsertCollectionAtomic`. Intermediate INSERTs must not poison
the next `UpsertTarget`'s return value via `last_insert_rowid()`
drift.
**Inputs:**
- Call `UpsertTarget("X", ...)` once.
- Insert 3 collection cycles (snapshot + runs + results) for that
  target via `InsertCollectionAtomic`.
- Call `UpsertTarget("X", ...)` again.
**Expected Behavior:**
- The second `UpsertTarget` returns the same id as the first.
- Every snapshot's `target_id` equals that id.
**Failure Expectation:** Snapshot rows with `target_id` values that
climb across cycles is the v0.3.x bug shape.

---

## TC-SIG-103: Default Export Scope Ignores Orphaned Target IDs

**Linked Rules:** ARQ-SIGNALS-R090, INV-SIGNALS-14
**Scenario:** Existing v0.3.x stores carry orphan `target_id`
values in `snapshots`. Default export (R084 scope) must not surface
them.
**Inputs:**
- 1 row in `targets` (id=1, name="X").
- 5 snapshots: one with `target_id=1` (canonical), four with
  `target_id ∈ {99, 100, 101, 102}` (orphans — no row in `targets`
  with those ids).
- Bare default export.
**Expected Behavior:**
- `metadata.snapshot_count == 1`.
- `snapshots.ndjson` contains exactly the row whose `target_id=1`.
- `query_runs.ndjson` and `query_results.ndjson` contain only the
  matching cycle's rows.
**Failure Expectation:** A default export that includes any orphan
row is a regression on R090.

---

## TC-SIG-104: --all Still Shows Orphaned Snapshots

**Linked Rules:** ARQ-SIGNALS-R085, ARQ-SIGNALS-R090
**Scenario:** Forensic `--all` exports must remain complete so
operators can diagnose corruption.
**Inputs:** Same fixture as TC-SIG-103.
**Expected Behavior:**
- `--all` export contains all 5 snapshots.
- `metadata.snapshot_count == 5`.
- The orphan rows (target_id ∈ {99, 100, 101, 102}) appear in
  `snapshots.ndjson`.
**Failure Expectation:** `--all` silently filtering orphans makes
corruption invisible to forensic exports — a usability regression
on R085.

---

## TC-SIG-105: --target-id Composes With Orphan Filter

**Linked Rules:** ARQ-SIGNALS-R085, ARQ-SIGNALS-R090
**Scenario:** `--target-id` is a physical-id filter. In default
scope it follows R090 (orphans hidden). In `--all` scope it does
not (orphans visible).
**Inputs:** Same fixture as TC-SIG-103.
**Expected Behavior:**
- `--target-id 1` (default scope): 1 snapshot returned.
- `--target-id 99` (default scope): 0 snapshots; metadata
  `snapshot_count == 0`.
- `--all --target-id 99`: exactly the orphan row whose
  `target_id=99`; `snapshot_count == 1`.
**Failure Expectation:** `--all --target-id=<orphan>` returning 0
rows defeats forensic recovery.

---

## TC-SIG-106: signalsctl status and Default Export Agree on Target Count

**Linked Rules:** INV-SIGNALS-14, ARQ-SIGNALS-R084
**Scenario:** The user-visible target count from `signalsctl status`
must equal the number of distinct `target_id` values in the
default-scope export's `snapshots.ndjson`.
**Inputs:**
- A daemon configured with one enabled target named "X".
- 1 collection cycle has completed.
- `signalsctl status` and `signalsctl export` (no flags) called in
  succession.
**Expected Behavior:**
- `signalsctl status.targets` length = 1.
- `metadata.snapshot_count` from the export = 1.
- The single `snapshots.target_id` value present in the export
  equals `signalsctl status.targets[0].id`.
**Failure Expectation:** The two views disagreeing is the
producer/consumer split that R089+R090 close.

---

## TC-SIG-107: sqlite_sequence.targets Stays Bounded

**Linked Rules:** ARQ-SIGNALS-R089
**Status:** Sentinel. Documents the SQLite quirk so a future
schema change is deliberate, not silent.
**Scenario:** After many UPSERT-update cycles the
`sqlite_sequence.targets` counter still bumps (SQLite reserves the
next AUTOINCREMENT id even when DO UPDATE fires). The fix in R089
makes that bump harmless because `last_insert_rowid()` is no longer
trusted; this test pins the harmlessness.
**Inputs:** 50 sequential `UpsertTarget("X", ...)` calls.
**Expected Behavior:**
- After 50 calls, `targets` still has 1 row with `id = id1`.
- All 50 returned ids equal `id1`.
- `sqlite_sequence.targets` may have advanced past 50 (this is
  acceptable; the test does NOT assert it stays at 1).
**Failure Expectation:** `targets.id` drifting in lockstep with
`sqlite_sequence.targets` reproduces the v0.3.x bug.

---

## TC-SIG-110: Min Snapshot Interval — First Collection Always Runs

**Linked Rules:** ARQ-SIGNALS-R091
**Scenario:** A fresh daemon (no completed snapshots) MUST not skip
the first collection for any target.
**Inputs:** Empty `snapshots` table; one configured target "A";
`min_snapshot_interval = 60s`.
**Expected Behavior:** The target's first cycle runs to completion;
a snapshot row is written.
**Failure Expectation:** A first-cycle skip on an empty store is a
regression — the rule applies between completed snapshots, not
between zero snapshots.

---

## TC-SIG-111: Min Snapshot Interval — Second Collection Within Window Skipped

**Linked Rules:** ARQ-SIGNALS-R091, INV-SIGNALS-15
**Scenario:** A second collection request for the same target
within the window MUST be skipped.
**Inputs:** Target "A" with one snapshot at `T0`;
`min_snapshot_interval = 60s`; collection request at `T0 + 20s`.
**Expected Behavior:**
- No new `snapshots` row is written.
- No new `query_runs` rows are written.
- No new `query_results` rows are written.
- A `collection_skipped` audit event is emitted with
  `reason_category = "min_interval_not_elapsed"`,
  `last_collected_at = T0 (RFC3339)`,
  `elapsed_ms = 20000`,
  `min_interval_ms = 60000`.
**Failure Expectation:** Any new snapshot/runs/results row
produced for that target before `T0 + 60s` is a regression.

---

## TC-SIG-112: Min Snapshot Interval — After Window Succeeds

**Linked Rules:** ARQ-SIGNALS-R091
**Scenario:** Once `min_snapshot_interval` has elapsed since the
last completed snapshot, the next cycle for that target runs
normally.
**Inputs:** Target "A" with snapshot at `T0`; collection request
at `T0 + min_interval` or later.
**Expected Behavior:** Collection runs; new snapshot row written.

---

## TC-SIG-113: Min Snapshot Interval — Per-Target Independence

**Linked Rules:** ARQ-SIGNALS-R091
**Scenario:** Two targets are independent. A recently-collected
target does not block a different target whose own interval has
elapsed.
**Inputs:** Targets "A" and "B"; "A" snapshot at `T0`; "B" has
never collected; `min_snapshot_interval = 60s`; cycle request at
`T0 + 20s` covering both targets.
**Expected Behavior:**
- "A" is skipped (per R091).
- "B" runs to completion (first-cycle for B; R091 vacuous).

---

## TC-SIG-114: signalsctl collect now Respects the Interval by Default

**Linked Rules:** ARQ-SIGNALS-R091, ARQ-SIGNALS-R092
**Scenario:** Manual `signalsctl collect now` does NOT bypass R091
unless `--force` is supplied.
**Inputs:** Target "A" with snapshot at `T0`; operator runs
`signalsctl collect now` at `T0 + 20s` without `--force`.
**Expected Behavior:** Target "A" is skipped per R091. The HTTP
response still indicates `accepted_targets: ["A"]` (the request
was queued and processed) but the per-target outcome is "skipped"
in audit.
**Failure Expectation:** A bare `signalsctl collect now` that bypasses
the interval is a regression on R091.

---

## TC-SIG-115: --force / force=true Bypasses the Interval

**Linked Rules:** ARQ-SIGNALS-R092
**Scenario:** Explicit operator override forces a collection
within the interval window.
**Inputs:** Target "A" with snapshot at `T0`; operator runs
`signalsctl collect now --force` at `T0 + 20s`.
**Expected Behavior:**
- Collection runs to completion despite the elapsed time being
  below `min_snapshot_interval`.
- A new `snapshots` row is written.
- `collection_started` and `collection_completed` audit events
  carry `forced=true`.
**Failure Expectation:** `--force` failing to bypass the interval,
or running without `forced=true` in audit, is a regression on
R092.

---

## TC-SIG-116: Skipped Collection Leaves No Rows (INV-SIGNALS-15)

**Linked Rules:** INV-SIGNALS-15, ARQ-SIGNALS-R091
**Scenario:** A skipped-by-interval collection MUST not create
any database side effects beyond the audit event.
**Inputs:** Target "A" with snapshot at `T0`; collection
attempted at `T0 + 5s` (well within the window).
**Expected Behavior:** Snapshot/runs/results row counts are
identical before and after the skipped attempt.

---

## TC-SIG-117: Skip Reason Surfaced in Audit

**Linked Rules:** ARQ-SIGNALS-R091
**Scenario:** A skipped-by-interval collection MUST emit an
audit event whose structured attributes name the reason and the
relevant timing fields, so an operator inspecting logs can tell
why the cycle skipped.
**Inputs:** Same as TC-SIG-111.
**Expected Behavior:** The single emitted audit event has:
- `audit_event = "collection_skipped"`
- `target = "A"`
- `reason_category = "min_interval_not_elapsed"`
- `last_collected_at` is a non-empty RFC3339 string
- `elapsed_ms` is an integer ≥ 0
- `min_interval_ms` is an integer > 0
**Failure Expectation:** A bare or unstructured skip log breaks
operator observability.

---

## TC-SIG-099: Backlog Replay Shape (DESIGN-ONLY)

**Linked Rules:** ARQ-SIGNALS-R087
**Status:** PLANNED (design-only; no implementation in this PR slice).
**Scenario:** Validates the on-the-wire shape any future Mode B
delivery transport must produce. When implemented, a backlog of N
pending snapshots replayed upstream shall arrive as N independent
ZIPs.
**Inputs:** N pending snapshots (N ≥ 2) accumulated during a
delivery outage.
**Expected Behavior on the receiving side:**
- N ZIPs received, ordered by `collected_at` ascending.
- Each ZIP carries `metadata.snapshot_count == 1`.
- The first N−1 ZIPs carry `metadata.ingest_mode == "history_only"`.
- The last ZIP (newest snapshot) carries
  `metadata.ingest_mode == "analyze"`.
- Each ZIP is independently ingestable per R084.
**Failure Expectation:** Any multi-snapshot ZIP, any out-of-order
delivery, or any `ingest_mode` mismatch on the analyze/history split
is a regression on R087. Out of scope (per R088): the transport,
spool storage shape, retry policy, and delivery observability —
those are deferred to a future spec slice.

---

## TC-SIG-118: application_name Set From a Single Constant

**Linked Rules:** ARQ-SIGNALS-R106, INV-SIGNALS-16
**Scenario:** Verify that every PostgreSQL connection opened by
Signals reports `application_name = 'arq-signals'` and that the
value originates from one named constant rather than scattered
string literals.
**Inputs:** A valid `TargetConfig` passed to
`collector.BuildConnConfig`.
**Expected Behavior:**
- The returned `pgx.ConnConfig.RuntimeParams["application_name"]`
  equals the exported package constant
  `collector.AppName` (value: `"arq-signals"`).
- A repository-wide check finds the literal `"arq-signals"` as
  the value of a single `const` declaration. Other references
  use the constant.
**Failure Expectation:** A second string literal setting
`application_name`, or a mismatch between the runtime parameter
and the constant, is a regression on R106.

---

## TC-SIG-119: pg_stat_statements_v1 SQL Excludes Self and Other Databases

**Linked Rules:** ARQ-SIGNALS-R106, INV-SIGNALS-17, INV-SIGNALS-18
**Scenario:** Inspect the registered SQL for
`pg_stat_statements_v1` and verify the static filters required by
R106 are present.
**Inputs:** `pgqueries.All()` filtered to `pg_stat_statements_v1`.
**Expected Behavior:**
- SQL references `current_database()` (or a join on
  `pg_database.datname = current_database()`) to scope rows to
  the connected database.
- SQL contains a `NOT EXISTS` (or equivalent anti-join) against
  `pg_stat_activity` filtering `application_name = 'arq-signals'`
  on `userid` + `dbid`.
- SQL still passes `pgqueries.LintQuery` (read-only, no
  dangerous keywords, no embedded semicolons).
- The collector's `ID`, `Category`, `RequiresExtension`,
  `RetentionClass`, and `ResultKind` remain unchanged.
**Failure Expectation:** Either filter missing — or a rename of
the field set, retention class, or extension gate — is a
regression on R106.

---

## TC-SIG-120: pg_stat_statements_v1 Existing Behavior Unchanged

**Linked Rules:** ARQ-SIGNALS-R106, ARQ-SIGNALS-R037
**Scenario:** Confirm the R037 dynamic-column contract still
holds after the self-filter is layered in.
**Inputs:** `pg_stat_statements_v1` registration.
**Expected Behavior:**
- The collector still selects from `pg_stat_statements` with a
  wildcard projection (no fixed column list).
- The collector still declares `RequiresExtension =
  "pg_stat_statements"` and is gated behind the EA-R001 channel
  when the extension is absent.
- The query continues to pass the static linter.
**Failure Expectation:** Tightening the projection, dropping the
extension gate, or otherwise narrowing the dynamic-capture
surface is a regression on R037.

---

## TC-CONNECT-001: Guided connect emits a secret-free target block

**Linked Rules:** ARQ-SIGNALS-CONNECT-AC001, INV001, INV002
**Scenario:** A correctly-configured passwordless target is taken
through `connect --auto`: it connects, passes the read-only safety
check, and the operator is handed a ready-to-paste `targets:` block.
**Inputs:** A target with a resolvable cloud credential and a
least-privilege role; faked detector/resolver/diagnoser seams.
**Expected Behavior:**
- `Outcome.Success` is true and the rendered block carries
  `auth_method` and `sslmode: verify-full`.
- The block contains no password, token, key, or secret reference
  value.
**Failure Expectation:** Emitting any credential material, or a
block missing `verify-full`, regresses INV001/INV002.

---

## TC-CONNECT-002: Detection proposes the documented auth method

**Linked Rules:** ARQ-SIGNALS-CONNECT-AC002, AC003, FC001
**Scenario:** Confirm the detection table — ambient identity AND a
matching host pattern proposes the cloud method; identity alone or
host alone falls back to password; multiple identities with no host
match is reported ambiguous, never guessed.
**Inputs:** The spec's environment fixtures (env-only, no network).
**Expected Behavior:**
- Each documented (identity, host) pair proposes its method.
- A host pattern disambiguates when several identities are present.
- Two identities with a non-cloud host yields `Ambiguous` with notes
  naming `--auth-method`.
**Failure Expectation:** Guessing under ambiguity, or a network probe
during detection, regresses AC002/AC003/FC001/NFR001.

---

## TC-CONNECT-003: No secret is ever printed

**Linked Rules:** ARQ-SIGNALS-CONNECT-AC004, INV001
**Scenario:** Drive every outcome path — success, resolve failure,
auth failure, role failure, guidance — and assert no secret value
appears in any rendered output.
**Inputs:** Seams that carry a sentinel secret value.
**Expected Behavior:** The sentinel never appears in the emitted
block, the failure message, or the guidance; resolve/diagnose detail
is redacted.
**Failure Expectation:** Any path echoing the sentinel regresses
INV001.

---

## TC-CONNECT-004: Missing-grant and over-privileged guidance

**Linked Rules:** ARQ-SIGNALS-CONNECT-AC005, AC006, FC004, FC005
**Scenario:** A connect that succeeds but is rejected for a missing
grant returns the exact grant guidance for the detected method; an
over-privileged role fails the read-only safety check and is reported
without a config block.
**Inputs:** Diagnoser returning `auth` then `role` categories across
all methods.
**Expected Behavior:**
- The grant guidance names the method-specific `GRANT`/role step.
- The role-failure outcome emits no config block.
**Failure Expectation:** Emitting a block for a failed role, or
generic non-actionable guidance, regresses AC005/AC006.

---

## TC-CONNECT-005: `--write` append is safe and idempotent-guarded

**Linked Rules:** ARQ-SIGNALS-CONNECT-AC007
**Scenario:** `--write` is dry-run by default; when set it appends a
secret-free block to `targets:` (creating the key if absent) and
refuses a duplicate target `name`.
**Inputs:** Config files with and without a `targets:` key, and one
already containing the target name.
**Expected Behavior:**
- Dry-run writes nothing.
- Write appends a secret-free block; the file re-parses; a duplicate
  name is refused with an actionable error.
**Failure Expectation:** Overwriting, corrupting, or duplicating a
target regresses AC007.

---

## TC-CONNECT-006: All methods covered; usage and non-TTY paths

**Linked Rules:** ARQ-SIGNALS-CONNECT-AC008, INV004, FC006, usage
**Scenario:** Every supported `auth_method` has detection, connection,
and guidance; an explicit `--auth-method` overrides detection; a
password method with no TTY and no supplied password is reported (not
blocked); missing `--user`/`--host` is a usage error.
**Inputs:** All six methods; override flag; no-TTY/no-password case;
empty required flags.
**Expected Behavior:**
- `GuidanceFor` returns method-specific guidance for each method.
- Override wins over detection (single method per run).
- The no-source password path reports FC006 without prompting.
- Missing required flags map to a usage error before any dial.
**Failure Expectation:** A method without guidance, a second method
slipping through, blocking on input, or dialing on a usage error
regresses AC008/INV004/FC006.
