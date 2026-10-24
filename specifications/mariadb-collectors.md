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

## MariaDB vs MySQL — catalog differences (confirmed live)

- `GLOBAL_VARIABLES` / `GLOBAL_STATUS` live in **`information_schema`** on MariaDB
  (MySQL 8 moved them to `performance_schema`, which MariaDB leaves OFF by
  default). The MariaDB catalog reads `information_schema.GLOBAL_*`.
- `information_schema.INNODB_METRICS` and the `performance_schema` replication
  views are not available under the baseline `SELECT, PROCESS` role / default
  config, so the MariaDB catalog omits them. Every MariaDB collector succeeds
  under the baseline role.

## Validated catalog (columns confirmed live against MariaDB 11.4)

| ID | Category | Source |
|----|----------|--------|
| `mariadb_identity_v1` | server | `SELECT VERSION(), @@version_comment` |
| `mariadb_global_variables_v1` | config | `information_schema.GLOBAL_VARIABLES` (IN-list) |
| `mariadb_global_status_v1` | resources | `information_schema.GLOBAL_STATUS` (IN-list) |
| `mariadb_processlist_v1` | activity | `information_schema.PROCESSLIST` — **HighSensitivity**, `INFO` redacted |
| `mariadb_table_statistics_v1` | schema | `information_schema.TABLES` (excl. system schemas) |
| `mariadb_engines_v1` | server | `information_schema.ENGINES` |
| `mariadb_schemata_v1` | schema | `information_schema.SCHEMATA` |

## Rules

- **MDC-R001** | Every catalog statement is a read-only `SELECT`; no writes.
- **MDC-R002** | A collector whose statement errors records `Err` and does not
  abort the others (partial collection captured, not fatal).
- **MDC-R003** | With `redactSensitive=true`, each `SensitiveColumns` value is nil
  in every row of that collector's result.
- **MDC-R004** | Any collector selecting a query-text column (`INFO`) MUST be
  `HighSensitivity` with that column in `SensitiveColumns`.
- **MDC-R005** | `CatalogFor(mariadb)` returns exactly `mariadbCatalog()`; no
  cross-engine leak, no PostgreSQL fallback.

## Invariants

- **INV-MDC-01** | Collection never issues a non-`SELECT` statement.
- **INV-MDC-02** | `mariadb_processlist_v1` is `HighSensitivity` with `INFO` in
  `SensitiveColumns`.
- **INV-MDC-03** | No MariaDB collector selecting a query-text column escapes the
  HighSensitivity + redaction contract.

## Failure conditions

| Trigger | Response |
|---------|----------|
| A collector's SQL errors (missing view / permission) | Captured in `Result.Err`; other collectors still run (MDC-R002). |

## Constraints / NFR

- Read-only; no writes; no superuser. Monitoring role = `GRANT SELECT, PROCESS
  ON *.*` (see `specifications/mariadb-grants.md`).
- Columns confirmed live; regenerate against the pinned MariaDB image if the
  server version changes shape.

## Traceability

Acceptance: `specifications/mariadb-collectors.acceptance.md`.
Matrix: `features/signals/traceability.md` (MDC-R001..R005, INV-MDC-01..03).
