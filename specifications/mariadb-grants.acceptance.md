# Acceptance Tests: mariadb-grants

## Feature

`specifications/mariadb-grants.md` — the least-privilege MariaDB monitoring role
and its cross-user exposure characterization (#626). A configuration-decision
spec: the grant model is exercised by the `integration-mariadb` live
qualification; the sensitive-exposure surface is enforced by the catalog
redaction guard.

## Test Cases

### TC-MDG-01: the full catalog collects under SELECT+PROCESS (live)

**Rule:** MDG-R001, MDG-R004

**Given:** a MariaDB 11.4 server and a `signals_ro` role granted `SELECT, PROCESS
ON *.*`. **When:** the normal daemon collects. **Then:** all 15 collectors persist
`status=success` (perf_schema-off / standalone / Galera-not-connected =
legitimate empty). **Expected Result:** Confirmed live by the
`integration-mariadb` qualification (`TestMariaDBLiveCadenceQualification`).

---

### TC-MDG-02: cross-user SQL / locked data is redacted by default (invariant)

**Rule:** MDG-R003

**Given:** `mariadbCatalog()`. **When:** each collector that selects a cross-user
SQL or locked-data column (`INFO`, `trx_query`, `lock_data`, `DIGEST_TEXT`) is
inspected. **Then:** it is `HighSensitivity` with that column in
`SensitiveColumns`. **Expected Result:** Pass —
`TestMariaDBCatalogNoUnredactedSensitiveColumn` (MDC-R004/R008) enforces this
catalog-wide. Redaction is defense in depth; the `PROCESS` grant is the primary
boundary.

---

### TC-MDG-03: no write/admin grant is required (invariant)

**Rule:** MDG-R001

**Given:** the documented role. **When:** reviewed. **Then:** it carries only
`SELECT` + `PROCESS` (read-only monitoring privileges); no write, DDL, or
superuser privilege. **Expected Result:** Documented; the collectors are
read-only SELECTs (`TestMariaDBCatalogReadOnly`, INV-MDC-01).
