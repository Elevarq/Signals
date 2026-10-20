# Acceptance Tests: singlestore-snapshot-assembly

## Feature

`specifications/singlestore-snapshot-assembly.md` — assemble SingleStore
collector results into an engine=singlestore snapshot artifact.

## Test Cases

### TC-SSA-01: metadata declares SingleStore (normal)

**Rule:** SSA-R001, INV-SSA-01

**Given:** two collector `Result`s and `BuildSnapshot(info, "singlestore", "8.5.22", results)`.

**When:** `MetadataJSON()` is marshaled.

**Then:** the JSON contains `"engine":"singlestore"` and `"engine_version":"8.5.22"`;
`schema_version` is `signals-snapshot.v1`; `pg_version` is empty/absent.

**Expected Result:** Pass when the snapshot declares the engine and omits pg_version.

---

### TC-SSA-02: one NDJSON line per collector in order (boundary)

**Rule:** SSA-R002

**Given:** a snapshot built from 3 collector results.

**When:** `QueryResultsNDJSON()` is produced.

**Then:** it has exactly 3 non-empty lines; each parses to an object with
`collector_id`, `category`, and `rows`; the `collector_id` order matches the
input order.

**Expected Result:** Pass when every collector appears once, in order.

---

### TC-SSA-03: ZIP contains the files with a verifiable manifest (invariant)

**Rule:** SSA-R003, INV-SSA-02

**Given:** a built snapshot.

**When:** `WriteZIP(buf)` runs and the archive is read back.

**Then:** it contains `metadata.json`, `query_results.ndjson`, and
`manifest.json`; for each data file, the manifest's SHA-256 equals the SHA-256
of the bytes stored in the ZIP.

**Expected Result:** Pass when the archive is complete and integrity-verifiable.

---

### TC-SSA-04: PostgreSQL identity leaves engine empty (compat boundary)

**Rule:** SSA-R001

**Given:** `BuildSnapshot(info, "postgresql", "18.2", results)`.

**When:** `MetadataJSON()` is marshaled.

**Then:** it contains **no** `engine` key (SetEngineIdentity leaves PostgreSQL
empty) — the assembler does not force an engine onto PostgreSQL.

**Expected Result:** Pass when PostgreSQL identity stays v1-shaped.
