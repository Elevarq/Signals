# Acceptance Tests: engine-connection-defaults

## Feature

`specifications/engine-connection-defaults.md` — engine-aware default port /
database for configured targets (#623). Pure configuration cases need no live
server; they assert the resolved `TargetConfig` passed toward the connection
layer.

## Test Cases

### TC-ECONN-01: engine-only env config resolves the correct default port (normal)

**Rule:** ECONN-R001, INV-ECONN-01/03

**Given:** `SIGNALS_TARGET_HOST` set, `SIGNALS_TARGET_ENGINE` ∈
{postgresql(omitted), mysql, mariadb, singlestore, clickhouse}, no
`SIGNALS_TARGET_PORT`. **When:** env config loads. **Then:** the resolved target
port is 5432 / 3306 / 3306 / 3306 / 9000 respectively. **Expected:** Pass for all
five.

---

### TC-ECONN-02: non-PostgreSQL env target does not default database to postgres (invariant)

**Rule:** ECONN-R002, INV-ECONN-01

**Given:** `SIGNALS_TARGET_ENGINE` = a non-PostgreSQL engine and no
`SIGNALS_TARGET_DBNAME`. **When:** env config loads. **Then:** the resolved
`DBName` is empty, never `postgres`. **And** with the engine omitted/PostgreSQL,
`DBName` defaults to `postgres`. **Expected:** Pass when only PostgreSQL gets the
`postgres` database default.

---

### TC-ECONN-03: explicit port and database always win (invariant)

**Rule:** ECONN-R001, INV-ECONN-02

**Given:** a non-PostgreSQL engine with explicit `SIGNALS_TARGET_PORT` and
`SIGNALS_TARGET_DBNAME`. **When:** env config loads. **Then:** the resolved port
and database are the explicit values, not the engine defaults. **Expected:** Pass
when explicit configuration overrides defaults.

---

### TC-ECONN-04: dbname required only for PostgreSQL (boundary/failure)

**Rule:** ECONN-R003

**Given:** `ValidateStrict` over a non-PostgreSQL target with empty `DBName`, and
separately a PostgreSQL target with empty `DBName`. **When:** validated. **Then:**
the non-PostgreSQL target is accepted (no dbname hard error); the PostgreSQL
target is a hard error. **Expected:** Pass when the dbname requirement is
engine-aware.

---

### TC-ECONN-05: YAML and env parity for an engine-only target (normal)

**Rule:** ECONN-R001

**Given:** a YAML target with an engine and no port, and an equivalent env target.
**When:** both load. **Then:** both resolve to the same engine default port.
**Expected:** Pass when YAML and env produce equivalent resolved connection
settings for equivalent values.

---

### TC-ECONN-06: unknown/unsupported engine is rejected, never downgraded (failure)

**Rule:** ECONN-R004

**Given:** a target with an explicit unsupported engine. **When:** `ValidateStrict`
runs. **Then:** a hard error (EID-R002); the engine is never silently downgraded
to PostgreSQL. **Expected:** Pass when unsupported engines fail closed.
