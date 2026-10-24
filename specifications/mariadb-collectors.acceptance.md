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
seven `mariadb_*_v1` collectors, each with a non-empty category and SQL.
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
