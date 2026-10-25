# Workload Measurement & Provenance Model

## Status

ACTIVE

## Purpose

Define a lossless, engine-neutral model for query-workload evidence across the
five 2.0 engines (Elevarq/Signals #533): a **typed query identity**, explicit
**units / scope / observation semantics**, and **redaction** of sample SQL — all
while preserving each engine's native payload. The model distinguishes cumulative
counters, interval deltas, and event records so a consumer never mis-reads one as
another, and it guarantees a workload identity is **never equated across
engines**.

This spec has two layers:

- **Enforced now (producer, CI-gated):** every engine's workload collector
  carries its native typed identity and redacts its sample/normalized SQL
  (WMM-R001, WMM-R003). Verified by the cross-engine workload-identity guard.
- **Deferred model (additive envelope):** the normalized `source/units/scope/
  observed_from/observed_to/reset-uptime` envelope that will accompany the native
  payload (WMM-R004). It is documented here but **not yet emitted** — it is the
  additive layer gated on the #530 report-contract agreement with Analyzer. The
  export carries engine-native workload evidence today; this spec does not change
  that.

Signals stays read-only and local-first; PostgreSQL report semantics are
unchanged; native evidence is always retained.

## Per-engine workload identity + units (confirmed live)

| Engine | Workload collector | Identity kind | Identity column | Sample SQL (redacted) | Observation | Key units |
|--------|--------------------|---------------|-----------------|-----------------------|-------------|-----------|
| PostgreSQL | `pg_stat_statements_v1` | `queryid` | `queryid` (via `s.*`) | statement text (RedactStatementSecrets) | cumulative since reset | `total_exec_time` ms, `calls` count, `rows` count, `shared_blks_*` blocks |
| MySQL | `mysql_statement_digests_v1` | `digest` | `DIGEST` | `DIGEST_TEXT` | cumulative counters | `SUM_TIMER_WAIT` picoseconds, `COUNT_STAR` count, `SUM_ROWS_*` count |
| MariaDB | `mariadb_statement_digests_v1` | `digest` | `DIGEST` | `DIGEST_TEXT` | cumulative counters | `SUM_TIMER_WAIT` picoseconds, `COUNT_STAR` count |
| SingleStore | `singlestore_plancache_v1` | `plan_id` | `PLAN_ID` | `QUERY_TEXT` | cumulative totals | `EXECUTION_TIME`/`AVERAGE_EXEC_TIME`/`CPU_TIME` ms, `COMMITS`/`ROWCOUNT` count |
| ClickHouse | `clickhouse_query_log_v1` | `normalized_query_hash` | `normalized_query_hash` | `query` | **event record** (per-query) | `query_duration_ms` ms, `read_rows`/`result_rows` count, `memory_usage` bytes |

## The model

- **Query identity = `{kind, value, scope}`.** `kind` is the engine-native
  identity kind above; `value` is the identity column's value; `scope` carries
  the engine + database so identities are self-describing. Two engines' MySQL-wire
  `digest` kinds (MySQL vs MariaDB) are still **distinct** because their `scope`
  differs — they are never equated.
- **Observation semantics** are explicit per source: `cumulative` counters
  (pg_stat_statements, MySQL/MariaDB digests, SingleStore plancache — totals since
  the last reset/restart), `interval-delta` (none today), and `event-record`
  (ClickHouse `query_log` — one row per finished query, not a running total). A
  consumer must not difference an event-record series as if it were a cumulative
  counter, nor sum a cumulative counter as if it were per-event.
- **Units are explicit per field** (table above): notably MySQL/MariaDB
  `SUM_TIMER_WAIT` is **picoseconds**, PostgreSQL/SingleStore times are
  **milliseconds**, ClickHouse `memory_usage` is **bytes**. A normalized envelope
  must state units per field — never assume a common time unit.
- **Reset/uptime markers:** where the engine exposes them (e.g. a stats-reset
  timestamp, server uptime), the observation window (`observed_from` /
  `observed_to`) is bounded by them; cumulative counters are meaningless without
  the reset reference.

## Rules

- **WMM-R001** (enforced) | Every engine's workload collector selects its native
  typed identity column (table above). This is the producer guarantee that a
  typed identity exists per engine.
- **WMM-R002** (model / consumer) | A workload identity is `{kind, value, scope}`
  and is NEVER equated across engines — even identical `kind`s (MySQL vs MariaDB
  `digest`) are distinguished by `scope`. Consumers (Analyzer, #530) must not
  cross-equate identities.
- **WMM-R003** (enforced) | The sample/normalized SQL accompanying a workload
  identity is redacted by default: PostgreSQL statement text
  (`RedactStatementSecrets`), MySQL/MariaDB `DIGEST_TEXT`, SingleStore
  `QUERY_TEXT`, ClickHouse `query`. The non-sensitive identity (hash/id) is
  retained.
- **WMM-R004** (deferred model) | The additive normalized envelope — `source`,
  per-field `units`, `scope`, `observed_from`/`observed_to`, reset/uptime markers,
  and the cumulative/interval/event classification — will accompany the native
  payload without loss. It is NOT emitted yet; it is gated on the #530 Analyzer
  contract agreement. Native evidence is carried today.

## Invariants

- **INV-WMM-01** | A workload identity value is never presented as equatable with
  one from a different engine (distinct `scope`).
- **INV-WMM-02** | No raw or sample SQL text leaves the instance in a default
  (non-high-sensitivity) collection (WMM-R003); only the non-sensitive typed
  identity is retained.
- **INV-WMM-03** | Native engine payload/evidence always accompanies (is never
  replaced by) any normalized representation.

## Failure conditions

| Trigger | Response |
|---------|----------|
| A workload collector drops its typed identity column | Guard test fails (WMM-R001 regression). |
| A sample-SQL column escapes redaction | Guard / per-engine redaction test fails (WMM-R003 / INV-WMM-02). |
| A consumer equates two engines' identities | Model violation (WMM-R002) — consumer-side, owned by Analyzer. |

## Traceability

Acceptance: `specifications/workload-measurement-model.acceptance.md`. WMM-R001 /
WMM-R003 + INV-WMM-01/02 are CI-gated by the cross-engine workload-identity guard
(`internal/export/workload_model_test.go`) plus each engine's redaction test.
WMM-R002 (never-equated) + WMM-R004 (the deferred envelope) are model/consumer
obligations tracked with the #530 report contract. Ties to
`multi-engine-report-contract.md` (§3 workload) and each engine's collector spec.
