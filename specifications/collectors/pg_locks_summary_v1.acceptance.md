# Acceptance Tests: pg_locks_summary_v1

## Feature

`specifications/collectors/pg_locks_summary_v1.md`

## Test Cases

### TC-LOCKS-01: Aggregated by (locktype, mode, granted)

**Rule:** Normal + privacy invariant

**Scenario:** Target has several locks held and at least one
backend waiting.

**Given:**
- Concurrent sessions where one holds `AccessExclusiveLock` on a
  relation and another waits on `AccessShareLock` for the same
  relation.

**When:**
- A collection cycle runs.

**Then:**
- At least two rows: one `granted = true` and one
  `granted = false`.
- The `granted = false` row has `max_wait_seconds > 0`.
- No row carries per-relation OIDs, transaction IDs, or tuple
  identifiers.

**Expected Result:** Pass when the aggregation holds and no
per-object identifiers leak.

---

### TC-LOCKS-02: No contention → no waiting rows

**Rule:** Boundary

**Scenario:** Target has only normal traffic; no waiters.

**Given:**
- Target with no blocked sessions.

**When:**
- A collection cycle runs.

**Then:**
- All emitted rows have `granted = true`.
- `max_wait_seconds` is NULL everywhere.

**Expected Result:** Pass when no waiting rows appear.

---

### TC-LOCKS-03: Collector's own backend excluded

**Rule:** Scope filter

**Scenario:** The collector's session acquires locks while querying
pg_locks; it must not self-count.

**Given:**
- Any target.

**When:**
- A collection cycle runs.

**Then:**
- No row includes the collector's `pg_backend_pid()`.
- `distinct_pids` for each row excludes it.

**Expected Result:** Pass when the collector PID is absent from
contributions.

---

### TC-LOCKS-04: Sort order deterministic

**Rule:** Invariant

**Scenario:** Two consecutive cycles against a stable workload
produce rows in the same order.

**Given:**
- Stable concurrent workload.

**When:**
- Two cycles run back to back.

**Then:**
- Row order is identical: `granted DESC, locktype, mode`.

**Expected Result:** Pass when ordering matches.
