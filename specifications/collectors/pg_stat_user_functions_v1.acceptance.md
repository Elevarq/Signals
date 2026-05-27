# Acceptance Tests: pg_stat_user_functions_v1

## Feature

`specifications/collectors/pg_stat_user_functions_v1.md`

## Test Cases

### TC-FUNC-01: track_functions = 'pl' populates the view

**Rule:** Normal

**Scenario:** PL/pgSQL functions are being tracked.

**Given:**
- `track_functions = 'pl'`.
- A PL/pgSQL function has been invoked.

**When:**
- A collection cycle runs.

**Then:**
- At least one row is emitted.
- Rows ordered `total_time DESC, funcid ASC`.

**Expected Result:** Pass when rows appear in the specified order.

---

### TC-FUNC-02: track_functions = 'none' yields empty result, not error

**Rule:** FC-01

**Scenario:** The GUC is set to its default of `'none'`.

**Given:**
- `track_functions = 'none'`.

**When:**
- A collection cycle runs.

**Then:**
- Result is `[]`.
- `QueryRun.RowCount = 0`.
- No error.
- The analyzer-side coverage model, upon observing both this empty
  result AND `pg_settings_v1.track_functions = 'none'`, emits an
  info-level note "function tracking disabled" rather than
  concluding no functions run.

**Expected Result:** Pass when the empty result is emitted without
error.

---

### TC-FUNC-03: pg_catalog functions excluded

**Rule:** Scope filter

**Scenario:** Only user-schema functions appear.

**Given:**
- Target with `track_functions = 'all'` and pg_catalog functions
  invoked.

**When:**
- A collection cycle runs.

**Then:**
- No row with `schemaname = 'pg_catalog'` or
  `schemaname = 'information_schema'`.

**Expected Result:** Pass when the filter holds.

---

### TC-FUNC-04: Cumulative semantics — raw values only

**Rule:** Semantics

**Scenario:** The collector emits raw cumulative time values, not
deltas.

**Given:**
- The registered SQL.

**When:**
- Static inspection.

**Then:**
- No `LAG()`, no subtraction against a stored previous sample.

**Expected Result:** Pass when the SQL passes the static check.
