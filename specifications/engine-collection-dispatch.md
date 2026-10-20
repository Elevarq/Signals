# Engine Collection Dispatch

## Status

ACTIVE

## Purpose

Make Signals' collection cycle engine-aware: decide, per target, which
collection backend applies, and ensure the existing PostgreSQL collection path
(`collectTarget`, which connects with `pgx`) is **only ever used for
PostgreSQL targets**. A non-PostgreSQL target (e.g. SingleStore over the MySQL
wire) must never be connected with `pgx`; it is skipped by the PostgreSQL cycle
with a clear, audited outcome and handled by the engine-specific path instead.

This is the dispatch seam that lets the SingleStore collection path be wired in
later without perturbing — or being mis-handled by — the PostgreSQL cycle
(Elevarq/Signals#529/#548, under epic #524).

## Scope

- `SelectBackend(TargetConfig) Backend` — a pure mapping from a target's
  effective engine to a collection backend.
- A guard at the top of `collectTarget` that returns early (skip + audit) when
  the target's backend is not PostgreSQL.

## Interfaces

- **`Backend`** enum: `BackendPostgreSQL`, `BackendMySQLWire`.
- **`SelectBackend(t) Backend`:**
  - `EffectiveEngine()` `postgresql` (incl. empty default) → `BackendPostgreSQL`.
  - `mysql` / `singlestore` → `BackendMySQLWire`.

## Behaviors

- **Given** a PostgreSQL (or engine-unset) target, **when** the cycle runs,
  **then** `collectTarget` proceeds exactly as before (pgx collection).
- **Given** a `singlestore` target, **when** the cycle runs, **then**
  `collectTarget` skips it early — before any pgx connection — and records an
  audited `collection_skipped_engine` outcome.

## Rules

| ID | Rule |
|----|------|
| **ECD-R001** | `SelectBackend` maps `postgresql`/empty → `BackendPostgreSQL`; `mysql`/`singlestore` → `BackendMySQLWire`. |
| **ECD-R002** | `collectTarget` runs only `BackendPostgreSQL` targets; a non-PostgreSQL target returns early with an audited skip and no pgx connection. |

## Invariants

| ID | Invariant |
|----|-----------|
| **INV-ECD-01** | `pgx` is never used to open a connection to a non-PostgreSQL target. |
| **INV-ECD-02** | PostgreSQL targets (engine unset or `postgresql`) are collected exactly as before — no behavior change. |

## Failure conditions

| Trigger | Response |
|---------|----------|
| A non-PostgreSQL target reaches the PostgreSQL cycle | Early skip + `collection_skipped_engine` audit; `collectTarget` returns nil (not an error — it is simply not this backend's work). |

## Constraints

- `SelectBackend` is pure and deterministic; no I/O.
- The guard adds no new import and no behavior for PostgreSQL targets.

## NFR overrides

- **Safety:** prevents a pgx connection to a non-PostgreSQL server (INV-ECD-01).
- **Compatibility:** PostgreSQL collection is untouched (INV-ECD-02).

## Traceability

- Parent: Elevarq/Signals#529 (engine model), #548 (SingleStore connect), epic
  #524. Uses `config.EffectiveEngine` and the `engineid` constants.
- Acceptance: `engine-collection-dispatch.acceptance.md`.
- Next: wire `BackendMySQLWire` to the SingleStore collection path
  (mysqlwire + SingleStore collectors #550/#551/#553).
