# Acceptance Tests: pg_stat_io_v1

## Feature

`specifications/collectors/pg_stat_io_v1.md`

## Test Cases

### TC-STATIO-01: Normal emission on PG16+

**Rule:** Normal behavior

**Scenario:** Collector runs against PG16+ target with non-zero I/O
history.

**Given:**
- Target PG major version ≥ 16.
- Monitoring role has `pg_monitor`.
- Workload has executed since the last `stats_reset`.

**When:**
- A collection cycle runs.

**Then:**
- At least one row is emitted.
- Every row has all 18 declared columns present (may be NULL for
  kernel-never-executed tuples, but columns are in the schema).
- Rows are ordered `backend_type, object, context`.
- `op_bytes` is populated (non-NULL) for every row.

**Expected Result:** Pass when shape and ordering match.

---

### TC-STATIO-02: Filtered out on PG < 16

**Rule:** FC-01

**Scenario:** Target is PG 15; the collector is not eligible.

**Given:**
- Target PG major version = 15.

**When:**
- The pgqueries filter evaluates eligibility.

**Then:**
- This collector is excluded from the eligible set.
- `collector_status.json` carries one entry with
  `status = "skipped"` and `reason = "version_unsupported"` per
  `specifications/extension-absent-emission.md` (EA-R001),
  distinguishable from extension-absent via the completeness
  model.

**Expected Result:** Pass when the collector_status entry is
present and the collector is absent from the run manifest.

---

### TC-STATIO-03: Permission denied is captured

**Rule:** FC-02

**Scenario:** Monitoring role lacks `pg_monitor`.

**Given:**
- Target PG ≥ 16.
- Role lacks `pg_monitor` membership and cannot read `pg_stat_io`.

**When:**
- A collection cycle runs.

**Then:**
- The query fails with SQLSTATE 42501.
- `QueryRun.Error` is populated.
- The transaction is rolled back to its savepoint; subsequent
  collectors in the cycle continue to run.

**Expected Result:** Pass when the error is recorded and the cycle
continues.

---

### TC-STATIO-04: Cumulative semantics — no server-side delta

**Rule:** DS-R002 (via cross-cutter)

**Scenario:** The collector's SQL does not perform inter-sample
subtraction.

**Given:**
- The registered SQL for `pg_stat_io_v1`.

**When:**
- Static inspection of the SQL.

**Then:**
- SQL contains no `LAG()`, no `WITH RECURSIVE` over a previous
  sample, no self-join on a stored prior-sample table.
- Emits raw cumulative values as returned by `pg_stat_io`.

**Expected Result:** Pass when the SQL passes the static check.
