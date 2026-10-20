# SingleStore Collectors (POC)

## Status

ACTIVE

## Purpose

Collect read-only diagnostic evidence from a SingleStore instance over the
MySQL wire, producing collector results for a snapshot that declares
`engine=singlestore`. The collector SQL and result columns are **validated
against a live SingleStore workspace** (not authored blind) — see the
traceability note. Covers the POC slice of Elevarq/Signals #548 (identity),
#549 (topology), #550 (schema/storage), #551 (plancache workload), #553
(resources).

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

## Behaviors

- **Given** a live SingleStore connection, **when** `Collect` runs the catalog,
  **then** each collector yields rows (or a captured, non-fatal error) and the
  identity collector reports a non-empty `@@memsql_version`.
- **Given** high-sensitivity collection is disabled, **when** the plancache
  collector runs, **then** `QUERY_TEXT` is NULLed in every persisted row while
  other columns survive (redact path).

## Rules

| ID | Rule |
|----|------|
| **SSC-R001** | Every catalog statement is a read-only `SELECT`; no writes (Signals safety). |
| **SSC-R002** | A collector whose statement errors records `Err` and does not abort the others (partial collection is captured, not fatal). |
| **SSC-R003** | With `redactSensitive=true`, each `SensitiveColumns` value is set to nil in every row of that collector's result. |

## Invariants

| ID | Invariant |
|----|-----------|
| **INV-SSC-01** | Collection never issues a non-`SELECT` statement. |
| **INV-SSC-02** | `singlestore_plancache_v1` is marked HighSensitivity with `QUERY_TEXT` in SensitiveColumns (it carries application SQL text). |

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
