# Acceptance Tests: workload-measurement-model

## Feature

`specifications/workload-measurement-model.md` — the engine-neutral workload
identity / units / provenance model. These cases cover the ENFORCED producer
layer (typed identity present + sample-SQL redaction); the deferred normalized
envelope (WMM-R004) and the consumer never-equate obligation (WMM-R002) are
model/consumer items tracked with #530, not producer tests.

## Test Cases

### TC-WMM-01: every engine's workload collector carries its typed identity (normal)

**Rule:** WMM-R001

**Given:** the workload collector for each engine (PostgreSQL
`pg_stat_statements_v1`, MySQL `mysql_statement_digests_v1`, MariaDB
`mariadb_statement_digests_v1`, SingleStore `singlestore_plancache_v1`,
ClickHouse `clickhouse_query_log_v1`). **When:** its SQL is inspected. **Then:**
it selects the documented identity column (`queryid`, `DIGEST`, `DIGEST`,
`PLAN_ID`, `normalized_query_hash`). **Expected:** Pass when every engine carries
a typed identity.

---

### TC-WMM-02: sample SQL is redacted; the typed identity is retained (invariant)

**Rule:** WMM-R003, INV-WMM-02

**Given:** each engine's workload collector. **When:** redaction is applied.
**Then:** the sample-SQL column is HighSensitivity + in `SensitiveColumns`
(MySQL/MariaDB `DIGEST_TEXT`, SingleStore `QUERY_TEXT`, ClickHouse `query`;
PostgreSQL via `RedactStatementSecrets`), and the non-sensitive typed identity is
NOT redacted. **Expected:** Pass when sample SQL is redacted and the identity
survives.

---

### TC-WMM-03: identity kinds are self-describing and not cross-equated (model — scope)

**Rule:** WMM-R002, INV-WMM-01

**Given:** the per-engine identity kinds. **When:** compared. **Then:** MySQL and
MariaDB both use kind `digest` but are distinguished by `scope` (engine); no
producer path emits a cross-engine-equated identity. **Expected:** Documented +
guarded at the producer boundary (each engine's identity is scoped to its own
catalog); the consumer never-equate guarantee is the Analyzer obligation (#530).

---

### TC-WMM-04: normalized envelope is deferred, native evidence retained (boundary)

**Rule:** WMM-R004, INV-WMM-03

**Given:** the current export. **When:** a workload collector is exported.
**Then:** native engine payload is present; the normalized envelope
(source/units/scope/observed window) is NOT yet emitted (deferred, gated on #530).
**Expected:** Documented — native evidence is never replaced; the envelope lands
additively once the Analyzer contract is agreed.
