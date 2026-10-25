# Acceptance Tests: mysql-grants

## Feature

`specifications/mysql-grants.md` — the least-privilege MySQL monitoring role and
its cross-user exposure characterization (#545). A configuration-decision spec:
the grant model is exercised by the `integration-mysql` live qualification; the
sensitive-exposure surface is enforced by the catalog redaction guard.

## Test Cases

### TC-MYG-01: the full catalog collects under SELECT+PROCESS (live)

**Rule:** MYG-R001, MYG-R004

**Given:** a MySQL 8.4 server and a `signals_ro` role granted `SELECT, PROCESS ON
*.*`. **When:** the normal daemon collects. **Then:** all 16 collectors persist
`status=success` (replication/Group-Replication absence = legitimate empty).
**Expected Result:** Confirmed live by the `integration-mysql` qualification
(`TestMySQLLiveCadenceQualification`).

---

### TC-MYG-02: cross-user SQL / locked data is redacted by default (invariant)

**Rule:** MYG-R003

**Given:** `mysqlCatalog()`. **When:** each collector that selects a cross-user
SQL or locked-data column (`INFO`, `trx_query`, `DIGEST_TEXT`, `LOCK_DATA`) is
inspected. **Then:** it is `HighSensitivity` with that column in
`SensitiveColumns` (redacted by default). **Expected Result:** Pass —
`TestMySQLCatalogNoUnredactedSensitiveColumn` (MYC-R004/R008) enforces this
catalog-wide. Redaction is defense in depth; the `PROCESS` grant is the primary
boundary.

---

### TC-MYG-03: no write/admin grant is required (invariant)

**Rule:** MYG-R001

**Given:** the documented role. **When:** reviewed. **Then:** it carries only
`SELECT` + `PROCESS` (both read-only monitoring privileges); no write, DDL, or
superuser privilege. **Expected Result:** Documented; the collectors are
read-only SELECTs (`TestMySQLCatalogReadOnly`, INV-MYC-01).
