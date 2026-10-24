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
