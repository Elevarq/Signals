# SingleStore Engine Identity Resolution

## Status

ACTIVE

## Purpose

Positively identify a database server reached over the **MySQL wire
protocol** as either **SingleStore** or **MySQL**, capturing the engine's
own version, so that a Signals snapshot can declare its engine identity
explicitly. SingleStore speaks the MySQL wire protocol, so a shared
connector reaches both — but the two must never be confused
(Elevarq/Signals#526: *"Never report SingleStore as MySQL"*).

This specification covers the **pure identity-resolution rule** only: given
the values of a small set of server variables, decide the engine and
version, or fail closed. Live connectivity (the MySQL-wire connector that
reads those variables) and persistence into the snapshot envelope are
specified and wired separately; this rule is the testable core both depend
on.

PostgreSQL is **out of scope** here — PostgreSQL is reached over its own
protocol and identified by the existing `pgqueries.Discovery` path. This
resolver runs **only** on MySQL-wire connections and never sees a
PostgreSQL server.

## Scope

A single pure function that maps a probe of MySQL-wire server variables to
an engine identity:

```
Resolve(Probe) -> (engine, version, error)
```

`engine` is one of the engine constants (`singlestore`, `mysql`); an
indeterminate probe yields an error and no engine (fail closed).

## Interfaces

**Input — `Probe`** (values already read from the server; empty string
means "variable absent or unreadable"):

| Field | Source | Notes |
|-------|--------|-------|
| `MemsqlVersion` | `SELECT @@memsql_version` | Present **only** on SingleStore; empty elsewhere. |
| `VersionComment` | `SELECT @@version_comment` | e.g. `SingleStore`, `MemSQL`, `MySQL Community Server - GPL`, `mariadb.org binary distribution`. |
| `Version` | `SELECT VERSION()` | MySQL-compatible version string; **cannot** distinguish SingleStore from MySQL on its own. |

**Output:** `engine` (constant string), `version` (best available engine
version string), `error` (non-nil only on indeterminate identity).

**Engine constants:** `EnginePostgreSQL = "postgresql"`,
`EngineMySQL = "mysql"`, `EngineSingleStore = "singlestore"`.

## Behaviors

- **Given** a probe where `MemsqlVersion` is non-empty, **when** `Resolve`
  runs, **then** engine is `singlestore` and version is `MemsqlVersion`.
- **Given** a probe where `MemsqlVersion` is empty but `VersionComment`
  names SingleStore/MemSQL, **when** `Resolve` runs, **then** engine is
  `singlestore` and version is the `Version` string.
- **Given** a probe whose `VersionComment`/`Version` indicate MySQL or
  MariaDB and carry **no** SingleStore signal, **when** `Resolve` runs,
  **then** engine is `mysql`.
- **Given** a probe with no recognizable signal, **when** `Resolve` runs,
  **then** it returns an error and no engine.

## Rules

| ID | Rule |
|----|------|
| **SS-ID-R001** | If `MemsqlVersion` is non-empty → `singlestore`, version = `MemsqlVersion`. This signal wins over everything else. |
| **SS-ID-R002** | Else if `VersionComment` or `Version` contains (case-insensitive) `singlestore` or `memsql` → `singlestore`, version = `Version` (fallback `VersionComment`). |
| **SS-ID-R003** | Else if `VersionComment` or `Version` contains (case-insensitive) `mysql` or `mariadb` → `mysql`, version = `Version` (fallback `VersionComment`). |
| **SS-ID-R004** | Else → error `indeterminate engine identity`; return no engine (fail closed). |

## Invariants

| ID | Invariant |
|----|-----------|
| **INV-SSID-01** | `singlestore` is returned **only** when a SingleStore-specific signal is present (SS-ID-R001 or the SingleStore branch of SS-ID-R002). Never inferred from a MySQL-compatible `VERSION()` alone. |
| **INV-SSID-02** | A server exposing a non-empty `@@memsql_version` is **never** classified as `mysql`. |
| **INV-SSID-03** | An indeterminate probe is **never** guessed — it is always an error. |

## Failure conditions

| Trigger | Response |
|---------|----------|
| All probe fields empty / unreadable | Error: `indeterminate engine identity`; no engine returned. |
| Only a MySQL-compatible `Version` with an empty/unrecognized `VersionComment` and no `MemsqlVersion` | Error (SS-ID-R004) — we do not assume MySQL from a bare version string. |

## Constraints

- Pure function: no I/O, no DB, no global state; deterministic for a given
  `Probe`.
- Case-insensitive substring matching on `VersionComment`/`Version`.
- No secret material appears in inputs or outputs (version strings only).

## NFR overrides

- **Correctness over coverage:** when in doubt, fail closed (INV-SSID-03) —
  a wrong engine label is worse than no label.
- **PostgreSQL unaffected:** this rule is additive and never runs on the
  PostgreSQL path; existing PostgreSQL identity/behavior is unchanged.

## Traceability

- Parent: Elevarq/Signals#526 (SingleStore support), #548 (connectivity +
  engine detection), #529 (engine identity model), #531 (persist engine +
  version identity).
- Acceptance: `singlestore-engine-identity.acceptance.md`.
- Next increments (separate specs): MySQL-wire connector (#535) that reads
  the `Probe`; snapshot-envelope persistence of `engine` + `engine_version`
  (#531); live Dev Image qualification (#555).
