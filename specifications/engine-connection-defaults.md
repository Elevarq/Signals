# Engine-Aware Connection Defaults

## Status

ACTIVE

## Purpose

Define engine-aware connection defaults for configured targets so a correctly
declared non-PostgreSQL engine never inherits PostgreSQL connection settings
(Elevarq/Signals #623, audit finding F4). This is the cross-engine umbrella for
the default-port / default-database behavior that `singlestore-connection-hardening.md`
(SCH-R006) touched only for SingleStore; it governs all five engines for both the
environment-variable and YAML configuration paths.

Signals stays read-only and local-first. PostgreSQL behavior is unchanged: an
omitted engine is PostgreSQL (`EffectiveEngine`), keeps port 5432, and still
requires a database. Engine is never inferred from port/host/database.

## Scope

- `config.DefaultPortForEngine(engine) int` and its application to both the env
  target path and the YAML path (targets with an unset port).
- The environment target database default (no PostgreSQL leak to other engines).
- `ValidateStrict` database-required rule, made engine-aware.

Out of scope: inferring engine from connection shape (forbidden); changing the
connectors; the collector catalogs.

## Protocol default ports

| Engine | Default port | Protocol |
|--------|-------------|----------|
| PostgreSQL (and empty/legacy) | 5432 | PostgreSQL wire |
| MySQL | 3306 | MySQL wire |
| MariaDB | 3306 | MySQL wire |
| SingleStore | 3306 | MySQL wire |
| ClickHouse | 9000 | ClickHouse native |

## Database default

- PostgreSQL requires a database to connect; an omitted database defaults to
  `postgres` (the universal PostgreSQL default) — unchanged legacy behavior.
- MySQL / MariaDB / SingleStore / ClickHouse connect **server-wide** for
  monitoring (every collector statement is a fully-qualified `information_schema`
  / `performance_schema` / `system.*` read), so a database is **optional**. An
  omitted database is left empty — never defaulted to `postgres`. An explicit
  database is always honored (e.g. for a scoped connection).

## Rules

- **ECONN-R001** | When a target's port is unset (0), it defaults to
  `DefaultPortForEngine(EffectiveEngine())` (table above). An explicitly
  configured port always wins. Applied to both the env and YAML target paths so
  equivalent values yield equivalent resolved targets.
- **ECONN-R002** | The environment target database defaults to `postgres` ONLY
  when the effective engine is PostgreSQL (engine empty/omitted or `postgresql`).
  A non-PostgreSQL engine never silently receives database `postgres`.
- **ECONN-R003** | `dbname` is required (a hard validation error) only for
  PostgreSQL targets. A non-PostgreSQL target may omit it (server-wide
  monitoring). An explicit `dbname` is always honored for any engine.
- **ECONN-R004** | Engine is never inferred from port, host, or database name
  (#623 non-goal). An unsupported explicit engine is rejected by `ValidateStrict`
  (EID-R002), never downgraded to PostgreSQL.

## Invariants

- **INV-ECONN-01** | A non-PostgreSQL target never silently receives port 5432 or
  database `postgres`.
- **INV-ECONN-02** | Explicit user configuration (port, database, engine) always
  overrides a default.
- **INV-ECONN-03** | An omitted engine resolves to PostgreSQL with port 5432 and
  database `postgres` (legacy backward compatibility, unchanged).

## Behaviors

- **Given** `SIGNALS_TARGET_ENGINE=mysql` (or mariadb/singlestore) and no port,
  **when** the env config loads, **then** the target port is 3306 and the
  database is empty (not `postgres`).
- **Given** `SIGNALS_TARGET_ENGINE=clickhouse` and no port, **when** the env
  config loads, **then** the target port is 9000 (ClickHouse native) and the
  database is empty.
- **Given** an explicit `SIGNALS_TARGET_PORT` / `SIGNALS_TARGET_DBNAME`, **when**
  the env config loads, **then** those values win regardless of engine.
- **Given** a YAML target with an engine and no port, **when** the config loads,
  **then** the resolved port is the engine default (equivalent to the env path).
- **Given** a non-PostgreSQL target with no database, **when** `ValidateStrict`
  runs, **then** it is NOT a hard error; a PostgreSQL target with no database
  still is.

## Failure conditions

| Trigger | Response |
|---------|----------|
| Non-ASCII/invalid `SIGNALS_TARGET_PORT` | Hard error from `parseEnvInt` (unchanged). |
| PostgreSQL target with empty dbname | `ValidateStrict` hard error (dbname required for PostgreSQL). |
| Unsupported explicit engine | `ValidateStrict` hard error (EID-R002); never downgraded. |

## Traceability

Acceptance: `specifications/engine-connection-defaults.acceptance.md`. Supersedes
the SingleStore-only default-port note in `singlestore-connection-hardening.md`
(SCH-R006) by generalizing it to all five engines. Ties to `engine-identity.md`
(EID / `EffectiveEngine`).
