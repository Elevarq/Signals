# Acceptance Tests: pg_stat_progress_* family

## Feature

`specifications/collectors/pg_stat_progress_family_v1.md`

## Test Cases

### TC-PROG-01: All six family members registered

**Rule:** Normal

**Scenario:** Catalog enumerates the registered collectors.

**Given:**
- The catalog has been initialised at process start.

**When:**
- The test queries the catalog by ID for each family member.

**Then:**
- All six collectors are registered:
  `pg_stat_progress_vacuum_v1`, `pg_stat_progress_analyze_v1`,
  `pg_stat_progress_create_index_v1`,
  `pg_stat_progress_cluster_v1`,
  `pg_stat_progress_basebackup_v1`,
  `pg_stat_progress_copy_v1`.
- Each has `Category = "progress"`.

**Expected Result:** Pass when every ID resolves and the category
is set consistently.

---

### TC-PROG-02: Consistent configuration across the family

**Rule:** Invariant

**Scenario:** Every family member must carry identical scheduling
and retention metadata so downstream cadence / cleanup behaviour
is uniform.

**Given:**
- All six collectors are registered.

**When:**
- The test inspects `Cadence`, `RetentionClass`, `ResultKind`,
  `MinPGVersion`.

**Then:**
- `Cadence == Cadence5m` for all six.
- `RetentionClass == RetentionShort` for all six.
- `ResultKind == ResultRowset` for all six.
- `MinPGVersion == 14` for all six.

**Expected Result:** Pass when every family member shares the
documented configuration.

---

### TC-PROG-03: SQL passes the linter

**Rule:** Invariant — read-only safety

**Scenario:** The SQL for each collector must satisfy the
read-only linter (no DDL/DML, no dangerous functions, single
SELECT).

**Given:**
- All six collectors are registered.

**When:**
- `pgqueries.LintQuery(q.SQL)` runs for each family member.

**Then:**
- No linter error is returned.

**Expected Result:** Pass when every family member's SQL is
linter-clean.

---

### TC-PROG-04: Family excluded on PG 13 — FC-01

**Rule:** Failure condition — FC-01

**Scenario:** Catalog filter runs against an instance reporting
`server_version_num < 140000`.

**Given:**
- `PGMajorVersion = 13`.

**When:**
- `pgqueries.Filter(...)` returns the set applicable to PG 13.

**Then:**
- No `pg_stat_progress_*_v1` collector appears in the result set.
- The status surfaces in `collector_status.json` as `skipped,
  reason=version_unsupported` via the EA-R001 channel.

**Expected Result:** Pass when the version gate cleanly excludes
the entire family on PG 13 and below.

---

### TC-PROG-05: Family included on PG 14, 17, 18

**Rule:** Normal

**Scenario:** Catalog filter runs against PG 14, 17, and 18.

**Given:**
- `PGMajorVersion ∈ {14, 17, 18}`.

**When:**
- `pgqueries.Filter(...)` is asked for each major.

**Then:**
- All six family members appear in the result on every supported
  major.

**Expected Result:** Pass when the family travels forward through
PG 14, 17, and 18 without per-major dropouts.

---

### TC-PROG-06: Explicit column projection (no SELECT *)

**Rule:** Invariant — stable schema

**Scenario:** The SQL must list its columns explicitly so the
canonical schema is preserved when an upstream view changes shape.

**Given:**
- All six collectors are registered.

**When:**
- The test scans each `q.SQL` for `SELECT *`.

**Then:**
- No family member contains `SELECT *` (case-insensitive).

**Expected Result:** Pass when every member uses explicit column
projection.

---

### TC-PROG-07: Deterministic ORDER BY

**Rule:** Invariant — deterministic output

**Scenario:** Stable row order is required so cross-cycle diffing
is not perturbed by view-internal ordering.

**Given:**
- All six collectors are registered.

**When:**
- The test scans each SQL for an `ORDER BY` clause.

**Then:**
- Every family member contains `ORDER BY`.

**Expected Result:** Pass when every member's SQL has an explicit
ordering clause.
