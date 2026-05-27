# Acceptance Tests: pg_vector_columns_v1

## Feature

`specifications/collectors/pg_vector_columns_v1.md`

## Test Cases

### TC-VEC-01: Extension absent → collector not eligible

**Rule:** FC-01

**Scenario:** pgvector is not installed on the target. The collector
must be gated out — no query, no rows in `query_results.ndjson`,
no error.

**Given:**
- Target at PG 16 without the `vector` extension.

**When:**
- `pgqueries.Filter()` runs with `Extensions: []`.

**Then:**
- `pg_vector_columns_v1` is not in the eligible set.
- `collector_status.json` carries one entry with
  `status = "skipped"` and `reason = "extension_missing"` per
  `specifications/extension-absent-emission.md` (EA-R001).
- `EvidenceCompleteness` consumes that entry to surface the
  collector as `ExtensionUnavailable`.

**Expected Result:** Pass when the collector_status entry is
present and the collector is absent from the run manifest.

---

### TC-VEC-02: Extension installed + vector column present → rows emitted

**Rule:** Normal

**Scenario:** pgvector is installed; at least one table has a
`vector` column.

**Given:**
- Target at PG 14+ with `vector` extension.
- Table `embeddings(id int, v vector(1536))`.

**When:**
- A collection cycle runs.

**Then:**
- Row for `embeddings.v` is emitted.
- `dimension = 1536`.
- `atttypname = 'vector'`.
- `likely_toasted = true` when `avg_width > 2000` (post-analysis)
  or `false` when no stats are available yet.
- `has_index` reflects whether an HNSW / IVFFlat / btree index covers
  the column.

**Expected Result:** Pass when the row appears with correct
derivations.

---

### TC-VEC-03: Extension installed + no vector columns → empty result

**Rule:** FC-02

**Scenario:** pgvector is installed (extension-available), but no
user table uses a vector type. This is a normal state — an extension
can be installed without being used.

**Given:**
- Target at PG 14+ with `vector` extension.
- No user tables with vector columns.

**When:**
- A collection cycle runs.

**Then:**
- Result is `[]`.
- `QueryRun.RowCount = 0`.
- No collection-level error.
- `collector_status.json` carries one entry with
  `status = "success"` and `row_count = 0` (extension present,
  query ran, no rows matched).

**Expected Result:** Pass when the empty-array output is emitted
without error.

---

### TC-VEC-04: Small-dimension inline vector is not flagged toasted

**Rule:** Boundary — `likely_toasted` derivation

**Scenario:** Small vectors fit inline; `likely_toasted` should be
false.

**Given:**
- Table `small_vecs(id int, v vector(64))` — 256 bytes per vector.
- Stats have been collected.

**When:**
- A collection cycle runs.

**Then:**
- Row for `small_vecs.v` has `dimension = 64`.
- `likely_toasted = false` (avg_width below the 2000-byte threshold).

**Expected Result:** Pass when small vectors are not flagged as
likely toasted.

---

## Coverage Notes

Covers FC-01, FC-02, and the main spec invariants. FC-03 (PG < 14
exclusion) and FC-04 (permission denied) are covered by unit tests
in `tests/signals_vector_columns_test.go`.
