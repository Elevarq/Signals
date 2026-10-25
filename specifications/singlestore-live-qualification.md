# SingleStore Live Qualification

## Status

ACTIVE

## Purpose

Prove, repeatably, that the **normal Signals producer path** — the real
`cmd/signals` daemon collecting a **real SingleStore** server, persisting through
the shared store, and exporting through the **production exporter** — produces
truthful, successful evidence for every expected SingleStore collector. This
replaces the superseded producer-evidence anti-patterns: a hand-built
`singlestore_collect_fixture.json` captured from `singlestore.Collect()` directly,
and conformance ZIPs assembled from hand-inserted SQLite rows.

Two qualifications, both driven by the normal daemon (never a direct `Collect()`
call, never the standalone `internal/singlestore` ZIP writer):

- **Cadence qualification (#583)** — the daemon collects SingleStore on schedule
  and persists independent snapshots; every expected collector's persisted
  `query_run` is asserted as `status='success'` with reconciled result evidence,
  not merely counted by distinct `query_id`. An injected/controlled collector
  failure must make the qualification fail, not pass.
- **Export reconciliation (#584)** — one exact live snapshot is reconciled
  end-to-end through the production exporter: for every successful collector,
  the collected row count equals the persisted result row count equals the
  exported payload row count, with documented redaction as the only allowed
  transformation.

This owns the live-qualification invariant of epic #555: the SingleStore engine
is proven through the same producer/exporter path as PostgreSQL, against the
pinned reproducible environment (#577), with success asserted as `status`, never
as an attempt/row count.

## Coverage receipt — live vs fixture (explicit, per #584)

- **Live producer evidence** runs BOTH in CI and locally (#577/#555): the
  `//go:build singlestore_live` test, driven by `scripts/singlestore-live-qual.sh`
  against the pinned Dev Image. In CI it runs as the pinned `integration-singlestore`
  job (the SingleStore Dev Image as a service container, EXTERNAL mode — the
  harness only builds, drives the real daemon, and verifies); locally it brings
  the Dev Image up/down itself. It is the ONLY source of producer evidence. The
  Dev Image is multi-GB and amd64-only, so `integration-singlestore` is the
  slowest engine lane; it is nonetheless a required dependency of the terminal
  `ci-gate` like every other engine (SLQ-R010), so SingleStore cannot go green
  without the PostgreSQL-mandatory gate. This supersedes the earlier stance that
  the Dev Image was "not a CI service" and the hand-built golden fixture it stood
  in for (`singlestore_collect_fixture.json`, retired with #577/#589).
- **Deterministic CI coverage** is the verifier's logic and the export
  serialization: `singlestore_qual_test.go` (normal `go test`, no build tag)
  exercises every assertion — including that an injected non-success collector
  makes the qualification fail, and that a legitimate zero-row success passes —
  and reconciles a snapshot persisted via `InsertCollectionAtomic` through the
  production `export.Builder`. These CI inputs are **serialization/verifier
  coverage, not producer evidence**; they never stand in for the live run.

## Interfaces

- **Environment** — `scripts/singlestore-dev-fixture.sh {up|down}`: the pinned
  SingleStore Dev Image (`@sha256:a1382fb7…`, `@@memsql_version` 9.1.x) seeded by
  `examples/singlestore-init.sql` (read-only `signals_ro`, `SELECT,PROCESS` +
  `CLUSTER` grants, deterministic schema + workload). A dedicated container name
  (`ss-dev-fixture`) so a developer's own SingleStore container is never touched.
- **Producer** — `cmd/signals` (daemon) + `cmd/signalsctl` (`collect now
  --force`, `export`), configured by `examples/singlestore-daemon.yaml`.
- **Verifier** (`internal/collector/singlestore_qual.go`, no build tag):
  - `SingleStoreExpectedCollectors() []string` — the expected collector id set
    (from `singlestore.Catalog()`).
  - `VerifyCadenceSnapshot(s QualSnapshot, expected []string, versionPrefix string) error`
  - `VerifyExportReconciliation(s QualSnapshot, z QualExport, expected []string, versionPrefix string) error`
  - readers `ReadQualSnapshots(db) ([]QualSnapshot, error)` and
    `ReadQualExport(zipPath) (QualExport, error)`.
- **Orchestration + live assertion** — `internal/collector/singlestore_live_test.go`
  (`//go:build singlestore_live`) reads `SINGLESTORE_QUAL_DB`,
  `SINGLESTORE_QUAL_ZIP`, `SINGLESTORE_QUAL_SNAPSHOT` and runs the verifiers
  against live artifacts.

## Behaviors

- **Given** the seeded live server and a full grant (`SELECT,PROCESS,CLUSTER`),
  **when** the daemon runs a collection cycle, **then** all 15 SingleStore
  collectors persist `status='success'` (the two CLUSTER-elevated collectors,
  `cluster_status` and `mv_events`, succeed because the qualification grant
  includes `CLUSTER`), each with a `query_results` row whose decoded row count
  equals the `query_run.row_count`.
- **Given** two or more scheduled cycles and a daemon restart against the same
  store, **when** snapshots are enumerated, **then** each is a distinct ULID
  (no overwrite), engine `singlestore`, version `9.1.x`, and the restart appends.
- **Given** one exact live snapshot, **when** exported through the production
  exporter, **then** `metadata.json` identifies `engine=singlestore` +
  `engine_version`; `collector_status.json` / `query_catalog.json` contain only
  SingleStore collectors; per successful collector collected = persisted =
  exported; `MV_PLANCACHE.QUERY_TEXT`, `MV_PROCESSLIST.INFO`, and
  `PIPELINES.CONFIG_JSON` are redacted; every ZIP member is listed in
  `manifest.json` with a SHA-256.

## Rules

- **SLQ-R001** — The producer is the normal `cmd/signals` daemon; qualification
  never calls `singlestore.Collect()` directly nor the `internal/singlestore` ZIP
  writer. (#583/#584 non-goals)
- **SLQ-R002** — For each enumerated snapshot, `VerifyCadenceSnapshot` asserts:
  engine `singlestore`; version matches the prefix; the set of run `query_id`s
  equals exactly the expected collector set; every expected collector's run is
  `status='success'`; and every run's `snapshot_id` equals the enclosing
  snapshot id. (#583)
- **SLQ-R003** — For each `status='success'` run, a `query_results` row MUST
  exist and its decoded row count MUST equal the run's `row_count`, including a
  legitimate zero-row result (0 == 0 passes). A success run with no result row,
  or a row-count mismatch, is a violation. (#583)
- **SLQ-R004** — A non-`success` status for any expected collector (failed or
  skipped) makes `VerifyCadenceSnapshot` fail with an error naming the collector
  and its status — the qualification never passes on distinct `query_id`s alone.
  (#583 "injected failure fails loudly")
- **SLQ-R005** — The cadence run proves ≥ 2 independent snapshots with unique
  ULIDs; a daemon restart against the same store appends further snapshots
  without overwriting prior ones. The real-cadence expected count is derived from
  the daemon's interval semantics (baseline at t=0 + one cycle per interval) and
  recorded before execution. (#583)
- **SLQ-R006** — `VerifyExportReconciliation` asserts `metadata.json` carries
  `engine=singlestore` and a non-empty `engine_version`; and neither
  `collector_status.json` nor `query_catalog.json` contains any collector absent
  from the expected SingleStore set (no foreign PostgreSQL collector). (#584,
  reuses the M3 seam)
- **SLQ-R007** — For every successful collector in the exact snapshot,
  collected (`query_run.row_count`) == persisted (decoded `query_results` row in
  the store) == exported (payload row count in the ZIP `query_results.ndjson`).
  Failed/skipped runs appear with their true status and are never emitted as a
  successful empty result. (#584)
- **SLQ-R008** — Default redaction holds in the exported payloads: no
  `QUERY_TEXT` (MV_PLANCACHE), `INFO` (MV_PROCESSLIST), or `CONFIG_JSON`
  (PIPELINES) value is present (null/absent) when high-sensitivity collectors
  are disabled. (#584, #618)
- **SLQ-R009** — Every ZIP member is listed in `manifest.json` with a non-empty
  SHA-256, and `manifest.json` lists no member absent from the ZIP. (#584)
- **SLQ-R010** — The SingleStore live qualification runs in CI as a pinned
  `integration-singlestore` job (the Dev Image as a service container) and is a
  dependency of the terminal `ci-gate`, so SingleStore routes through the same
  PostgreSQL-mandatory gate as every other engine. In CI the harness runs in
  EXTERNAL mode (`SINGLESTORE_QUAL_EXTERNAL=1`): the service container is already
  up and seeded, so the script only builds, drives the real daemon, and verifies
  — it does not bring the Dev Image up or down. Locally (`SINGLESTORE_QUAL_EXTERNAL`
  unset) the script brings the pinned Dev Image up via
  `scripts/singlestore-dev-fixture.sh` and tears it down on exit. (#577/#555)

## Invariants

- **INV-SLQ-01** — Collection is read-only; the qualification issues no writes to
  SingleStore beyond the one-time deterministic seed applied by the fixture.
- **INV-SLQ-02** — Qualification uses a dedicated container name and an isolated
  temporary Signals store; it never deletes or mutates a developer's pre-existing
  SingleStore container or store. (#583 design constraint)
- **INV-SLQ-03** — `status='success'` is the success signal; a snapshot/run row
  existing, or a distinct-`query_id` count, is never sufficient. (#583)

## Failure conditions

- Any expected collector missing, non-`success`, or lacking reconciled result
  evidence → qualification error (loud failure), never a pass.
- A ZIP member absent from `manifest.json` (or vice versa) → error.
- A redacted column present with a non-null value in an exported payload → error.

## Constraints

- Pinned Dev Image by digest; version-prefix guard (`9.`) on bring-up.
- No cloud, AWS, or license; the Dev Image ships a built-in developer license.
- The live test is `//go:build singlestore_live` and SKIPs unless the
  `SINGLESTORE_QUAL_*` environment is provided by the orchestrator.
- In CI (`SINGLESTORE_QUAL_EXTERNAL=1`) the Dev Image is an already-running,
  already-seeded service container; the harness neither starts nor stops it and
  must not invoke `scripts/singlestore-dev-fixture.sh`. The multi-GB amd64 image
  makes this the slowest engine lane; its service-container health check is
  given a long start period and generous retries.

## NFR overrides

None beyond the Signals safety baseline (read-only, no secret in output,
redaction by default).

## Traceability

Acceptance: `specifications/singlestore-live-qualification.acceptance.md`.
Matrix: `features/signals/traceability.md` (SLQ-R001..R010, INV-SLQ-01..03).
