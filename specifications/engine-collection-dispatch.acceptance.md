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

**Then:** the result is not `BackendPostgreSQL` — `collectTarget` therefore
routes it to the SingleStore session (MySQL-wire), never pgx.

**Expected Result:** Pass when the dispatcher keeps non-PostgreSQL targets off
the pgx path.

---

### TC-ECD-05: SingleStore collected through the production boundary (#532)

**Rule:** ECD-R002, ECD-R003, ECD-R005, INV-ECD-03

**Given:** a configured `Engine == singlestore` target and a fake injected at the
DB/network boundary only (scalar identity probe + row runner); dispatch,
session, identity validation, orchestration, mapping, persistence, and export
run for real.

**When:** the production `collectTarget` runs the target.

**Then:** a snapshot with `engine=singlestore` is persisted via the common
`InsertCollection`, one `query_run` per collector in the catalog, and the
normal exporter emits `metadata.engine == singlestore`.

**Expected Result:** Pass; the pre-fix skip (which persisted nothing) fails this.
Tests: `internal/collector::TestCollectTarget_SingleStoreProductionBoundary`,
`internal/export::TestExportMetadataDeclaresEngine`.

---

### TC-ECD-06: non-SingleStore server is a target-scoped failure, no fallback

**Rule:** ECD-R004, ECD-R006

**Given:** a target configured `engine=singlestore` whose server identifies as
plain MySQL (no `@@memsql_version`; MySQL `version_comment`).

**When:** `collectTarget` runs it.

**Then:** collection fails explicitly (refusing to collect), nothing is
persisted, and it is NOT collected as MySQL.

**Expected Result:** Pass. Test:
`internal/collector::TestCollectTarget_SingleStoreIdentityMismatch`.

---

### TC-ECD-07: unsupported engine skipped explicitly (no SingleStore routing)

**Rule:** ECD-R006

**Given:** a target with `Engine == EngineMySQL`.

**When:** `collectTarget` runs it.

**Then:** it is skipped explicitly (`engine_unsupported`); the SingleStore
session is never entered; no snapshot is persisted.

**Expected Result:** Pass. Test:
`internal/collector::TestCollectTarget_UnsupportedEngineSkipped`.

---

### TC-ECD-08: new production write requires explicit engine (no silent default)

**Rule:** ECD-R005

**Given:** a new collection snapshot with no engine identity.

**When:** the production write path `InsertCollection` is called.

**Then:** it is rejected — a new write never silently becomes PostgreSQL; the
`snapshots.engine` column default is for historical rows only.

**Expected Result:** Pass. Test:
`internal/db::TestInsertCollection_RejectsImplicitEngine`.

---

### TC-ECD-09: engine round-trips; historical rows default to PostgreSQL

**Rule:** ECD-R005, INV-ECD-02

**Given:** a persisted `engine=singlestore` snapshot and a pre-#532 snapshot
written with no engine.

**When:** read back through the common snapshot reads.

**Then:** the SingleStore snapshot reports `engine=singlestore`; the legacy
write defaults to `postgresql`.

**Expected Result:** Pass. Test:
`internal/db::TestSnapshotEngineIdentityRoundTrip`.
