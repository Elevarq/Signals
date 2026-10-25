# ClickHouse Backend (CH-1 foundation)

## Status

ACTIVE

## Purpose

Add ClickHouse as a collectable engine. ClickHouse speaks neither the PostgreSQL
(pgx) nor the MySQL wire protocol, so it is a **third collection backend**
(`BackendClickHouse`) with its own transport, identity, and catalog — structurally
parallel to the pgx path, NOT an entry in the MySQL-wire `CatalogFor` registry.

CH-1 is the **backend foundation**: the native-protocol connection, engine
identity, backend dispatch, config support, and the catalog, all behind the SAME
common collection lifecycle and engine-neutral persistence / export / verifier
used by every other engine. The full `system.*` diagnostic catalog is delivered
by CH-2 (`specifications/clickhouse-collectors.md`); fixtures + live qualification
+ the `integration-clickhouse` CI job are CH-3. Design record: epic #527.

## Scope

- `internal/clickhouse`: `ConnConfig` + `Open` (ClickHouse native via
  `ClickHouse/clickhouse-go/v2`), `Identify` (version), `Collector`/`Result`,
  `Collect`, `NewRunner`, `IsPermissionDenied`, `Catalog()` (full set in CH-2,
  `specifications/clickhouse-collectors.md`).
- `SelectBackend`: `clickhouse → BackendClickHouse`; `config.SupportedEngines`
  includes `clickhouse`; `config.EngineClickHouse`.
- `collector`: `collectClickHouse` session (+ `chConnect` seam,
  `WithClickHouseConnectorForTest`), `mapClickHouseCollection`, the
  `BackendClickHouse` dispatch case.
- `export.catalogFor`: a `clickhouse` branch (the one reporting touch-point).

## Interfaces

- **`clickhouse.Open(ctx, ConnConfig) (*sql.DB, error)`** — native protocol
  (9000) via clickhouse-go; a non-ClickHouse server fails the handshake here.
- **`clickhouse.Identify(ctx, ScalarQuery) (version, error)`** — `SELECT
  version()`; a successful native connection answering a non-empty version IS
  the positive identification.
- **`collector.collectClickHouse`** — the ClickHouse engine session, behind the
  common lifecycle; a connection/identity failure is a target-scoped failure,
  never a silent skip.

## Behaviors

- **Given** an `engine=clickhouse` target, **when** `collectTarget` runs, **then**
  `SelectBackend` routes it to `BackendClickHouse` → `collectClickHouse`, which
  connects over the native protocol (never pgx / never MySQL wire), validates the
  server is ClickHouse, runs the ClickHouse catalog, and persists one
  `engine=clickhouse` snapshot through the common `InsertCollectionAtomic`.
- **Given** a successful collection, **when** exported, **then** `metadata.json`
  declares `engine=clickhouse` + `engine_version`, and `collector_status.json` /
  `query_catalog.json` contain only ClickHouse collectors (no foreign engine).

## Rules

- **CHB-R001** — `SelectBackend(clickhouse) == BackendClickHouse`; `clickhouse`
  is in `SupportedEngines` (config accepts it, ValidateStrict does not reject it).
- **CHB-R002** — The ClickHouse session opens a native connection via
  clickhouse-go built from the normal `TargetConfig` + the normal credential
  resolver — never pgx, never the MySQL wire, never a test-only mechanism in
  production.
- **CHB-R003** — The session validates ClickHouse identity (non-empty
  `version()`) before collecting; failure is a target-scoped collection failure.
- **CHB-R004** — A successful collection is persisted via the common
  `InsertCollectionAtomic` (snapshot `engine=clickhouse`, one run per collector,
  per-run NDJSON) and exported via the normal exporter; per-collector failures
  are failed/skipped runs (partial outcome), and a permission-denied error
  (`ACCESS_DENIED`/code 497) degrades to `skipped`, not `failed`.
- **CHB-R005** — Failed ClickHouse runs carry a canonical `reason`
  (`classifyRunError`: timeout / object_missing / execution_error); never empty.
- **CHB-R006** — `export.catalogFor(clickhouse)` returns the ClickHouse catalog
  ids, so a ClickHouse export's reporting files contain only ClickHouse
  collectors.

## Invariants

- **INV-CHB-01** — Collection never issues a non-`SELECT` statement (`HasWrite`
  false for every catalog SQL).
- **INV-CHB-02** — `pgx` and the MySQL-wire connector are never used for a
  ClickHouse target; a ClickHouse server is never collected via another backend.
- **INV-CHB-03** — PostgreSQL / SingleStore / MySQL / MariaDB behavior is
  unchanged; ClickHouse is purely additive (a new backend + a new dependency).

## Failure conditions

| Trigger | Response |
|---------|----------|
| Native connect/handshake fails (not ClickHouse, unreachable) | Target-scoped collection failure (CHB-R003). |
| Collector permission denied (ACCESS_DENIED / 497) | Run recorded `skipped`/`permission_denied` (CHB-R004). |
| Other collector error | Run recorded `failed` with a canonical reason (CHB-R005); others still run. |

## Constraints / NFR

- Read-only; the least-privilege grant model is owned by
  `specifications/clickhouse-grants.md` (#565): a baseline `SELECT` on the 11
  metadata `system.*` tables (no cross-user query visibility) plus an OPTIONAL,
  separately-approved `SELECT` on `system.query_log`/`query_thread_log` for the
  two query-workload collectors (which otherwise degrade to skipped). Writes are
  `ACCESS_DENIED`; no write/admin grant is ever required. Native-protocol TLS + CA
  (verify-ca / verify-full, fail-closed) is implemented in #558 —
  `specifications/clickhouse-connection-hardening.md` (CCH-R001..R005), sharing
  the `internal/dbtls` verify crypto with the MySQL-wire path.
- New module dependency `ClickHouse/clickhouse-go/v2` (pinned); it passes
  govulncheck (0 affecting vulnerabilities) and must clear the Security-Scan gate.

## Traceability

Acceptance: `specifications/clickhouse-backend.acceptance.md`.
Matrix: `features/signals/traceability.md` (CHB-R001..R006, INV-CHB-01..03).
