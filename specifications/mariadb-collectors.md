# MariaDB Collectors

## Status

ACTIVE

## Purpose

Define the read-only diagnostic collector set for a **MariaDB** target, collected
over the shared MySQL-wire backend. MariaDB is added as a **catalog + registry
entry** only — dispatch, the wire session, identity resolution, and reporting are
engine-generic and unchanged (engine-collection-dispatch ECD-R008). Identity was
already made distinct from MySQL in M5 (`engineid` SS-ID-R003: a MariaDB server
resolves to `mariadb`, never `mysql`). This spec owns only the MariaDB catalog
SQL + handling metadata.

Every statement is a read-only `SELECT`, confirmed live against a MariaDB 11.4
server — not authored blind.

## Scope

- `mariadbCatalog() []Collector` (in `internal/singlestore`) and its registration
  in `CatalogFor(engine)` for `engine == mariadb`.
- `SelectBackend`: `mariadb → BackendMySQLWire`; `config.SupportedEngines`
  includes `mariadb`.

## MariaDB vs MySQL — catalog differences (confirmed live, #616)

Parity with the MySQL catalog is by **capability**, not by reusing MySQL's SQL —
MariaDB's system views genuinely differ (this is why the catalogs are separate):

- `GLOBAL_VARIABLES` / `GLOBAL_STATUS` live in **`information_schema`** on MariaDB
  (MySQL 8 moved them to `performance_schema`). The MariaDB catalog reads
  `information_schema.GLOBAL_*`. Galera/wsrep state is read the same way, from
  `information_schema.GLOBAL_STATUS`.
- InnoDB transactions + locks are in **`information_schema`**: `INNODB_TRX`,
  `INNODB_LOCKS`, `INNODB_LOCK_WAITS` (MySQL 8.0 removed `INNODB_LOCKS` /
  `INNODB_LOCK_WAITS` and uses `performance_schema.data_locks` /
  `data_lock_waits` instead — those do **not** exist on MariaDB).
  `information_schema.INNODB_METRICS` IS available under the baseline `SELECT,
  PROCESS` role (an earlier spec claim that it was unavailable was wrong —
  disproven live as `signals_ro` against MariaDB 11.4.13).
- `performance_schema` is **OFF by default** on MariaDB, so
  `performance_schema.metadata_locks` and
  `performance_schema.events_statements_summary_by_digest` return an **empty
  result** (exit 0, no error) unless an operator has enabled perf_schema — they
  are perf_schema-gated, not absent. For the statement-digest workload signal
  (`mariadb_statement_digests_v1`) an empty table is ambiguous (perf_schema OFF
  vs perf_schema ON with no recorded statements), so it is disambiguated into a
  DEGRADED / NOT_ASSESSED signal (MDC-R010) rather than a silent success.
  **Prerequisite:** to collect the statement-digest workload signal the server
  must have `performance_schema=ON` (MariaDB: set `performance_schema=ON` in the
  server config / `my.cnf` and restart; it cannot be toggled at runtime). Signals
  never enables it — that is a server/deploy concern (for the demo, tracked in
  Elevarq/timeseries-demo).
- MariaDB has **Galera (wsrep)**, not Group Replication, and exposes replica
  state through **GTID server variables** (`gtid_current_pos`, `gtid_binlog_pos`,
  `gtid_slave_pos`), not a `SHOW SLAVE STATUS` table or the MySQL
  `performance_schema.replication_*` views.

## Validated catalog (columns confirmed live against MariaDB 11.4.13)

| ID | Category | Source |
|----|----------|--------|
| `mariadb_identity_v1` | server | `SELECT VERSION(), @@version_comment` |
| `mariadb_global_variables_v1` | config | `information_schema.GLOBAL_VARIABLES` (IN-list) | connection, durability + config-sizing tunables (buffer pool, log, temp/sort/join buffers, table/thread caches, io_capacity); version-tolerant IN-list (#660) |
| `mariadb_global_status_v1` | resources | `information_schema.GLOBAL_STATUS` (IN-list) |
| `mariadb_processlist_v1` | activity | `information_schema.PROCESSLIST` — **HighSensitivity**, `INFO` redacted |
| `mariadb_table_statistics_v1` | schema | `information_schema.TABLES` (excl. system schemas) |
| `mariadb_engines_v1` | server | `information_schema.ENGINES` |
| `mariadb_schemata_v1` | schema | `information_schema.SCHEMATA` |
| `mariadb_columns_v1` | schema | `information_schema.COLUMNS` (excl. system schemas, `ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION`) — #538 Replay Tier-1 evidence; per-column name, `COLUMN_TYPE` (DDL-faithful, engine-native: MariaDB retains `bigint(20)`/`int(11)`), `IS_NULLABLE`, ordinal, key, default, extra. Not HighSensitivity. |
| `mariadb_index_inventory_v1` | schema | `information_schema.STATISTICS` (excl. system schemas, `ORDER BY TABLE_SCHEMA, TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX`) — #538 Replay Tier-1 evidence; per-index name, ordered columns (`SEQ_IN_INDEX`), uniqueness (`NON_UNIQUE`). `INDEX_NAME='PRIMARY'` carries PK identity. `CARDINALITY` (Tier-2) NOT collected. Not HighSensitivity. |
| `mariadb_innodb_metrics_v1` | resources | `information_schema.INNODB_METRICS WHERE ENABLED=1` |
| `mariadb_innodb_transactions_v1` | activity | `information_schema.INNODB_TRX` (oldest-first, LIMIT 200) — **HighSensitivity**, `trx_query` redacted (lowercase result key) |
| `mariadb_innodb_locks_v1` | activity | `information_schema.INNODB_LOCKS` (LIMIT 500) — **HighSensitivity**, `lock_data` redacted (locked key value, lowercase result key) |
| `mariadb_innodb_lock_waits_v1` | activity | `information_schema.INNODB_LOCK_WAITS` (LIMIT 500) — trx/lock ids only |
| `mariadb_metadata_locks_v1` | activity | `performance_schema.metadata_locks` (PENDING-first, LIMIT 500) — perf_schema-gated (empty when off) |
| `mariadb_statement_digests_v1` | workload | `performance_schema.events_statements_summary_by_digest` (top-200 by latency) — **HighSensitivity**, `DIGEST_TEXT` redacted; **perf_schema-gated (`PerfSchemaGated`)**: a 0-row result with perf_schema OFF is recorded `skipped`/`perf_schema_disabled` (DEGRADED / NOT_ASSESSED, MDC-R010), not a silent success |
| `mariadb_replication_status_v1` | topology | GTID/binlog/read_only server variables (`@@global.server_id`, `log_bin`, `read_only`, `gtid_current_pos`, `gtid_binlog_pos`, `gtid_slave_pos`) |
| `mariadb_galera_status_v1` | topology | `information_schema.GLOBAL_STATUS` (`WSREP_*` IN-list) — Galera cluster state; standalone = `Disconnected`/`OFF`, a distinct legitimate state |

## Rules

- **MDC-R001** | Every catalog statement is a read-only `SELECT`; no writes.
- **MDC-R002** | A collector whose statement errors records `Err` and does not
  abort the others (partial collection captured, not fatal).
- **MDC-R003** | With `redactSensitive=true`, each `SensitiveColumns` value is nil
  in every row of that collector's result.
- **MDC-R004** | Any collector selecting a query-text column (`INFO`,
  `trx_query`, `DIGEST_TEXT`) MUST be `HighSensitivity` with that column — in its
  exact result-row case (`INFO`/`DIGEST_TEXT` uppercase, `trx_query` lowercase) —
  in `SensitiveColumns`.
- **MDC-R005** | `CatalogFor(mariadb)` returns exactly `mariadbCatalog()`; no
  cross-engine leak, no PostgreSQL fallback.
- **MDC-R006** | The activity collectors (`mariadb_innodb_transactions_v1`,
  `mariadb_innodb_locks_v1`, `mariadb_innodb_lock_waits_v1`,
  `mariadb_metadata_locks_v1`) are point-in-time session evidence, bounded by an
  explicit `LIMIT`; an idle server yields a legitimate zero-row success.
- **MDC-R007** | The topology / perf_schema-gated collectors return a legitimate
  zero-row success in their disabled states — standalone (no replica GTID),
  Galera not connected (`WSREP_READY=OFF`), or `performance_schema` off
  (`mariadb_metadata_locks_v1` empty). Each is distinct from a collector error or
  a permission denial; native fields are preserved verbatim when present.
  **Exception:** `mariadb_statement_digests_v1` no longer treats the
  perf_schema-off empty result as a bare success — it emits a DEGRADED /
  NOT_ASSESSED signal per MDC-R010, so a consumer can tell "the workload signal
  is unavailable because perf_schema is OFF" from "perf_schema is ON and there
  are no slow queries".
- **MDC-R008** | Sensitive payload beyond query text is redacted by the same
  mechanism: `mariadb_innodb_locks_v1` is `HighSensitivity` with `lock_data` (the
  locked index-key value — row data, lowercase result key) in `SensitiveColumns`.
  Native lock descriptors (mode, type, table, index) are preserved.
- **MDC-R010** | **performance_schema prerequisite (digest workload signal).**
  `mariadb_statement_digests_v1` reads
  `performance_schema.events_statements_summary_by_digest`, which is **empty
  whenever `performance_schema` is OFF** (the MariaDB default). A bare 0-row
  result is therefore ambiguous: it can mean "perf_schema ON, no statements
  recorded yet" (a legitimate empty success) OR "perf_schema OFF, the workload
  signal is unavailable" (NOT a success — the signal was never assessed). The
  collector MUST disambiguate these. It is flagged `PerfSchemaGated`, and when it
  returns **0 rows with no error** the runtime fires one cheap read-only probe
  `SELECT @@performance_schema AS performance_schema`. If the probe shows
  perf_schema **OFF** (value `0` / `OFF`), the collector run is recorded
  **`status=skipped`, `reason=perf_schema_disabled`** — a DEGRADED / NOT_ASSESSED
  signal, distinct from both a genuine empty success and a collector error. If
  the probe shows perf_schema **ON** (and the collector still returned 0 rows),
  the run stays a legitimate **`status=success`** ("no slow queries"). A
  collector that returned rows is never probed. The probe is read-only
  (`SELECT` only, INV-MDC-01 preserved) and runs at most once per
  digest-collector invocation (only on the 0-row path).
- **MDC-R011** | `perf_schema_disabled` is the canonical skip reason for the
  perf_schema-gated degrade (MDC-R010); it is pinned to
  `metrics.CollectorSkippedReasons`. The probe failing (e.g. a server that does
  not expose `@@performance_schema`, or a probe query error) does NOT turn the
  run into a failure — the run stays its pre-probe outcome (the 0-row success),
  because the probe is a diagnostic enrichment, not a correctness gate.
- **MDC-R009** | `mariadb_columns_v1` + `mariadb_index_inventory_v1` provide the
  Analyzer-Replay Tier-1 evidence (`replay-evidence-contract.md`): per-column
  name/type/nullability/ordinal and per-index name/ordered-columns/uniqueness,
  with `INDEX_NAME='PRIMARY'` carrying primary-key identity (no separate PK
  collector). Ordinal position and composite-index column ordering are preserved
  (never flattened to an unordered set). The acquisition SQL is identical to
  MySQL's (empirically verified equivalent across MySQL 8.0/8.4/9.7 and MariaDB
  10.11/11.4/11.8/12.3), but the collector identity is MariaDB's and `COLUMN_TYPE`
  is emitted engine-native — MariaDB keeps the integer display width
  (`bigint(20)`, `int(11)`) MySQL 8.0 dropped, never normalized to MySQL — so the
  DDL reconstructs correctly on MariaDB. Per-column / per-index selectivity and
  cardinality are Tier-2 and out of scope. Both collectors are schema metadata and
  are not `HighSensitivity`.

## Invariants

- **INV-MDC-01** | Collection never issues a non-`SELECT` statement.
- **INV-MDC-02** | `mariadb_processlist_v1` is `HighSensitivity` with `INFO` in
  `SensitiveColumns`.
- **INV-MDC-03** | No MariaDB collector selecting a query-text column (`INFO`,
  `trx_query`, `DIGEST_TEXT`) or locked-key data (`lock_data`) escapes the
  HighSensitivity + redaction contract (MDC-R004 / MDC-R008). The catalog-wide
  scan matches column names case-insensitively but each `SensitiveColumns` entry
  uses the exact result-row case so runtime redaction actually fires.

## Failure conditions

| Trigger | Response |
|---------|----------|
| A collector's SQL errors (missing view / permission) | Captured in `Result.Err`; other collectors still run (MDC-R002). |
| A standalone server (no replica GTID) | `mariadb_replication_status_v1` reports the standalone state (empty GTID positions), never a failure. |
| Galera not configured / not connected | `mariadb_galera_status_v1` reports `WSREP_READY=OFF` / `Disconnected`, a legitimate state, never a failure. |
| `performance_schema` off (MariaDB default) — `mariadb_metadata_locks_v1` | A successful zero-row result, never a failure. |
| `performance_schema` off (MariaDB default) — `mariadb_statement_digests_v1` | DEGRADED / NOT_ASSESSED: the 0-row result is probed (`SELECT @@performance_schema`) and recorded `status=skipped`, `reason=perf_schema_disabled` (MDC-R010), never a silent success and never a failure. Enabling perf_schema on the server is a deploy concern (tracked in Elevarq/timeseries-demo), out of Signals' scope. |
| `performance_schema` on, no statements recorded — `mariadb_statement_digests_v1` | A legitimate zero-row `success` ("no slow queries"); the probe shows perf_schema ON (MDC-R010). |
| The `@@performance_schema` probe itself errors | The run keeps its pre-probe 0-row `success` outcome; the probe never turns a success into a failure (MDC-R011). |
| Idle server (no transactions/locks) | The activity collectors return successful zero-row results. |
| Busy server | The activity collectors are bounded by `LIMIT` (MDC-R006). |

## Constraints / NFR

- Read-only; no writes; no superuser. Monitoring role = `GRANT SELECT, PROCESS
  ON *.*` (see `specifications/mariadb-grants.md`). `PROCESS` surfaces all
  sessions in `INNODB_TRX` / `PROCESSLIST`; no additional grant is required
  (confirmed live as `signals_ro` against MariaDB 11.4.13 — every collector ran
  clean).
- The activity collectors are bounded: `INNODB_TRX` LIMIT 200 (oldest first),
  `INNODB_LOCKS` / `INNODB_LOCK_WAITS` / `metadata_locks` LIMIT 500.
- Columns confirmed live; regenerate against the pinned MariaDB image if the
  server version changes shape.

## Traceability

Acceptance: `specifications/mariadb-collectors.acceptance.md`.
Matrix: `features/signals/traceability.md` (MDC-R001..R011, INV-MDC-01..03).
The Tier-1 evidence contract this serves is Analyzer's
`replay-evidence-contract.md` (#3891 follow-on); the live semantic qualification
that the evidence survives collection → persistence → export is
`TestMariaDBLiveReplayTier1Evidence` + `VerifyReplayTier1Evidence`.
