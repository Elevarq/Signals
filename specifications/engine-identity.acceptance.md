# Acceptance Tests: Engine Identity Foundation

## Feature

`specifications/engine-identity.md`

## Test Cases

### TC-EID-01: Normal — empty engine resolves to PostgreSQL

**Rule:** EID-R001, INV-EID-01 — happy path / default.

**Given:** a `TargetConfig` with no `engine` set.
**When:** `EffectiveEngine()` is called.
**Then:** it returns `postgresql`.

### TC-EID-02: Normal — explicit supported engine is preserved

**Rule:** EID-R001.

**Given:** a `TargetConfig` with `engine: postgresql`.
**When:** `EffectiveEngine()` is called.
**Then:** it returns `postgresql`.

### TC-EID-03: Invalid — unsupported engine is rejected (fail-closed)

**Rule:** EID-R002, failure condition.

**Given:** a config whose target sets `engine: singlestore` (no collection
backend exists yet, so it is not in `SupportedEngines`).
**When:** `ValidateStrict` runs.
**Then:** it returns an error naming the unsupported engine; it does NOT
silently fall back to PostgreSQL.

### TC-EID-04: Boundary — legacy snapshot row with empty engine reads as PostgreSQL

**Rule:** EID-R004, INV-EID-01.

**Given:** a snapshot persisted before migration 007 (engine column empty/NULL
semantics), read back after migration.
**When:** the snapshot is loaded.
**Then:** its engine reads as `postgresql`.

### TC-EID-05: Normal — engine persists and round-trips through migration 007

**Rule:** EID-R003.

**Given:** a migrated store and a snapshot written with engine `postgresql`.
**When:** it is read back.
**Then:** the engine column is `postgresql`; migration 007 applied cleanly.

### TC-EID-06: Normal — export metadata.json carries engine

**Rule:** EID-R004.

**Given:** a PostgreSQL snapshot in export scope.
**When:** a snapshot ZIP is produced.
**Then:** `metadata.json` carries `"engine": "postgresql"`, and the export is
otherwise semantically identical to the pre-engine export (INV-EID-02).
