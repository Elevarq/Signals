# Acceptance Tests: pg_prepared_xacts_v1

## Feature

`specifications/collectors/pg_prepared_xacts_v1.md`

## Test Cases

### TC-2PC-01: Prepared transaction with derived age

**Rule:** Normal

**Scenario:** A prepared transaction exists on the target.

**Given:**
- `max_prepared_transactions > 0`.
- One prepared transaction with a known `gid` and a known
  preparation time.

**When:**
- A collection cycle runs.

**Then:**
- A row for that `gid` is present.
- `age_seconds` matches wall-clock elapsed time since PREPARE
  (within one cadence interval).
- `age_xids` is a non-negative integer.

**Expected Result:** Pass when the row appears with accurate derived
fields.

---

### TC-2PC-02: max_prepared_transactions = 0 → empty result

**Rule:** FC-01

**Scenario:** The GUC is at its default of 0; no 2PC is possible.

**Given:**
- `max_prepared_transactions = 0`.

**When:**
- A collection cycle runs.

**Then:**
- Result = `[]`.
- No error.

**Expected Result:** Pass when the empty result is emitted.

---

### TC-2PC-03: Deterministic ordering by prepared time

**Rule:** Invariant

**Scenario:** Two prepared transactions exist; the older appears
first.

**Given:**
- Two prepared transactions, `gid_a` prepared before `gid_b`.

**When:**
- A collection cycle runs.

**Then:**
- `gid_a` appears before `gid_b` in the output.

**Expected Result:** Pass when the ordering matches.

---

### TC-2PC-04: Permission denied handled

**Rule:** FC-02

**Scenario:** Rare but possible — `pg_prepared_xacts` rejected for
the role.

**Given:**
- A target returning 42501 for this view.

**When:**
- A collection cycle runs.

**Then:**
- `QueryRun.Error` populated.
- Cycle continues.

**Expected Result:** Pass when the error is recorded without
aborting the cycle.
