# Engine Collection Dispatch

## Status

ACTIVE

## Purpose

Make Signals' collection cycle engine-aware: each configured target is
collected through an **engine-specific session** behind a **common collection
lifecycle**, and a successful collection produces an **engine-aware snapshot**
through the **normal persistence/export pipeline**. PostgreSQL remains the
control and is collected exactly as before.

The PostgreSQL collection path (`collectTarget`, which connects with `pgx`) is
used **only** for PostgreSQL targets. A SingleStore target is collected through
its own session — a MySQL-wire connection (never `pgx`), SingleStore identity
validation, the existing SingleStore collectors, and engine-aware snapshot
assembly — then persisted and exported through the **same** lifecycle as
PostgreSQL.

This supersedes the earlier narrow form of this spec in which a non-PostgreSQL
target was **skipped** (`collection_skipped_engine`). A *supported* engine
(PostgreSQL, SingleStore) is now **dispatched to its session**, never skipped.
An *unsupported* engine still fails/skips per the explicit unsupported-engine
policy (ECD-R006).

This owns the behavioral invariant of Elevarq/Signals#532 (epic #524, #526):

> Every supported target is collected through an engine-specific session behind
> a common collection lifecycle; a successful collection produces an
> engine-aware `signals-snapshot.v1` through the normal persistence/export
> pipeline; PostgreSQL behavior remains compatible.

## Scope

- `SelectBackend(TargetConfig) Backend` — pure mapping from a target's effective
  engine to a collection backend.
- A dispatch at the engine boundary inside the common collection lifecycle that
  routes a target to its engine session (PostgreSQL inline path, or the
  SingleStore session) before the engine-specific connection is opened, and
  converges both on the common persistence/export.
- The SingleStore session: MySQL-wire connection lifecycle built from the normal
  `TargetConfig` + resolved credentials, SingleStore identity validation, the
  existing `singlestore.Catalog()`/`Collect()`, and mapping of results onto the
  engine-neutral persistence records.
- Engine-neutral persistence + engine-aware export (see Persistence contract).

## Interfaces

- **`Backend`** enum: `BackendPostgreSQL`, `BackendMySQLWire`.
- **`SelectBackend(t) Backend`:** `postgresql`/empty → `BackendPostgreSQL`;
  `mysql`/`singlestore` → `BackendMySQLWire`.
- **engine session** — a target-scoped collection implementation. PostgreSQL's
  session is the existing inline `collectTarget` body (not extracted — minimum
  change). SingleStore's session is `collectSingleStore(...)`, returning the
  engine-neutral records (`db.Snapshot`, `[]db.QueryRun`, `[]db.QueryResult`)
  the common persistence consumes.

## Persistence contract (engine-neutral)

The storage model — `snapshots` (collection event) → `query_runs` (one per
collector/query) → `query_results` (payload rows) → `query_catalog` — is
**engine-neutral**; the only PostgreSQL-shaped element is the `pg_version`
column name, carried forward as the engine-neutral version string. The minimal
extension is an `engine` discriminator on `snapshots` and an `engine`/
`engine_version` pair in the exported `metadata.json`. SingleStore `[]Result`
maps directly: one snapshot row, one `query_runs` row per collector
(`query_id` = collector id), one `query_results` row per run (NDJSON of rows),
`query_catalog` entries per collector. No PostgreSQL-shaped remodeling; no
parallel persistence/export path.

## Behaviors

- **Given** a PostgreSQL/engine-unset target, **when** the cycle runs, **then**
  `collectTarget` proceeds exactly as before (pgx) and persists/exports as
  before, with `metadata.json engine = "postgresql"`.
- **Given** a `singlestore` target, **when** the cycle runs, **then** the common
  lifecycle (circuit breaker, min-snapshot-interval, target registration)
  applies, the SingleStore session opens a MySQL-wire connection, validates it
  is SingleStore, runs the collectors, and the results are persisted + exported
  through the normal pipeline with `metadata.json engine = "singlestore"`.
- **Given** an unsupported engine, **when** the cycle runs, **then** it is
  skipped/failed per the explicit unsupported-engine policy (no SingleStore
  routing of a generic MySQL target).

## Rules

| ID | Rule |
|----|------|
| **ECD-R001** | `SelectBackend` maps `postgresql`/empty → `BackendPostgreSQL`; `mysql`/`singlestore` → `BackendMySQLWire`. |
| **ECD-R002** | `collectTarget` dispatches each supported target to its engine session behind the common lifecycle; a `singlestore` target is collected via its session (NOT skipped). The former `collection_skipped_engine` outcome no longer fires for a supported engine. |
| **ECD-R003** | The SingleStore session opens its connection with the MySQL-wire connector built from the normal `TargetConfig` + the normal credential resolver — never `pgx`, never a test-only DSN mechanism in production. |
| **ECD-R004** | The SingleStore session validates engine identity (the connection is positively SingleStore) before collecting; a non-SingleStore or unidentifiable server is a target-scoped collection failure, not a silent skip and not a MySQL fallback. |
| **ECD-R005** | A successful SingleStore collection is persisted via the common `InsertCollectionAtomic` (snapshot with `engine=singlestore`, one run per collector, per-run NDJSON payload, catalog entries) and exported via the normal exporter, yielding a `signals-snapshot.v1` whose `metadata.json` declares `engine`/`engine_version`. Per-collector failures are represented as failed/skipped runs (partial outcome), never dropping the whole snapshot. |
| **ECD-R006** | An unsupported engine is skipped/failed per the existing explicit unsupported-engine policy. A generic `mysql` target is NOT routed through the SingleStore session merely because the wire protocol is shared. |
| **ECD-R007** | The exporter emits `engine` and `engine_version` in `metadata.json` for every engine (PostgreSQL → `postgresql`), sourced from the persisted snapshot's engine identity. |

## Invariants

| ID | Invariant |
|----|-----------|
| **INV-ECD-01** | `pgx` is never used to open a connection to a non-PostgreSQL target. |
| **INV-ECD-02** | PostgreSQL targets (engine unset or `postgresql`) are collected, persisted, and exported exactly as before — no behavior change beyond the additive `engine` metadata field. |
| **INV-ECD-03** | A supported, enabled target never results in a silent skip: the outcome is a persisted snapshot (SUCCESS/PARTIAL) or an explicit target-scoped collection failure. |
| **INV-ECD-04** | Persistence and export are a single common pipeline for all engines — no engine-specific parallel persistence or export path. |

## Failure conditions

| Trigger | Response |
|---------|----------|
| SingleStore connection/identity fails | Target-scoped collection failure (explicit outcome, audited); no pgx, no silent skip. |
| A SingleStore collector errors | Represented as a failed run within the snapshot (partial outcome); the snapshot is still persisted/exported. |
| Unsupported engine | Explicit unsupported-engine skip/fail per existing policy. |

## Constraints

- `SelectBackend` is pure and deterministic; no I/O.
- PostgreSQL behavior changes only by the minimum extraction required to place
  it behind the dispatch; no opportunistic PG cleanup.
- No plugin framework; no generalized abstractions beyond what PostgreSQL +
  SingleStore require.

## NFR overrides

- **Safety:** no `pgx` to a non-PostgreSQL server (INV-ECD-01); credentials via
  the normal resolver, never persisted/logged.
- **Compatibility:** PostgreSQL collection/persistence/export untouched beyond
  the additive engine metadata (INV-ECD-02).

## Traceability

- Parent: Elevarq/Signals#532 (engine/session boundary), #529 (engine model),
  #548 (SingleStore connect), epic #524 / #526. Uses `config.EffectiveEngine`,
  the `engineid` constants, `mysqlwire`, and `singlestore.Catalog/Collect`.
- Acceptance: `engine-collection-dispatch.acceptance.md`.
- Supersedes the skip-only form (ECD-R002 formerly "skip non-PostgreSQL").
