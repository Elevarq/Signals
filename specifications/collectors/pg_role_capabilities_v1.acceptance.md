# Acceptance Tests: pg_role_capabilities_v1

## Feature

`specifications/collectors/pg_role_capabilities_v1.md`

## Test Cases

### TC-ROLE-01: Monitoring role with pg_monitor emits expected capability matrix

**Rule:** Normal

**Scenario:** The standard monitoring role is a member of
`pg_monitor`.

**Given:**
- Role is a `pg_monitor` member; not a superuser.

**When:**
- A collection cycle runs.

**Then:**
- `is_pg_monitor = true`.
- `is_pg_read_all_stats = true` (pg_monitor includes this).
- `is_pg_read_all_settings = true` (pg_monitor includes this).
- `is_superuser = false`.
- `can_read_all_stats = true`.
- `default_transaction_read_only = 'on'` (session-level).

**Expected Result:** Pass when the matrix matches the expected
membership.

---

### TC-ROLE-02: Superuser path

**Rule:** Effective-capability derivation

**Scenario:** Role is a superuser.

**Given:**
- Role with `rolsuper = true`.

**When:**
- A collection cycle runs.

**Then:**
- `is_superuser = true`.
- `can_read_all_stats = true` and `can_read_all_settings = true`
  regardless of pg_monitor membership.

**Expected Result:** Pass when superuser implies full capability.

---

### TC-ROLE-03: Missing built-in role on older PG version recorded in probe_errors

**Rule:** FC-01

**Scenario:** Target is PG 10.4; `pg_read_all_settings` did not
exist in that point release.

**Given:**
- Target PG 10.4.

**When:**
- A collection cycle runs.

**Then:**
- `is_pg_read_all_settings = false`.
- The JSON side-channel field `probe_errors` (or equivalent)
  records "role pg_read_all_settings not found on this PG version".

**Expected Result:** Pass when the absence is recorded distinctly
from non-membership.

---

### TC-ROLE-04: Exactly one row per collection cycle

**Rule:** Invariant

**Scenario:** The collector emits exactly one row per sample.

**Given:**
- Any target.

**When:**
- A collection cycle runs.

**Then:**
- `QueryRun.RowCount = 1`.

**Expected Result:** Pass when the row count is 1.
