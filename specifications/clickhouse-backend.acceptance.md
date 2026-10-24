# Acceptance Tests: clickhouse-backend

## Feature

`specifications/clickhouse-backend.md` — ClickHouse as a third collection backend
(CH-1 foundation): native connectivity, identity, backend dispatch, config
support, minimal catalog, engine-neutral persistence/export.

## Test Cases

### TC-CHB-01: catalog is read-only (invariant)

**Rule:** INV-CHB-01

**Given:** `clickhouse.Catalog()`. **When:** `HasWrite` is evaluated per SQL.
**Then:** false for every statement. **Expected:** Pass.

---

### TC-CHB-02 / 03: Collect runs the catalog; a collector error is non-fatal

**Rule:** CHB-R004

**Given:** a stub runner. **When:** `Collect` runs. **Then:** one result per
collector with rows; an injected error is captured in `Result.Err`, others still
run. **Expected:** Pass (`TestCollectRunsCatalog`, `TestCollectPartialFailure`).

---

### TC-CHB-04: Identify confirms ClickHouse (boundary)

**Rule:** CHB-R003

**Given:** a scalar returning a version / an empty string. **When:** `Identify`
runs. **Then:** a non-empty version succeeds; an empty version is an identity
error. **Expected:** Pass.

---

### TC-CHB-05: permission-denied detection (failure)

**Rule:** CHB-R004

**Given:** ClickHouse ACCESS_DENIED / code 497 error strings. **When:**
`IsPermissionDenied` runs. **Then:** true for access errors, false otherwise.
**Expected:** Pass.

---

### TC-CHB-06: dispatch routes engine=clickhouse through the ClickHouse backend (normal)

**Rule:** CHB-R001, CHB-R002, CHB-R003, INV-CHB-02

**Given:** an `engine=clickhouse` target + a fake native connector identifying as
ClickHouse. **When:** `collectTarget` runs. **Then:** one `engine=clickhouse`
snapshot is persisted, every run a `clickhouse_*` collector, all
`status='success'`, snapshot version = the identified version.
**Expected:** Pass (`TestCollectTarget_ClickHouseProductionBoundary`).

---

### TC-CHB-07: mapping — engine + canonical reasons + skip-on-denied (normal/failure)

**Rule:** CHB-R004, CHB-R005

**Given:** results incl. success, ACCESS_DENIED, and a timeout error. **When:**
`mapClickHouseCollection` maps them. **Then:** snapshot `engine=clickhouse`;
success run carries its measured duration; the denied run is
`skipped`/`permission_denied`; the timeout run is `failed`/`timeout`.
**Expected:** Pass (`TestMapClickHouseCollection`).

---

### TC-CHB-08: SelectBackend + config support (normal)

**Rule:** CHB-R001

**Given:** engine `clickhouse`. **When:** `SelectBackend` / `ValidateStrict`.
**Then:** `BackendClickHouse`; config does not reject it.
**Expected:** Pass (`TestSelectBackend`, `TestValidateAcceptsClickHouseEngine`).

---

### TC-CHB-09: ClickHouse export reconciles, only ClickHouse collectors (normal, CI serialization)

**Rule:** CHB-R006, INV-CHB-01

**Given:** a `engine=clickhouse` snapshot persisted via `InsertCollectionAtomic`,
exported through the production `export.Builder`. **When:**
`VerifyExportReconciliation` runs. **Then:** metadata `engine=clickhouse` +
version; collected==persisted==exported; a foreign PostgreSQL catalog row is
excluded. **Expected:** Pass (`TestClickHouseExportSerializationReconciles`).
