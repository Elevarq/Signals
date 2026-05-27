# Acceptance Tests: bloat_estimate_v1

## Feature

`specifications/collectors/bloat_estimate_v1.md`

## Test Cases

### TC-BLOAT-01: Registered with the documented configuration

**Rule:** Normal

**Scenario:** Catalog enumerates the registered collector.

**Given:**
- The catalog has been initialised at process start.

**When:**
- `pgqueries.ByID("bloat_estimate_v1")` resolves the
  registration.

**Then:**
- The collector exists with:
  - `Category = "tables"`,
  - `Cadence = Cadence6h`,
  - `RetentionClass = RetentionMedium`,
  - `Timeout = 30s`,
  - `ResultKind = ResultRowset`,
  - no `MinPGVersion` and no `RequiresExtension`.

**Expected Result:** Pass when the registration matches the spec.

---

### TC-BLOAT-02: SQL passes the linter

**Rule:** Invariant — read-only safety

**Scenario:** The query catalog linter (R013, INV-SIGNALS-05)
must accept the collector's SQL — no DDL/DML keywords, no
dangerous functions, single SELECT.

**Given:**
- The collector is registered.

**When:**
- `pgqueries.LintQuery(q.SQL)` runs against the registered SQL.

**Then:**
- No error is returned.

**Expected Result:** Pass when the linter is clean.

---

### TC-BLOAT-03: System-schema and non-table relkinds excluded

**Rule:** Invariant — INV-SIGNALS-12 + relkind filter

**Scenario:** Only ordinary tables, materialised views, and
partitioned parents are emitted.

**Given:**
- The collector is registered.

**When:**
- The SQL is inspected.

**Then:**
- The SQL filters `relkind IN ('r', 'm', 'p')`.
- The SQL filters out `pg_catalog`, `information_schema`,
  `pg_toast`, `pg_temp_*`, and `pg_toast_temp_*` namespaces.

**Expected Result:** Pass when both scope filters are present
structurally.

---

### TC-BLOAT-04: Output column set matches the spec

**Rule:** Invariant — stable contract

**Scenario:** Downstream consumers (analyzer ingest + exports)
depend on the column list. The collector must emit every
spec-defined column.

**Given:**
- The collector is registered.

**When:**
- The SQL is inspected for column references in its outer SELECT
  list.

**Then:**
- The SQL emits:
  `schemaname`, `tablename`, `table_oid`, `relkind`,
  `actual_size_bytes`, `expected_size_bytes`, `bloat_bytes`,
  `bloat_ratio`, `reltuples`, `n_live_tup`, `n_dead_tup`,
  `last_autovacuum`, `stats_missing`.

**Expected Result:** Pass when every required column appears in
the outer SELECT list.

---

### TC-BLOAT-05: No SELECT *

**Rule:** Invariant — stable contract

**Scenario:** Explicit column projection prevents accidental
schema drift.

**Given:**
- The collector is registered.

**When:**
- The test scans `q.SQL` for `SELECT *`.

**Then:**
- The SQL contains no `SELECT *` (case-insensitive).

**Expected Result:** Pass when column projection is explicit.

---

### TC-BLOAT-06: Deterministic ORDER BY

**Rule:** Invariant — deterministic output

**Scenario:** Row order must be stable across cycles.

**Given:**
- The collector is registered.

**When:**
- The outer SELECT is inspected.

**Then:**
- The outer SELECT ends with
  `ORDER BY schemaname, tablename`.

**Expected Result:** Pass when the ORDER BY is present and total.

---

### TC-BLOAT-07: Formula references the documented constants

**Rule:** Invariant — reproducible derivation

**Scenario:** The spec documents the estimation formula with
specific constants (TUPLE_HDR=23, NULL_BMP=4, ALIGN_PAD=8,
PAGE_HDR=24). The SQL must use these so the formula stays
reproducible from the spec alone.

**Given:**
- The collector is registered.

**When:**
- The test searches the SQL for the constants and for
  `current_setting('block_size')`.

**Then:**
- The SQL contains the integer literals `23`, `4`, `8`, `24`.
- The SQL uses `current_setting('block_size')` for the page
  size.

**Expected Result:** Pass when the formula's constants are
present verbatim.

---

### TC-BLOAT-08: No pgstattuple dependency

**Rule:** Invariant — runs everywhere

**Scenario:** The collector must work on managed-PG services
that don't expose the `pgstattuple` extension (RDS / Aurora /
Cloud SQL / AlloyDB / Azure Flex).

**Given:**
- The collector is registered.

**When:**
- The test inspects `q.RequiresExtension` and the SQL.

**Then:**
- `q.RequiresExtension == ""`.
- The SQL contains no reference to `pgstattuple`,
  `pgstatindex`, or `pgstattuple_approx`.

**Expected Result:** Pass when the collector has no extension
dependency.

---

### TC-BLOAT-09: Included on every supported PG major

**Rule:** Normal — no MinPGVersion gate

**Scenario:** The collector relies only on stable catalog
columns. It must travel across every supported major.

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
