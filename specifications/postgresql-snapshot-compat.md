# PostgreSQL Snapshot Compatibility Under Engine Identity

## Status

ACTIVE

## Purpose

Protect PostgreSQL snapshot behavior as engine identity is added to the
snapshot envelope (Elevarq/Signals#528, the regression gate that must exist
*before* engine-dispatch changes). Specifically: the additive
`engine`/`engine_version` metadata fields must be **invisible to existing
PostgreSQL snapshots** — a PostgreSQL export stays byte-compatible with the
historical `signals-snapshot.v1` shape, and old v1 snapshots that predate the
fields remain readable.

This is the first, unit-level slice of #528. The broader #528 scope (live PG
14–18 fixture matrix, savepoint recovery, permission-degradation gates against
pinned disposable containers) is a separate integration unit; this slice locks
the snapshot-envelope contract that the Increment-1 schema change introduced.

## Scope

A single method that applies a resolved engine identity to snapshot metadata
while preserving PostgreSQL's historical shape:

```
(*Metadata).SetEngineIdentity(engine, version string)
```

plus the serialization guarantees the regression tests assert.

## Interfaces

- **Input:** a resolved `engine` (an `engineid` constant) and its `version`.
- **Effect on `Metadata`:**
  - PostgreSQL (`engineid.EnginePostgreSQL`) or empty → **no engine fields
    set** (`Engine` and `EngineVersion` stay empty); `PGVersion` continues to
    carry the PostgreSQL version via the existing path.
  - Non-PostgreSQL (`mysql`, `singlestore`) → `Engine` and `EngineVersion`
    are set; `PGVersion` is not used.

## Behaviors

- **Given** a PostgreSQL identity, **when** `SetEngineIdentity` runs, **then**
  `Engine` and `EngineVersion` remain empty, so the marshaled snapshot is
  identical to the historical v1 shape.
- **Given** a SingleStore identity, **when** `SetEngineIdentity` runs, **then**
  `Engine == "singlestore"` and `EngineVersion` is the engine version.
- **Given** a historical v1 metadata JSON with no engine fields, **when** it is
  unmarshaled, **then** it succeeds and `Engine` is empty (forward/backward
  compatible).

## Rules

| ID | Rule |
|----|------|
| **PGC-R001** | For PostgreSQL or an empty engine, `SetEngineIdentity` leaves `Engine` and `EngineVersion` empty (PostgreSQL output is never changed by the engine fields). |
| **PGC-R002** | For a non-PostgreSQL engine, `SetEngineIdentity` sets `Engine` and `EngineVersion` from the arguments. |

## Invariants

| ID | Invariant |
|----|-----------|
| **INV-PGC-01** | A PostgreSQL snapshot's marshaled JSON contains **no** `engine`/`engine_version` keys (the fields are `omitempty` and never set for PostgreSQL) — byte-compatible with `signals-snapshot.v1`. |
| **INV-PGC-02** | A historical `signals-snapshot.v1` metadata document (no engine fields) always unmarshals without error. |
| **INV-PGC-03** | The engine fields are additive only: their presence never removes or alters `PGVersion` or any existing v1 field. |

## Failure conditions

| Trigger | Response |
|---------|----------|
| (none — pure in-memory metadata shaping; no I/O, no failure modes) | n/a |

## Constraints

- Pure in-memory operation; no I/O, deterministic.
- `SchemaVersion` stays `signals-snapshot.v1` (additive change, no format bump).

## NFR overrides

- **Compatibility:** the historical PostgreSQL snapshot contract is preserved
  exactly (INV-PGC-01..03) — this is the guard that lets engine-dispatch land
  safely.
- **Safety:** read-only/local-first posture is unaffected (metadata shaping only).

## Traceability

- Parent: Elevarq/Signals#528 (protect PostgreSQL collection + historical
  report behavior), under epic #524. Builds on
  `specifications/singlestore-engine-identity.md`.
- Acceptance: `postgresql-snapshot-compat.acceptance.md`.
- Remainder of #528 (live PG 14–18 fixtures, savepoint/degradation gates) is a
  separate integration unit.
