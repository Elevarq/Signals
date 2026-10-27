# ClickHouse Collectors (CH-2)

## Status

ACTIVE

## Purpose

Define the read-only diagnostic collector set for a ClickHouse target, collected
over the CH-1 native backend (`internal/clickhouse`). CH-2 replaces the CH-1
foundation's minimal identity-only catalog with the full `system.*` diagnostic
surface. Every statement is a read-only `SELECT` over `system.*`, confirmed live
against ClickHouse 24.8 (all 14 collectors run clean under `GRANT SELECT ON
system.*`; query text redacted). Children #559–#565 of epic #527.

## Scope

- `clickhouse.Catalog()` — the 14-collector set below.
- Read-only monitoring grant + sensitive-data handling (#565).

Out of scope: the shared `Collect`/`NewRunner` runtime (CH-1), fixtures + live
qualification + the `integration-clickhouse` CI job (CH-3).

## Validated catalog (columns + live run confirmed against ClickHouse 24.8)

| ID | Category | system table | Issue |
|----|----------|--------------|-------|
| `clickhouse_identity_v1` | server | `version()`, `uptime()` | CH-1 |
| `clickhouse_clusters_v1` | topology | `system.clusters` | #559 |
| `clickhouse_databases_v1` | schema | `system.databases` | #560 |
| `clickhouse_tables_v1` | schema | `system.tables` (excl. system schemas) | #560 |
| `clickhouse_parts_v1` | storage | `system.parts` (active, aggregated per partition, bounded — CHC-R006) | #562 |
| `clickhouse_replicas_v1` | topology | `system.replicas` | #563 |
| `clickhouse_merges_v1` | resources | `system.merges` | #563 |
| `clickhouse_mutations_v1` | resources | `system.mutations` (not done) | #563 |
| `clickhouse_metrics_v1` | resources | `system.metrics` | #564 |
| `clickhouse_events_v1` | resources | `system.events` | #564 |
| `clickhouse_asynchronous_metrics_v1` | resources | `system.asynchronous_metrics` | #564 |
| `clickhouse_settings_v1` | config | `system.settings` (IN-list) | #564 |
| `clickhouse_query_log_v1` | workload | `system.query_log` (recent) | #561; #533 adds `normalized_query_hash` as the typed workload identity (non-sensitive UInt64); `query` stays **HighSensitivity**-redacted |
| `clickhouse_query_thread_log_v1` | workload | `system.query_thread_log` (recent) | #561 |

## Behaviors

- **Given** a single-node ClickHouse and a `SELECT`-granted monitoring role,
  **when** the catalog runs, **then** every collector returns rows or a
  legitimate empty result — `replicas` / `merges` / `mutations` /
  `query_thread_log` are empty on a non-replicated idle node (the tables exist,
  so the SELECT succeeds with 0 rows, not a failure).
- **Given** high-sensitivity collection disabled, **when** `query_log` /
  `query_thread_log` run, **then** each `query` value is redacted (nil).

## Rules

- **CHC-R001** — Every catalog statement is a read-only `SELECT` over `system.*`;
  no writes (`HasWrite` false for all).
- **CHC-R002** — A collector whose statement errors records `Err` and does not
  abort the others (partial collection captured).
- **CHC-R003** — An empty `system.*` result (idle / single-node) is a successful
  zero-row collection, never a failure.
- **CHC-R004** — `clickhouse_query_log_v1` and `clickhouse_query_thread_log_v1`
  select the application SQL `query` column and MUST be `HighSensitivity` with
  `query` in `SensitiveColumns`; with `redactSensitive=true` the value is nil.
- **CHC-R005** — `Catalog()` is the 14-collector set above, identity first; the
  query-log collectors bound their result to a recent window + `LIMIT` so a busy
  server does not produce an unbounded payload.
- **CHC-R006** — `clickhouse_parts_v1` is **bounded and aggregated** (#562): it
  emits one row per `(database, table, partition_id)` —
  `count() AS parts, sum(rows) AS rows, sum(bytes_on_disk) AS bytes_on_disk`,
  `WHERE active AND database NOT IN (system schemas)`,
  `GROUP BY database, table, partition_id ORDER BY parts DESC LIMIT 10000` — never
  one row per active part. A busy MergeTree can hold millions of active parts, so
  the per-part shape was unbounded in rows/memory/payload. Aggregation bounds
  cardinality to the schema (≈ number of partitions, hard-capped at 10000) and
  the parts-per-partition count is the diagnostic signal (merge pressure). The
  `ORDER BY parts DESC` keeps the worst-offending partitions if the cap is hit.
  **Producer-contract change:** the emitted columns change from
  `{part_type, rows, bytes_on_disk}` per part to `{partition_id, parts, rows,
  bytes_on_disk}` per partition; Analyzer consumer coordination tracked in the
  companion Analyzer issue (filed from #562).

## Invariants

- **INV-CHC-01** — Collection never issues a non-`SELECT` statement.
- **INV-CHC-02** — No collector selecting a query-text column escapes the
  HighSensitivity + redaction contract (CHC-R004).
- **INV-CHC-03** — No storage collector emits unbounded per-part rows:
  `clickhouse_parts_v1` output cardinality is `≤ 10000` regardless of the number
  of parts on the server (CHC-R006).

## Failure conditions

| Trigger | Response |
|---------|----------|
| `system.query_log` absent (query-logging disabled on the target) | That collector records `Err`; others still run (CHC-R002). The default config has query_log enabled, so this is an edge. |
| A collector permission-denied (ACCESS_DENIED / 497) | `skipped`/`permission_denied` (CH-1 CHB-R004). |

## Constraints / NFR

- Read-only; monitoring role `GRANT SELECT ON system.*` (validated: reads the
  surface; writes `ACCESS_DENIED`). #565.
- Columns confirmed live; regenerate against the pinned image if the server
  version changes shape.

## Traceability

Acceptance: `specifications/clickhouse-collectors.acceptance.md`.
Matrix: `features/signals/traceability.md` (CHC-R001..R006, INV-CHC-01..03).
