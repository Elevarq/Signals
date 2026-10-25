# Acceptance Tests: mysql-support

## Feature

`specifications/mysql-support.md` — the operator-facing MySQL support page (#547).
It is an index that cites the authoritative specs; its correctness is that it
agrees with them (umbrella-owns-decisions).

## Test Cases

### TC-MYS-01: MySQL support page agrees with the version matrix (consistency)

**Rule:** MYS-R001, MYS-R002

**Given:** the MySQL version policy. **When:** `version-support-policy.md` and
`mysql-support.md` are compared. **Then:** both state MySQL 8.0 + 8.4 as the
qualified (live CI) lines, forward releases best-effort, <8.0 unsupported — no
independent or conflicting claim. **Expected Result:** The shared matrix is the
single source; the `integration-mysql` matrix covering 8.0 + 8.4 is enforced by
`TestMySQLVersionMatrixQualifies80And84` (TC-VSP-01). Agreement is by citation.

---

### TC-MYS-02: identity is a positive MySQL signal, never inferred (behavioral)

**Rule:** MYS-R001

**Given:** a MySQL-wire target. **When:** identified. **Then:** `engine=mysql` is
resolved from the version probe, distinct from MariaDB / SingleStore, never
inferred from port/protocol. **Expected Result:** Covered by the engineid resolve
tests (`singlestore-engine-identity.md`); this page documents, not re-tests, that
behavior.
