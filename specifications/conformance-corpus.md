# Producer Conformance Corpus

## Status

ACTIVE

## Purpose

Define the **producer conformance corpus** (Elevarq/Signals #530, consumed by
Analyzer #3890): real Signals production-exporter artifacts for MySQL, MariaDB,
and ClickHouse that prove the multi-engine report contract against the actual
consumer. The corpus closes the last #530 verification step — the report
*envelope* (`signals-snapshot.v1`) is already agreed and implemented
consumer-side; what remained was demonstrating that artifacts from the **real**
Signals collection → persistence → production-export path flow through Analyzer's
**real** ingest → dispatch → normalization path.

A hand-built ZIP is NOT equivalent to producer conformance. Every corpus artifact
is the verbatim output of the normal `cmd/signals` daemon + production exporter —
never hand-constructed or edited to make the consumer accept it.

## Scope

- `scripts/gen-conformance-corpus.sh` — the reproducible generator.
- The `emit_conformance_artifact` hook in `scripts/{mysql,mariadb,clickhouse}-live-qual.sh`,
  gated on `SIGNALS_CORPUS_OUT` (no behaviour change when unset; CI unaffected).
- The per-artifact provenance records (`features/signals/conformance-corpus/*.provenance.json`).

Out of scope: the report envelope itself (owned by `multi-engine-report-contract.md`
— not redesigned); the Analyzer-side consumption tests (owned by Analyzer #3890).

## Interfaces

- **`bash scripts/gen-conformance-corpus.sh [OUT_DIR]`** — runs the real live
  qualification per engine (local docker, pinned dev fixtures) and captures the
  exact exported ZIP it verified, plus provenance, into `OUT_DIR`.
- **`SIGNALS_CORPUS_OUT`** — when set, each live-qual publishes
  `<engine>-signals.zip` (verbatim production-exporter output) + a provenance JSON
  after the live Go verifier passes.

## Behaviors

- **Given** a pinned engine dev fixture + seeded representative workload/schema,
  **when** the generator runs, **then** it produces one ZIP per engine via the
  real daemon → exporter, each carrying meaningful evidence (engine identity +
  version, collector status, table statistics, statement/digest workload, and —
  for MySQL/MariaDB — the Tier-1 Replay columns + index inventory), not merely
  empty-success collectors.
- **Given** a produced artifact, **then** a provenance record identifies the
  Signals commit, engine, engine version, fixture/workload, generation mechanism,
  and artifact SHA-256 — so the corpus is reproducible, not an unexplained binary.
- **Given** the corpus handed to Analyzer #3890, **then** each artifact flows
  through Analyzer's real `RunWithConfig` ingest → dispatch → normalization and is
  accepted with the correct engine identity/version and route, with no PostgreSQL
  path selected — the #530 contract loop.

## Rules

- **CC-R001** | Every corpus artifact is produced by the real `cmd/signals`
  collection → persistence → production-export path and captured verbatim; it is
  never hand-constructed or edited. The generator captures the ZIP only **after**
  the live qualification verifier passes.
- **CC-R002** | Every artifact carries a provenance record with: Signals commit,
  engine, engine version, qualification fixture/workload, generation mechanism,
  and artifact SHA-256.
- **CC-R003** | The corpus is reproducible: `scripts/gen-conformance-corpus.sh` at
  the recorded commit regenerates equivalent artifacts from the pinned fixtures.
- **CC-R004** | Artifacts contain representative evidence (identity, version,
  status, table statistics, statement/digest workload; MySQL/MariaDB additionally
  the Tier-1 columns + index inventory), not only empty-success collectors.
- **CC-R005** | The corpus proves the contract loop with the **real** Analyzer
  consumer path; hand-built fixtures are not accepted as producer conformance.
  Analyzer validation is never weakened to accept an artifact — a producer↔consumer
  disagreement is a contract defect to be attributed to the violating side.

## Invariants

- **INV-CC-01** | Generation issues no writes to the target engine beyond the
  one-time deterministic seed (inherits the live-qualification read-only invariant).
- **INV-CC-02** | The `SIGNALS_CORPUS_OUT` hook is inert when unset — the live
  qualifications' CI behaviour is unchanged.

## Failure conditions

| Trigger | Response |
|---------|----------|
| The live verifier fails | The generator aborts before emitting any artifact (CC-R001). |
| `python3` / `unzip` absent at generation | Provenance `engine_version` best-effort empty; the ZIP + hash are still emitted (generation-time tooling, not a producer contract). |
| Analyzer rejects a real artifact | Treated as a contract defect; attribute to the violating side, never by weakening Analyzer validation (CC-R005). |

## Constraints / NFR

- Local docker + Go toolchain; no cloud/AWS. Pinned dev-fixture images per engine
  (MySQL 8.4, MariaDB 11.4, ClickHouse 24.8 — representative qualified versions).

## Traceability

Acceptance: `specifications/conformance-corpus.acceptance.md`. Cites
`multi-engine-report-contract.md` (envelope), the per-engine
`*-live-qualification.md`, and `mysql-collectors.md` / `mariadb-collectors.md`
(Tier-1 evidence). Consumer side: Analyzer #3890
(`engine_conformance_producer_test.go`).
