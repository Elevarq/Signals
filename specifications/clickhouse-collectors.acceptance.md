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
