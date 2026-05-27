# Acceptance Tests: pg_tablespaces_v1

## Feature

`specifications/collectors/pg_tablespaces_v1.md`

## Test Cases

### TC-TS-01: Default-only configuration (hyperscaler case)

**Rule:** Normal — hyperscaler expectation

**Scenario:** Target is on RDS / Cloud SQL / Azure Flex / AlloyDB;
only `pg_default` and `pg_global` exist.

**Given:**
- Target with detected platform in the hyperscaler set.

**When:**
- A cycle runs.

**Then:**
- Two rows: `pg_default`, `pg_global`.
- All per-tablespace GUC columns (`seq_page_cost`, etc.) are NULL
  on both rows.
- `size_bytes` is populated for `pg_default`.

**Expected Result:** Pass when the default-only shape appears.

---

### TC-TS-02: Custom tablespace with cost overrides

**Rule:** Normal — self-hosted case

**Scenario:** Target has a user-created tablespace with
`random_page_cost` set via `ALTER TABLESPACE ... SET`.

**Given:**
- Tablespace `ts_fast` with
  `ALTER TABLESPACE ts_fast SET (random_page_cost = 1.1,
  effective_io_concurrency = 200)`.

**When:**
- A cycle runs.

**Then:**
- Row for `ts_fast` has `random_page_cost = 1.1`,
  `effective_io_concurrency = 200`,
  `seq_page_cost = NULL`, `maintenance_io_concurrency = NULL`.
- `spcoptions_raw` contains both options verbatim.

**Expected Result:** Pass when the explicit SET values are extracted
and unspecified options are NULL.

---

### TC-TS-03: pg_tablespace_size failure tolerated

**Rule:** FC-01

**Scenario:** Size function errors for a tablespace the role
cannot stat.

**Given:**
- Tablespace the collector cannot read size for.

**When:**
- A cycle runs.

**Then:**
- Row emitted with `size_bytes = NULL`.
- No collection-level error.

**Expected Result:** Pass when the row is present with NULL size.

---

### TC-TS-04: Ordering by spcname

**Rule:** Invariant

**Scenario:** Rows are alphabetical.

**Given:**
- Target with multiple tablespaces.

**When:**
- A cycle runs.

**Then:**
- Rows ordered `spcname` ascending.

**Expected Result:** Pass when ordering matches.
