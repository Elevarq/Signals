# SingleStore Snapshot Assembly (POC)

## Status

ACTIVE

## Purpose

Turn the SingleStore collector results (`singlestore-collectors.md`) into a
self-describing snapshot artifact that declares `engine=singlestore` — proving
the SingleStore collection path produces a real, packaged snapshot, not just
in-memory rows. For the POC this is a **standalone assembler** that reuses
`snapshot.Metadata`; unifying it with the production SQLite-backed export
pipeline (which is `pg_version`-centric) is a follow-up, not POC scope.

## Scope

- `Snapshot{ Metadata snapshot.Metadata; Collectors []Result }`.
- `BuildSnapshot(info BuildInfo, engine, version string, results []Result) Snapshot`
  — sets metadata (schema version, collector identity, timestamp, target) and
  applies engine identity via `Metadata.SetEngineIdentity`.
- Serializers: `MetadataJSON()`, `QueryResultsNDJSON()` (one line per
  collector), and `WriteZIP(io.Writer)` emitting `metadata.json`,
  `query_results.ndjson`, and a `manifest.json` of SHA-256 integrity hashes.

## Interfaces

- `type BuildInfo struct { CollectorVersion, CollectorCommit, CollectedAt string; Target snapshot.TargetInfo }`
- `MetadataJSON() ([]byte, error)` — the `metadata.json` bytes.
- `QueryResultsNDJSON() ([]byte, error)` — newline-delimited JSON, one object
  per collector: `{ "collector_id", "category", "rows": [...] }`.
- `WriteZIP(w io.Writer) error` — a ZIP with the two files plus
  `manifest.json` mapping each file name to its SHA-256.

## Behaviors

- **Given** collector results from a SingleStore instance, **when**
  `BuildSnapshot` runs with `engine="singlestore"`, **then** the metadata
  carries `engine=singlestore` + `engine_version`, `schema_version` is
  `signals-snapshot.v1`, and `pg_version` is empty.
- **Given** a built snapshot, **when** `MetadataJSON` is marshaled, **then** it
  contains an `"engine":"singlestore"` key.
- **Given** a built snapshot, **when** `WriteZIP` runs, **then** the archive
  contains `metadata.json`, `query_results.ndjson`, and `manifest.json`, and
  each manifest hash equals the SHA-256 of its file's bytes.

## Rules

| ID | Rule |
|----|------|
| **SSA-R001** | `BuildSnapshot` sets `Engine`/`EngineVersion` via `Metadata.SetEngineIdentity` (PostgreSQL identity would leave them empty; SingleStore sets them). |
| **SSA-R002** | `query_results.ndjson` has exactly one JSON object per collector result, in catalog order. |
| **SSA-R003** | `manifest.json` lists each emitted file with its SHA-256 computed over the exact bytes written. |

## Invariants

| ID | Invariant |
|----|-----------|
| **INV-SSA-01** | A SingleStore snapshot's `metadata.json` carries `engine=singlestore` and an empty `pg_version`. |
| **INV-SSA-02** | The manifest hash of each file matches that file's bytes (integrity is verifiable). |

## Failure conditions

| Trigger | Response |
|---------|----------|
| JSON marshal error on a result row | Propagated from the serializer as an error (no partial ZIP is returned). |

## Constraints / NFR

- Pure assembly over in-memory results; no DB, no network.
- `SchemaVersion` stays `signals-snapshot.v1` (engine identity is additive).
- Independent of and does not alter the PostgreSQL export pipeline.

## Traceability

- Parent: Elevarq/Signals #526/#531, builds on `singlestore-collectors.md` and
  `snapshot.Metadata.SetEngineIdentity` (postgresql-snapshot-compat.md).
- Acceptance: `singlestore-snapshot-assembly.acceptance.md`.
- Follow-up: fold into the production store/export pipeline with engine-aware
  `metadata.json` emission (removes the standalone assembler).
