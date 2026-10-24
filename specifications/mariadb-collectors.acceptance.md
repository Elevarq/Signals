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

**Given:** the activity / replication / perf_schema-gated collectors against an
idle, standalone, perf_schema-off server. **When:** each runs and returns zero
rows. **Then:** each is a successful empty result (`Err == ""`), distinct from a
collector error or a permission denial. **Expected:** Pass — the
`integration-mariadb` live qualification exercises this against the pinned idle
MariaDB 11.4 service container (perf_schema off, standalone, no Galera).
