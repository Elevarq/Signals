# Acceptance Tests: singlestore-grants

## Feature

`specifications/singlestore-grants.md` — least-privilege grant metadata on the
SingleStore collector catalog.

## Test Cases

### TC-SSG-01: Only cluster_status is elevated (invariant)

**Rule:** SSG-R002, INV-SSG-01

**Given:** `Catalog()`.

**When:** inspected.

**Then:** `singlestore_cluster_status_v1` has `ElevatedPrivilege == "CLUSTER"`;
every other collector has an empty `ElevatedPrivilege` (baseline SELECT+PROCESS).

**Expected Result:** Pass when exactly the cluster view is marked elevated.

---

### TC-SSG-02: Golden fixture replays into an engine=singlestore snapshot (#555)

**Rule:** (collectors + assembly contract; #555 deterministic fixture)

**Given:** the committed `testdata/singlestore_collect_fixture.json` — a real,
redacted, row-trimmed capture of the 10 collectors from a live workspace.

**When:** it is loaded into `[]Result` and passed to `BuildSnapshot(info,
"singlestore", version, results)`.

**Then:** `MetadataJSON` declares `engine=singlestore`; `QueryResultsNDJSON`
has one line per collector; the plancache `QUERY_TEXT` and processlist `INFO`
are redacted (null) in the fixture (no SQL text committed).

**Expected Result:** Pass — CI validates the full catalog shape with no live DB.
