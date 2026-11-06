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

---

### TC-MDC-17: table_statistics carries AUTO_INCREMENT, NULL carried as absent (normal + boundary)

**Rule:** MDC-R014

**Given:** `mariadb_table_statistics_v1`.

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

### TC-MDC-18: table_statistics carries TABLE_COLLATION (normal)

**Rule:** MDC-R015

**Given:** `mariadb_table_statistics_v1`.

**When:** its SQL is inspected, and the collector is run against a target that
returns a table with a `TABLE_COLLATION`.

**Then:** the SQL selects `TABLE_COLLATION` from `information_schema.TABLES`, is a
read-only `SELECT`, and is not `HighSensitivity`; in the normalized evidence the
table's row carries the collation value under `TABLE_COLLATION`.

**Expected Result:** Pass when `TABLE_COLLATION` is selected and carried through.

---

### TC-MDC-19: columns carry CHARACTER_SET_NAME/COLLATION_NAME, NULL carried as absent (normal + boundary)

**Rule:** MDC-R015

**Given:** `mariadb_columns_v1`.

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

### TC-MDC-20: global_variables carries the batch-2 observability/replication/InnoDB/charset variables (normal)

**Rule:** MDC-R016

**Given:** `mariadb_global_variables_v1`.

**When:** its SQL IN-list is inspected.

**Then:** it contains each batch-2 variable — observability (`slow_query_log`,
`long_query_time`, `log_output`, `performance_schema`), replication/HA config
(`gtid_strict_mode`, `gtid_domain_id`, `expire_logs_days`,
`rpl_semi_sync_master_enabled`, `rpl_semi_sync_slave_enabled`,
`log_slave_updates`), InnoDB internals (`innodb_adaptive_hash_index`,
`innodb_flush_neighbors`, `innodb_change_buffering`, `innodb_read_io_threads`,
`innodb_write_io_threads`), and server charset/collation
(`character_set_server`, `collation_server`), alongside the existing #660 sizing
variables; the statement is a read-only `SELECT` and the collector is not
`HighSensitivity`. MySQL-only names (`super_read_only`, `gtid_mode`, …) are not
present on the MariaDB collector.

**Expected Result:** Pass when every MariaDB batch-2 variable is in the IN-list
and no MySQL-only name leaks in.

---

### TC-MDC-21: version-absent batch-2 variable is carried as absent, never an error (boundary / invalid)

**Rule:** MDC-R016

**Given:** `mariadb_global_variables_v1` run against a MariaDB version where a
batch-2 variable does not exist.

**When:** the version-tolerant IN-list SELECT executes.

**Then:** the absent variable simply returns no row — the collector result is a
successful partial set, never a failure, and no credential variable is ever named
in the IN-list.

**Expected Result:** Pass when an absent variable yields no row (no error) and the
IN-list contains no credential/secret variable name.

---

### TC-MDC-22: global_status carries the workload/InnoDB counters (normal)

**Rule:** MDC-R017

**Given:** `mariadb_global_status_v1`.

**When:** its SQL IN-list is inspected.

**Then:** it contains each workload counter (`Created_tmp_disk_tables`,
`Created_tmp_tables`, `Select_scan`, `Select_full_join`, `Sort_merge_passes`,
`Handler_read_rnd_next`, `Threads_created`, `Table_open_cache_hits`,
`Table_open_cache_misses`) and each InnoDB-internals counter
(`Innodb_buffer_pool_pages_dirty`, `Innodb_buffer_pool_pages_total`,
`Innodb_buffer_pool_wait_free`, `Innodb_log_waits`, `Innodb_row_lock_waits`,
`Innodb_row_lock_time`, `Innodb_deadlocks`), alongside the existing #656 buffer
-pool read counters; the statement is a read-only `SELECT` and the collector is
not `HighSensitivity`. MariaDB returns `GLOBAL_STATUS` names uppercased and the
IN-list matches case-insensitively.

**Expected Result:** Pass when every workload/InnoDB counter is in the IN-list.

---

### TC-MDC-23: version-absent global_status counter is carried as absent, never an error (boundary / invalid)

**Rule:** MDC-R017

**Given:** `mariadb_global_status_v1` run against a MariaDB build where a counter
does not exist.

**When:** the version-tolerant IN-list SELECT executes.

**Then:** the absent counter simply returns no row — the collector result is a
successful partial set, never a failure and never a fabricated zero, and no
credential/secret name is ever present in the IN-list.

**Expected Result:** Pass when an absent counter yields no row (no error) and the
IN-list contains no credential/secret name.

---

### TC-MDC-24: user-accounts collector is secret-free with a computed empty-password flag (normal / safety)

**Rule:** MDC-R018, INV-MDC-04

**Given:** `mariadb_user_accounts_v1`.

**When:** its SQL and (via `Collect`) its normalized rows are inspected for a
`mysql.user` view fixture carrying `root`@`%`, an anonymous `''`@`localhost`, and
a passwordless account.

**Then:** the `SELECT` list is exactly `User`, `Host`, `plugin`, and the computed
boolean `((Password = '' OR Password IS NULL) AND (authentication_string = '' OR
authentication_string IS NULL)) AS has_empty_password`; the statement names no
`Password` / `authentication_string` / hash column in its select list; it reads
the read-only `mysql.user` view (NOT `mysql.global_priv`); the collector is a
read-only `SELECT` and is not `HighSensitivity`; and every returned row carries
`User` / `Host` / `plugin` / `has_empty_password` and NO `Password` /
`authentication_string` / hash key.

**Expected Result:** Pass when the empty-password signal is the derived boolean,
the lowest-exposure view source is read, and no hash column is selected or
present in the evidence.

---

### TC-MDC-25: user-privileges collector carries grantee/privilege/grantable (normal)

**Rule:** MDC-R019

**Given:** `mariadb_user_privileges_v1`.

**When:** its SQL is inspected and `Collect` runs over a `USER_PRIVILEGES`
fixture with a `*.*` grant and a `WITH GRANT OPTION` row.

**Then:** the `SELECT` reads `GRANTEE`, `PRIVILEGE_TYPE`, `IS_GRANTABLE` from
`information_schema.USER_PRIVILEGES`; it is read-only and not `HighSensitivity`;
the returned rows carry the grantee, the privilege type, and the `IS_GRANTABLE`
flag; and a permission-denied error degrades (recorded in `Err`) without
aborting the other collectors.

**Expected Result:** Pass when the privilege shape is captured and a denial is a
non-fatal skip.

---

### TC-MDC-26: no MariaDB collector selects a password/hash column (invariant / safety)

**Rule:** INV-MDC-04

**Given:** `mariadbCatalog()`.

**When:** every collector's SQL select list is scanned (case-insensitively) for
a password/hash column name (`Password`, `authentication_string`, or any
`*password*` / hash token appearing as a selected column, not inside the computed
`has_empty_password` boolean or an IN-list predicate).

**Then:** no collector selects such a column, no collector reads
`mysql.global_priv`, and no normalized evidence key of the security collectors is
a password/hash column.

**Expected Result:** Pass when the catalog is provably hash-free in its select
lists and evidence.

---

### TC-MDC-27: global_variables carries sql_mode (normal)

**Rule:** MDC-R020

**Given:** `mariadb_global_variables_v1`.

**When:** its SQL IN-list is inspected.

**Then:** it contains `sql_mode` — the comma-separated server SQL-mode flag
string (a shared MySQL/MariaDB server variable) consumed by the Analyzer
`sql_mode` advisory rule (Analyzer #4010) — alongside the existing #660/#679
variables; the statement is a read-only `SELECT`, the IN-list carries no
credential variable, and `sql_mode` absent on a given build simply returns no row
(never an error); the collector is not `HighSensitivity`.

**Expected Result:** Pass when `sql_mode` is in the IN-list and the version-tolerant,
credential-free allowlist posture holds.
