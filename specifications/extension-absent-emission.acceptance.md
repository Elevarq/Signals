# Acceptance Tests: extension-absent-emission

## Feature

`specifications/extension-absent-emission.md` — uniform reporting
of absent extensions, unsupported PG versions, and disabled
collectors via `collector_status.json`.

## Test Cases

### TC-EXTABS-01: Missing extension recorded in collector_status.json

**Rule:** EA-R001, INV-EA-01

**Scenario:** An extension-gated collector runs against a target
that does not have the required extension installed.

**Given:**
- A collector with `RequiresExtension: pg_stat_statements`.
- Target PostgreSQL where `pg_stat_statements` is NOT in
  `pg_extension`.

**When:**
- A collection cycle runs.

**Then:**
- `collector_status.json` carries exactly one entry for this
  collector with: `attempted = false`, `status = "skipped"`,
  `reason = "extension_missing"`, `row_count = 0`.
- `query_results.ndjson` contains zero rows attributed to this
  collector.
- `QueryRun.Error` for the collector is empty (no execution was
  attempted).

**Expected Result:** Pass when the status entry is present with
the expected shape and the result file carries no rows for this
collector.

---

### TC-EXTABS-02: Extension present yields a success status entry

**Rule:** INV-EA-01

**Scenario:** The extension IS installed; the collector runs and
its query succeeds.

**Given:**
- A collector with `RequiresExtension: pg_stat_statements`.
- Target with the extension installed and rows present.

**When:**
- A collection cycle runs.

**Then:**
- `collector_status.json` carries one entry with
  `status = "success"` and a non-zero `row_count`.
- Real rows appear in `query_results.ndjson`.
- No entry in either file has `status = "skipped"` for this
  collector.

**Expected Result:** Pass when the success entry and real rows are
both present.

---

### TC-EXTABS-03: Unsupported PG major recorded in collector_status.json

**Rule:** EA-R001

**Scenario:** A version-gated collector runs against a target
whose PG major version is below the collector's minimum (e.g.
`pg_stat_io_v1` against PG ≤ 15).

**Given:**
- A collector declaring a `MinPGVersion` higher than the target's
  major version.

**When:**
- A collection cycle runs.

**Then:**
- `collector_status.json` carries one entry with
  `status = "skipped"` and `reason = "version_unsupported"`.
- `query_results.ndjson` contains zero rows attributed to this
  collector.

**Expected Result:** Pass when the version_unsupported entry is
present and no rows leak into the result file.

---

### TC-EXTABS-04: Disabled-by-config recorded in collector_status.json

**Rule:** EA-R001

**Scenario:** A collector explicitly disabled via configuration is
not run.

**Given:**
- A collector configured with `enabled = false` (or the equivalent
  per-collector toggle).

**When:**
- A collection cycle runs.

**Then:**
- `collector_status.json` carries one entry with
  `status = "skipped"` and `reason = "config_disabled"`.

**Expected Result:** Pass when the disabled entry is present.

---

### TC-EXTABS-05: Runtime failure recorded as failed (not skipped)

**Rule:** EA-R001 (failed-status branch)

**Scenario:** Preconditions hold (extension present, version
supported, config enabled) but the query fails at execution time
(e.g. permission denied mid-query).

**Given:**
- The collector's preconditions all pass.
- The query returns a permission error.

**When:**
- A collection cycle runs.

**Then:**
- `collector_status.json` carries one entry with
  `attempted = true`, `status = "failed"`, and
  `reason = "permission_denied"` (or the appropriate runtime
  reason).
- `QueryRun.Error` carries the underlying PG error string.

**Expected Result:** Pass when the failed entry is present with
attempted=true and the underlying error is recorded.

---

## Coverage Notes

Covers EA-R001 (canonical channel), EA-R002 (status file
ubiquity), and INV-EA-01 (one entry per registered collector;
`query_results.ndjson` carries only real query rows). EA-R003
(collector-spec cross-reference) is covered by per-collector
specs themselves; EA-R004 (analyzer contract) is covered by tests
in the Arq Analyzer repo against the `EvidenceCompleteness` model.
