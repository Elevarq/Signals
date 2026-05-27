# Acceptance Tests: pg_class_storage_v1

## Feature

`specifications/collectors/pg_class_storage_v1.md`

## Test Cases

### TC-CLSTG-01: TOAST accounting populated for TOAST-bearing tables

**Rule:** Normal

**Scenario:** Collector emits main, TOAST, and index sizes
separately for a table with TOAST.

**Given:**
- User table `t_wide` with a `text` column large enough to force
  TOAST creation.

**When:**
- A collection cycle runs.

**Then:**
- The row for `t_wide` has `has_toast = true`, a non-zero
  `reltoastrelid`, a non-NULL `toast_pages` and `toast_bytes`,
  and `total_bytes = main_bytes + toast_bytes + indexes_bytes`
  (within expected rounding).

**Expected Result:** Pass when TOAST accounting holds.

---

### TC-CLSTG-02: TOAST fields NULL when reltoastrelid = 0

**Rule:** Invariant — `has_toast` derivation

**Scenario:** Table with only inline-storage columns has no TOAST
relation.

**Given:**
- User table `t_narrow(id int, flag bool)`.

**When:**
- A collection cycle runs.

**Then:**
- The row for `t_narrow` has `has_toast = false`, `reltoastrelid = 0`,
  `toast_pages = NULL`, `toast_bytes = NULL`.

**Expected Result:** Pass when no-TOAST rows carry NULLs (not zeros).

---

### TC-CLSTG-03: Dropped relation during scan is tolerated

**Rule:** FC-01

**Scenario:** A table is dropped between the `pg_class` scan and
the size-function call.

**Given:**
- A user table existed at the start of the query but was dropped
  mid-flight.

**When:**
- A collection cycle runs.

**Then:**
- The collector does not raise an "relation does not exist" error.
- Either the row is silently skipped, OR the row appears with
  size fields NULL.

**Expected Result:** Pass when the collection completes without error.

---

### TC-CLSTG-04: Collector honors 60s timeout on very large schemas

**Rule:** NFR, FC-02

**Scenario:** Target has tens of thousands of relations and
`pg_total_relation_size()` calls are slow.

**Given:**
- Target with > 10,000 user tables.

**When:**
- A collection cycle runs with the collector's declared 60-second
  timeout.

**Then:**
- Either the query completes within 60s, OR it is cancelled via
  `statement_timeout`, a `QueryRun.Error` is recorded, and the
  cycle proceeds to the next collector.

**Expected Result:** Pass when either completion or clean timeout
is observed.
