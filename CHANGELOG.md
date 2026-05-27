# Changelog

All notable changes to Arq Signals will be documented in this file.

This project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.8.0] - 2026-05-15

### Added

- **`pg_statistic_ext_data_v1` + `_mcv_v1` collectors** (#171):
  sampled-stats sibling of `pg_statistic_ext_v1` (#131). The
  metadata sibling emits "which CREATE STATISTICS objects
  exist"; these collectors emit the byte-encoded values used
  in downstream planner-cost analysis. Two-collector split keeps the MCV blob —
  the only multivariate-stats kind that may contain PII —
  behind the HighSensitivity floor:
  - `pg_statistic_ext_data_v1` (kinds `d` / `f` / `e`) — ships
    ungated; these kinds encode statistical models, not sampled
    values.
  - `pg_statistic_ext_data_mcv_v1` (kind `m`) — HighSensitivity-
    gated, same posture as `pg_stats_extended_v1`'s per-column
    MCV / histogram blob.
  Owner-only refusals on `pg_statistic_ext_data` (post-PG12
  visibility) surface as per-object availability rows
  (`kind_data=NULL`, `available=false`) rather than dropping
  the row or failing the snapshot.

### Fixed

- **Release pipeline: `cosign attest --type spdxjson`** (#180):
  v0.7.0's release used `--type spdx` (SPDX tag-value plaintext
  format) on a JSON SBOM file, causing cosign to embed the JSON
  as a JSON-encoded string in the predicate field of the in-toto
  Statement. The in-toto envelope was structurally valid and
  OIDC-bound (rekor tlog 1550874655 / 1550875004 for v0.7.0),
  but `cosign verify-attestation` rejected it with a proto
  syntax error. Switched both attest steps (GHCR + Docker Hub)
  to `--type spdxjson`. v0.8.0 verifies cleanly with stock
  `cosign verify-attestation --type spdxjson` per the command
  in `docs/release-verification.md`. v0.7.0 still requires the
  manual-decode fallback documented in section 5c.

## [0.7.0] - 2026-05-15

### Added

- **API bearer-token strength validation** (#135): operator-supplied
  `ARQ_SIGNALS_API_TOKEN` / `ARQ_SIGNALS_API_TOKEN_FILE` values are
  now rejected when below the minimum strength bar — 32 characters
  AND >= 8 distinct characters. In `env=prod` the rejection is a
  hard error (startup fails fast); in `env=dev`/`lab` it surfaces
  as a warning. Auto-generated tokens (the default when none is
  configured) are unaffected — `cmd/arq-signals/main.go` produces
  32 random bytes from `crypto/rand`. New exported helper:
  `config.WeakAPITokenReason(string) string` returns the closed
  human-readable rejection reason or `""` for strong tokens.
  Token values never appear in returned errors / warnings
  (closed-output discipline for log + audit pipelines).

- **Helm chart: API token Secret support** (#136): values gain
  `api.tokenSecretName` (was a documented stub before; now wired)
  and a new `api.tokenSecretKey` (default `token`). When set, the
  deployment injects the value as `ARQ_SIGNALS_API_TOKEN` via
  `secretKeyRef` — the token never lands in a ConfigMap or
  rendered manifest beyond the Secret reference name. Production-
  ready alternative to the dev-mode auto-generate path. Token
  rotation is now declarative: rotate the Secret + restart the pod.
  Generation recipe in the values comment.

### Security (DevSecOps baseline — #160 closed)

- **Local security preflight bundle** (#160 first slice): `scripts/
  preflight.sh` gains `secrets` (gitleaks staged + current-commit),
  `vuln` (govulncheck), and `security` subcommands; `all`
  appends `security` after the Go gates. Each gate emits an
  actionable `Install locally with: …` hint when its tool is
  missing and exits 127 so missing-tool skips are distinguishable
  from real findings.

- **Push/PR CI gate** (#161): the `security-scan` job in
  `.github/workflows/ci.yml` now runs
  `scripts/preflight.sh secrets` + `preflight.sh vuln` on every
  push/PR. Local pre-push hook and CI run identical commands so
  the two surfaces can't drift.

- **Nightly evidence workflow** (#162):
  `.github/workflows/nightly-security.yml` runs four
  scheduled-evidence jobs at 07:23 UTC daily — full-history
  Gitleaks, Anchore syft SBOM (SPDX-JSON), Anchore Grype against
  the SBOM, and OpenSSF Scorecard. Artefacts upload to the
  workflow run and (where supported) to the Security tab as
  SARIF.

- **Semgrep + OSV-Scanner gates** (#163): `preflight.sh semgrep`
  runs `p/golang` + `p/security-audit`; `preflight.sh osv` runs
  `osv-scanner --recursive .`. Both fold into `security` and
  the push/PR CI job. Two repo-side fixes the new gates
  surfaced: `go.mod` `go` directive bumped 1.25.0 → 1.25.10
  (OSV-Scanner stdlib-advisory hygiene) and an inline
  `// nosemgrep` carve-out for `math/rand`-as-ULID-entropy in
  `internal/collector/collector.go` (sortable-unique IDs, not
  security tokens — crypto/rand would slow ID generation for
  zero benefit).

- **KubeLinter + Conftest gate** (#164): `preflight.sh kube-lint`
  renders the Helm chart with `helm template
  deploy/helm/arq-signals` and runs kube-linter (CIS-aligned
  defaults) + conftest against the new `policy/security.rego`.
  Rego policy covers defence-in-depth requirements kube-linter
  doesn't enforce today: `automountServiceAccountToken=false`,
  privileged forbidden, allowPrivilegeEscalation forbidden,
  `capabilities.drop` includes `ALL`, runAsNonRoot, hostNetwork
  /hostPID/hostIPC forbidden.

- **Cosign-attested SBOM** (#165): the release pipeline already
  cosign-signed the published image (GHCR + Docker Hub) and
  produced the SBOM in two forms; #165 adds `cosign attest
  --type spdx --predicate sbom.spdx.json` so `cosign
  verify-attestation` succeeds against the same workflow OIDC
  identity. `docs/release-verification.md` gains a new "5c.
  cosign-signed SBOM attestation" section plus the verify
  command in the Quick-verify block.

- **golangci-lint baseline cleaned + CI-enforced** (#173):
  `.golangci.yml` pinned with errcheck carve-outs for idiomatic
  Close/Rollback/Encode patterns; the 50-finding baseline
  (errcheck 42 / staticcheck 7 / unused 1) was burned down so
  `preflight.sh lint` joined the push/PR gate alongside
  secrets/vuln/semgrep/osv/kube-lint.

### Fixed

- **`pg_statistic_ext_v1` stxkind comment** (#176): registration
  comment had `d`/`f`/`m` codes rotated. Corrected per PG's
  `include/statistics/statistics.h` (`d` = ndistinct, `f` =
  dependencies, `m` = MCV). SQL was emitting `stxkind` verbatim;
  only the source comment was wrong.

## [0.6.0] - 2026-05-14

### Added

- **`pgss_capacity_v1` collector** (#132): emits `pg_stat_statements_info.dealloc` plus the current `count(*)` from `pg_stat_statements`, so the analyzer can detect (a) the extension has already evicted tracked statements because `pg_stat_statements.max` was too low (`dealloc > 0`) and (b) near-cap state before eviction starts (`tracked_count / max ≥ 0.9`). Settings (`pg_stat_statements.max`, `.track`, `.track_utility`, `.track_planning`) are already collected by the existing `pg_settings_v1` collector — no amendment there. Gated on PG 14+ and the `pg_stat_statements` extension; graceful degradation when either is missing. Consumed by downstream eviction-pressure and track-top analysis. New spec: `specifications/collectors/pgss_capacity_v1.md`.

## [0.5.0] - 2026-05-13

### Fixed

- **`pg_stat_progress_vacuum_v1` column list** — the PG 17 / 18
  overrides referenced `indrelid`, which is not a column on
  `pg_stat_progress_vacuum` in any PG version. Caught during the
  cross-major smoke test (PG 14 / 15 / 16 / 17 / 18) before
  Beta. The overrides now populate the real PG-17 columns
  (`max_dead_tuple_bytes`, `dead_tuple_bytes`, `num_dead_item_ids`,
  `indexes_total`, `indexes_processed`) and the PG-18 addition
  `delay_time`. Two regression-guard tests added — one pins
  `indrelid` out of the resolved SQL on every supported major,
  the other pins the real column names INTO the PG 17 / 18
  resolved SQL so a missed override can't silently emit NULL
  stubs.

### Added

- **`pg_stat_statements_v1` self-filter + fixed `application_name`**
  (R106) — Signals' own probe queries no longer pollute customer
  workload analysis, and cross-database statistics no longer leak
  into snapshots. Every connection (collector pool, `arqctl
  doctor` probes, `arqctl connect test`) now sets
  `application_name = arq-signals` in its startup parameters,
  sourced from a single Go constant (`collector.AppName`). The
  `pg_stat_statements_v1` SQL scopes rows to the connected
  database (`pg_database.datname = current_database()`) and
  suppresses rows attributable to Signals' own sessions via a
  `NOT EXISTS` correlated subquery against `pg_stat_activity`
  (matched on `userid` / `dbid`). `SELECT s.*` preserves the
  R037 dynamic-column contract; ID, category, retention class,
  and `RequiresExtension` are unchanged. Operator caveat: if a
  non-Signals application sets its own `application_name` to
  `arq-signals`, its rows are also suppressed — flagged as
  misconfiguration in `docs/collectors.md`.
- **`index_bloat_estimate_v1`** collector (R105, closes #117) —
  sibling of `bloat_estimate_v1` for indexes. One row per
  non-system `relkind ∈ {i, I}` index with the canonical
  index-tuple statistical formula:
  `expected ≈ CEIL(reltuples × (Σ key_avg_width + 8 + 4) /
  GREATEST(block_size - 24, 1)) × block_size`. Width sum is
  bounded by `pg_index.indnkeyatts` so INCLUDE columns
  (PG 11+ covering indexes) are skipped, matching the PG-wiki
  convention. Expression-key indexes emit `stats_missing = TRUE`.
  Partitioned-index parents (`relkind = 'I'`) surface with
  `actual_size_bytes = 0`. No extension dependency. Category
  `indexes`, cadence 6h, retention medium, timeout 30s. Pairs
  with R103 / `index_health_summary_v1`: a bloated + unused
  index is the highest-priority drop candidate.
- **`bloat_estimate_v1`** collector (R104, closes #110).
  Statistical table-bloat estimate that runs on every PG —
  including managed services (RDS / Aurora / Cloud SQL / AlloyDB
  / Azure Flex) where `pgstattuple` isn't available. One row per
  non-system `relkind ∈ {r, m, p}` relation carrying actual vs
  expected size, floored `bloat_bytes`, `bloat_ratio` ∈ [0, 1]
  (NULL when stats missing), and the corroborating signals
  `n_live_tup` / `n_dead_tup` / `last_autovacuum`. Formula pinned
  to `current_setting('block_size')` so non-default 4K/16K
  configurations work. No extension dependency. Category
  `tables`, cadence 6h, retention medium, timeout 30s. Index-
  bloat sibling deferred to a follow-up issue.
- **`index_health_summary_v1`** collector (R103, closes #109).
  Centralises the canonical index-audit derivation that every
  analyzer was previously reinventing. One row per non-system
  index with: identity (`schemaname`/`tablename`/`indexname`/
  `index_oid`), size, scan counters, correctness flags
  (`is_unique`/`is_primary`/`is_valid`/`is_ready`), ordered
  `column_set`, and a `health_findings` text-array drawn from
  `{unused, large_unused, invalid, not_ready, redundant,
  duplicate}` plus `duplicate_of` / `redundant_with` pointers.
  Duplicate detection via lower-OID twin on the same table;
  redundant detection via strict-prefix match against a larger
  index on the same table. Category `indexes`, cadence 6h,
  retention medium, timeout 30s. System schemas excluded
  (INV-SIGNALS-12).
- **`pg_stat_progress_*` family** — six new collectors covering
  in-flight PostgreSQL operations (R102, closes #108):
  `pg_stat_progress_vacuum_v1`, `pg_stat_progress_analyze_v1`,
  `pg_stat_progress_create_index_v1`,
  `pg_stat_progress_cluster_v1`,
  `pg_stat_progress_basebackup_v1`,
  `pg_stat_progress_copy_v1`. Category `progress`, cadence 5m,
  retention short, PG 14+ only. Empty rowsets on quiet clusters
  are the success state. Column drift handled per-major via
  `RegisterOverride` for `pg_stat_progress_vacuum` (PG 17
  byte-denominated dead-tuple accounting) and
  `pg_stat_progress_copy` (PG 17 `tuples_skipped`); consumers see
  a stable canonical schema across majors.
- **`pg_stat_replication_slots_v1`** collector (R101, closes #111).
  One row per logical replication slot from
  `pg_stat_replication_slots`: spill counters
  (`spill_txns` / `spill_count` / `spill_bytes`), stream counters
  (`stream_txns` / `stream_count` / `stream_bytes`), and totals
  (`total_txns` / `total_bytes`, `stats_reset`). Surfaces the
  leading indicators of slot saturation, under-sized
  `logical_decoding_work_mem`, and downstream consumer
  back-pressure — complementing the existing
  `replication_slots_risk_v1` (which exposes slot identity and
  retained WAL). Category `replication`, cadence 5m, retention
  short, PG 14+ only; excluded with `reason=version_unsupported`
  via EA-R001 on PG 13 and below. Empty rowset on instances with
  no logical slots is the success state.

## [0.4.0] - 2026-05-12

Feature release adding operator-safety surfaces (circuit breaker
with manual pause/resume, config reload without restart),
classified connection diagnostics, doctor-level pre-flight
extensions, per-target sensitivity profiles, and per-class
retention. Closes the SOC 2 / ISO 27001-aligned review findings
from the post-0.3.2 audit.

No breaking changes. Every new surface is additive on top of the
0.3.x configuration shape. Existing configs continue to work
byte-for-byte.

### Added

#### Operator tooling
- **`arqctl connect test`** (R096): classified connection
  diagnostic. Categorises failures into `ok` / `dns` / `tcp` /
  `tls` / `auth` / `startup` / `role` / `password_resolve` /
  `config`. Supports single-target, multi-target, and ad-hoc
  `--dsn` modes. JSON output via `--json`.
- **`arqctl collect pause` / `resume`** (R097): operator-triggered
  per-target pause and resume. State is in-memory; daemon restart
  resets to closed. Audit-event trail survives restart in journald.

#### Operator-safety
- **Per-target circuit breaker** (R097): auto-disables a target
  after `fail_threshold` consecutive cycle errors (default 3) and
  auto-recovers after `open_cooldown` (default 5m). Manual pause
  takes priority over the auto state. `POST /collect/pause` /
  `/collect/resume` API endpoints. Audit events:
  `circuit_opened` / `_closed` / `_paused` / `_resumed` carry
  actor + reason on manual transitions.
- **Config reload via SIGHUP + `POST /reload`** (R100): re-reads
  the config file, validates, and (on success) swaps the target
  list in place. Pools for removed and connection-modified
  targets are closed so the next cycle re-dials with new params.
  v1 scope is targets only; retention / circuit thresholds /
  poll_interval remain set-at-construction.

#### Pre-flight diagnostics
- **`arqctl doctor` C5 collector_prerequisites** (R095 extended):
  classifies each enabled collector per target as
  `available` / `extension_missing` / `version_unsupported` /
  `config_disabled` via the same `pgqueries.GatedIDsByReason`
  logic the daemon runs at cycle start.
- **`arqctl doctor` C6 snapshot_freshness**: reads the daemon's
  SQLite store and reports per-target snapshot age against
  `2 × poll_interval`. Pre-daemon runs are WARN (informational),
  not FAIL.

#### Configuration
- **Per-target sensitivity profiles** (R098): each target may
  carry a `collectors.profile` of `default` / `restricted` /
  `custom`. `restricted` drops every HighSensitivity collector
  for that target only. `custom` supports `include` / `exclude`
  lists. Profiles NEVER widen eligibility beyond the daemon-wide
  R075 gate (INV-SENS-01).
- **Per-class retention** (R099): `signals.retention` structured
  block with `short_days` / `medium_days` / `long_days`. Honours
  the existing per-collector `RetentionClass` metadata. Flat
  `retention_days` (legacy) and structured retention are
  mutually exclusive (FC-21).

#### Observability
- **`arq_signal_circuit_state{target, state}` gauge** (R097):
  per-target circuit state, one row per (target, state). Active
  row has value 1; alert on `state="open"` or `state="paused"`.
- **`arq_signal_eligible_collectors{target}` gauge**: per-target
  collector-coverage gauge for drift alerting. Updated at the
  top of each cycle.
- **`docs/metrics-consumer-guide.md`**: operator-facing reference
  for every published metric with scrape config, label cardinality
  bounds, and recommended alerting rules. INV-SIGNALS-07
  reaffirmed (no SQL / dbname / hostname / secret material in any
  label).

### Spec rules
- R095 extended with C5 (collector_prerequisites) + C6 (snapshot_freshness)
- R096 `arqctl connect test` classified connection diagnostic
- R097 per-target circuit breaker + operator pause / resume
- R098 per-target sensitivity profiles
- R099 per-class retention
- R100 configuration reload (SIGHUP + POST /reload)
- FC-13..FC-24 covering the new failure conditions

### Fixed (audit / SOC 2 hardening)

Twelve findings from the post-0.4.0 review pass:

- `doctor.CheckRoleSafe` role-check error path now goes through
  `collector.RedactError`, closing the last unredacted operator-
  visible error channel in the doctor surface.
- `config_reload_rejected` audit + HTTP body now redact errors
  via `collector.RedactDSN` and cap at 512 chars to prevent
  config-file content leaking into the audit stream.
- `circuit_paused` / `circuit_resumed` carry the operator
  `actor` + `reason` directly. The supplemental `_request` events
  are gone — one state change = one audit event with full causal
  context.
- `ValidateStrict` rejects negative `signals.circuit.fail_threshold`
  and `signals.circuit.open_cooldown` (silently-rewritten typo
  trap closed).
- Field-by-field reload-immutability documented on the
  `Collector` struct so future reload-scope widenings see the
  invariant before introducing a data race.
- HTTP-level tests for `/collect/pause`, `/collect/resume`,
  `/reload` (bearer-auth enforcement + audit-event emission).
- Empty-fleet pause/resume responses serialise to `[]` not
  `null`.
- Pause-of-unknown-target emits a `circuit_pause_noop` audit event
  before the early return.
- Metrics consumer guide aligned with the actual
  `classifyCollectionFailure` enum (extracted to
  `metrics.CollectionFailureReasons` constant; new
  doc-alignment test guards against future drift).
- `SetCircuitState` writes the new active state first, then zeroes
  others (transient scrape window shows two states active
  rather than zero).
- `conntest.runParallel` honours parent context cancellation on
  the semaphore acquire.
- Cross-major catalog drift CI lint (R081 hardening): any future
  divergence in a collector's output column set across PG
  majors fails the build.

### Internal
- New packages: `internal/circuit`, `internal/conntest`,
  `internal/doctor`.
- `BuildSafeDSN` extracted from `internal/doctor` to
  `internal/collector/secrets.go` (shared by doctor C4 + conntest).
- `RedactError` exported from `internal/collector/secrets.go`.
- `db.GetTargetIdentity` returns value (not pointer) — sibling-
  consistent with `GetTargetName`.
- `db.DeleteQueryRunsOlderThanByClass` for per-class retention.

## [0.3.2] - 2026-04-25

Hardening release addressing the post-0.3.1 Codex review. Thirteen
findings closed across collection safety, export integrity, API
error handling, and supply-chain hygiene. No breaking changes.

This release focuses on correctness, safety, and audit completeness
under failure conditions.

### Fixed
- Unsupported PostgreSQL versions now fail closed: targets running a
  PG major below the supported minimum (currently 14) are rejected
  immediately after discovery and surfaced with a bounded reason
  (`version_unsupported`).
- Skipped collector status completeness: `collector_status.json` now
  reports every gated collector with its precise reason
  (`version_unsupported`, `extension_missing`, `config_disabled`)
  instead of silently omitting them. Unscoped exports also synthesise
  the file from `query_runs` rather than emitting an empty list.
- Skipped/failed runs no longer advance cadence: only `status='success'`
  rows count toward the next-due time, so transient failures and
  gating misconfigurations are retried on the next cycle instead of
  being hidden behind a full cadence window.
- Timeout setup failures now fail collection safely: a `SET LOCAL`
  failure on `statement_timeout`, `lock_timeout`, or
  `idle_in_transaction_session_timeout` aborts the cycle rather than
  warning and continuing without timeout safety.
- Export integrity hardened: a successful `query_run` whose result is
  missing or whose payload fails to decode now fails the export with
  a bounded error instead of silently omitting it.
- `/status` surfaces database read errors with HTTP 500 instead of
  returning zero counts that can mask storage failures.
- `/export` validates `since`/`until` as RFC3339 and rejects inverted
  ranges with HTTP 400 and bounded JSON error bodies.
- `retention_days <= 0` documented as cleanup disabled: warning text,
  schema appendix, and validation behaviour now agree that
  non-positive values disable cleanup.
- Savepoint error handling improved: `SAVEPOINT`,
  `ROLLBACK TO SAVEPOINT`, and `RELEASE SAVEPOINT` failures now abort
  the cycle with explicit errors instead of being discarded.
- `sslmode` validation tightened: values outside the libpq enum
  (`disable`, `allow`, `prefer`, `require`, `verify-ca`,
  `verify-full`) are rejected at startup. Production TLS still treats
  only `verify-ca` and `verify-full` as strong.
- `/collect/now` body size limit added: a 64 KiB cap via
  `http.MaxBytesReader`. Oversize requests return HTTP 413 with an
  `error=body_too_large` audit event.
- Panic recovery now returns JSON: the recovery middleware emits
  `Content-Type: application/json` so clients that branch on the
  response content type no longer break on a 500.
- gitleaks release install now checksum-verified in CI and release
  workflows: the tarball SHA-256 is checked against the upstream
  `gitleaks_<version>_checksums.txt` file before extraction.

### Tests
- 19 new tests in `tests/signals_codex_post_031_test.go` covering
  every fix above, plus an updated `TestCollectNowLargeBody` for the
  413 contract.

### Notes
- No breaking changes.
- No spec changes.
- API responses remain backward compatible; new validations only
  reject previously undefined/invalid input.
- Docker base image digest pinning remains deferred — `golang:1.25-alpine`
  and `alpine:3.21` continue to be tag-pinned. The remaining half of
  L-003 from the Codex review is scheduled for a follow-up that
  updates the Dockerfile, Trivy baseline, and release-verification
  doc together.
- R080 (per-collector export view) is not included in this release.

---

## [0.3.1] - 2026-04-25

### Added
- Target narrowing for POST /collect/now (R082 Phase 1)
- request_id and reason support with end-to-end audit trace (R082 Phase 2)
- Managed mode with dual-token authentication and actor separation (R083)
- Control-plane documentation and audit model documentation

### Fixed
- Audit completeness: no silent request loss (overlap path now emits collect_now_dropped)
- Audit metadata allowlist for curated fields (mode_configured event preserved)
- Warning on empty control-plane token after rotation

### Tests
- Adversarial tests for malformed JSON, large payloads, and concurrent requests
- Race detector coverage for concurrent POST /collect/now

### Notes
- No breaking changes
- No spec changes
- R080 (per-collector export view) not included in this release

---

## [0.3.0] - 2026-04-25

### Added

- High-sensitivity collectors are now explicit opt-in (R075)
- Strict startup configuration validation with fail-fast behavior (R076)
- Atomic collection-cycle persistence (R077)
- Structured audit logging and export metadata for compliance visibility (R078)
- Optional Prometheus `/metrics` endpoint for Arq Signal health (R079)
- Version-aware query catalog with per-major PostgreSQL support (14–18, 19 placeholder) (R081)
- Multi-arch container release (linux/amd64 + linux/arm64) with SBOM, provenance, and cosign signing
- Release verification documentation

### Fixed

- PostgreSQL 18 compatibility issues in `pg_stat_io_v1` and `pg_stat_wal_v1`
- Concurrency issue in ULID generation during multi-target collection
- Connection string construction safety improvements
- Production-readiness fixes from April 2026 code review

---

## [0.2.1] - 2026-03-23

### Security

- Upgrade Go from 1.24 to 1.25 (resolves CVE-2026-25679 HIGH,
  CVE-2026-27142, CVE-2026-27139)
- Upgrade runtime base image from Alpine 3.20 to 3.21
- Add container securityContext: readOnlyRootFilesystem, drop ALL
  capabilities, disallow privilege escalation
- Add pod-level seccompProfile (RuntimeDefault) and runAsGroup
- Add Dockerfile HEALTHCHECK instruction

### Changed

- Helm chart version: 0.2.0 → 0.2.1
- Trivy config findings: 1 HIGH + 3 MEDIUM + 6 LOW → 0 HIGH + 1 MEDIUM (false positive) + 1 LOW (false positive)

---

## [0.2.0] - 2026-03-14

### Added

- **Diagnostic Pack 1** — 9 new collectors: server identity, extension
  inventory, checkpoint/bgwriter health, long-running transactions,
  blocking locks, login role inventory, connection utilization, planner
  statistics staleness, pg_stat_statements reset check
- **Server Survival Pack** — 8 new collectors: replication slot risk,
  replication status/lag, checkpointer stats (PG 17+), vacuum health,
  idle-in-transaction offenders, database sizes, largest relations,
  temp I/O pressure
- Dynamic pg_stat_statements capture (SELECT *) for cross-version
  compatibility across PostgreSQL 14–18
- Savepoint-based query isolation — single query failure no longer
  aborts the entire collection transaction
- STDD specification expanded to 56 requirements
- Smoke-tested against PostgreSQL 14, 15, 16, 17, and 18

### Fixed

- NULL payload on zero-row query results (EncodeNDJSON nil guard)
- pg_stat_bgwriter column compatibility on PG 17+ (uses SELECT *)
- Planner stats staleness query round() type compatibility
- Transaction commit error handling (commit failure now blocks
  downstream persistence)

### Changed

- Total collectors: 12 → 29
- Total tests: 94 → 135
- Total STDD requirements: 26 → 56
- Replication-related collectors return empty results gracefully
  when replication is not configured
- pg_stat_checkpointer collector skipped on PG < 17 via version filter

---

## [0.1.0] - 2026-03-14

### Added

- Initial open-source release of Arq Signals
- PostgreSQL diagnostic signal collector with 12 versioned SQL collectors
- Read-only enforcement at three independent layers (static linter, session-level, per-query transaction)
- Cadence-based scheduling (5m, 15m, 1h, 6h, 24h, 7d)
- Automatic PostgreSQL version and extension detection
- Query filtering by PostgreSQL version and available extensions
- NDJSON result format with automatic gzip compression for payloads exceeding 4KB
- Snapshot export as ZIP archive (`arq-snapshot.v1` format)
- SQLite local storage with WAL mode
- HTTP API with bearer token authentication (health, status, collect, export)
- CLI tool (`arqctl`) for collection and export operations
- Concurrent multi-target collection with configurable worker pool
- Per-query and per-target timeout budgets
- Credential management via file, environment variable, or pgpass (never cached or exported)
- Docker support with non-root runtime (UID 10001, Alpine 3.20)
- BSD-3-Clause license

### Security

- Three-layer read-only enforcement prevents any write operations
- Static SQL linter rejects DDL, DML, and dangerous functions at startup
- Credentials are never stored in SQLite or included in exports
- No outbound network connections except to configured PostgreSQL targets
- API binds to loopback by default
