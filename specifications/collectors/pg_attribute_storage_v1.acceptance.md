# Acceptance Tests: pg_attribute_storage_v1

## Feature

`specifications/collectors/pg_attribute_storage_v1.md`

## Test Cases

### TC-ATTSTG-01: Attribute storage modes correctly emitted

**Rule:** Normal

**Scenario:** Target has columns covering all four `attstorage`
values (`p`, `e`, `x`, `m`).

**Given:**
- Table with columns of type `int` (PLAIN), `bytea` (EXTENDED by
  default), and a column set to STORAGE EXTERNAL via `ALTER COLUMN
  ... SET STORAGE EXTERNAL`.

**When:**
- A collection cycle runs.

**Then:**
- Each column's `attstorage` is emitted as the corresponding char.

**Expected Result:** Pass when the storage modes match the DDL.

---

### TC-ATTSTG-02: attcompression NULL on PG < 14

**Rule:** PG version gating

**Scenario:** Target is PG 13; `attcompression` does not exist.

**Given:**
- Target at PG 13.

**When:**
- A collection cycle runs.

**Then:**
- Every row's `attcompression` is NULL.
- Other columns are populated normally.

**Expected Result:** Pass when NULL appears on PG < 14 and the row
shape is stable.

---

### TC-ATTSTG-03: System columns excluded

**Rule:** Scope filter (attnum > 0, not dropped)

**Scenario:** System columns like `xmin`, `ctid` (negative attnum)
and dropped columns do not appear.

**Given:**
- User table with one ALTER-dropped column.

**When:**
- A collection cycle runs.

**Then:**
- No row for `xmin`, `cmin`, `ctid`, etc.
- No row for the dropped column.

**Expected Result:** Pass when the filter holds.

---

### TC-ATTSTG-04: Missing pg_stats entry yields NULL avg_width without error

**Rule:** FC-01

**Scenario:** A newly created table has never been analyzed; no
`pg_stats` row exists for its columns.

**Given:**
- Freshly created table `t_new(id int, body text)` with no
  ANALYZE run.

**When:**
- A collection cycle runs.

**Then:**
- Rows for `t_new` are present.
- `avg_width` is NULL.
- No collection error.

**Expected Result:** Pass when rows appear with NULL avg_width and
the cycle succeeds.
