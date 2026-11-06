# Acceptance Tests: mysql-collectors

## Feature

`specifications/mysql-collectors.md` — the read-only MySQL diagnostic catalog
and its registration in the per-engine `CatalogFor` registry.

## Test Cases

### TC-MYC-01: every MySQL catalog statement is read-only (invariant)

**Rule:** MYC-R001, INV-MYC-01

**Given:** `mysqlCatalog()`.

**When:** `HasWrite` is evaluated for each collector's SQL.

**Then:** it is false for every statement (all begin with `SELECT`).

**Expected Result:** Pass when no catalog statement is a write.

---

### TC-MYC-02: expected MySQL collector set (normal)

**Rule:** MYC-R005

**Given:** `mysqlCatalog()`.

**When:** its collector ids are enumerated.

**Then:** they are exactly the sixteen `mysql_*_v1` collectors in the spec, each
with a non-empty category and SQL.

**Expected Result:** Pass when the set matches.

---

### TC-MYC-03: processlist is HighSensitivity with INFO redacted (failure/boundary)

**Rule:** MYC-R003, MYC-R004, INV-MYC-02, INV-MYC-03

**Given:** `mysql_processlist_v1` and a row carrying `INFO`.

**When:** `Collect` runs with `redactSensitive=true`.

**Then:** the collector is `HighSensitivity`, `INFO` is in `SensitiveColumns`,
and the returned `INFO` value is nil.

**Expected Result:** Pass when the query-text column is redacted.

---

### TC-MYC-04: CatalogFor registry routes by engine (normal)

**Rule:** MYC-R005

**Given:** engines `mysql`, `singlestore`, and `postgresql`/`mariadb`/unknown.

**When:** `CatalogFor(engine)` is called.

**Then:** `mysql` → the MySQL catalog ids; `singlestore` → the SingleStore
catalog ids; any other engine → nil (no cross-engine leak, no PostgreSQL
fallback).

**Expected Result:** Pass when the registry maps each engine correctly.

---

### TC-MYC-05: no sensitive column escapes the redaction contract (invariant)

**Rule:** INV-MYC-03, MYC-R004, MYC-R008

**Given:** every collector in `mysqlCatalog()`.

**When:** each SQL is scanned (case-insensitively) for a query-text column
(`INFO`, `DIGEST_TEXT`, `trx_query`) or locked-key data (`LOCK_DATA`).

**Then:** any collector selecting one is `HighSensitivity` with that column — in
its exact result-row case — in `SensitiveColumns`.

**Expected Result:** Pass when the contract holds for the whole catalog.

---

### TC-MYC-06: InnoDB transactions redact trx_query (failure/boundary)

**Rule:** MYC-R004, MYC-R006, INV-MYC-03

**Given:** `mysql_innodb_transactions_v1` and a row carrying `trx_query` (live SQL
text, lowercase result key).

**When:** `Collect` runs with `redactSensitive=true`.

**Then:** the collector is `HighSensitivity`, `trx_query` is in
`SensitiveColumns`, and the returned `trx_query` value is nil.

**Expected Result:** Pass when the transaction's live SQL text is redacted.

---

### TC-MYC-07: data locks redact LOCK_DATA (failure/boundary)

**Rule:** MYC-R008, INV-MYC-03

**Given:** `mysql_data_locks_v1` and a row carrying `LOCK_DATA` (the locked
index-key value, uppercase result key).

**When:** `Collect` runs with `redactSensitive=true`.

**Then:** the collector is `HighSensitivity`, `LOCK_DATA` is in
`SensitiveColumns`, the returned `LOCK_DATA` value is nil, and the native lock
descriptors (e.g. `OBJECT_NAME`, `LOCK_MODE`, `LOCK_STATUS`) are preserved.

**Expected Result:** Pass when the locked key value is redacted and descriptors
are kept.

---

### TC-MYC-08: replication/activity zero-row states are a success, not a failure (boundary)

**Rule:** MYC-R006, MYC-R007

**Given:** the activity and replication collectors
(`mysql_innodb_transactions_v1`, `mysql_data_locks_v1`,
`mysql_data_lock_waits_v1`, `mysql_replica_applier_status_v1`,
`mysql_group_replication_members_v1`) against an idle, standalone server.

**When:** each runs and returns zero rows.

**Then:** each is recorded as a successful empty result (`Err == ""`), distinct
from a collector error (unavailable) or a permission denial.

**Expected Result:** Pass — the live qualification (`integration-mysql`) exercises
this against the pinned idle MySQL 8.4 service container.

---

### TC-MYC-09: Replay Tier-1 evidence survives collection → export with semantics intact (live)

**Rule:** MYC-R009

**Given:** the seeded `signals_fixture.replay_fixture` table (nullable + NOT NULL
columns, a primary key, an ordinary index, a UNIQUE index, and a composite index
`idx_tenant_created (tenant_id, created_at)` with deliberately observable order)
collected by the normal daemon and exported through the production exporter.

**When:** `VerifyReplayTier1Evidence(export, "mysql")` runs over the exported ZIP.

**Then:** `mysql_columns_v1` preserves column order (ORDINAL_POSITION 1..N),
per-column `IS_NULLABLE` (note=YES, others=NO) and DDL-faithful engine-native
`COLUMN_TYPE` (MySQL `bigint`/`int`); `mysql_index_inventory_v1` carries PRIMARY
(NON_UNIQUE=0, id), uq_email (NON_UNIQUE=0), idx_status (NON_UNIQUE=1), and
idx_tenant_created with columns in the exact order [tenant_id, created_at].

**Expected Result:** Pass — `TestMySQLLiveReplayTier1Evidence` across every pinned
`integration-mysql` leg (8.0, 8.4, 9.7); the deterministic verifier logic is
covered by `TestVerifyReplayTier1Evidence_*`.

---

### TC-MYC-10: global_status carries the InnoDB buffer-pool read counters (normal)

**Rule:** MYC-R010

**Given:** `mysql_global_status_v1`.

**When:** its SQL IN-list is inspected.

**Then:** it contains both `Innodb_buffer_pool_reads` and
`Innodb_buffer_pool_read_requests` (the cumulative counters for Analyzer's
buffer-pool-read-pressure rate rule), alongside the existing live counters.

**Expected Result:** Pass when both counters are in the IN-list.

---

### TC-MYC-11: deadlock counter is collected unconditionally with its enabled-state (boundary)

**Rule:** MYC-R011

**Given:** `mysql_innodb_deadlocks_v1`.

**When:** its SQL is inspected and run.

**Then:** it selects the single `lock_deadlocks` row from
`information_schema.INNODB_METRICS` by `NAME` (returning `NAME`, `COUNT`,
`STATUS`) with **no** `STATUS='enabled'` filter, is a read-only `SELECT`, and is
not `HighSensitivity`. The row is present regardless of the lock-module default
so the consumer can read `COUNT` when `STATUS='enabled'` or treat it as
NOT_ASSESSED otherwise.

**Expected Result:** Pass when the deadlock row is collected unconditionally with
its `STATUS` surfaced.

---

### TC-MYC-12: table_statistics carries AUTO_INCREMENT, NULL carried as absent (normal + boundary)

**Rule:** MYC-R012

**Given:** `mysql_table_statistics_v1`.

**When:** its SQL is inspected, and the collector is run against a target that
returns one table WITH an AUTO_INCREMENT counter and one table WITHOUT (NULL).

**Then:** the SQL selects `AUTO_INCREMENT` from `information_schema.TABLES`
alongside `TABLE_ROWS` / `DATA_LENGTH` / `INDEX_LENGTH`, is a read-only `SELECT`,
and is not `HighSensitivity`; in the normalized evidence the AI table's row
carries the counter value under `AUTO_INCREMENT` and the non-AI table's row
carries `AUTO_INCREMENT = nil` (absent) — never an error and never a fabricated
zero.

**Expected Result:** Pass when `AUTO_INCREMENT` is selected and a NULL is carried
through as nil.
