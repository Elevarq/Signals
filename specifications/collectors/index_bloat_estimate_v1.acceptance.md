# Acceptance Tests: index_bloat_estimate_v1

## Feature

`specifications/collectors/index_bloat_estimate_v1.md`

## Test Cases

### TC-IDXBLOAT-01: Registered with the documented configuration

**Rule:** Normal

**Scenario:** Catalog enumerates the registered collector.

**Given:**
- The catalog has been initialised at process start.

**When:**
- `pgqueries.ByID("index_bloat_estimate_v1")` resolves the
  registration.

**Then:**
- The collector exists with:
  - `Category = "indexes"`,
  - `Cadence = Cadence6h`,
  - `RetentionClass = RetentionMedium`,
  - `Timeout = 30s`,
  - `ResultKind = ResultRowset`,
  - no `MinPGVersion`, no `RequiresExtension`.

**Expected Result:** Pass when the registration matches the spec.

---

### TC-IDXBLOAT-02: SQL passes the linter

**Rule:** Invariant — read-only safety

**Given:**
- The collector is registered.

**When:**
- `pgqueries.LintQuery(q.SQL)` runs against the registered SQL.

**Then:**
- No error is returned.

**Expected Result:** Pass when the linter is clean.

---

### TC-IDXBLOAT-03: System-schema and non-index relkinds excluded

**Rule:** Invariant — INV-SIGNALS-12 + relkind filter

**Given:**
- The collector is registered.

**When:**
- The SQL is inspected.

**Then:**
- The SQL filters `relkind IN ('i', 'I')`.
- The SQL excludes `pg_catalog`, `information_schema`,
  `pg_toast`, `pg_temp_*`, `pg_toast_temp_*` namespaces.

**Expected Result:** Pass when both scope filters are present
structurally.

---

### TC-IDXBLOAT-04: Output column set matches the spec

**Rule:** Invariant — stable contract

**Given:**
- The collector is registered.

**When:**
- The SQL is inspected for column references in its outer SELECT
  list.

**Then:**
- The SQL emits:
  `schemaname`, `tablename`, `indexname`, `index_oid`,
  `relkind`, `actual_size_bytes`, `expected_size_bytes`,
  `bloat_bytes`, `bloat_ratio`, `reltuples`, `is_unique`,
  `is_primary`, `stats_missing`.

**Expected Result:** Pass when every required column appears in
the outer SELECT list.

---

### TC-IDXBLOAT-05: No SELECT *

**Rule:** Invariant — stable contract

**Given:**
- The collector is registered.

**When:**
- The test scans `q.SQL` for `SELECT *`.

**Then:**
- The SQL contains no `SELECT *` (case-insensitive).

**Expected Result:** Pass when column projection is explicit.

---

### TC-IDXBLOAT-06: Deterministic ORDER BY

**Rule:** Invariant — deterministic output

**Given:**
- The collector is registered.

**When:**
- The outer SELECT is inspected.

**Then:**
- The outer SELECT ends with
  `ORDER BY schemaname, tablename, indexname`.

**Expected Result:** Pass when the ORDER BY is present and total.

---

### TC-IDXBLOAT-07: Formula references the documented constants

**Rule:** Invariant — reproducible derivation

**Scenario:** The spec documents the index estimation formula
with specific constants (INDEX_TUPLE_HDR=8, ITEM_PTR=4,
PAGE_HDR=24). The SQL must use these so the formula stays
reproducible from the spec alone.

**Given:**
- The collector is registered.

**When:**
- The test searches the SQL for the constants and for
  `current_setting('block_size')`.

**Then:**
- The SQL contains the integer literals `8`, `4`, `24`.
- The SQL uses `current_setting('block_size')` for the page
  size.

**Expected Result:** Pass when the formula's constants are
present verbatim.

---

### TC-IDXBLOAT-08: No pgstattuple / pgstatindex dependency

**Rule:** Invariant — runs everywhere

**Given:**
- The collector is registered.

**When:**
- The test inspects `q.RequiresExtension` and the SQL.

**Then:**
- `q.RequiresExtension == ""`.
- The SQL contains no reference to `pgstattuple`, `pgstatindex`,
  or `pgstattuple_approx`.

**Expected Result:** Pass when the collector has no extension
dependency.

---

### TC-IDXBLOAT-09: Included on every supported PG major

**Rule:** Normal — no MinPGVersion gate

**Given:**
- The collector is registered with no `MinPGVersion`.

**When:**
- `pgqueries.Filter(...)` is invoked for each major in
  `{14, 15, 16, 17, 18}`.

**Then:**
- The collector appears in the filtered set on every supported
  major.

**Expected Result:** Pass when the collector is universally
available.

---

### TC-IDXBLOAT-10: Width sum bounded by indnkeyatts (INCLUDE columns skipped)

**Rule:** Invariant — match canonical width definition

**Scenario:** PG 11+ supports covering indexes with INCLUDE
columns. Those columns extend the tuple footprint but the
PG-wiki convention computes index bloat against key columns
only. The SQL must bound its width sum by `indnkeyatts`.

**Given:**
- The collector is registered.

**When:**
- The SQL is inspected for the indkey-ordinality bounding.

**Then:**
- The SQL contains `pos.ord <= i.indnkeyatts` (or an equivalent
  bound) within its column-width CTE.

**Expected Result:** Pass when key-column-only summing is
structurally enforced.
