# Engine Identity Foundation

## Status

ACTIVE

## Purpose

Give every collection target and persisted snapshot an explicit **engine
identity**, so later work can dispatch collection and reporting on it. This is
the identity *dimension* only — it adds no collection backend, no per-engine
collector catalog, and no reporting dispatch. PostgreSQL is the sole engine that
collects today; the identity defaults to PostgreSQL everywhere so existing
behaviour is byte-for-byte preserved.

Part of the multi-engine rebuild on `poc/singlestore` (atop the M0 PostgreSQL
regression lock, #528). The engine the wire protocol shares (MySQL-wire for
SingleStore/MySQL/MariaDB) is irrelevant here; this spec is purely about the
declared identity.

## Interfaces

### Inputs

- `TargetConfig.engine` (YAML `engine`, optional string). Permitted values are
  the members of `SupportedEngines`. Empty is permitted and means PostgreSQL.
- Reserved engine identifiers (constants), not all yet collectable:
  `postgresql`, `singlestore`, `mysql`, `mariadb`.

### Outputs

- `TargetConfig.EffectiveEngine()` → the configured engine, or `postgresql`
  when empty.
- `Snapshot.Engine` — the engine identity persisted with each snapshot.
- Export `metadata.json` carries a top-level `engine` field.

## Rules

- **EID-R001** — `EffectiveEngine()` returns `postgresql` when `engine` is
  empty, and the configured value otherwise.
- **EID-R002** — `ValidateStrict` rejects a non-empty `engine` that is not a
  member of `SupportedEngines`. For this milestone `SupportedEngines` admits
  only `postgresql`; the set grows as each engine's collection backend lands.
- **EID-R003** — the PostgreSQL collection path persists the target's effective
  engine (`postgresql`) on the snapshot it writes.
- **EID-R004** — export `metadata.json` carries `engine`, sourced from the
  scoped snapshot; an empty/legacy snapshot engine is reported as `postgresql`.

## Invariants

- **INV-EID-01** — an empty engine ALWAYS normalises to `postgresql`, in
  config, in persistence (legacy rows), and in export. There is no state in
  which an empty engine is surfaced to a consumer.
- **INV-EID-02** — introducing engine identity changes no PostgreSQL output
  semantically: a PostgreSQL export is identical except for the additive,
  always-`postgresql` `engine` field. Guarded by the M0 lock and a golden.

## Failure conditions

- A configured `engine` outside `SupportedEngines` → `ValidateStrict` returns a
  configuration error naming the unsupported engine (fail-closed; never a
  silent fallback to PostgreSQL).

## Constraints

- No collection backend, no `catalogFor` registry, no SingleStore collectors,
  no per-engine `collector_status`/`query_catalog` dispatch. Those are M2/M3.
- Migration `007_snapshot_engine.sql` adds the `engine` column with a
  `postgresql` default so existing rows and the forward schema agree.

## NFR overrides

None. The M0 PostgreSQL regression lock (integration-pg 14–18 + golden) is the
non-functional guarantee that this change does not regress PostgreSQL.
