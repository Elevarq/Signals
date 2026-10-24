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
