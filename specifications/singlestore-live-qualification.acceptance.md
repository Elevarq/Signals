# Acceptance Tests: singlestore-live-qualification

## Feature

`specifications/singlestore-live-qualification.md` — the normal `cmd/signals`
daemon collecting a real SingleStore and exporting through the production
exporter, with success asserted as `status='success'` and reconciled result
evidence, never an attempt/row count.

## Test Cases

### TC-SLQ-01: full-grant snapshot — every expected collector succeeds (normal, live)

**Rule:** SLQ-R002, SLQ-R003, INV-SLQ-03

**Given:** a live seeded SingleStore (full `SELECT,PROCESS,CLUSTER` grant) and a
snapshot collected by the normal daemon.

**When:** `VerifyCadenceSnapshot` runs over the persisted snapshot.

**Then:** engine is `singlestore`, version matches `9.`, the run `query_id` set
equals exactly the expected collector set, every collector is `status='success'`,
and each success has a `query_results` row whose decoded row count equals
`query_run.row_count`.

**Expected Result:** Pass only when all 10 collectors succeeded with reconciled
evidence.

---

### TC-SLQ-02: injected non-success fails loudly (failure)

**Rule:** SLQ-R004, INV-SLQ-03

**Given:** a synthetic snapshot identical to a good one except one expected
collector's run is `status='failed'` (and another variant `status='skipped'`).

**When:** `VerifyCadenceSnapshot` runs.

**Then:** it returns an error naming that collector and its status — it does NOT
pass on the distinct-`query_id` count.

**Expected Result:** Pass (CI, deterministic) when the verifier rejects the
non-success collector.

---

### TC-SLQ-03: legitimate zero-row success passes (boundary)

**Rule:** SLQ-R003

**Given:** a success run with `row_count == 0` and a persisted result row whose
decoded row count is 0.

**When:** `VerifyCadenceSnapshot` runs.

**Then:** it passes (0 == 0 is legitimate, not a failure).

**Expected Result:** Pass (CI, deterministic).

---

### TC-SLQ-04: missing result for a success run fails (failure)

**Rule:** SLQ-R003

**Given:** a success run with no `query_results` row, or a result whose decoded
row count differs from `row_count`.

**When:** `VerifyCadenceSnapshot` runs.

**Then:** it returns an error for that run.

**Expected Result:** Pass (CI, deterministic) when the verifier rejects it.

---

### TC-SLQ-05: cadence — multiple independent snapshots, unique ULIDs, restart appends (normal, live)

**Rule:** SLQ-R005

**Given:** the daemon run on a short poll against the live server, then
restarted against the same store.

**When:** snapshots are enumerated before and after the restart.

**Then:** there are ≥ 2 snapshots before restart, all ULIDs unique (no
overwrite), and the restart strictly appends (count after > count before).

**Expected Result:** Pass (live) when cadence + restart-append hold.

---

### TC-SLQ-06: export reconciliation — collected == persisted == exported (normal, live)

**Rule:** SLQ-R006, SLQ-R007

**Given:** one exact live snapshot exported through the production exporter.

**When:** `VerifyExportReconciliation` runs over the snapshot + the ZIP.

**Then:** `metadata.json` has `engine=singlestore` + non-empty `engine_version`;
`collector_status.json` / `query_catalog.json` contain only SingleStore
collectors; and for each successful collector collected == persisted == exported.

**Expected Result:** Pass (live) when the three counts reconcile.

---

### TC-SLQ-07: export reconciliation — mismatch fails (failure)

**Rule:** SLQ-R007

**Given:** a synthetic (snapshot, export) pair where one collector's exported
payload row count differs from its persisted/collected count, and a variant
where a foreign PostgreSQL collector appears in `query_catalog`.

**When:** `VerifyExportReconciliation` runs.

**Then:** it returns an error in each case.

**Expected Result:** Pass (CI, deterministic) when the verifier rejects both.

---

### TC-SLQ-08: default redaction held in exported payloads (normal, live)

**Rule:** SLQ-R008

**Given:** high-sensitivity collectors disabled; the exported `query_results`
payloads for MV_PLANCACHE and MV_PROCESSLIST.

**When:** `VerifyExportReconciliation` inspects the payloads.

**Then:** no `QUERY_TEXT` or `INFO` value is present (null/absent).

**Expected Result:** Pass when redaction holds; a leaked value fails.

---

### TC-SLQ-09: manifest covers every ZIP member (normal, live + CI serialization)

**Rule:** SLQ-R009

**Given:** the exported ZIP.

**When:** `VerifyExportReconciliation` cross-checks `manifest.json` against the
ZIP members.

**Then:** every member has a non-empty SHA-256 in the manifest and the manifest
lists no absent member.

**Expected Result:** Pass when the manifest and members agree.

---

### TC-SLQ-10: production-exporter serialization reconciles (CI, deterministic)

**Rule:** SLQ-R006, SLQ-R007, SLQ-R009

**Given:** a SingleStore snapshot persisted via `InsertCollectionAtomic` and
exported through the production `export.Builder` (serialization/verifier
coverage, explicitly NOT producer evidence).

**When:** `VerifyExportReconciliation` runs over it.

**Then:** metadata, no-foreign-collector, reconciliation, and manifest checks all
pass — exercising the ZIP reader + reconciliation deterministically in CI.

**Expected Result:** Pass (CI) — the deterministic serialization leg of the
coverage receipt.

---

### TC-SLQ-11: runs in CI through the PostgreSQL-mandatory gate (structural)

**Rule:** SLQ-R010

**Given:** `.github/workflows/ci.yml`.

**When:** the workflow is parsed (the M0 guard test
`TestCIGateRequiresPostgreSQL`).

**Then:** an `integration-singlestore` job exists and is in `ci-gate.needs` (so
it routes through the same terminal gate as PostgreSQL and cannot add a
PostgreSQL-free green path). The job runs the harness in EXTERNAL mode
(`SINGLESTORE_QUAL_EXTERNAL=1`) against the pinned Dev Image service container.

**Expected Result:** Pass when `integration-singlestore` is wired into the gate.
