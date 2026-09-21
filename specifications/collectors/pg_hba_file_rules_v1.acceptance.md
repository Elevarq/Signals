# Acceptance Tests: pg_hba_file_rules_v1

## Feature

`specifications/collectors/pg_hba_file_rules_v1.md`

## Test Cases

### TC-HBA-01: Normal — rules emitted against a real cluster

**Rule:** Happy path (Output columns, Scope filter)

**Scenario:** A monitoring role granted `SELECT` on `pg_hba_file_rules`
and `EXECUTE` on `pg_hba_file_rules()` collects against a live server
whose `pg_hba.conf` has the default local + host rules.

**Given:**
- A reachable target; the collecting role has both grants from the spec's
  "Required grants" section (the CI harness applies them).

**When:**
- The `pg_hba_file_rules_v1` collector runs during a cycle and the
  snapshot is exported.

**Then:**
- The collector run is `status=success`.
- The `query_results` payload contains one object per HBA rule.
- Every spec-declared column is present on each object (`rule_number`,
  `file_name`, `line_number`, `type`, `database`, `user_name`, `address`,
  `netmask`, `auth_method`, `options`, `error`).
- `database`, `user_name`, and `options` decode as JSON arrays.

*Covered by the output-contract integration harness
(`TestIntegration_CollectorOutputContractAgainstRealPG`), which runs every
registered collector against ephemeral PostgreSQL 14–18.*

---

### TC-HBA-02: Boundary — version columns stubbed on PG ≤ 15, real on PG ≥ 16

**Rule:** Invariant (stable column set; #210 stub pattern)

**Scenario:** The same collector runs on PG15 and on PG16+.

**Given:**
- The default (base) SQL and the registered version overrides.

**When:**
- The effective SQL is resolved for each major.

**Then:**
- The column set is identical across majors.
- On PG ≤ 15 `rule_number` and `file_name` are typed NULL stubs (no
  override registered for PG14/PG15).
- On PG 16, 17, 18 an override supplies the real `rule_number` and
  `file_name` columns.

*Covered by `TestPgHbaFileRulesVersionColumns`.*

---

### TC-HBA-03: Invalid — collector shape guards

**Rule:** Registration contract

**Scenario:** The collector is registered with the wrong category,
sensitivity, or degrade flag.

**Given:**
- The registered `pg_hba_file_rules_v1` QueryDef.

**When:**
- Its fields are inspected.

**Then:**
- Category is `server`, `ResultKind` is rowset, `MinPGVersion` is 10.
- `PrivilegedViewDegrade` is true; `OwnerOnlyDegrade` and
  `HighSensitivity` are false.
- `PrivilegedViewDegrade` is confined to exactly this collector across the
  whole registry.

*Covered by `TestPgHbaFileRulesRegistered` and
`TestOnlyPgHbaFileRulesIsPrivilegedViewDegrade`.*

---

### TC-HBA-04: Failure — permission denied degrades to skipped, not failed

**Rule:** FC-01 (PrivilegedViewDegrade)

**Scenario:** The collecting role lacks the `SELECT`/`EXECUTE` grants (the
default `pg_monitor` deployment), so `pg_hba_file_rules` raises SQLSTATE
42501.

**Given:**
- A `pg_hba_file_rules_v1` collector run that returns a permission-denied
  error.

**When:**
- The failure is classified.

**Then:**
- The run is recorded `status=skipped, reason=privilege_restricted`.
- The reason differs from `budget_exhausted`, so the cycle is not marked
  partial; the run reconstructs as a skip (`Attempted=false`).
- A non-permission error on the same collector stays `status=failed`.

*Covered by `TestClassifyQueryFailurePrivilegedViewPermissionDeniedDegradesToSkipped`,
`TestClassifyQueryFailurePrivilegedViewNonPermissionStaysFailed`, and
`TestPrivilegeRestrictedIsNotBudgetExhausted`.*
