# ClickHouse Least-Privilege Grants

## Status

ACTIVE

## Purpose

Define the minimal ClickHouse privileges the Signals collectors require, so
collection runs as a locked-down read-only role, and make the broad cross-user
query-log visibility an **optional, separately-approved** grant rather than a
default (Elevarq/Signals #565, audit finding F2). Source-permission-first: the
database grant is the primary collection boundary; Signals-side redaction is
defense in depth. The grant ladder was determined **empirically against the
pinned ClickHouse 24.8 dev image**, not guessed.

PostgreSQL configuration/behavior is unchanged (separate engine).

## Empirical findings (live, ClickHouse 24.8.14.39)

The 14-collector catalog reads 13 `system.*` tables (plus `version()`, which needs
no grant). Running as a restricted role:

| Grant | Collectors that succeed |
|-------|-------------------------|
| `GRANT SELECT ON system.{asynchronous_metrics, clusters, databases, events, merges, metrics, mutations, parts, replicas, settings, tables}` | identity + the 11 metadata/topology/resources/schema collectors (12 of 14). `system.query_log` / `system.query_thread_log` return **ACCESS_DENIED**. |
| `+ GRANT SELECT ON system.query_log, system.query_thread_log` | the above **+ the 2 query-workload collectors** (14 of 14). |

Writes (`CREATE`/`INSERT`/…) are denied under a `SELECT`-only grant — the role is
read-only by construction.

## Decision

- **Baseline monitoring role = `SELECT` on the 11 metadata tables above.** It
  covers identity, topology (clusters/replicas), schema (databases/tables),
  storage (parts/merges/mutations), and resources (metrics/events/
  asynchronous_metrics) — everything except the broad query logs. It confers
  **no cross-user query visibility**.
- **Query-workload access is OPTIONAL and must be separately approved.**
  `GRANT SELECT ON system.query_log, system.query_thread_log` enables
  `clickhouse_query_log_v1` + `clickhouse_query_thread_log_v1`. These tables
  expose **every user's** normalized query text, user, and schema — an
  incremental privacy impact that an operator opts into deliberately.
- **Without the optional grant the two query-log collectors degrade to
  `skipped`** (ACCESS_DENIED → ClickHouse error code 497 → `skipped` /
  `permission_denied`, per `clickhouse-backend.md` CHB-R004) — never `failed`.
  "Broad logs inaccessible" is an explicit degraded capability, not an error.
- **Redaction is defense in depth, not the boundary.** `clickhouse_query_log_v1`
  is `HighSensitivity` with `query` redacted by default (the non-sensitive
  `normalized_query_hash` identity is retained, #533). The DB grant is the
  primary control; redaction protects the payload if the optional grant is in
  place.

## Rules

| ID | Rule |
|----|------|
| **CHG-R001** | The baseline role is `SELECT` on the 11 metadata `system.*` tables; read-only (writes are ACCESS_DENIED). It collects 12/14 collectors and confers no cross-user query visibility. |
| **CHG-R002** | The two query-workload collectors (`clickhouse_query_log_v1`, `clickhouse_query_thread_log_v1`) require the optional `SELECT` on `system.query_log` + `system.query_thread_log`; without it they degrade to `skipped` (ACCESS_DENIED → 497), never `failed` (CHB-R004). |
| **CHG-R003** | The optional query-log grant exposes cross-user query text/user/schema and must be separately approved; it is NOT part of the default baseline. Signals redacts `query` by default (defense in depth); the DB grant is the primary boundary. |
| **CHG-R004** | Signals never requires a write, admin, or `SYSTEM`/superuser grant on ClickHouse; the role is `SELECT`-only. |

## Invariants

| ID | Invariant |
|----|-----------|
| **INV-CHG-01** | Collection never requires more than `SELECT` on `system.*` tables — never a write/admin privilege. |
| **INV-CHG-02** | Broad cross-user query-log visibility is opt-in (the optional grant), never conferred by the baseline role. |

## Recommended roles (operator-facing)

```sql
-- Baseline: metadata/topology/resources/schema, no cross-user query visibility.
CREATE USER signals_ro IDENTIFIED BY '<password>';
GRANT SELECT ON system.asynchronous_metrics, system.clusters, system.databases,
                system.events, system.merges, system.metrics, system.mutations,
                system.parts, system.replicas, system.settings, system.tables
  TO signals_ro;

-- Optional (separately approved): query-workload evidence. Exposes every user's
-- query text/user/schema; Signals redacts `query` by default.
-- GRANT SELECT ON system.query_log, system.query_thread_log TO signals_ro;
```

The dev-image fixture (`examples/clickhouse-init.sql`) grants `SELECT ON
system.*` — i.e. the baseline PLUS the optional grant — so the live qualification
exercises the full 14-collector catalog (the "optional approved" scenario).

## Traceability

Acceptance: `specifications/clickhouse-grants.acceptance.md`. Pairs with
`clickhouse-backend.md` (CHB-R004 permission-denied → skipped) and
`clickhouse-collectors.md` (query redaction, #533 normalized_query_hash).
Empirical grant ladder confirmed live (this issue).
