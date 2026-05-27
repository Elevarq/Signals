# Acceptance Tests: pg_stat_wal_v1

## Feature

`specifications/collectors/pg_stat_wal_v1.md`

## Test Cases

### TC-WAL-01: Single row emitted on PG 14+

**Rule:** Normal

**Scenario:** Target at PG 14 or later with recent WAL activity.

**Given:**
- Target PG major ≥ 14.

**When:**
- A collection cycle runs.

**Then:**
- Exactly one row emitted.
- All 9 declared columns present.

**Expected Result:** Pass when the row count is 1 and the schema
matches.

---

### TC-WAL-02: Filtered out on PG < 14

**Rule:** FC-01

**Scenario:** Target is PG 13.

**Given:**
- Target PG major = 13.

**When:**
- The pgqueries filter evaluates eligibility.

**Then:**
- The collector is excluded from the eligible set.
- `collector_status.json` carries one entry with
  `status = "skipped"` and `reason = "version_unsupported"`.
- Zero rows in `query_results.ndjson` for this collector.

**Expected Result:** Pass when the collector_status entry is
present and the collector is absent from the run manifest.

---

### TC-WAL-03: Counter reset detected via stats_reset

**Rule:** FC-03

**Scenario:** `pg_stat_wal.stats_reset` moves forward between two
samples.

**Given:**
- Sample T1 with `stats_reset = X`, `wal_bytes = 5e9`.
- Sample T2 with `stats_reset = X + 1m`, `wal_bytes = 42`.

**When:**
- The analyzer computes the delta.

**Then:**
- No delta emitted.
- CoverageNote produced.

**Expected Result:** Pass when the reset is handled per
delta-semantics.

---

### TC-WAL-04: Permission denied captured

**Rule:** FC-02

**Scenario:** Role lacks `pg_monitor`.

**Given:**
- Target PG 14+ with restricted role.

**When:**
- A collection cycle runs.

**Then:**
- `QueryRun.Error` populated.
- Cycle continues.

**Expected Result:** Pass when the error is recorded without
aborting the cycle.
