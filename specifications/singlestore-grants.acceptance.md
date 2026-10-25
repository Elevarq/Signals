# Acceptance Tests: singlestore-grants

## Feature

`specifications/singlestore-grants.md` — least-privilege grant metadata on the
SingleStore collector catalog.

## Test Cases

### TC-SSG-01: Exactly the CLUSTER-elevated collectors are marked (invariant)

**Rule:** SSG-R002, INV-SSG-01

**Given:** `Catalog()`.

**When:** inspected.

**Then:** `singlestore_cluster_status_v1` and `singlestore_mv_events_v1` each have
`ElevatedPrivilege == "CLUSTER"`; every other collector has an empty
`ElevatedPrivilege` (baseline SELECT+PROCESS).

**Expected Result:** Pass when exactly those two views are marked elevated, and no
other collector is.

---

### TC-SSG-02: RETIRED (#577/#589) — golden-fixture replay superseded by the live CI qualification

The committed `testdata/singlestore_collect_fixture.json` was a hand-captured,
row-trimmed snapshot of the catalog. It drifted from the real catalog and its
provenance could not prove the capture revision (#589), so it was never a
verifiable gate. It is retired along with the `TestGoldenFixtureReplaysIntoSnapshot`
replay test and the `singlestore_integration` DSN catalog test.

The full catalog shape + `engine=singlestore` assembly + redaction are now proven
every CI run by the `integration-singlestore` job (the real `cmd/signals` daemon
→ production exporter against the pinned Dev Image service container, EXTERNAL
mode — `specifications/singlestore-live-qualification.md` SLQ-R006..R010 /
TC-SLQ-06, TC-SLQ-08, TC-SLQ-11), which replays the *current* catalog rather than
a frozen snapshot. The deterministic serialization leg (`singlestore_qual_test.go`,
TC-SLQ-10) gives no-DB CI coverage of the verifier/export logic. This is strictly
stronger than the retired fixture replay.

---

### TC-SSG-03: cross-user SQL is redacted; current-user-only without PROCESS (#554)

**Rule:** SSG-R004, INV-SSG-02

**Given:** `Catalog()`. **When:** each collector that selects a cross-user SQL
column (`QUERY_TEXT` in `MV_PLANCACHE`, `INFO` in `MV_PROCESSLIST`) or connection
credentials (`CONFIG_JSON` in `PIPELINES`) is inspected. **Then:** it is
`HighSensitivity` with that column in `SensitiveColumns` (redacted by default).
Without `PROCESS` the live-workload views are current-user-only (a documented
state, not a denial); the `CLUSTER`-elevated collectors degrade to skipped
(TC-SSG-01).

**Expected Result:** Pass — `TestSensitiveColumnsAreHighSensitivity` enforces the
redaction surface catalog-wide (QUERY_TEXT/INFO/CONFIG_JSON). Redaction is defense
in depth; the `PROCESS`/`CLUSTER` grants are the primary boundary.
