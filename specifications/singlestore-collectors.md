# SingleStore Collectors (POC)

## Status

ACTIVE

## Purpose

Collect read-only diagnostic evidence from a SingleStore instance over the
MySQL wire, producing collector results for a snapshot that declares
`engine=singlestore`. The collector SQL and result columns are **validated
against a live SingleStore workspace** (not authored blind) — see the
traceability note. Covers the POC slice of Elevarq/Signals #548 (identity),
#549 (topology), #550 (schema/storage), #551 (plancache workload), #552
(process list / activities), #553 (resources).

## Scope

- A SingleStore collector **catalog** — a set of `{ID, Category, SQL,
  HighSensitivity, SensitiveColumns}` entries over `information_schema`
  `MV_*` views and cluster views.
- A **runner** seam (`QueryRunner`) so collection is unit-testable without a
  live server, with a `*sql.DB`-backed implementation for the live path.
- `Collect` runs the catalog, returning per-collector rows (or a captured
  error), applying redaction to sensitive columns when high-sensitivity
  collection is disabled.

## Interfaces

- `type Collector struct { ID, Category, SQL string; HighSensitivity bool; SensitiveColumns []string }`
- `Catalog() []Collector` — the validated set.
- `type QueryRunner func(ctx, sql string) ([]map[string]any, error)`
- `NewRunner(*sql.DB) QueryRunner` — generic single-statement scan to maps.
- `Collect(ctx, QueryRunner, cat []Collector, redactSensitive bool) []Result`
  where `Result { CollectorID, Category string; Rows []map[string]any; Err string }`.

## Validated catalog (columns confirmed live)

| ID | Category | Source | Notes |
|----|----------|--------|-------|
| `singlestore_identity_v1` | server | `@@memsql_version`, `@@version_comment`, `VERSION()` | engine + version |
| `singlestore_aggregators_v1` | topology | `information_schema.AGGREGATORS` | HOST/PORT/ROLE/STATE/NODE_ID |
| `singlestore_leaves_v1` | topology | `information_schema.LEAVES` | HOST/PORT/AVAILABILITY_GROUP/PAIR_*/STATE |
| `singlestore_cluster_status_v1` | topology | `information_schema.MV_CLUSTER_STATUS` | NODE_ID/ROLE/STATE/POSITION |
| `singlestore_table_statistics_v1` | schema | `information_schema.TABLE_STATISTICS` | ROWS/MEMORY_USE/STORAGE_TYPE/PARTITION_TYPE |
| `singlestore_plancache_v1` | workload | `information_schema.MV_PLANCACHE` | **HighSensitivity**: `QUERY_TEXT` redacted on opt-out |
| `singlestore_global_variables_v1` | config | `information_schema.MV_GLOBAL_VARIABLES` | selected memory/connection vars |
| `singlestore_activities_v1` | resources | `information_schema.MV_ACTIVITIES` | CPU/elapsed/memory per activity |
| `singlestore_processlist_v1` | activity | `information_schema.MV_PROCESSLIST` | **HighSensitivity**: `INFO` (live statement text) redacted on opt-out |
| `singlestore_distributed_partitions_v1` | resources | `information_schema.DISTRIBUTED_PARTITIONS` | per-partition host/role/state |
| `singlestore_pipelines_v1` | workload | `information_schema.PIPELINES` | #618; native streaming ingest. **HighSensitivity**: `CONFIG_JSON` redacted (carries connection strings / credentials — Kafka/S3). Empty when no pipelines = legitimate success. |
| `singlestore_sysinfo_memory_v1` | resources | `information_schema.MV_SYSINFO_MEM` | #618; per-node host/cgroup/memsql memory bytes (distributed-native). Baseline grant. |
| `singlestore_sysinfo_disk_v1` | resources | `information_schema.MV_SYSINFO_DISK` | #618; per-node mount usage + cumulative IO. Baseline grant. |
| `singlestore_mv_events_v1` | activity | `information_schema.MV_EVENTS` | #618; cluster lifecycle events (severity/type/details). **Requires `CLUSTER`** → `ElevatedPrivilege: "CLUSTER"` (degrades to skip without it). DETAILS is event metadata (db name/status), not SQL or credentials. |
| `singlestore_nodes_v1` | topology | `information_schema.MV_NODES` | #618; per-node capacity (cpu/memory/disk/uptime/version), richer than aggregators/leaves. Baseline grant. |

## Behaviors

- **Given** a live SingleStore connection, **when** `Collect` runs the catalog,
  **then** each collector yields rows (or a captured, non-fatal error) and the
  identity collector reports a non-empty `@@memsql_version`.
- **Given** high-sensitivity collection is disabled, **when** the plancache
  collector runs, **then** `QUERY_TEXT` is NULLed in every persisted row while
  other columns survive (redact path).
- **Given** high-sensitivity collection is disabled, **when** the pipelines
  collector runs, **then** `CONFIG_JSON` is NULLed in every persisted row while
  pipeline name/state/user/time survive (#618 — no connection credentials leave
  the instance).
- **Given** a server with no pipelines and no cluster events, **when** the
  pipelines / events collectors run, **then** each is a successful zero-row
  result (not a failure).
- **Given** the baseline `SELECT, PROCESS` role without `CLUSTER`, **when**
  `singlestore_mv_events_v1` runs, **then** its permission error is captured as a
  per-collector skip (ElevatedPrivilege degrade), not a fatal failure.

## Rules

| ID | Rule |
|----|------|
| **SSC-R001** | Every catalog statement is a read-only `SELECT`; no writes (Signals safety). |
| **SSC-R002** | A collector whose statement errors records `Err` and does not abort the others (partial collection is captured, not fatal). |
| **SSC-R003** | With `redactSensitive=true`, each `SensitiveColumns` value is set to nil in every row of that collector's result. |
| **SSC-R004** | Any collector that selects a query-text column (`QUERY_TEXT`, `INFO`) MUST be `HighSensitivity` with that column in `SensitiveColumns` (query-text handling, #554). |
| **SSC-R005** | Any collector that selects a secret-bearing config column — currently `CONFIG_JSON` (`PIPELINES`), which can embed connection strings / credentials — MUST be `HighSensitivity` with that column in `SensitiveColumns`. Operational fields (pipeline name/state/user/time) are preserved; only the secret payload is nilled under default collection (#618). |
| **SSC-R006** | A collector needing a privilege beyond the baseline `SELECT, PROCESS` declares it in `ElevatedPrivilege`; `singlestore_mv_events_v1` needs `CLUSTER` (confirmed live) and so degrades to a per-collector skip, not a fault, under the baseline role (see `singlestore-grants.md` SSG-R002/R003). |

## Invariants

| ID | Invariant |
|----|-----------|
| **INV-SSC-01** | Collection never issues a non-`SELECT` statement. |
| **INV-SSC-02** | `singlestore_plancache_v1` is marked HighSensitivity with `QUERY_TEXT` in SensitiveColumns (it carries application SQL text). |
| **INV-SSC-03** | No collector selecting a query-text column escapes the HighSensitivity + redaction contract (SSC-R004) — guards new collectors against leaking SQL text. |
| **INV-SSC-04** | No collector selecting a secret-bearing config column (`CONFIG_JSON`) escapes the HighSensitivity + redaction contract (SSC-R005) — guards against leaking pipeline connection credentials. |

## Failure conditions

| Trigger | Response |
|---------|----------|
| A collector's SQL errors (missing view / permission) | Captured in `Result.Err`; other collectors still run (SSC-R002). |

## Constraints / NFR

- Read-only, bounded by the caller's context/timeout.
- Column set fixed to validated columns; no `SELECT *` in persisted collectors.
- PostgreSQL path unaffected (separate package).

## Traceability

- Parent: Elevarq/Signals #548/#549/#550/#551/#553, epic #526. Builds on
  `singlestore-wire-connect.md` (connection + `Identify`).
- **Validation:** catalog SQL + columns verified live against a SingleStore
  S-00 workspace (US East 1) via the integration harness; see
  `singlestore-collectors.acceptance.md` TC-SSC-LIVE.
- Acceptance: `singlestore-collectors.acceptance.md`.
