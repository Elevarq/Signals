# Acceptance Tests: mariadb-support

## Feature

`specifications/mariadb-support.md` — the operator-facing MariaDB support page
(#638). It is an index that cites the authoritative specs; its correctness is that
it agrees with them (umbrella-owns-decisions).

## Test Cases

### TC-MDS-01: MariaDB support page agrees with the version matrix (consistency)

**Rule:** MDS-R001, MDS-R002

**Given:** the MariaDB version policy. **When:** `version-support-policy.md` and
`mariadb-support.md` are compared. **Then:** both state MariaDB 10.11 + 11.4 +
11.8 + 12.3 LTS as the qualified (live CI) lines, other maintained releases
best-effort, 10.6-and-earlier unsupported — no independent or conflicting claim,
and both state MariaDB qualification is independent of MySQL. **Expected Result:**
The shared matrix is the single source; the `integration-mariadb` matrix covering
the four LTS lines is enforced by `TestMariaDBVersionMatrixQualifiesLTS` (TC-VSP-04).
Agreement is by citation.

---

### TC-MDS-02: identity is a positive MariaDB signal, never inferred (behavioral)

**Rule:** MDS-R001

**Given:** a MySQL-wire target that is MariaDB. **When:** identified. **Then:**
`engine=mariadb` is resolved from the version probe, distinct from MySQL /
SingleStore, never inferred from port/protocol and never treated as MySQL.
**Expected Result:** Covered by the engineid resolve tests
(`singlestore-engine-identity.md`); this page documents, not re-tests, that
behavior.
