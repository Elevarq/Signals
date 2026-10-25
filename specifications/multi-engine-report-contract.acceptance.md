# Acceptance Tests: multi-engine-report-contract

## Feature

`specifications/multi-engine-report-contract.md` — the producer-side
`signals-snapshot.v1` export contract, uniform across the five 2.0 engines.

These cases run over the REAL `export.Builder.WriteTo` path (deterministic,
no live DB — contract-SHAPE conformance, not producer evidence; producer
evidence is each engine's live qualification). A representative engine-tagged
snapshot is seeded per engine and exported; the resulting ZIP is asserted
against the contract.

## Test Cases

### TC-MERC-01: every engine's export has all seven members (normal)

**Rule:** MERC-R001

**Given:** a seeded snapshot for each engine in {postgresql, singlestore, mysql,
mariadb, clickhouse}. **When:** exported via `WriteTo`. **Then:** the ZIP contains
`metadata.json`, `collector_status.json`, `snapshots.ndjson`, `query_catalog.json`,
`query_runs.ndjson`, `query_results.ndjson`, `manifest.json`. **Expected:** Pass
when all seven are present for every engine.

---

### TC-MERC-02: schema_version is unchanged for every engine (invariant)

**Rule:** MERC-R002, INV-MERC-01

**Given:** each engine's export. **When:** `metadata.json` is read. **Then:**
`schema_version == "signals-snapshot.v1"`. **Expected:** Pass for all five.

---

### TC-MERC-03: engine identity is engine-correct and byte-compatible for PostgreSQL (normal/boundary)

**Rule:** MERC-R003

**Given:** each engine's export (PostgreSQL seeded with an empty snapshot engine,
as the real PostgreSQL collector persists). **When:** `metadata.json` is read.
**Then:** `engine` is always present and engine-correct (empty/legacy →
`postgresql`); a non-PostgreSQL export additionally carries a non-empty
`engine_version`; a PostgreSQL export does NOT carry `engine_version` (byte-compat
— its version stays in `pg_version` within `snapshots.ndjson`/`query_runs.ndjson`).
**Expected:** Pass when identity is engine-correct and PostgreSQL gains no new
metadata field.

---

### TC-MERC-04: no cross-engine collector leak (invariant)

**Rule:** MERC-R004, INV-MERC-02

**Given:** each engine's export. **When:** `collector_status.json` and
`query_catalog.json` collector ids are enumerated. **Then:** every id belongs to
that engine's catalog; no foreign-engine collector appears. **Expected:** Pass
when each export is engine-pure.

---

### TC-MERC-05: run status is within the enum and success carries payload (normal)

**Rule:** MERC-R005

**Given:** each engine's export. **When:** `query_runs.ndjson` is read. **Then:**
every row's `status` ∈ {success, failed, skipped}; a `success` row has a
`row_count` and a joinable `query_results` payload. **Expected:** Pass when the
status enum and success-payload join hold.

---

### TC-MERC-06: manifest covers every member (invariant)

**Rule:** MERC-R006

**Given:** each engine's export. **When:** `manifest.json` is read. **Then:**
`algorithm == "sha256"` and every other ZIP member appears with a non-empty
`sha256`, and the manifest lists no absent member. **Expected:** Pass when the
manifest and members agree for every engine.

---

### TC-MERC-07: consumer forward-compatibility sign-off (OPEN — Analyzer)

**Rule:** MERC-R008

**Given:** the five-engine conformance corpus (real export ZIPs) + the retained
1.x PostgreSQL goldens. **When:** the Analyzer ingest path consumes them.
**Then:** historical PostgreSQL ingests identically; a new `engine` value and
additive fields are forward-ignored (ignore-with-notice), not hard-failed.
**Expected Result:** Confirmed by the Analyzer owners — the OPEN acceptance of
#530; not closable on the Signals side.
