# MySQL Live Qualification

## Status

ACTIVE

## Purpose

Prove, repeatably and **in CI**, that the normal Signals producer path — the real
`cmd/signals` daemon collecting a real MySQL server, persisting through the
shared store, and exporting through the production exporter — produces truthful,
successful evidence for every expected MySQL collector. This is the MySQL analog
of the SingleStore live qualification
(`specifications/singlestore-live-qualification.md`), reusing the same
engine-generic verifier; only the catalog, engine string, and version prefix
differ.

Unlike SingleStore (whose multi-GB Dev Image kept its live qualification
opt-in/local), MySQL's official image is a lightweight CI **service container**,
so the MySQL live qualification runs as a pinned CI job (`integration-mysql`)
wired into the PostgreSQL-mandatory terminal gate (`ci-gate.needs`), and is also
runnable locally.

## Coverage receipt — live vs fixture (explicit)

- **Live producer evidence** runs BOTH in CI (`integration-mysql`, a pinned MySQL
  service container) and locally (`scripts/mysql-live-qual.sh` against the pinned
  MySQL Dev Image via `scripts/mysql-dev-fixture.sh`). The `//go:build mysql_live`
  verifier asserts the artifacts the real daemon + exporter produced.
- **Deterministic (no-Docker) CI coverage** of the shared verifier logic and the
  MySQL export serialization already exists: `internal/collector/singlestore_qual_test.go`
  (fails-loudly / zero-row / reconciliation) and
  `tests/mysql_export_serialization_test.go` (a MySQL snapshot through the
  production `export.Builder`). These are serialization/verifier coverage, not
  producer evidence.

## Interfaces

- **Environment** — `scripts/mysql-dev-fixture.sh {up|down}`: the pinned MySQL
  Dev Image (`mysql@sha256:76f0358…`, `VERSION()` 8.4.x) seeded by
  `examples/mysql-init.sql` (read-only `signals_ro` with `SELECT, PROCESS` +
  deterministic schema/workload). A dedicated container name so a developer's own
  MySQL container is never touched. In CI the MySQL is a service container seeded
  by a job step; the fixture script is the local path.
- **Producer** — `cmd/signals` + `cmd/signalsctl` (`collect now --force`,
  `export`), configured by `examples/mysql-daemon.yaml`.
- **Verifier** — the shared `internal/collector/singlestore_qual.go`
  (`VerifyCadenceSnapshot(s, expected, engine, versionPrefix)`,
  `VerifyExportReconciliation`, `ExpectedCollectorsFor("mysql")`,
  `ReadQualSnapshots`, `ReadQualExport`) — engine-generic.
- **Orchestration + live assertion** — `scripts/mysql-live-qual.sh` drives the
  real daemon; `internal/collector/mysql_live_test.go` (`//go:build mysql_live`)
  reads `MYSQL_QUAL_*` and runs the verifiers against live artifacts.

## Behaviors

- **Given** the seeded live MySQL and a `SELECT, PROCESS` role, **when** the
  daemon runs a cycle, **then** all nine MySQL collectors persist
  `status='success'` — `mysql_replication_status_v1` is a legitimate zero-row
  success on a standalone server — each reconciled to a `query_results` row whose
  decoded row count equals `query_runs.row_count`.
- **Given** two cycles + a daemon restart, **when** snapshots are enumerated,
  **then** each is a distinct ULID (no overwrite), `engine=mysql`, version
  `8.x`, and the restart appends.
- **Given** one exact snapshot exported through the production exporter, **then**
  `metadata.json` declares `engine=mysql` + `engine_version`;
  `collector_status.json` / `query_catalog.json` contain only MySQL collectors;
  per successful collector collected == persisted == exported; `INFO` is redacted;
  the manifest covers every member.

## Rules

- **MLQ-R001** — The producer is the normal `cmd/signals` daemon; qualification
  never calls `singlestore.Collect`/`CatalogFor` directly as a producer nor a
  standalone ZIP writer. (mirrors SLQ-R001)
- **MLQ-R002** — Each enumerated MySQL snapshot passes `VerifyCadenceSnapshot`
  with `engine="mysql"`, `expected=ExpectedCollectorsFor("mysql")`,
  `versionPrefix="8."`: engine/version, exactly the expected collector set, every
  collector `status='success'`, reconciled result evidence, snapshot_id match.
- **MLQ-R003** — A legitimate zero-row success (`mysql_replication_status_v1` on a
  standalone server) passes; it is never a failure.
- **MLQ-R004** — One exact snapshot passes `VerifyExportReconciliation`:
  metadata engine/version, no foreign PostgreSQL collector, collected == persisted
  == exported per collector, `INFO` redacted, manifest complete.
- **MLQ-R005** — The MySQL live qualification runs in CI as a pinned
  `integration-mysql` job and is a dependency of the terminal `ci-gate` (so it
  routes through the same PostgreSQL-mandatory gate, #528 req #4).

## Invariants

- **INV-MLQ-01** — Collection is read-only; the qualification issues no writes to
  MySQL beyond the one-time deterministic seed.
- **INV-MLQ-02** — A dedicated container/service + isolated temporary Signals
  store; never deletes a developer's pre-existing MySQL container or store.
- **INV-MLQ-03** — `status='success'` is the success signal; a row/snapshot
  existing, or a distinct-`query_id` count, is never sufficient.

## Failure conditions

- Any expected collector missing / non-`success` / lacking reconciled evidence →
  qualification error (loud failure).
- A foreign PostgreSQL collector in the export, a redaction leak, or a
  member/manifest mismatch → error.

## Constraints

- Pinned MySQL image by digest; version-prefix guard (`8.`). No cloud/AWS.
- The live test is `//go:build mysql_live` and SKIPs unless `MYSQL_QUAL_*` is set
  by the orchestrator.

## Traceability

Acceptance: `specifications/mysql-live-qualification.acceptance.md`.
Matrix: `features/signals/traceability.md` (MLQ-R001..R005, INV-MLQ-01..03).
