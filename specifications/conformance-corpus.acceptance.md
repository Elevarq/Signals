# Acceptance Tests: conformance-corpus

## Feature

`specifications/conformance-corpus.md` — the #530 producer conformance corpus
(real Signals production-exporter artifacts for MySQL/MariaDB/ClickHouse) and its
reproducible generator + provenance.

## Test Cases

### TC-CC-01: artifacts are real producer output, captured verbatim (integration)

**Rule:** CC-R001, INV-CC-01

**Given:** a pinned engine dev fixture. **When:** `gen-conformance-corpus.sh`
runs. **Then:** each `<engine>-signals.zip` is the exact ZIP the live
qualification verifier passed on (real daemon → production exporter), copied
verbatim — no hand construction/editing. **Expected Result:** The emit hook runs
only after `go test -tags <engine>_live` passes (scripts/*-live-qual.sh).

---

### TC-CC-02: provenance identifies and reproduces each artifact (structural)

**Rule:** CC-R002, CC-R003

**Given:** a produced artifact. **When:** its provenance record is read. **Then:**
it carries Signals commit, engine, engine version, fixture, generation mechanism,
and SHA-256; re-running the generator at that commit reproduces an equivalent
artifact. **Expected Result:** `features/signals/conformance-corpus/<engine>-signals.provenance.json`
records all fields; the hash matches the committed consumer-side artifact.

---

### TC-CC-03: artifacts carry representative evidence (behavioral)

**Rule:** CC-R004

**Given:** a corpus artifact. **When:** its `collector_status.json` + payloads are
inspected. **Then:** it contains identity, version, collector status, table
statistics, and statement/digest workload; MySQL/MariaDB additionally carry the
Tier-1 columns + index inventory — not only empty-success collectors. **Expected
Result:** MySQL 18 / MariaDB 17 / ClickHouse 14 collectors, status=success, with
the seeded `replay_fixture` columns + indexes present (MySQL/MariaDB).

---

### TC-CC-04: the corpus proves the loop against the real Analyzer consumer (integration, cross-repo)

**Rule:** CC-R005

**Given:** the corpus handed to Analyzer #3890. **When:** each artifact is driven
through Analyzer's real `RunWithConfig`. **Then:** ingest → metadata validation →
engine dispatch → engine-specific normalization succeeds with the correct engine
identity/version + route and no PostgreSQL path; Analyzer validation is not
weakened. **Expected Result:** Analyzer `engine_conformance_producer_test.go`
passes for mysql/mariadb/clickhouse; SingleStore conformance stays green;
PostgreSQL is the regression control.
