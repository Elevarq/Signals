# Acceptance Tests: engine-collection-dispatch

## Feature

`specifications/engine-collection-dispatch.md` — per-target backend selection;
the PostgreSQL (pgx) cycle only runs PostgreSQL targets.

## Test Cases

### TC-ECD-01: PostgreSQL / unset → BackendPostgreSQL (normal, compat)

**Rule:** ECD-R001, INV-ECD-02

**Given:** a `TargetConfig` with `Engine == ""` and one with `Engine == EnginePostgreSQL`.

**When:** `SelectBackend(t)` is called for each.

**Then:** both return `BackendPostgreSQL`.

**Expected Result:** Pass when the default/PostgreSQL target uses the pgx backend.

---

### TC-ECD-02: SingleStore → BackendMySQLWire (boundary)

**Rule:** ECD-R001

**Given:** a `TargetConfig` with `Engine == EngineSingleStore`.

**When:** `SelectBackend(t)` is called.

**Then:** it returns `BackendMySQLWire`.

**Expected Result:** Pass when SingleStore selects the MySQL-wire backend.

---

### TC-ECD-03: MySQL → BackendMySQLWire (boundary)

**Rule:** ECD-R001

**Given:** a `TargetConfig` with `Engine == EngineMySQL`.

**When:** `SelectBackend(t)` is called.

**Then:** it returns `BackendMySQLWire`.

**Expected Result:** Pass when MySQL selects the MySQL-wire backend.

---

### TC-ECD-04: non-PostgreSQL backend is not PostgreSQL (invariant)

**Rule:** INV-ECD-01

**Given:** a SingleStore target.

**When:** `SelectBackend(t)` is evaluated.

**Then:** the result is not `BackendPostgreSQL` — the guard in `collectTarget`
therefore skips it before any pgx connection.

**Expected Result:** Pass when the dispatcher keeps non-PostgreSQL targets off
the pgx path.
