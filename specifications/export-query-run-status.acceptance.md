# Acceptance Tests: Query-Run Status in Snapshot Export

## Feature

`specifications/export-query-run-status.md`

## Test Cases

### TC-EQRS-01: Success run exports status/reason

**Rule:** R118 (normal)

**Scenario:** A successful collector run is packaged into an export
ZIP.

**Given:**
- A persisted run with `status = "success"`, `reason = ""`, and a
  result payload.

**When:**
- The export ZIP is built and `query_runs.ndjson` is read.

**Then:**
- The row for that run carries `status == "success"` and
  `reason == ""`, alongside all nine pre-existing fields.

**Expected Result:** Pass.
(`TestExportQueryRunsCarryStatusAndReason`)

---

### TC-EQRS-02: Owner-only skip is distinguishable without SQLSTATE parsing

**Rule:** R118 (normal — the #250 bug)

**Scenario:** An R116 owner-only skip
(`skipped` / `privilege_owner_only`) with the driver error text
recorded is exported.

**Given:**
- A persisted run with `status = "skipped"`,
  `reason = "privilege_owner_only"`, and
  `error = "ERROR: permission denied for table pg_statistic_ext_data (SQLSTATE 42501)"`.

**When:**
- The export ZIP is built and `query_runs.ndjson` is read.

**Then:**
- The row carries `status == "skipped"` and
  `reason == "privilege_owner_only"`.
- The `error` text is preserved verbatim (INV-02).

**Expected Result:** Pass.
(`TestExportQueryRunsOwnerOnlySkipDistinguishable`)

---

### TC-EQRS-03: Genuine failure exports as failed

**Rule:** R118 (boundary)

**Scenario:** A real permission failure on a non-owner-only
collector is exported next to the TC-EQRS-02 skip.

**Given:**
- A persisted run with `status = "failed"`,
  `reason = "permission_denied"`, and a non-empty `error`.

**When:**
- The export ZIP is built and `query_runs.ndjson` is read.

**Then:**
- The row carries `status == "failed"` and
  `reason == "permission_denied"`.
- Its `status` differs from the owner-only skip row's `status`
  (INV-01): the two are distinguishable from the rows alone.

**Expected Result:** Pass.
(`TestExportQueryRunsOwnerOnlySkipDistinguishable`)

---

### TC-EQRS-04: Unexpected persisted status is exported verbatim

**Rule:** FC-01 (invalid)

**Scenario:** A row whose `status` column holds a value outside the
known set (written by a foreign tool or a future daemon) is exported.

**Given:**
- A persisted run whose `status` is `"quarantined"` (not a known
  value).

**When:**
- The export ZIP is built and `query_runs.ndjson` is read.

**Then:**
- The row carries `status == "quarantined"` — emitted verbatim, not
  coerced, not dropped.

**Expected Result:** Pass.
(`TestExportQueryRunsUnknownStatusVerbatim`)
