# Acceptance Tests: workload-measurement-provenance

## Feature

`specifications/workload-measurement-provenance.md` — lossless raw-counter
workload capture + provenance.

## Test Cases

### TC-WMP-01: Workload collectors emit raw counters, no in-collector delta

**Rule:** WMP-R001, INV-WMP-01

**Given:** the workload collectors (`singlestore_plancache_v1`,
`singlestore_activities_v1`).

**When:** their SQL is inspected.

**Then:** each selects raw cumulative counters and its SQL contains no `LAG`,
no delta/self-join, and no lossy `GROUP BY` aggregation.

**Expected Result:** Pass when workload capture is raw and lossless.

---

### TC-WMP-02: Snapshot carries provenance

**Rule:** WMP-R002, WMP-R003, INV-WMP-02

**Given:** a snapshot assembled from collector results via `BuildSnapshot(info,
"singlestore", version, results)`.

**When:** metadata + query_results are produced.

**Then:** metadata carries `engine=singlestore`, a non-empty `engine_version`,
`collected_at`, and `target`; each query_results line carries `collector_id`
and `category`.

**Expected Result:** Pass when every result is attributable to engine, time,
and collector.
