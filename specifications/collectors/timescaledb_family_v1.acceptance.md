# Acceptance Tests: timescaledb_family_v1

## Feature

`specifications/collectors/timescaledb_family_v1.md` (R114, R115)

Test evidence types follow `features/arq-signals/traceability.md`:
BEHAVIORAL tests run without a live server against the registry;
INTEGRATION tests are build-tag guarded (`//go:build integration`)
and gated on `SIGNALS_TEST_TSDB_DSN` (TimescaleDB target) /
`SIGNALS_TEST_PG_DSN` (plain PostgreSQL target).

## Test Cases

### TC-TSDB-01: Plain PostgreSQL — family skips cleanly

**Rule:** Failure condition — FC-TSDB-01 (normal)

**Given:** a PostgreSQL target with no `timescaledb` row in
`pg_extension`.

**When:** a collection cycle runs (or `pgqueries.Filter` /
`GatedIDsByReason` is evaluated without the extension).

**Then:**
- No family query executes; no family rows in `query_results.ndjson`.
- Every family member appears in `collector_status.json` with
  `status=skipped, reason=extension_missing` (EA-R001).
- The snapshot succeeds.

---

### TC-TSDB-02: Empty TimescaleDB database

**Rule:** Normal — FC-TSDB-07

**Given:** TimescaleDB (supported version, Community edition)
installed; no hypertables, caggs, or user jobs.

**When:** a collection cycle runs.

**Then:**
- `timescaledb_extension_v1` emits exactly one row with non-NULL
  `extversion`, `extension_schema`, `license`, and all capability
  flags populated.
- Inventory members emit empty rowsets with `status=success`.
- `timescaledb_jobs_v1` may contain built-in jobs (telemetry,
  log-retention) — rows with `job_id < 1000` are valid output.

---

### TC-TSDB-03: Hypertable inventory

**Rule:** Normal

**Given:** a hypertable created over a time column.

**Then:** `timescaledb_hypertables_v1` contains one row for it
(schema, name, owner, `num_dimensions ≥ 1`, `num_chunks`,
`compression_enabled`); on TimescaleDB ≥ 2.20 the row additionally
carries `primary_dimension` / `primary_dimension_type`; on < 2.20
those columns are absent — both are accepted (dynamic capture).

---

### TC-TSDB-04: Dimensions

**Rule:** Normal

**Given:** the TC-TSDB-03 hypertable (one time dimension; optionally
one space dimension).

**Then:** `timescaledb_dimensions_v1` has one row per dimension with
`dimension_type` ∈ {`Time`, `Space`}, the time row carrying
`time_interval` (or `integer_interval`), ordered deterministically.

---

### TC-TSDB-05: Chunks + summary agree

**Rule:** Normal

**Given:** a hypertable with ≥ 2 chunks (data spanning multiple
chunk intervals).

**Then:**
- `timescaledb_chunks_v1` has one row per chunk with
  parent hypertable, chunk schema/name, `range_start`/`range_end`
  (or integer variants), `is_compressed=false`.
- `timescaledb_chunk_summary_v1` has exactly one row for the
  hypertable with `chunk_count` equal to the number of chunk rows,
  `compressed_chunk_count=0`, and range bounds matching the
  min/max chunk ranges.

---

### TC-TSDB-06: Compression settings and stats

**Rule:** Normal

**Given:** compression enabled on the hypertable
(`segmentby`/`orderby` configured) and at least one chunk compressed,
at least one not.

**Then:**
- `timescaledb_compression_settings_v1` has a row for the hypertable
  with its `segmentby`/`orderby` values.
- `timescaledb_compression_stats_v1` reports
  `number_compressed_chunks ≥ 1`, `total_chunks` > 
  `number_compressed_chunks`, and non-NULL
  `before_compression_total_bytes` / `after_compression_total_bytes`.
- `timescaledb_chunks_v1` shows `is_compressed=true` for the
  compressed chunk; the summary's `compressed_chunk_count` agrees.

---

### TC-TSDB-07: Continuous aggregate

**Rule:** Normal + FC-TSDB-08

**Given:** a continuous aggregate over the hypertable with a refresh
policy.

**Then:**
- `timescaledb_continuous_aggregates_v1` has one row: cagg
  view schema/name, source hypertable, materialization hypertable,
  `materialized_only`, and `view_definition` populated.
- `timescaledb_jobs_v1` contains the refresh-policy job
  (`proc_name = 'policy_refresh_continuous_aggregate'`) with its
  `schedule_interval` and `config`.
- With `high_sensitivity_collectors_enabled: false`, the collector
  still runs and `view_definition` is NULL in every persisted row
  (redact path, R075).

---

### TC-TSDB-08: Retention policy

**Rule:** Normal

**Given:** `add_retention_policy` configured on the hypertable.

**Then:** `timescaledb_jobs_v1` contains a row with
`proc_name = 'policy_retention'`, the target hypertable schema/name,
`schedule_interval`, and `config` carrying `drop_after` (or
`drop_created_before`); `timescaledb_job_stats_v1` carries its
`job_id` with `last_run_status` / `next_start` /
`total_runs`/`total_successes`/`total_failures` once the job has run
(NULL stats accepted before first run on ≥ 2.23, absent row accepted
on < 2.23 — upstream JOIN change).

---

### TC-TSDB-09: Background job metadata

**Rule:** Normal

**Given:** any TimescaleDB database (built-in jobs exist).

**Then:** every `timescaledb_jobs_v1` row carries `job_id`,
`application_name`, `proc_schema`/`proc_name`, `schedule_interval`,
`max_runtime`, `max_retries`, `retry_period`, `scheduled`, `owner`;
`timescaledb_job_stats_v1` rows join on `job_id`.

---

### TC-TSDB-10: job_errors least-privilege — partial by design

**Rule:** Failure condition — FC-TSDB-05

**Given:** the collector connects as a least-privilege role (LOGIN +
`pg_monitor`, not a member of the database-owner role) and a job
owned by another role has failed.

**Then:** `timescaledb_job_errors_v1` returns zero rows with
`status=success` — never an error; `timescaledb_job_stats_v1`
still shows the failure (`total_failures ≥ 1`) for that job. With
the sensitivity opt-out active, any visible `err_message` values are
NULL-ed (redact path).

---

### TC-TSDB-11: Apache-2 edition

**Rule:** Failure condition — FC-TSDB-06

**Given:** an Apache-2 build (`-oss` image).

**Then:** `timescaledb_extension_v1.license = 'apache'`; all family
members run with `status=success`; compression/cagg/policy rowsets
are empty (TSL features unavailable); the snapshot succeeds.

---

### TC-TSDB-12: TimescaleDB below 2.14 — version gate

**Rule:** Failure condition — FC-TSDB-02 (R115)

**Given:** `pg_extension` reports `timescaledb` with
`extversion < 2.14` (registry-level simulation acceptable; no live
legacy server required).

**Then:** every member except `timescaledb_extension_v1` is gated
with `status=skipped, reason=version_unsupported`;
`timescaledb_extension_v1` remains eligible; the snapshot succeeds.

---

### TC-TSDB-13: Registration, linting, and gating properties

**Rule:** Invariants (BEHAVIORAL, no live server — the failing-first
test set for this slice)

**Then, for every family member:**
- registered with `Category = "timescaledb"`,
  `RequiresExtension = "timescaledb"`, `MinPGVersion = 14`;
- SQL passes `pgqueries.LintQuery` (SELECT/WITH-only, no mutating
  keywords or denylisted functions);
- absent from `pgqueries.Filter` output when `timescaledb` is not in
  the extension list, present when it is;
- listed under `extension_missing` by `GatedIDsByReason` when the
  extension is absent;
- `timescaledb_continuous_aggregates_v1` and
  `timescaledb_job_errors_v1` are `HighSensitivity = true` with
  `SensitiveColumns` exactly `["view_definition"]` /
  `["err_message"]`; every other member is low-sensitivity;
- `timescaledb_chunks_v1` SQL bounds its output (LIMIT 5000,
  newest-first ordering).

---

### TC-TSDB-14: Missing view/function at execution time

**Rule:** Failure condition — FC-TSDB-03 (R115)

**Given:** a family query fails with SQLSTATE 42P01 (undefined
table) or 42883 (undefined function) — e.g. extension API schema not
on the collector role's `search_path`.

**Then:** the failure is isolated to that collector's savepoint
(R038); the run is recorded `status=failed, reason=object_missing`;
all other collectors in the cycle execute; the snapshot succeeds.

---

### TC-TSDB-15: Snapshot size bound on chunk-heavy targets

**Rule:** Invariant — bounded output

**Given:** a hypertable with more than 5000 chunks (or a registry/SQL
level assertion of the cap).

**Then:** `timescaledb_chunks_v1` emits at most 5000 rows, newest
`chunk_creation_time` first (uniform across time- and
integer-dimension hypertables); `timescaledb_chunk_summary_v1.chunk_count`
reports the true total, making the truncation detectable.
