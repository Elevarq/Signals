# ClickHouse Live Qualification

## Status

ACTIVE

## Purpose

Prove, repeatably and **in CI**, that the normal Signals producer path — the real
`cmd/signals` daemon collecting a real ClickHouse server, persisting through the
shared store, and exporting through the production exporter — produces truthful,
successful evidence for every expected ClickHouse collector. This is the ClickHouse analog
of the SingleStore live qualification
(`specifications/singlestore-live-qualification.md`), reusing the same
engine-generic verifier; only the catalog, engine string, and version prefix
differ.

Unlike SingleStore (whose multi-GB Dev Image kept its live qualification
opt-in/local), ClickHouse's official image is a lightweight CI **service container**,
so the ClickHouse live qualification runs as a pinned CI job (`integration-clickhouse`)
wired into the PostgreSQL-mandatory terminal gate (`ci-gate.needs`), and is also
runnable locally.

## Coverage receipt — live vs fixture (explicit)

- **Live producer evidence** runs BOTH in CI (`integration-clickhouse`, a pinned ClickHouse
  service container) and locally (`scripts/clickhouse-live-qual.sh` against the pinned
  ClickHouse Dev Image via `scripts/clickhouse-dev-fixture.sh`). The `//go:build clickhouse_live`
  verifier asserts the artifacts the real daemon + exporter produced.
- **Deterministic (no-Docker) CI coverage** of the shared verifier logic and the
  ClickHouse export serialization already exists: `internal/collector/singlestore_qual_test.go`
  (fails-loudly / zero-row / reconciliation) and
  `tests/mariadb_export_serialization_test.go` (a ClickHouse snapshot through the
  production `export.Builder`). These are serialization/verifier coverage, not
  producer evidence.

## Interfaces

- **Environment** — `scripts/clickhouse-dev-fixture.sh {up|down}`: the pinned ClickHouse
  Dev Image (`clickhouse/clickhouse-server@sha256:1ffa82edee000a42c09313bd9f1293d94c570aee74babc1b3ca9983a35fa597b…`, `version()` 24.8.x) seeded by
  `examples/clickhouse-init.sql` (read-only `signals_ro` with `SELECT ON system.*` +
  deterministic schema/workload). A dedicated container name so a developer's own
  ClickHouse container is never touched. In CI the ClickHouse is a service container seeded
  by a job step; the fixture script is the local path.
- **Producer** — `cmd/signals` + `cmd/signalsctl` (`collect now --force`,
  `export`), configured by `examples/clickhouse-daemon.yaml`.
- **Verifier** — the shared `internal/collector/singlestore_qual.go`
  (`VerifyCadenceSnapshot(s, expected, engine, versionPrefix)`,
  `VerifyExportReconciliation`, `ExpectedCollectorsFor("clickhouse")`,
  `ReadQualSnapshots`, `ReadQualExport`) — engine-generic.
- **Orchestration + live assertion** — `scripts/clickhouse-live-qual.sh` drives the
  real daemon; `internal/collector/clickhouse_live_test.go` (`//go:build clickhouse_live`)
  reads `CH_QUAL_*` and runs the verifiers against live artifacts.

## Behaviors

- **Given** the seeded live ClickHouse and a `SELECT ON system.*` role, **when** the
  daemon runs a cycle, **then** all nine ClickHouse collectors persist
  `status='success'` — `mariadb_replication_status_v1` is a legitimate zero-row
  success on a standalone server — each reconciled to a `query_results` row whose
  decoded row count equals `query_runs.row_count`.
- **Given** two cycles + a daemon restart, **when** snapshots are enumerated,
  **then** each is a distinct ULID (no overwrite), `engine=clickhouse`, version
  `8.x`, and the restart appends.
- **Given** one exact snapshot exported through the production exporter, **then**
  `metadata.json` declares `engine=clickhouse` + `engine_version`;
  `collector_status.json` / `query_catalog.json` contain only ClickHouse collectors;
  per successful collector collected == persisted == exported; `INFO` is redacted;
  the manifest covers every member.

## Rules

- **CHLQ-R001** — The producer is the normal `cmd/signals` daemon; qualification
  never calls `singlestore.Collect`/`CatalogFor` directly as a producer nor a
  standalone ZIP writer. (mirrors SLQ-R001)
- **CHLQ-R002** — Each enumerated ClickHouse snapshot passes `VerifyCadenceSnapshot`
  with `engine="clickhouse"`, `expected=ExpectedCollectorsFor("clickhouse")`,
  `versionPrefix="24."`: engine/version, exactly the expected collector set, every
  collector `status='success'`, reconciled result evidence, snapshot_id match.
- **CHLQ-R003** — A legitimate zero-row success (`mariadb_replication_status_v1` on a
  standalone server) passes; it is never a failure.
- **CHLQ-R004** — One exact snapshot passes `VerifyExportReconciliation`:
  metadata engine/version, no foreign PostgreSQL collector, collected == persisted
  == exported per collector, `INFO` redacted, manifest complete.
- **CHLQ-R005** — The ClickHouse live qualification runs in CI as a pinned
  `integration-clickhouse` job and is a dependency of the terminal `ci-gate` (so it
  routes through the same PostgreSQL-mandatory gate, #528 req #4).

## Invariants

- **INV-CHLQ-01** — Collection is read-only; the qualification issues no writes to
  ClickHouse beyond the one-time deterministic seed.
- **INV-CHLQ-02** — A dedicated container/service + isolated temporary Signals
  store; never deletes a developer's pre-existing ClickHouse container or store.
- **INV-CHLQ-03** — `status='success'` is the success signal; a row/snapshot
  existing, or a distinct-`query_id` count, is never sufficient.

## Failure conditions

- Any expected collector missing / non-`success` / lacking reconciled evidence →
  qualification error (loud failure).
- A foreign PostgreSQL collector in the export, a redaction leak, or a
  member/manifest mismatch → error.

## Constraints

- Pinned ClickHouse image by digest; version-prefix guard (`24.`). No cloud/AWS.
- The live test is `//go:build clickhouse_live` and SKIPs unless `CH_QUAL_*` is set
  by the orchestrator.

## Traceability

Acceptance: `specifications/clickhouse-live-qualification.acceptance.md`.
Matrix: `features/signals/traceability.md` (CHLQ-R001..R005, INV-CHLQ-01..03).
