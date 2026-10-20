# Acceptance Tests: postgresql-snapshot-compat

## Feature

`specifications/postgresql-snapshot-compat.md` — PostgreSQL snapshot stays
v1-compatible as engine identity is added (#528 regression gate).

## Test Cases

### TC-PGC-01: PostgreSQL identity leaves engine fields empty (normal)

**Rule:** PGC-R001, INV-PGC-01

**Given:** a `Metadata{PGVersion: "PostgreSQL 18.2 …"}`.

**When:** `m.SetEngineIdentity(engineid.EnginePostgreSQL, "18.2")` runs.

**Then:** `m.Engine == ""` and `m.EngineVersion == ""`; `m.PGVersion` is
unchanged.

**Expected Result:** Pass when PostgreSQL identity never populates the engine
fields.

---

### TC-PGC-02: SingleStore identity sets engine fields (boundary)

**Rule:** PGC-R002

**Given:** a `Metadata{}`.

**When:** `m.SetEngineIdentity(engineid.EngineSingleStore, "8.5.22")` runs.

**Then:** `m.Engine == "singlestore"` and `m.EngineVersion == "8.5.22"`.

**Expected Result:** Pass when a non-PostgreSQL engine is recorded.

---

### TC-PGC-03: Empty engine is treated as PostgreSQL (boundary)

**Rule:** PGC-R001

**Given:** a `Metadata{}`.

**When:** `m.SetEngineIdentity("", "")` runs.

**Then:** `m.Engine == ""` and `m.EngineVersion == ""`.

**Expected Result:** Pass when an absent engine never writes engine fields.

---

### TC-PGC-04: PostgreSQL metadata marshals without engine keys (invariant)

**Rule:** INV-PGC-01

**Given:** a PostgreSQL `Metadata` (engine fields empty).

**When:** it is marshaled to JSON.

**Then:** the JSON contains neither an `"engine"` nor an `"engine_version"`
key — byte-compatible with `signals-snapshot.v1`.

**Expected Result:** Pass when `omitempty` keeps the PostgreSQL shape identical.

---

### TC-PGC-05: Historical v1 metadata unmarshals (invalid/legacy input)

**Rule:** INV-PGC-02, INV-PGC-03

**Given:** a historical v1 JSON document:
`{"schema_version":"signals-snapshot.v1","collector_version":"1.2.3","pg_version":"PostgreSQL 17.4 …","target":{"name":"t","host":"h","port":5432,"dbname":"d"}}`
(no engine fields).

**When:** it is unmarshaled into `Metadata`.

**Then:** no error; `Engine == ""`; `PGVersion` and `Target` are preserved.

**Expected Result:** Pass when pre-engine v1 snapshots remain readable and the
engine fields are purely additive.
