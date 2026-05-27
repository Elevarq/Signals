# Acceptance Tests: pg_proc_config_v1

## Feature

`specifications/collectors/pg_proc_config_v1.md`

## Test Cases

### TC-PROCCF-01: Functions with proconfig are emitted

**Rule:** Normal

**Scenario:** A function has been altered with `ALTER FUNCTION ...
SET random_page_cost = 1.1`.

**Given:**
- User function `slow_report()` with
  `ALTER FUNCTION slow_report() SET random_page_cost = 1.1` applied.

**When:**
- A collection cycle runs.

**Then:**
- Row for `slow_report` is present.
- `proconfig` includes `'random_page_cost=1.1'`.
- `proargtypes_oids` is populated for overload disambiguation.

**Expected Result:** Pass when the row appears with correct
proconfig contents.

---

### TC-PROCCF-02: Functions with no proconfig are excluded

**Rule:** Scope filter

**Scenario:** Plain functions without SET overrides do not appear.

**Given:**
- A user function with `proconfig IS NULL`.

**When:**
- A collection cycle runs.

**Then:**
- The function does not appear in the output.

**Expected Result:** Pass when only proconfig-bearing rows are
present.

---

### TC-PROCCF-03: proconfig preserved as raw text[]

**Rule:** Invariant

**Scenario:** Multiple SET clauses on the same function are each
preserved as distinct array elements.

**Given:**
- Function with `ALTER FUNCTION ... SET random_page_cost = 1.1
  SET work_mem = '64MB'`.

**When:**
- A collection cycle runs.

**Then:**
- `proconfig` is an array containing both
  `'random_page_cost=1.1'` and `'work_mem=64MB'`.
- No parsing/splitting is done at collection time.

**Expected Result:** Pass when the raw array is preserved.

---

### TC-PROCCF-04: pg_catalog functions excluded

**Rule:** Scope filter

**Scenario:** Built-in functions with `proconfig` set (rare) are
not emitted.

**Given:**
- Target with standard catalog.

**When:**
- A collection cycle runs.

**Then:**
- No row with `schemaname = 'pg_catalog'` or
  `schemaname = 'information_schema'`.

**Expected Result:** Pass when the filter holds.
