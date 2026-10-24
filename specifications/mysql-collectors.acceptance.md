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

**Then:** they are exactly the nine `mysql_*_v1` collectors in the spec, each
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

### TC-MYC-05: no query-text column escapes the redaction contract (invariant)

**Rule:** INV-MYC-03

**Given:** every collector in `mysqlCatalog()`.

**When:** each SQL is scanned for a query-text column (`INFO`).

**Then:** any collector selecting it is `HighSensitivity` with it in
`SensitiveColumns`.

**Expected Result:** Pass when the contract holds for the whole catalog.
