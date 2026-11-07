# Acceptance Tests: clickhouse-live-qualification

## Feature

`specifications/clickhouse-live-qualification.md` — the normal `cmd/signals` daemon
collecting a real ClickHouse and exporting through the production exporter, verified
with the shared engine-generic verifier; runs in CI (`integration-clickhouse`) and
locally.

## Test Cases

### TC-MLQ-01: full-grant snapshot — every expected ClickHouse collector succeeds (normal, live)

**Rule:** CHLQ-R002, CHLQ-R003, INV-CHLQ-03

**Given:** a live seeded ClickHouse (`SELECT ON system.*`) and a snapshot collected by the
normal daemon.

**When:** `VerifyCadenceSnapshot(s, ExpectedCollectorsFor("clickhouse"), "clickhouse", "24.")`
runs over the persisted snapshot.

**Then:** engine is `clickhouse`, version matches `8.`, the run set equals exactly the
nine ClickHouse collectors, every collector is `status='success'` (replication a
legitimate zero-row success), and each success reconciles to a `query_results`
row whose decoded row count equals `row_count`.

**Expected Result:** Pass only when all nine collectors succeeded with reconciled
evidence.

---

### TC-MLQ-02: cadence — multiple independent snapshots, unique ULIDs, restart appends (normal, live)

**Rule:** CHLQ-R002

**Given:** the daemon run on a short poll, then restarted against the same store.

**When:** snapshots are enumerated before/after the restart.

**Then:** ≥ 2 snapshots, all ULIDs unique (no overwrite), restart strictly
appends; every snapshot passes `VerifyCadenceSnapshot`.

**Expected Result:** Pass when cadence + restart-append hold.

---

### TC-MLQ-03: export reconciliation — collected == persisted == exported (normal, live)

**Rule:** CHLQ-R004

**Given:** one exact live snapshot exported through the production exporter.

**When:** `VerifyExportReconciliation` runs over the snapshot + the ZIP.

**Then:** metadata `engine=clickhouse` + non-empty `engine_version`; only ClickHouse
collectors in `collector_status.json` / `query_catalog.json`; collected ==
persisted == exported per successful collector; `INFO` redacted; manifest
complete.

**Expected Result:** Pass when the three counts reconcile and no PG collector
leaks.

---

### TC-MLQ-04: runs in CI through the PostgreSQL-mandatory gate (structural)

**Rule:** CHLQ-R005

**Given:** `.github/workflows/ci.yml`.

**When:** the workflow is parsed (the M0 guard test
`TestCIGateRequiresPostgreSQL`).

**Then:** an `integration-clickhouse` job exists and is in `ci-gate.needs` (so it
routes through the same terminal gate as PostgreSQL and cannot add a
PostgreSQL-free green path).

**Expected Result:** Pass when `integration-clickhouse` is wired into the gate.

---

### TC-MLQ-05: replicated cluster — cluster-scope collectors surface real replication (normal, live)

**Rule:** CHLQ-R006, CHLQ-R007

**Given:** the pinned 1-shard / 2-replica ClickHouse cluster + Keeper
(`examples/clickhouse-cluster`) brought up by
`scripts/clickhouse-cluster-fixture.sh up`, seeded with a read-only `signals_ro`
role (`SELECT ON system.*` + `SHOW TABLES ON signals_demo.*`) and a
`ReplicatedMergeTree` created `ON CLUSTER`.

**When:** the fixture `verify` runs and the normal daemon qualifies against node
`ch1` (`CH_QUAL_EXTERNAL=1 scripts/clickhouse-live-qual.sh all`).

**Then:** `system.clusters` reports `signals_cluster` with 2 replicas
(`clickhouse_clusters_v1`), `system.replicas` carries `signals_demo.repl` with
live state (`clickhouse_replicas_v1`), every ClickHouse collector persists
`status='success'`, and the export reconciles — all under a read-only role (no
data-read grant; `SHOW TABLES` is metadata-only).

**Expected Result:** Pass — proven locally (11 snapshots, all collectors
`status=success`, export reconciled) and in CI via `integration-clickhouse-replicated`.
