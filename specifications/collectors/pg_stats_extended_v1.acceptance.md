# Acceptance Tests: pg_stats_extended_v1

## Feature

`specifications/collectors/pg_stats_extended_v1.md`

## Test Cases

### TC-PGSEXT-01: Registered with category=schema

**Rule:** Registration shape

**Scenario:** A consumer of the registry asks for the collector
by ID.

**Given:**
- The collector is compiled into the binary.

**When:**
- `pgqueries.ByID("pg_stats_extended_v1")` is called.

**Then:**
- A non-nil `*QueryDef` is returned.
- `Category == "schema"`.

**Expected Result:** Pass.

---

### TC-PGSEXT-02: Linter passes

**Rule:** SQL hygiene

**Scenario:** The collector's SQL is fed through the project's
linter (deterministic-output, explicit columns, schema-filter,
no SELECT *).

**Given:**
- The collector's SQL string.

**When:**
- `pgqueries.LintQuery(q.SQL)` is called.

**Then:**
- Returns nil.

**Expected Result:** Pass.

---

### TC-PGSEXT-03: HighSensitivity flag set

**Rule:** Sensitivity gating

**Scenario:** The output contains real customer data
(most_common_vals, histogram_bounds), so the collector must be
gated off by default.

**Given:**
- The QueryDef.

**When:**
- Inspecting `q.HighSensitivity`.

**Then:**
- It is `true`.

**Expected Result:** Pass.

---

### TC-PGSEXT-04: Filter omits when HighSensitivityEnabled=false

**Rule:** Default gating posture

**Scenario:** A standard collection cycle (operator has not opted
in) must not execute this collector.

**Given:**
- `FilterParams{PGMajorVersion: 18, HighSensitivityEnabled: false}`.

**When:**
- `pgqueries.Filter(...)` is called.

**Then:**
- The returned slice does NOT include `pg_stats_extended_v1`.
- `pgqueries.GatedIDsByReason(...)["config_disabled"]` includes
  `pg_stats_extended_v1`.

**Expected Result:** Pass.

---

### TC-PGSEXT-05: Filter includes when HighSensitivityEnabled=true

**Rule:** Opt-in surface

**Scenario:** Operator explicitly enabled high-sensitivity
collection.

**Given:**
- `FilterParams{PGMajorVersion: 18, HighSensitivityEnabled: true}`.

**When:**
- `pgqueries.Filter(...)` is called.

**Then:**
- The returned slice includes `pg_stats_extended_v1`.

**Expected Result:** Pass.

---

### TC-PGSEXT-06: SQL emits required output columns

**Rule:** Output schema

**Scenario:** The collector promises a fixed set of columns to
downstream consumers (the analyzer).

**Given:**
- The QueryDef.

**When:**
- The SQL string is inspected.

**Then:**
- It includes (case-insensitive): `schemaname`, `tablename`,
  `attname`, `most_common_vals`, `most_common_freqs`,
  `histogram_bounds`.

**Expected Result:** Pass.

---

### TC-PGSEXT-07: SQL excludes array-only columns

**Rule:** Disproportionate-volume exclusion

**Scenario:** Even within the high-sensitivity surface, certain
pg_stats columns apply only to array/composite types and add
disproportionate volume.

**Given:**
- The QueryDef.

**When:**
- The SQL string is inspected.

**Then:**
- It does NOT contain (case-insensitive): `most_common_elems`,
  `most_common_elem_freqs`, `elem_count_histogram`.

**Expected Result:** Pass.

---

### TC-PGSEXT-08: Schema filter excludes system schemas

**Rule:** Standard schema filter

**Scenario:** System catalogs are not included in user-schema
analysis.

**Given:**
- The QueryDef.

**When:**
- The SQL string is inspected.

**Then:**
- It excludes `pg_catalog`, `information_schema`, `pg_toast`,
  and matches `pg_temp_%` exclusion.

**Expected Result:** Pass.

---

### TC-PGSEXT-09: ORDER BY for deterministic output

**Rule:** Deterministic ordering invariant

**Scenario:** Equal inputs must produce byte-identical output
for snapshot stability.

**Given:**
- The QueryDef.

**When:**
- The SQL string is inspected.

**Then:**
- It contains `ORDER BY`.

**Expected Result:** Pass.

---

### TC-PGSEXT-10: RetentionShort

**Rule:** Sensitive samples must not persist

**Scenario:** Output contains real data values; long retention
would compound exposure risk.

**Given:**
- The QueryDef.

**When:**
- Inspecting `q.RetentionClass`.

**Then:**
- It equals `RetentionShort`.

**Expected Result:** Pass.

---

### TC-PGSEXT-11: Cadence is daily

**Rule:** Cadence policy

**Scenario:** Histograms change slowly; daily collection
matches the spec's RetentionShort + 24h policy.

**Given:**
- The QueryDef.

**When:**
- Inspecting `q.Cadence`.

**Then:**
- It equals `CadenceDaily`.

**Expected Result:** Pass.

---

### TC-PGSEXT-12: Eligible on PG 14+

**Rule:** Minimum PG version

**Scenario:** The collector must run on the full supported PG
matrix (14..18) — the underlying `pg_stats` view's relevant
columns are available since PG 9.x.

**Given:**
- `FilterParams{PGMajorVersion: 14, HighSensitivityEnabled: true}`.

**When:**
- `pgqueries.Filter(...)` is called.

**Then:**
- The returned slice includes `pg_stats_extended_v1`.

**Expected Result:** Pass.
