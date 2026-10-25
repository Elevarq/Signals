# Acceptance Tests: clickhouse-grants

## Feature

`specifications/clickhouse-grants.md` — least-privilege ClickHouse grants (#565).
The empirical grant ladder was confirmed live (ClickHouse 24.8); the deterministic
guard below keeps the catalog's optional-grant surface in lockstep with the spec.

## Test Cases

### TC-CHG-01: only the two query-log collectors read the optional tables (invariant)

**Rule:** CHG-R001, CHG-R002, INV-CHG-02

**Given:** `clickhouse.Catalog()`. **When:** each collector's SQL is scanned for
`system.query_log` / `system.query_thread_log`. **Then:** exactly
`clickhouse_query_log_v1` and `clickhouse_query_thread_log_v1` reference them;
every other collector reads only baseline metadata `system.*` tables. **Expected:**
Pass — the optional-grant surface is exactly the two query-workload collectors, so
the baseline role collects the rest. Guards against a new collector silently
needing the optional (cross-user) grant.

---

### TC-CHG-02: query-log collector is HighSensitivity with query redacted (invariant)

**Rule:** CHG-R003

**Given:** `clickhouse_query_log_v1`. **When:** inspected. **Then:** it is
`HighSensitivity` with `query` in `SensitiveColumns` (redacted by default;
`normalized_query_hash` retained). **Expected:** Pass — redaction is the
defense-in-depth layer over the optional grant.

---

### TC-CHG-03: baseline collects metadata; query logs degrade to skipped (live, documented)

**Rule:** CHG-R001, CHG-R002, CHG-R004, INV-CHG-01

**Given:** a role with `SELECT` on the 11 metadata `system.*` tables but not the
query logs. **When:** the collectors run. **Then:** the 12 metadata/identity
collectors succeed; `clickhouse_query_log_v1` / `clickhouse_query_thread_log_v1`
return ACCESS_DENIED → recorded `skipped`/`permission_denied` (not failed); writes
are denied. **Expected Result:** Confirmed live against the pinned ClickHouse 24.8
dev image (this issue); the degrade path is CHB-R004.
