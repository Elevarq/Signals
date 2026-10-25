# Acceptance Tests: mariadb-live-qualification

## Feature

`specifications/mariadb-live-qualification.md` — the normal `cmd/signals` daemon
collecting a real MariaDB and exporting through the production exporter, verified
with the shared engine-generic verifier; runs in CI (`integration-mariadb`) and
locally.

## Test Cases

### TC-MLQ-01: full-grant snapshot — every expected MariaDB collector succeeds (normal, live)

**Rule:** MDLQ-R002, MDLQ-R003, INV-MDLQ-03

**Given:** a live seeded MariaDB (`SELECT, PROCESS`) on any qualified LTS line and a
snapshot collected by the normal daemon.

**When:** `VerifyCadenceSnapshot(s, ExpectedCollectorsFor("mariadb"), "mariadb", prefix)`
runs over the persisted snapshot, where `prefix` is the running leg's
`MARIADB_QUAL_VERSION_PREFIX` (`10.11.` / `11.4.` / `11.8.` / `12.3.`).

**Then:** engine is `mariadb`, version matches the leg's prefix, the run set equals
exactly the 15 MariaDB collectors, every collector is `status='success'`
(replication/Galera legitimate standalone successes, `performance_schema`-gated
collectors legitimate empty successes when it is off), and each success reconciles
to a `query_results` row whose decoded row count equals `row_count`.

**Expected Result:** Pass only when all 15 collectors succeeded with reconciled
evidence.

---

### TC-MLQ-02: cadence — multiple independent snapshots, unique ULIDs, restart appends (normal, live)

**Rule:** MDLQ-R002

**Given:** the daemon run on a short poll, then restarted against the same store.

**When:** snapshots are enumerated before/after the restart.

**Then:** ≥ 2 snapshots, all ULIDs unique (no overwrite), restart strictly
appends; every snapshot passes `VerifyCadenceSnapshot`.

**Expected Result:** Pass when cadence + restart-append hold.

---

### TC-MLQ-03: export reconciliation — collected == persisted == exported (normal, live)

**Rule:** MDLQ-R004

**Given:** one exact live snapshot exported through the production exporter.

**When:** `VerifyExportReconciliation` runs over the snapshot + the ZIP.

**Then:** metadata `engine=mariadb` + non-empty `engine_version`; only MariaDB
collectors in `collector_status.json` / `query_catalog.json`; collected ==
persisted == exported per successful collector; `INFO` redacted; manifest
complete.

**Expected Result:** Pass when the three counts reconcile and no PG collector
leaks.

---

### TC-MLQ-04: runs in CI through the PostgreSQL-mandatory gate (structural)

**Rule:** MDLQ-R005

**Given:** `.github/workflows/ci.yml`.

**When:** the workflow is parsed (the M0 guard test
`TestCIGateRequiresPostgreSQL`).

**Then:** an `integration-mariadb` job exists and is in `ci-gate.needs` (so it
routes through the same terminal gate as PostgreSQL and cannot add a
PostgreSQL-free green path).

**Expected Result:** Pass when `integration-mariadb` is wired into the gate.

---

### TC-MLQ-05: four MariaDB LTS legs are pinned in CI (structural)

**Rule:** MDLQ-R006, VSP-R006

**Given:** `.github/workflows/ci.yml`.

**When:** the `integration-mariadb` job's matrix is parsed
(`TestMariaDBVersionMatrixQualifiesLTS`).

**Then:** the matrix has four legs covering `10.11`, `11.4`, `11.8`, and `12.3`,
each pinned by image digest, each carrying its own `version_prefix`.

**Expected Result:** Pass when all four qualified LTS legs are present and pinned;
red when any is dropped or unpinned.
