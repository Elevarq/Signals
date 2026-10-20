# Engine-aware Target Configuration

## Status

ACTIVE

## Purpose

Let a collection target declare which database engine it is, so engine-aware
connection and collection can be selected per target. The field defaults to
PostgreSQL, so every existing single-engine configuration is unchanged. This
is the configuration-layer foundation for engine identity (Elevarq/Signals#529)
and for persisting engine identity in the snapshot (#531), and it gates the
later wiring of the MySQL-wire connector into the collector.

## Scope

- `TargetConfig.Engine` (YAML `engine`) — optional per-target engine.
- `(TargetConfig).EffectiveEngine()` — resolves the engine, defaulting to
  PostgreSQL when unset (mirrors `EffectiveAuthMethod`).
- `ValidateStrict` rejects a target whose `engine` is non-empty and not a
  supported engine (mirrors the `auth_method` keystone).

## Interfaces

- **Input:** `engine: <value>` in a target's YAML. Empty or absent → default.
- **`EffectiveEngine() string`:** returns the configured engine, or
  `EnginePostgreSQL` when unset.
- **Supported engines:** `SupportedEngines = {postgresql, mysql, singlestore}`
  (the `engineid` identity values).

## Behaviors

- **Given** a target with no `engine`, **when** `EffectiveEngine()` is called,
  **then** it returns `postgresql`, and `ValidateStrict` accepts it.
- **Given** a target with `engine: singlestore`, **when** validated, **then**
  `ValidateStrict` accepts it and `EffectiveEngine()` returns `singlestore`.
- **Given** a target with `engine: oracle`, **when** validated, **then**
  `ValidateStrict` returns a hard error naming the supported engines.

## Rules

| ID | Rule |
|----|------|
| **ETC-R001** | Empty/absent `engine` resolves to `EnginePostgreSQL`. |
| **ETC-R002** | A non-empty `engine` not in `SupportedEngines` is a `ValidateStrict` hard error listing the supported set (mirrors `auth_method`). |
| **ETC-R003** | A non-empty `engine` in `SupportedEngines` is accepted. |

## Invariants

| ID | Invariant |
|----|-----------|
| **INV-ETC-01** | A configuration with no `engine` on any target validates exactly as before and behaves as PostgreSQL (no behavior change for existing deployments). |
| **INV-ETC-02** | Engine matching is exact against the lowercase `engineid` constants — no normalization or fuzzy matching. |

## Failure conditions

| Trigger | Response |
|---------|----------|
| `engine` set to an unsupported value | `ValidateStrict` hard error: `target[i] (name): engine %q is not supported by this build (supported: …)`. |

## Constraints

- Additive YAML field; no change to existing keys.
- Pure validation / resolution; no I/O.

## NFR overrides

- **Compatibility:** PostgreSQL remains the default; existing configs are
  untouched (INV-ETC-01).

## Traceability

- Parent: Elevarq/Signals#529 (engine identity / target model), #531 (persist
  engine + version identity), epic #524. Uses `internal/engineid` constants.
- Acceptance: `engine-target-config.acceptance.md`.
- Next: wire `EffectiveEngine()` into target connection selection (the
  MySQL-wire connector for non-PostgreSQL engines).
