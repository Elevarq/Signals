# Acceptance Tests: Owner-Only Privilege Degradation

## Feature

`specifications/owner_only_privilege_degradation.md`

## Test Cases

### TC-OOPD-01: Owner-only collector + 42501 degrades to skipped

**Rule:** R116 (normal)

**Scenario:** A least-privilege `pg_monitor` role runs an owner-only
collector against `pg_statistic_ext_data`.

**Given:**
- A collector with `OwnerOnlyDegrade = true`.
- A query error carrying SQLSTATE `42501`.

**When:**
- `classifyQueryFailure(true, err)` is called.

**Then:**
- Returns `("skipped", "privilege_owner_only")`.

**Expected Result:** Pass. (`TestClassifyQueryFailureOwnerOnlyPermissionDeniedDegradesToSkipped`)

---

### TC-OOPD-02: Owner-only collector + non-permission error stays failed

**Rule:** R116 (boundary)

**Scenario:** The owner-only collector fails for a reason other than
privilege (timeout, connection reset).

**Given:**
- A collector with `OwnerOnlyDegrade = true`.
- A non-permission error.

**When:**
- `classifyQueryFailure(true, err)` is called.

**Then:**
- `status == "failed"`; `reason != "privilege_owner_only"`.

**Expected Result:** Pass. (`TestClassifyQueryFailureOwnerOnlyNonPermissionStaysFailed`)

---

### TC-OOPD-03: Non-owner collector + 42501 stays a real failure

**Rule:** R116 (invalid — degrade must not leak to other collectors)

**Scenario:** A collector that genuinely needs `pg_monitor` is denied.

**Given:**
- A collector with `OwnerOnlyDegrade = false`.
- A query error carrying SQLSTATE `42501`.

**When:**
- `classifyQueryFailure(false, err)` is called.

**Then:**
- Returns `("failed", "permission_denied")`.

**Expected Result:** Pass. (`TestClassifyQueryFailureNonOwnerPermissionDeniedStaysFailed`)

---

### TC-OOPD-04: Owner-only skip never marks the cycle partial

**Rule:** INV-01 / INV-02 (failure)

**Scenario:** A `privilege_owner_only` run must not be counted as a
failure or a budget-exhausted skip.

**Given:**
- `reasonPrivilegeOwnerOnly` and `reasonBudgetExhausted` constants.
- A persisted run `{Status: skipped, Reason: privilege_owner_only}`.

**When:**
- The two reasons are compared, and `BuildStatusFromRuns` reconstructs
  the run.

**Then:**
- `reasonPrivilegeOwnerOnly != reasonBudgetExhausted`.
- The reconstructed status is `skipped` with `Attempted=false`.

**Expected Result:** Pass. (`TestPrivilegeOwnerOnlyIsNotBudgetExhausted`)

---

### TC-OOPD-05: Advisory deduplicated per (target, collector, kind)

**Rule:** R117

**Scenario:** A persistent owner-only condition must be advised once, not
every poll.

**Given:**
- A collector with an empty dedup set.

**When:**
- `warnOnce(target, query, kind)` is called repeatedly.

**Then:**
- The first call for a key returns true; subsequent identical calls
  return false; a different target, collector, or kind warns
  independently.

**Expected Result:** Pass. (`TestWarnOnceDeduplicatesPerTargetQueryKind`)

---

### TC-OOPD-06: Extended-statistics-data collectors are flagged

**Rule:** INV-03

**Given:**
- The compiled collector registry.

**When:**
- `pgqueries.ByID("pg_statistic_ext_data_v1")` and
  `pgqueries.ByID("pg_statistic_ext_data_mcv_v1")` are inspected.

**Then:**
- Both return `OwnerOnlyDegrade = true`.

**Expected Result:** Pass. (`TestExtStatDataCollectorsAreOwnerOnlyDegrade`)

---

### TC-OOPD-07: Ordinary collector is not flagged

**Rule:** INV-03 (boundary)

**Given:**
- The compiled collector registry.

**When:**
- `pgqueries.ByID("pg_stat_user_tables_v1")` is inspected.

**Then:**
- `OwnerOnlyDegrade == false`.

**Expected Result:** Pass. (`TestOrdinaryCollectorIsNotOwnerOnlyDegrade`)

---

### TC-OOPD-08: Flag confined to exactly the two owner-only collectors

**Rule:** INV-03 (invariant guard)

**Given:**
- The full collector registry (`pgqueries.All()`).

**When:**
- Every collector's `OwnerOnlyDegrade` flag is enumerated.

**Then:**
- Exactly `{pg_statistic_ext_data_mcv_v1, pg_statistic_ext_data_v1}`
  carry the flag.

**Expected Result:** Pass. (`TestOnlyExtStatDataCollectorsAreOwnerOnlyDegrade`)
