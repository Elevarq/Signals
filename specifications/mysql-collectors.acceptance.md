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

---

### TC-MYC-13: table_statistics carries TABLE_COLLATION (normal)

**Rule:** MYC-R013

**Given:** `mysql_table_statistics_v1`.

**When:** its SQL is inspected, and the collector is run against a target that
returns a table with a `TABLE_COLLATION`.

**Then:** the SQL selects `TABLE_COLLATION` from `information_schema.TABLES`, is a
read-only `SELECT`, and is not `HighSensitivity`; in the normalized evidence the
table's row carries the collation value under `TABLE_COLLATION`.

**Expected Result:** Pass when `TABLE_COLLATION` is selected and carried through.

---

### TC-MYC-14: columns carry CHARACTER_SET_NAME/COLLATION_NAME, NULL carried as absent (normal + boundary)

**Rule:** MYC-R013

**Given:** `mysql_columns_v1`.

**When:** its SQL is inspected, and the collector is run against a target that
returns one string column WITH a charset/collation and one non-string column
WITHOUT (NULL for both).

**Then:** the SQL selects `CHARACTER_SET_NAME` and `COLLATION_NAME` from
`information_schema.COLUMNS`, is a read-only `SELECT`, and is not
`HighSensitivity`; in the normalized evidence the string column's row carries the
charset/collation values and the non-string column's row carries
`CHARACTER_SET_NAME = nil` and `COLLATION_NAME = nil` (absent) — never an error.

**Expected Result:** Pass when both columns are selected and a NULL is carried
through as nil.

---

### TC-MYC-15: global_variables carries the batch-2 observability/replication/InnoDB/charset variables (normal)

**Rule:** MYC-R014

**Given:** `mysql_global_variables_v1`.

**When:** its SQL IN-list is inspected.

**Then:** it contains each batch-2 variable — observability (`slow_query_log`,
`long_query_time`, `log_output`, `performance_schema`), replication/HA config
(`super_read_only`, `gtid_mode`, `enforce_gtid_consistency`,
`binlog_expire_logs_seconds`, `expire_logs_days`, `rpl_semi_sync_source_enabled`,
`rpl_semi_sync_replica_enabled`, `rpl_semi_sync_master_enabled`,
`rpl_semi_sync_slave_enabled`, `log_replica_updates`, `log_slave_updates`),
InnoDB internals (`innodb_adaptive_hash_index`, `innodb_flush_neighbors`,
`innodb_change_buffering`, `innodb_read_io_threads`, `innodb_write_io_threads`),
and server charset/collation (`character_set_server`, `collation_server`),
alongside the existing #660 sizing variables; the statement is a read-only
`SELECT` and the collector is not `HighSensitivity`.

**Expected Result:** Pass when every batch-2 variable is in the IN-list.

---

### TC-MYC-16: version-absent batch-2 variable is carried as absent, never an error (boundary / invalid)

**Rule:** MYC-R014

**Given:** `mysql_global_variables_v1` run against a MySQL version where a batch-2
variable does not exist (e.g. `rpl_semi_sync_source_enabled` on MySQL <8.0.26, or
`binlog_expire_logs_seconds` vs `expire_logs_days`).

**When:** the version-tolerant IN-list SELECT executes.

**Then:** the absent variable simply returns no row — the collector result is a
successful partial set, never a failure, and no credential variable is ever named
in the IN-list.

**Expected Result:** Pass when an absent variable yields no row (no error) and the
IN-list contains no credential/secret variable name.

---

### TC-MYC-17: global_status carries the workload/InnoDB counters (normal)

**Rule:** MYC-R015

**Given:** `mysql_global_status_v1`.

**When:** its SQL IN-list is inspected.

**Then:** it contains each workload counter (`Created_tmp_disk_tables`,
`Created_tmp_tables`, `Select_scan`, `Select_full_join`, `Sort_merge_passes`,
`Handler_read_rnd_next`, `Threads_created`, `Table_open_cache_hits`,
`Table_open_cache_misses`) and each InnoDB-internals counter
(`Innodb_buffer_pool_pages_dirty`, `Innodb_buffer_pool_pages_total`,
`Innodb_buffer_pool_wait_free`, `Innodb_log_waits`, `Innodb_row_lock_waits`,
`Innodb_row_lock_time`, `Innodb_deadlocks`), alongside the existing #656 buffer
-pool read counters; the statement is a read-only `SELECT` and the collector is
not `HighSensitivity`.

**Expected Result:** Pass when every workload/InnoDB counter is in the IN-list.

---

### TC-MYC-18: version-absent global_status counter is carried as absent, never an error (boundary / invalid)

**Rule:** MYC-R015

**Given:** `mysql_global_status_v1` run against a MySQL build where a counter
does not exist (e.g. `Innodb_deadlocks` where the build does not expose it).

**When:** the version-tolerant IN-list SELECT executes.

**Then:** the absent counter simply returns no row — the collector result is a
successful partial set, never a failure and never a fabricated zero, and no
credential/secret name is ever present in the IN-list.

**Expected Result:** Pass when an absent counter yields no row (no error) and the
IN-list contains no credential/secret name.
