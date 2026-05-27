# Acceptance Tests: index_health_summary_v1

## Feature

`specifications/collectors/index_health_summary_v1.md`

## Test Cases

### TC-IDXHEALTH-01: Registered with the documented configuration

**Rule:** Normal

**Scenario:** Catalog enumerates the registered collector.

**Given:**
- The catalog has been initialised at process start.

**When:**
- `pgqueries.ByID("index_health_summary_v1")` resolves the
  registration.

**Then:**
- The collector exists with:
  - `Category = "indexes"`,
  - `Cadence = Cadence6h`,
  - `RetentionClass = RetentionMedium`,
  - `Timeout = 30s`,
  - `ResultKind = ResultRowset`.

**Expected Result:** Pass when the registration matches the spec.

---

### TC-IDXHEALTH-02: SQL passes the linter

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

### TC-IDXHEALTH-03: System-schema indexes excluded

**Rule:** Invariant — INV-SIGNALS-12

**Scenario:** The collector must skip indexes in `pg_catalog`,
`information_schema`, `pg_toast`, and any `pg_temp_*` /
`pg_toast_temp_*` namespace.

**Given:**
- The collector is registered.

**When:**
- The SQL is inspected for system-schema exclusion.

**Then:**
- The SQL contains a `NOT IN ('pg_catalog', 'information_schema',
  'pg_toast')` filter against the namespace name and excludes
  `pg_temp_%` / `pg_toast_temp_%` via `NOT LIKE`.

**Expected Result:** Pass when system schemas are excluded
structurally (verified by SQL inspection).

---

### TC-IDXHEALTH-04: Output column set matches the spec

**Rule:** Invariant — stable contract

**Scenario:** Downstream consumers (analyzer ingest + exports)
depend on the column list. The collector must emit every spec-
defined column.

**Given:**
- The collector is registered.

**When:**
- The SQL is inspected for `AS <name>` aliases and bare
  column references in its outer SELECT list.

**Then:**
- The SQL emits the columns:
  `schemaname`, `tablename`, `indexname`, `index_oid`,
  `size_bytes`, `idx_scan`, `idx_tup_read`, `is_unique`,
  `is_primary`, `is_valid`, `is_ready`, `column_set`,
  `duplicate_of`, `redundant_with`, `health_findings`.

**Expected Result:** Pass when every required column appears in
the outer SELECT list.

---

### TC-IDXHEALTH-05: No SELECT *

**Rule:** Invariant — stable contract

**Scenario:** Explicit column projection prevents accidental
schema drift when an upstream view changes.

**Given:**
- The collector is registered.

**When:**
- The test scans `q.SQL` for `SELECT *`.

**Then:**
- The SQL contains no `SELECT *` (case-insensitive).

**Expected Result:** Pass when column projection is explicit.

---

### TC-IDXHEALTH-06: Deterministic ORDER BY

**Rule:** Invariant — deterministic output

**Scenario:** Row order is stable across cycles so cross-snapshot
diffing isn't perturbed by catalog-internal ordering.

**Given:**
- The collector is registered.

**When:**
- The SQL is inspected for `ORDER BY`.

**Then:**
- The outer SELECT carries
  `ORDER BY schemaname, tablename, indexname`.

**Expected Result:** Pass when the ORDER BY is present and total.

---

### TC-IDXHEALTH-07: Classification rules are written into the SQL

**Rule:** Invariant — derivation in one place

**Scenario:** The spec promises to centralise classification.
Verify the six tag literals appear in the SQL (as case-derived
output strings), so the analyzer-side ingest never has to
re-derive them.

**Given:**
- The collector is registered.

**When:**
- The test searches `q.SQL` (case-insensitive) for each tag
  literal.

**Then:**
- The SQL contains each of:
  `'unused'`, `'large_unused'`, `'invalid'`, `'not_ready'`,
  `'redundant'`, `'duplicate'`.

**Expected Result:** Pass when every tag literal is present in
the SQL.

---

### TC-IDXHEALTH-08: Included on every supported PG major

**Rule:** Normal — no MinPGVersion gate

**Scenario:** The collector relies only on stable catalog columns
(`pg_index`, `pg_class`, `pg_namespace`, `pg_attribute`,
`pg_stat_user_indexes`). It must travel across every Signals-
supported major (14, 15, 16, 17, 18) without exclusion.

**Given:**
- The collector is registered with no `MinPGVersion`.

**When:**
- `pgqueries.Filter(...)` is invoked for each major.

**Then:**
- The collector appears in the filtered set on every supported
  major.

**Expected Result:** Pass when the collector is universally
available.
