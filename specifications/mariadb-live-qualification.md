# MariaDB Live Qualification

## Status

ACTIVE

## Purpose

Prove, repeatably and **in CI**, that the normal Signals producer path — the real
`cmd/signals` daemon collecting a real MariaDB server, persisting through the
shared store, and exporting through the production exporter — produces truthful,
successful evidence for every expected MariaDB collector. This is the MariaDB analog
of the SingleStore live qualification
(`specifications/singlestore-live-qualification.md`), reusing the same
engine-generic verifier; only the catalog, engine string, and version prefix
differ.

Unlike SingleStore (whose multi-GB Dev Image kept its live qualification
opt-in/local), MariaDB's official image is a lightweight CI **service container**,
so the MariaDB live qualification runs as a pinned CI job (`integration-mariadb`)
wired into the PostgreSQL-mandatory terminal gate (`ci-gate.needs`), and is also
runnable locally.

Per `version-support-policy.md` (VSP-R006), `integration-mariadb` is a **matrix of
the four supported MariaDB LTS lines** — 10.11, 11.4, 11.8, 12.3 — each a pinned
digest driving the full qualification independently. The verifier's version prefix
is the matrix leg's exact LTS prefix (`10.11.` / `11.4.` / `11.8.` / `12.3.`),
passed as `MARIADB_QUAL_VERSION_PREFIX`. MariaDB qualification is independent of
MySQL's, and all four lines were verified live to need no version-specific catalog
SQL (including `information_schema.INNODB_LOCKS` / `INNODB_LOCK_WAITS`, which
MariaDB retains across every supported generation and MySQL 8.0 removed).

## Coverage receipt — live vs fixture (explicit)

- **Live producer evidence** runs BOTH in CI (`integration-mariadb`, a pinned MariaDB
  service container) and locally (`scripts/mariadb-live-qual.sh` against the pinned
  MariaDB Dev Image via `scripts/mariadb-dev-fixture.sh`). The `//go:build mariadb_live`
  verifier asserts the artifacts the real daemon + exporter produced.
- **Deterministic (no-Docker) CI coverage** of the shared verifier logic and the
  MariaDB export serialization already exists: `internal/collector/singlestore_qual_test.go`
  (fails-loudly / zero-row / reconciliation) and
  `tests/mariadb_export_serialization_test.go` (a MariaDB snapshot through the
  production `export.Builder`). These are serialization/verifier coverage, not
  producer evidence.

## Interfaces

- **Environment** — `scripts/mariadb-dev-fixture.sh {up|down}`: a pinned MariaDB
  Dev Image seeded by `examples/mariadb-init.sql` (read-only `signals_ro` with
  `SELECT, PROCESS` + deterministic schema/workload). Defaults to 11.4.13
  (`mariadb@sha256:250e1609…`); `MARIADB_IMAGE` + `EXPECTED_VERSION_PREFIX`
  override it so any qualified LTS line (10.11 / 11.8 / 12.3) can be driven
  locally. A dedicated container name so a developer's own MariaDB container is
  never touched. In CI the MariaDB is a per-leg service container seeded by a job
  step; the fixture script is the local path.
- **Producer** — `cmd/signals` + `cmd/signalsctl` (`collect now --force`,
  `export`), configured by `examples/mariadb-daemon.yaml`.
- **Verifier** — the shared `internal/collector/singlestore_qual.go`
  (`VerifyCadenceSnapshot(s, expected, engine, versionPrefix)`,
  `VerifyExportReconciliation`, `ExpectedCollectorsFor("mariadb")`,
  `ReadQualSnapshots`, `ReadQualExport`) — engine-generic.
- **Orchestration + live assertion** — `scripts/mariadb-live-qual.sh` drives the
  real daemon; `internal/collector/mariadb_live_test.go` (`//go:build mariadb_live`)
  reads `MARIADB_QUAL_*` and runs the verifiers against live artifacts.

## Behaviors

- **Given** the seeded live MariaDB and a `SELECT, PROCESS` role, **when** the
  daemon runs a cycle, **then** all 15 MariaDB collectors persist
  `status='success'` — `mariadb_replication_status_v1` / `mariadb_galera_status_v1`
  are legitimate standalone successes, and the `performance_schema`-gated
  collectors are legitimate empty successes when it is off — each reconciled to a
  `query_results` row whose decoded row count equals `query_runs.row_count`.
- **Given** two cycles + a daemon restart, **when** snapshots are enumerated,
  **then** each is a distinct ULID (no overwrite), `engine=mariadb`, version
  matching the matrix leg's LTS prefix (`10.11.` / `11.4.` / `11.8.` / `12.3.`),
  and the restart appends.
- **Given** one exact snapshot exported through the production exporter, **then**
  `metadata.json` declares `engine=mariadb` + `engine_version`;
  `collector_status.json` / `query_catalog.json` contain only MariaDB collectors;
  per successful collector collected == persisted == exported; `INFO` is redacted;
  the manifest covers every member.

## Rules

- **MDLQ-R001** — The producer is the normal `cmd/signals` daemon; qualification
  never calls `singlestore.Collect`/`CatalogFor` directly as a producer nor a
  standalone ZIP writer. (mirrors SLQ-R001)
- **MDLQ-R002** — Each enumerated MariaDB snapshot passes `VerifyCadenceSnapshot`
  with `engine="mariadb"`, `expected=ExpectedCollectorsFor("mariadb")`, and the
  running leg's `versionPrefix` (`MARIADB_QUAL_VERSION_PREFIX`, one of `10.11.` /
  `11.4.` / `11.8.` / `12.3.`; default `11.`): engine/version, exactly the expected
  collector set, every collector `status='success'`, reconciled result evidence,
  snapshot_id match.
- **MDLQ-R003** — A legitimate zero-row success (`mariadb_replication_status_v1` on a
  standalone server) passes; it is never a failure.
- **MDLQ-R004** — One exact snapshot passes `VerifyExportReconciliation`:
  metadata engine/version, no foreign PostgreSQL collector, collected == persisted
  == exported per collector, `INFO` redacted, manifest complete.
- **MDLQ-R005** — The MariaDB live qualification runs in CI as a pinned
  `integration-mariadb` job and is a dependency of the terminal `ci-gate` (so it
  routes through the same PostgreSQL-mandatory gate, #528 req #4).
- **MDLQ-R006** — `integration-mariadb` is a matrix of the four supported MariaDB
  LTS lines (10.11, 11.4, 11.8, 12.3), each a pinned digest running the full
  qualification independently with its own `MARIADB_QUAL_VERSION_PREFIX`; dropping
  a required LTS leg is caught by `TestMariaDBVersionMatrixQualifiesLTS` (VSP-R006).

## Invariants

- **INV-MDLQ-01** — Collection is read-only; the qualification issues no writes to
  MariaDB beyond the one-time deterministic seed.
- **INV-MDLQ-02** — A dedicated container/service + isolated temporary Signals
  store; never deletes a developer's pre-existing MariaDB container or store.
- **INV-MDLQ-03** — `status='success'` is the success signal; a row/snapshot
  existing, or a distinct-`query_id` count, is never sufficient.

## Failure conditions

- Any expected collector missing / non-`success` / lacking reconciled evidence →
  qualification error (loud failure).
- A foreign PostgreSQL collector in the export, a redaction leak, or a
  member/manifest mismatch → error.

## Constraints

- Pinned MariaDB images by digest (one per LTS leg); per-leg version-prefix guard
  (`10.11.` / `11.4.` / `11.8.` / `12.3.`). No cloud/AWS.
- The live test is `//go:build mariadb_live` and SKIPs unless `MARIADB_QUAL_*` is set
  by the orchestrator.

## Traceability

Acceptance: `specifications/mariadb-live-qualification.acceptance.md`.
Matrix: `features/signals/traceability.md` (MDLQ-R001..R006, INV-MDLQ-01..03).
