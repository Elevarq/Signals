# Acceptance Tests: index_health_summary_v2

## Feature

`specifications/collectors/index_health_summary_v2.md`

## Test Cases

### TC-IHV2-01: Registered with the documented configuration

**Rule:** Normal / Configuration

**Given:** the catalog is initialised at process start.
**When:** `pgqueries.ByID("index_health_summary_v2")` resolves.
**Then:** the collector exists with `Category = "indexes"`,
`Cadence = Cadence6h`, `RetentionClass = RetentionMedium`, `Timeout = 30s`,
`ResultKind = ResultRowset`.

---

### TC-IHV2-02: SQL passes the linter (INV-IHV2-05)

**Given:** the collector is registered.
**When:** `pgqueries.LintQuery(q.SQL)` runs.
**Then:** no error — no DDL/DML keyword (incl. the `CREATE`/`REINDEX` traps),
no dangerous function, single statement.

---

### TC-IHV2-03: System schemas excluded (INV-SIGNALS-12)

**Then:** the SQL excludes `pg_catalog`, `information_schema`, `pg_toast` via
`NOT IN`, and `pg_temp_%` / `pg_toast_temp_%` via `NOT LIKE`.

---

### TC-IHV2-04: Output column set matches the spec (INV contract)

**Then:** the outer SELECT emits exactly the spec columns: `schemaname`,
`tablename`, `indexname`, `index_oid`, `table_oid`, `size_bytes`, `idx_scan`,
`idx_tup_read`, `idx_tup_fetch`, `is_valid`, `is_ready`, `is_live`,
`is_primary`, `is_unique`, `is_exclusion`, `is_immediate`,
`is_replica_identity`, `is_constraint_backed`, `constraint_type`,
`build_state`, `access_method`, `relation_kind`, `is_partitioned`,
`key_column_count`, `include_column_count`, `structure_version`,
`structure_fingerprint`, `exact_duplicate_of`, `prefix_candidate_of`,
`prefix_candidate_basis`.

---

### TC-IHV2-05: No `SELECT *`

**Then:** the SQL contains no `SELECT *` (explicit projection).

---

### TC-IHV2-06: Deterministic ORDER BY (INV-IHV2-04)

**Then:** the outer SELECT carries `ORDER BY schemaname, tablename, indexname`.

---

### TC-IHV2-07: No safety synthesis on usage/state (R-IHV2-01)

**Given:** the collector is registered.
**When:** the SQL is inspected for coercion of usage counters / state booleans.
**Then:** no `COALESCE(` (any case) wraps `idx_scan` / `idx_tup_read` /
`idx_tup_fetch` to `0`, and no state boolean is `COALESCE(..., false)` — the
raw catalog/stats values pass through, NULL preserved.

---

### TC-IHV2-08: Controlled constraint-type codes (R-IHV2-03)

**Then:** the SQL contains the controlled literals `'primary'`, `'unique'`,
`'exclusion'`, and `'other'` derived from `pg_constraint` / `contype`, and joins
`pg_constraint` on `conindid`.

---

### TC-IHV2-09: Controlled build-state codes distinguish build vs residue (R-IHV2-04)

**Then:** the SQL contains the controlled literals `'active_build'`,
`'active_reindex'`, `'invalid_residue'`, `'not_ready_residue'`, `'ready'`, and
references `pg_stat_progress_create_index`; it distinguishes reindex from build
without using the forbidden `CREATE`/`REINDEX` keyword literals.

---

### TC-IHV2-10: Versioned semantic fingerprint derived in-DB (R-IHV2-05, R-IHV2-07)

**Then:** the SQL emits `structure_version` and a `structure_fingerprint`
computed with `md5(` over a normalization of `pg_get_indexdef(`; and
`exact_duplicate_of` is selected by equality of the fingerprint (+ version),
not by key-column equality.

---

### TC-IHV2-11: Prefix relationship is a labelled candidate, not a verdict (R-IHV2-06)

**Then:** `prefix_candidate_of` is accompanied by
`prefix_candidate_basis = 'key_column_left_prefix'`, and neither `'redundant'`
nor a drop verdict is emitted for it.

---

### TC-IHV2-12: Included on every supported PG major (14–18)

**Given:** the collector is registered with no `MinPGVersion`.
**When:** `pgqueries.Filter(...)` runs for each major.
**Then:** the collector appears in the filtered set on 14, 15, 16, 17, 18
(no version-gated column is read — NULL-distinct semantics are carried
implicitly through `pg_get_indexdef`, so PG14 does not error).

---

### TC-IHV2-13: v1 compatibility preserved (INV-IHV2-06)

**Given:** the catalog is initialised.
**When:** `pgqueries.ByID("index_health_summary_v1")` resolves.
**Then:** v1 is still registered with its documented `indexes` / `Cadence6h` /
`RetentionMedium` / 30s / `ResultRowset` configuration — v2 is additive.

---

### TC-IHV2-14: Inventory carries v2

**Then:** `specifications/collectors/collector-inventory.json` lists
`index_health_summary_v2` in the `indexes` category alongside v1 (CI gate
R119–R122).

---

### TC-IHV2-15: Concurrent-DDL capability evidence (R-IHV2-08, #294)

**Rule:** R-IHV2-08 — partitioning / relation-kind evidence

**Given:** the collector is registered.
**When:** the SQL is inspected, and executed against a database with an ordinary
index, a partitioned parent index, and a partition-local index.
**Then:**
- The SQL emits `relation_kind` and `is_partitioned`, derived from
  `pg_class.relkind` (`'I'` → `partitioned_index` / true; `'i'` → `index` /
  false), with no `COALESCE` to a safe default.
- The controlled literals `'index'` and `'partitioned_index'` are present.
- On execution: a partitioned parent index reports
  `relation_kind = partitioned_index`, `is_partitioned = true`; an ordinary
  index and a partition-local index report `relation_kind = index`,
  `is_partitioned = false` (parent vs partition-local distinguished).
