# Acceptance Tests: mariadb-collectors

## Feature

`specifications/mariadb-collectors.md` — the read-only MariaDB diagnostic catalog
and its registration in the per-engine `CatalogFor` registry.

## Test Cases

### TC-MDC-01: every MariaDB catalog statement is read-only (invariant)

**Rule:** MDC-R001, INV-MDC-01

**Given:** `mariadbCatalog()`. **When:** `HasWrite` is evaluated for each SQL.
**Then:** it is false for every statement. **Expected:** Pass when none is a write.

---

### TC-MDC-02: expected MariaDB collector set (normal)

**Rule:** MDC-R005

**Given:** `mariadbCatalog()`. **When:** ids are enumerated. **Then:** exactly the
fifteen `mariadb_*_v1` collectors, each with a non-empty category and SQL.
**Expected:** Pass when the set matches.

---

### TC-MDC-03: processlist is HighSensitivity with INFO redacted (boundary/failure)

**Rule:** MDC-R003, MDC-R004, INV-MDC-02

**Given:** `mariadb_processlist_v1` and a row carrying `INFO`. **When:** `Collect`
runs with `redactSensitive=true`. **Then:** it is `HighSensitivity`, `INFO` is in
`SensitiveColumns`, and the returned `INFO` value is nil. **Expected:** Pass when
redacted.

---

### TC-MDC-04: CatalogFor routes mariadb, no cross-engine leak (normal)

**Rule:** MDC-R005

**Given:** engines `mariadb`, `mysql`, `singlestore`, unknown. **When:**
`CatalogFor(engine)` is called. **Then:** `mariadb` → the MariaDB catalog ids;
other wire engines → their own; no id crosses engines; unknown → nil.
**Expected:** Pass when the registry maps correctly.

---

### TC-MDC-05: dispatch routes engine=mariadb through the wire session (normal)

**Rule:** engine-collection-dispatch ECD-R002/R004/R008

**Given:** an `engine=mariadb` target + a fake wire connector identifying as
MariaDB. **When:** `collectTarget` runs. **Then:** one `engine=mariadb` snapshot
is persisted, every run a `mariadb_*` collector, all `status='success'`; a server
identifying as `mysql` for a mariadb target is a target-scoped failure.
**Expected:** Pass (`TestCollectTarget_MariaDBProductionBoundary`,
`TestCollectTarget_MariaDBIdentityMismatchFails`).

---

### TC-MDC-06: InnoDB transactions redact trx_query (boundary/failure)

**Rule:** MDC-R004, MDC-R006, INV-MDC-03

**Given:** `mariadb_innodb_transactions_v1` and a row carrying `trx_query` (live
SQL, lowercase result key). **When:** `Collect` runs with `redactSensitive=true`.
**Then:** it is `HighSensitivity`, `trx_query` is in `SensitiveColumns`, and the
returned `trx_query` value is nil. **Expected:** Pass when redacted.

---

### TC-MDC-07: InnoDB locks redact lock_data (boundary/failure)

**Rule:** MDC-R008, INV-MDC-03

**Given:** `mariadb_innodb_locks_v1` and a row carrying `lock_data` (locked
index-key value, lowercase result key). **When:** `Collect` runs with
`redactSensitive=true`. **Then:** it is `HighSensitivity`, `lock_data` is in
`SensitiveColumns`, the returned `lock_data` is nil, and native descriptors
(`lock_mode`, `lock_table`) are preserved. **Expected:** Pass when the key value
is redacted and descriptors kept.

---

### TC-MDC-08: no sensitive column escapes the redaction contract (invariant)

**Rule:** INV-MDC-03, MDC-R004, MDC-R008

**Given:** every collector in `mariadbCatalog()`. **When:** each SQL is scanned
(case-insensitively) for a query-text column (`INFO`, `DIGEST_TEXT`, `trx_query`)
or locked-key data (`lock_data`). **Then:** any collector selecting one is
`HighSensitivity` with that column — in its exact result-row case — in
`SensitiveColumns`. **Expected:** Pass when the contract holds catalog-wide.

---

### TC-MDC-09: disabled states are zero-row successes, not failures (boundary)

**Rule:** MDC-R006, MDC-R007

**Given:** the activity / replication collectors and `mariadb_metadata_locks_v1`
against an idle, standalone, perf_schema-off server. **When:** each runs and
returns zero rows. **Then:** each is a successful empty result (`Err == ""`),
distinct from a collector error or a permission denial. (`mariadb_statement_digests_v1`
is the exception — it emits a DEGRADED signal instead; see TC-MDC-11.) **Expected:**
Pass — the `integration-mariadb` live qualification exercises this against the
pinned idle MariaDB 11.4 service container (perf_schema off, standalone, no Galera).

---

### TC-MDC-10: Replay Tier-1 evidence survives collection → export with semantics intact (live)

**Rule:** MDC-R009

**Given:** the seeded `signals_fixture.replay_fixture` table (nullable + NOT NULL
columns, a primary key, an ordinary index, a UNIQUE index, and a composite index
`idx_tenant_created (tenant_id, created_at)` with deliberately observable order)
collected by the normal daemon and exported through the production exporter.

**When:** `VerifyReplayTier1Evidence(export, "mariadb")` runs over the exported ZIP.

**Then:** `mariadb_columns_v1` preserves column order, per-column `IS_NULLABLE`,
and DDL-faithful **engine-native** `COLUMN_TYPE` (MariaDB `bigint(20)`/`int(11)`,
never normalized to MySQL); `mariadb_index_inventory_v1` carries PRIMARY
(NON_UNIQUE=0, id), uq_email (NON_UNIQUE=0), idx_status (NON_UNIQUE=1), and
idx_tenant_created with columns in the exact order [tenant_id, created_at].

**Expected Result:** Pass — `TestMariaDBLiveReplayTier1Evidence` across every
pinned `integration-mariadb` LTS leg (10.11, 11.4, 11.8, 12.3); the deterministic
verifier logic is covered by `TestVerifyReplayTier1Evidence_*`.

---

### TC-MDC-11: perf_schema OFF → statement-digest collector is DEGRADED / NOT_ASSESSED (failure)

**Rule:** MDC-R010, MDC-R011

**Given:** `mariadb_statement_digests_v1` run by `Collect` with a fake runner
that returns **0 rows, no error** for the digest SQL and **`performance_schema=0`**
for the `SELECT @@performance_schema` probe.

**When:** `Collect` runs the catalog and `mapWireCollection` classifies the
result.

**Then:** the collector's `Result` carries `Status="skipped"`,
`Reason="perf_schema_disabled"` and the mapped `QueryRun` is `status=skipped`,
`reason=perf_schema_disabled` (a DEGRADED / NOT_ASSESSED outcome), **not** a
silent `success` and **not** a `failed`. `perf_schema_disabled` is a member of
`metrics.CollectorSkippedReasons`.

**Expected:** Pass when the degrade is recorded distinctly.

---

### TC-MDC-12: perf_schema ON, 0 rows → legitimate empty success (normal)

**Rule:** MDC-R010

**Given:** `mariadb_statement_digests_v1` run with a fake runner returning **0
rows, no error** for the digest SQL and **`performance_schema=1`** for the probe.

**When:** `Collect` runs and the result is classified.

**Then:** the run stays `status=success`, `reason=""` ("no slow queries") — the
probe shows perf_schema ON so there is no degrade. **Expected:** Pass when the
empty-but-enabled result remains a success.

---

### TC-MDC-13: rows present → no probe, no degrade (boundary)

**Rule:** MDC-R010

**Given:** `mariadb_statement_digests_v1` run with a fake runner returning **≥1
row** for the digest SQL. The probe runner records whether it was invoked.

**When:** `Collect` runs.

**Then:** the `@@performance_schema` probe is **never issued** (a collector that
returned rows is never probed) and the run is `status=success`. **Expected:**
Pass when a non-empty result is left untouched and unprobed.

---

### TC-MDC-14: probe error does not turn a 0-row success into a failure (invalid)

**Rule:** MDC-R011

**Given:** `mariadb_statement_digests_v1` run with a fake runner returning **0
rows, no error** for the digest SQL and an **error** for the probe query.

**When:** `Collect` runs.

**Then:** the probe failure is swallowed — the run keeps its pre-probe outcome
(`status=success`, 0 rows), never `failed`, never `skipped`. **Expected:** Pass
when the diagnostic probe cannot downgrade a legitimate outcome.

---

### TC-MDC-15: global_status carries the InnoDB buffer-pool read counters (normal)

**Rule:** MDC-R012

**Given:** `mariadb_global_status_v1`.

**When:** its SQL IN-list is inspected.

**Then:** it contains both `Innodb_buffer_pool_reads` and
`Innodb_buffer_pool_read_requests` (the cumulative counters for Analyzer's
buffer-pool-read-pressure rate rule), alongside the existing live counters.

**Expected Result:** Pass when both counters are in the IN-list.

---

### TC-MDC-16: deadlock counter is collected unconditionally with its enabled-state (boundary)

**Rule:** MDC-R013

**Given:** `mariadb_innodb_deadlocks_v1`.

**When:** its SQL is inspected and run.

**Then:** it selects the single `lock_deadlocks` row from
`information_schema.INNODB_METRICS` by `NAME` (returning `NAME`, `COUNT`,
`ENABLED`) with **no** `ENABLED=1` filter, is a read-only `SELECT`, and is not
`HighSensitivity`. The row is present regardless of the lock-module default so
the consumer can read `COUNT` when `ENABLED=1` or treat it as NOT_ASSESSED
otherwise.

**Expected Result:** Pass when the deadlock row is collected unconditionally with
its `ENABLED` flag surfaced.
