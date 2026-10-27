# Acceptance Tests: clickhouse-collectors

## Feature

`specifications/clickhouse-collectors.md` — the full CH-2 ClickHouse `system.*`
diagnostic catalog.

## Test Cases

### TC-CHC-01: expected catalog set (normal)

**Rule:** CHC-R005, INV-CHC-01

**Given:** `clickhouse.Catalog()`. **When:** ids are enumerated. **Then:** exactly
the 14 `clickhouse_*_v1` collectors, identity first, each a read-only SELECT
(`HasWrite` false). **Expected:** Pass (`TestCatalogExpectedSet`,
`TestCatalogReadOnly`).

---

### TC-CHC-02: query-log collectors redact application SQL (boundary/failure)

**Rule:** CHC-R004, INV-CHC-02

**Given:** `clickhouse_query_log_v1` / `clickhouse_query_thread_log_v1` and a row
carrying `query`. **When:** `Collect` runs with `redactSensitive=true`. **Then:**
both are `HighSensitivity` with `query` in `SensitiveColumns`, and the returned
`query` value is nil. **Expected:** Pass (`TestQueryLogRedaction`).

---

### TC-CHC-03: empty single-node tables are successful zero-row results (live)

**Rule:** CHC-R003

**Given:** a single-node ClickHouse (no replicated tables, idle). **When:** the
full catalog runs as the `SELECT`-granted monitoring role. **Then:** every
collector succeeds — `replicas`/`merges`/`mutations`/`query_thread_log` return 0
rows (legitimate empty), none is a failure.

**Expected Result:** Pass — validated live against ClickHouse 24.8 (14/14
collectors clean, query redacted). The CH-3 live qualification locks this in CI.

---

### TC-CHC-04: parts collector is bounded and aggregated (normal + boundary)

**Rule:** CHC-R006, INV-CHC-03

**Given:** `clickhouse_parts_v1` from `clickhouse.Catalog()`. **When:** its SQL is
inspected. **Then:** it aggregates `system.parts` per `(database, table,
partition_id)` — `count()`/`sum(rows)`/`sum(bytes_on_disk)` with `GROUP BY …
partition_id`, `ORDER BY parts DESC` and a `LIMIT` — is read-only, keeps the
`active` + system-schema exclusion, and does **not** select the per-part
`part_type` column. So a MergeTree with millions of active parts cannot produce
an unbounded per-part payload; output cardinality is `≤ 10000`. **Expected:**
Pass (`TestPartsCollectorBoundedAggregated`). Live boundary: the CH-3
qualification seeds a deliberately multi-partition MergeTree table
(`examples/clickhouse-init.sql`) so the collector runs against a non-trivial
parts inventory and still reconciles collected == persisted == exported.
