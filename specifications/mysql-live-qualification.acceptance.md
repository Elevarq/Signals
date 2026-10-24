# Acceptance Tests: mysql-live-qualification

## Feature

`specifications/mysql-live-qualification.md` — the normal `cmd/signals` daemon
collecting a real MySQL and exporting through the production exporter, verified
with the shared engine-generic verifier; runs in CI (`integration-mysql`) and
locally.

## Test Cases

### TC-MLQ-01: full-grant snapshot — every expected MySQL collector succeeds (normal, live)

**Rule:** MLQ-R002, MLQ-R003, INV-MLQ-03

**Given:** a live seeded MySQL (`SELECT, PROCESS`) and a snapshot collected by the
normal daemon.

**When:** `VerifyCadenceSnapshot(s, ExpectedCollectorsFor("mysql"), "mysql", "8.")`
runs over the persisted snapshot.

**Then:** engine is `mysql`, version matches `8.`, the run set equals exactly the
nine MySQL collectors, every collector is `status='success'` (replication a
legitimate zero-row success), and each success reconciles to a `query_results`
row whose decoded row count equals `row_count`.

**Expected Result:** Pass only when all nine collectors succeeded with reconciled
evidence.

---

### TC-MLQ-02: cadence — multiple independent snapshots, unique ULIDs, restart appends (normal, live)

**Rule:** MLQ-R002

**Given:** the daemon run on a short poll, then restarted against the same store.

**When:** snapshots are enumerated before/after the restart.

**Then:** ≥ 2 snapshots, all ULIDs unique (no overwrite), restart strictly
appends; every snapshot passes `VerifyCadenceSnapshot`.

**Expected Result:** Pass when cadence + restart-append hold.

---

### TC-MLQ-03: export reconciliation — collected == persisted == exported (normal, live)

**Rule:** MLQ-R004

**Given:** one exact live snapshot exported through the production exporter.

**When:** `VerifyExportReconciliation` runs over the snapshot + the ZIP.

**Then:** metadata `engine=mysql` + non-empty `engine_version`; only MySQL
collectors in `collector_status.json` / `query_catalog.json`; collected ==
persisted == exported per successful collector; `INFO` redacted; manifest
complete.

**Expected Result:** Pass when the three counts reconcile and no PG collector
leaks.

---

### TC-MLQ-04: runs in CI through the PostgreSQL-mandatory gate (structural)

**Rule:** MLQ-R005

**Given:** `.github/workflows/ci.yml`.

**When:** the workflow is parsed (the M0 guard test
`TestCIGateRequiresPostgreSQL`).

**Then:** an `integration-mysql` job exists and is in `ci-gate.needs` (so it
routes through the same terminal gate as PostgreSQL and cannot add a
PostgreSQL-free green path).

**Expected Result:** Pass when `integration-mysql` is wired into the gate.
