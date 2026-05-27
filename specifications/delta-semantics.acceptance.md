# Acceptance Tests: delta-semantics

## Feature

`specifications/delta-semantics.md` — cumulative-counter handling.

## Test Cases

### TC-DELTA-01: Raw cumulative value emission

**Rule:** DS-R001, DS-R002

**Scenario:** A collector reading a cumulative view emits raw counter
values at collection time, not deltas.

**Given:**
- A collector declared with `Semantics: cumulative`.
- Target PostgreSQL with stable `pg_stat_database` counters.

**When:**
- Two collection cycles run, 15 minutes apart.

**Then:**
- Both cycles' `query_results.ndjson` rows carry the raw cumulative
  values observed at that moment.
- Neither row contains a pre-computed delta.
- The SQL registered in `pgqueries` for this collector contains no
  `LAG()` window and no self-join on a previous-sample table.

**Expected Result:** Pass when the raw cumulative values are present
and the SQL contains no inter-sample subtraction.

---

### TC-DELTA-02: Reset detection on stats_reset advance

**Rule:** DS-R004

**Scenario:** `stats_reset` moves forward between two samples; the
analyzer treats the delta as invalid.

**Given:**
- Sample T1 with `stats_reset = X` and `blks_read = 1000`.
- Sample T2 with `stats_reset = X + 1m` and `blks_read = 50`.

**When:**
- The analyzer's delta path is invoked across (T1, T2).

**Then:**
- No delta is produced for the (T1, T2) pair.
- A CoverageNote `Severity: info` is emitted indicating that a
  reset was detected.

**Expected Result:** Pass when no delta is emitted and the coverage
note is present.

---

### TC-DELTA-03: Monotonicity with no reset = valid delta

**Rule:** DS-R001, INV-DS-02

**Scenario:** Two consecutive samples with identical `stats_reset`
and increasing counters produce a positive delta.

**Given:**
- Sample T1 with `stats_reset = X`, `blks_read = 1000`.
- Sample T2 with `stats_reset = X`, `blks_read = 1500`.

**When:**
- The analyzer computes the delta.

**Then:**
- Delta = 500.
- Interval = T2 - T1.
- No CoverageNote is emitted.

**Expected Result:** Pass when delta equals 500 and no coverage note
is produced.

---

### TC-DELTA-04: Sample gap > 2× cadence recorded but not dropped

**Rule:** FC-DS-02

**Scenario:** Collection cycles were skipped; the gap is larger
than twice the nominal cadence.

**Given:**
- Collector cadence 15m.
- Sample T1, next successful sample at T1 + 50m (gap 3.3×).

**When:**
- The analyzer computes the delta.

**Then:**
- A delta is still computed.
- The derived-evidence record includes the actual sample interval
  so downstream consumers can weight accordingly.

**Expected Result:** Pass when the delta is produced and the sample
interval is attached.

---

## Coverage Notes

Covers DS-R001, DS-R002, DS-R004, INV-DS-02, FC-DS-01, FC-DS-02.
DS-R003 (spec-declaration requirement) is covered by the
per-collector traceability test — every `Semantics: cumulative`
spec line is asserted to exist for every cumulative collector.
