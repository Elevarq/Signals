# Changelog

All notable changes to Arq Signals will be documented in this file.

This project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- **Secret-store credential provider (`auth_method: secret_store`, #93,
  #97).** Fetch a static database password from a cloud secret store using
  the collector's ambient cloud identity and apply it as the connection
  password - keeping the credential out of Signals' config and off disk
  while leaving rotation to the vault. The backend is inferred from the
  shape of `secret_ref`: an AWS Secrets Manager ARN, an Azure Key Vault
  secret URI, or a GCP Secret Manager resource name; a reference matching
  none is a hard startup error naming the three accepted forms. **The AWS
  Secrets Manager path is production-grade** (region taken authoritatively
  from the ARN, never `AWS_REGION` / the SDK default chain / IMDS); Azure
  Key Vault and GCP Secret Manager references are validated at startup but
  their production fetchers are deferred behind the same interface and fail
  at connect time with a clear "backend not available in this build" error
  (backend support scaffolded; production fetchers tracked separately in
  #108) without stopping collection for other targets.
  An optional `secret_json_key` extracts a named key from a JSON secret
  (raw value otherwise); extraction failures never echo the raw secret. The
  fetched secret is cached per target with a reuse bound of `min(vault TTL,
  max_cache_ttl)` - with neither set it is re-fetched on every reconnect so
  a rotated secret is picked up without a restart. Validation enforces the
  passwordless and `verify-full` TLS floors at startup; the secret is never
  stored, exported, or logged (metadata only). Reuses the shared
  credential-provider scaffolding from #94. Live behaviour covered by an
  env-gated smoke (`ARQ_SIGNALS_INTEGRATION_LIVE=1`).
- **GCP Cloud SQL IAM credential provider (`auth_method:
  gcp_cloudsql_iam`, #93, #96).** Connect passwordlessly to Cloud SQL for
  PostgreSQL using Cloud SQL IAM database authentication: a short-lived
  Google OAuth2 access token acquired from the collector's ambient Google
  identity (Application Default Credentials - environment / GKE workload
  identity / service-account key / `gcloud auth application-default
  login`). The token (scope fixed at
  `https://www.googleapis.com/auth/sqlservice.login`) is the connection
  password over a direct libpq `verify-full` channel - the token-as-
  password seam, not the Cloud SQL Go Connector; no secret is stored in
  Signals' config. Tokens are cached per target and re-acquired ~5 minutes
  before their ~60 minute expiry, never shared across targets, and never
  logged or exported (metadata only). Validation enforces the passwordless
  and `verify-full` TLS floors at startup; an optional
  `gcp_impersonate_service_account` lets the ambient identity impersonate a
  per-target service account, and an undiscoverable identity or denied
  impersonation is a connect-time, target-scoped failure that does not stop
  collection for other targets. The Google SDK is linked only on the
  `gcp_cloudsql_iam` path - password targets require no Google credentials.
  Reuses the shared credential-provider scaffolding from #94. Live
  behaviour covered by an env-gated smoke
  (`ARQ_SIGNALS_INTEGRATION_LIVE=1`).
- **Azure Entra ID credential provider (`auth_method: azure_entra`, #93,
  #95).** Connect passwordlessly to Azure Database for PostgreSQL -
  Flexible Server using a short-lived Microsoft Entra ID access token
  acquired from the collector's ambient Azure identity (the
  `DefaultAzureCredential` chain: environment / AKS workload identity /
  managed identity / Azure CLI). The token (scope fixed at
  `https://ossrdbms-aad.database.windows.net/.default`) is the connection
  password; no secret is stored in Signals' config. Tokens are cached per
  target and re-acquired ~5 minutes before their ~60-90 minute expiry,
  never shared across targets, and never logged or exported (metadata
  only). Validation enforces the passwordless and `verify-full` TLS
  floors at startup; a user-assigned managed identity is disambiguated by
  the optional `azure_client_id` (then `AZURE_CLIENT_ID`), and an
  undiscoverable or ambiguous identity is a connect-time, target-scoped
  failure that does not stop collection for other targets. The Azure SDK
  is linked only on the `azure_entra` path - password targets require no
  Azure credentials. Reuses the shared credential-provider scaffolding
  from #94. Live behaviour covered by an env-gated smoke
  (`ARQ_SIGNALS_INTEGRATION_LIVE=1`).
- **AWS RDS/Aurora IAM credential provider (`auth_method: aws_rds_iam`,
  #93, #94).** Connect passwordlessly to Amazon RDS / Aurora PostgreSQL
  using a short-lived RDS IAM auth token minted from the collector's
  ambient AWS identity (SDK default credential chain: env / shared
  config / EC2 instance profile / ECS task role / EKS IRSA / Pod
  Identity). The token is the connection password; no secret is stored
  in Signals' config. Tokens are cached per target and re-minted ~3
  minutes before their 15-minute expiry, never shared across targets,
  and never logged or exported (metadata only). Validation enforces the
  passwordless and `verify-full` TLS floors at startup; a missing region
  is a startup warning resolved from `AWS_REGION` /
  `AWS_DEFAULT_REGION` / instance metadata (IMDS) at connect time. The
  AWS SDK is linked only on the `aws_rds_iam` path - password targets
  require no AWS credentials. Introduces the shared credential-provider
  scaffolding (`auth_method` dispatch, per-target token cache,
  `BeforeConnect` wiring) that sibling providers reuse. Live behaviour
  covered by an env-gated smoke (`ARQ_SIGNALS_INTEGRATION_LIVE=1`).

### Changed

- **Public binaries renamed to `signals` (daemon) and `signalsctl`
  (CLI) (#125).** The open-source collector now ships under its own
  unbranded names so it can stand on its own, independent of the
  commercial Elevarq products. The container image moves to
  `ghcr.io/elevarq/signals` (Docker Hub mirror `elevarq/signals`), the
  Helm chart is renamed to `signals`, and the demo/quickstart surface
  (Dockerfiles, compose files, Helm install commands, Prometheus/Grafana
  examples, cloud deploy templates, and user-facing help text) now uses
  the new names throughout. The old names `arq-signals` and `arqctl` are
  retained as deprecation aliases: invoking either prints a one-line
  stderr warning and otherwise behaves identically. The aliases are
  removed one release after launch (tracked in #62). The Go module path
  (`github.com/elevarq/arq-signals`), the GitHub repository URL, the
  `ARQ_SIGNALS_*` environment variables, and the PostgreSQL
  `application_name = 'arq-signals'` collector identity are intentionally
  unchanged in this phase (config and integration interfaces, not binary
  branding).

## [0.10.0-beta.5] - 2026-06-11

Re-cut of v0.10.0-beta.4: that tag's publish job died at the Docker
Hub login (expired mirror token) before the GHCR image, chart, and
GitHub Release published (#82). Same data-collection layer as
beta.4 - only the release pipeline changed (Docker Hub login is now
non-fatal; GHCR-only on mirror-credential failure). beta.4 preserved
below for history.

## [0.10.0-beta.4] - 2026-06-11

First release carrying the TimescaleDB / Tiger Data collector family.

### Added

- **TimescaleDB detection and metadata collectors (#73, #75).** Twelve
  `timescaledb_*_v1` collectors (extension/license detection,
  hypertables, dimensions, chunk summary + recent chunks, compression
  settings + stats, continuous aggregates, policies, jobs + job
  stats, job errors) behind feature detection - safe no-ops on plain
  PostgreSQL, least-privilege (documented views only), bounded,
  redaction-aware, with `object_missing` accounting and
  extension-version gating (R114/R115).
- TimescaleDB demo fixture `examples/timescaledb-demo/` exercising
  the full collector family deterministically (#76, #79); consumed by
  the arq-timeseries-demo product-demo repository.

### Fixed

- Builder image pinned to golang 1.25.11-alpine, matching go.mod
  (#77, #78) - image builds were broken on main.

### Security

- Diagnostic DSN builder (`collector.BuildSafeDSN`, used by `arqctl
  doctor` C4 and `arqctl connect test`) now libpq-quotes every
  string-valued field, so a password or other field value containing
  whitespace or an embedded `key=value` sequence can no longer inject,
  override, or remove connection parameters (e.g. silently downgrading
  TLS or re-targeting the host). `RedactDSN` masks the quoted password
  form too. New rule ARQ-SIGNALS-R111 / INV-SIGNALS-21. (#69)
- API auth: a valid bearer token now authenticates before the per-IP
  invalid-attempt limiter is consulted, so the `429` lockout can no
  longer be weaponised. Previously a shared source IP (NAT, reverse
  proxy, or co-located pod) flooding invalid tokens could lock the
  legitimate operator or Arq control plane out of
  pause/resume/reload/export. The brute-force throttle on invalid
  attempts is unchanged. New rule ARQ-SIGNALS-R112 /
  INV-SIGNALS-22. (#68)
- API can now terminate TLS at the daemon (`api.tls_cert_file` /
  `api.tls_key_file`, env `ARQ_SIGNALS_API_TLS_CERT_FILE` /
  `ARQ_SIGNALS_API_TLS_KEY_FILE`). Set both to serve HTTPS (minimum
  TLS 1.2), neither for plain HTTP (loopback default); setting exactly
  one is a hard config error — no cleartext fallback. Closes the
  cleartext bearer-token / `/export` exposure when the listener is
  bound beyond loopback (the Helm `0.0.0.0` bind). The Helm chart adds
  an `api.tls` value, fails rendering when `networkPolicy.enabled` but
  `targetCIDRs` still holds the `0.0.0.0/0` placeholder, and warns at
  install time when the exposed API has neither TLS nor a
  NetworkPolicy. New rule ARQ-SIGNALS-R113 / INV-SIGNALS-23. (#67)

### Added

- TimescaleDB / Tiger Data collector family (R114): 12 new
  collectors under category `timescaledb` — extension detection with
  edition + feature-probed capability flags, hypertables, dimensions,
  chunks (newest-first, capped at 5000, with a complete
  per-hypertable summary making truncation detectable), approximate
  hypertable sizes, compression settings and before/after stats,
  continuous aggregates, and background jobs / job stats / job errors
  (capped at 1000). Reads only the documented PUBLIC-readable
  `timescaledb_information` views and the monitoring-priced
  `hypertable_approximate_detailed_size()` /
  `hypertable_compression_stats()` functions — no internal
  `_timescaledb_*` catalogs, no per-chunk-locking exact size
  functions. Inert on plain PostgreSQL (`extension_missing` skip,
  INV-SIGNALS-24); `view_definition` / `err_message` follow the R075
  redact path. Supported: TimescaleDB 2.17–2.27 on PG 14–18
  (best-effort 2.14–2.16, detection-only below). Docs:
  `docs/collectors.md`, `docs/postgres-role.md`,
  `docs/timescaledb-collectors-design.md`,
  `docs/timescaledb-analyzer-roadmap.md`. (#73)
- Extension-version gating (R115): discovery now captures
  `pg_extension.extversion`; collectors may declare
  `RequiresExtensionMinVersion` (gated as `version_unsupported`,
  fail-open on unknown/unparsable versions), and SQLSTATE
  42P01/42883 execution failures are classified as the new
  structured `object_missing` reason instead of a generic
  `execution_error`. (#73)
- OpenSSF Best Practices **passing** badge awarded
  ([bestpractices.dev/projects/13020](https://www.bestpractices.dev/projects/13020));
  badge added to README, SECURITY.md, and `docs/release-verification.md`.
  Clears the last Scorecard `CIIBestPracticesID` finding. (#31)

## [0.10.0-beta.3] - 2026-05-28

**Re-cut after release-pipeline fix.** `v0.10.0-beta.2`'s container
image and GitHub Release were published successfully, but the
`publish-chart` job failed to sign the Helm chart with cosign — the
job authenticated GHCR via `helm registry login` (helm's own config),
not the docker keychain cosign reads. The fix
(`docker/login-action` before `Sign chart`) is now on `main`. Same
data-collection layer as beta.2; this artifact's chart is signed.
Also fixes the `prerelease` flag on the GitHub Release page that beta.2
was missing (`action-gh-release@v3` no longer auto-detects from the
semver suffix). (#50)

## [0.10.0-beta.2] - 2026-05-28

**Beta cut after Scorecard hardening.** `v0.10.0-beta.1` was tagged but
its release pipeline run was cancelled (stuck on multi-arch GHCR
publish); the GitHub Release page was never created. This Beta cut is
the same data-collection layer (no user-observable daemon behaviour
change vs beta.1) but built and published from a `main` whose CI /
supply-chain posture has been hardened — see Security below. Consumers
should pin `0.10.0-beta.2`; `:latest` is unmoved (pre-release).

### Security

- All GitHub Actions and Docker base images in
  `ci.yml` / `release.yml` / `nightly-security.yml` / `Dockerfile` are
  pinned by 40-char content SHAs (#44). Defends against tag-rewriting
  / repo-takeover supply-chain attacks; Dependabot
  (`.github/dependabot.yml`, #34) keeps both the SHA and the
  human-readable version comment fresh.
- `release.yml` top-level workflow permissions reduced to
  `contents: read`. Writes (`packages: write`, `id-token: write`,
  `attestations: write`, `contents: write` on the release job) are
  declared per-job following the `nightly-security.yml` pattern (#39).
- `golang.org/x/sys` bumped past **CVE-2026-39824** (#30). The
  vulnerable `windows.NewNTUnicodeString` is not reachable in our
  `linux` / `darwin` builds, but the bump clears the indirect-vuln
  trail.
- `go install golang.org/x/vuln/cmd/govulncheck` pinned by SHA to the
  v1.3.0 commit (#47).
- Native Go fuzz targets added for `RedactDSN` and `DecodeNDJSON`
  (#43). >450k iterations across both targets find no panics or
  property violations at this commit.
- Branch protection applied to `main`: required status checks
  (`test`, `security-scan`, `Analyze (go)`, `Analyze (actions)`,
  `CodeQL`), `strict` (require branch up-to-date), linear history,
  no force pushes, no deletions.

### Changed (deps)

- Dependabot routine bumps: `hadolint/hadolint-action` ->
  `3.3.0` (#35), `azure/setup-helm` -> `v5` (#36),
  `actions/download-artifact` -> `v8` (#37),
  `softprops/action-gh-release` -> `v3` (#38),
  `actions/upload-artifact` -> `v7` (#40), and a gomod minor-group
  bundle across four modules (#41).

## [0.10.0-beta.1] - 2026-05-28

**First Beta release.** The data-collection layer has been cleared as
Beta-ready by external review (four passes, all P0/P1 findings
resolved); the operator-facing documentation has been swept to align
with the current behavior. This release contains significant
correctness and observability improvements to the default export, the
sensitivity model, and the collection lifecycle, plus a new
Helm-chart OCI publishing path.

Pre-release: the container `:latest` floating tag does **not** move
to this image — explicit pin to `0.10.0-beta.1` is required. The
GitHub release is marked `prerelease`. Analyzer-side ingest-contract
work tracking these producer changes lives in `Elevarq/Arq` (#906,
#907) and is not yet shipped.

### Added

- Release workflow now publishes the Helm chart as an OCI artifact to
  `oci://ghcr.io/elevarq/charts/arq-signals`, cosign-signed with the
  same keyless GitHub OIDC identity as the container image. The chart
  version is stamped from the release tag at package time. (#3)
- Collector freshness metadata in `collector_status.json`: each entry
  carries its `cadence` and a `freshness` classification
  (`fresh`/`stale`), and target-scoped exports enumerate
  eligible-but-never-run collectors as `never_run` so consumers can
  detect missing coverage. (R107, #5)

### Changed

- **Sensitivity policy: collect-everything default, privacy opt-out
  with per-collector redact/skip behavior.** High-sensitivity
  collectors run by default
  (`signals.high_sensitivity_collectors_enabled` defaults to `true`).
  The opt-out (`= false`, a one-time startup setting) behaves per
  collector based on whether the row carries non-sensitive diagnostic
  columns:
  - **Redact** path — the 4 live `pg_stat_activity` collectors
    (`long_running_txns_v1`, `blocking_locks_v1`,
    `idle_in_txn_offenders_v1`, `wraparound_blockers_v1`) keep running
    with their `query_snippet` / `blocked_query` / `blocking_query`
    columns set to `NULL`. Non-sensitive columns (`pid`, `wait_event`,
    `txn_age_seconds`, `waiting_seconds`, …) survive, so
    blocking-lock-chain shape and idle-in-txn / long-running-tx
    visibility remain.
  - **Skip** path — DDL-definition collectors and other
    whole-row-sensitive collectors are dropped as before (recorded
    `status=skipped, reason=config_disabled`).
  Each collector declares its branch via `QueryDef.SensitiveColumns`
  (non-empty → redact; empty/nil → skip).
  `metadata.json.high_sensitivity_collectors_enabled` records the
  effective state. (R075 revised v2, #6)

### Fixed

- Default export no longer drops lower-cadence collectors. The default
  scope now assembles the latest run of **each** collector per active
  target (`latest-per-collector`) instead of only the single most
  recent snapshot, which could omit 15m/1h/6h/24h evidence right after
  a 5m cycle. `metadata.json` gains a `run_scope` marker
  (`latest-per-collector` | `snapshot`). Selector exports (`--all`,
  `--snapshot-id`, `--since/--until`) are unchanged. (R084, #5)
- Collection cycles that exhaust a target's per-cycle time budget now
  record every remaining due collector as
  `skipped`/`reason=budget_exhausted` instead of leaving them with no
  row, so the status inventory is always complete (one run per due
  collector). Such cycles are reported as `partial`, and the
  bookkeeping commit/persistence no longer runs under the elapsed
  budget so an over-budget cycle still persists its full inventory.
  (R108, #8)
- Disabled or removed targets no longer linger as active. The daemon
  reconciles `targets.enabled` against config on startup and on every
  reload (soft-disable; snapshots retained), and the default export +
  `GET /status` now exclude disabled targets. `--all` still surfaces
  their history for forensics. (R109, #7)
- Exports no longer tear when retention cleanup runs concurrently. An
  export composes the ZIP from several sequential reads of the local
  store; if retention's `DeleteSnapshotsOlderThan` /
  `DeleteQueryRunsOlderThanByClass` committed between those reads, the
  export could end up referencing a row that had just been removed
  (most visibly the `missing result payload for successful run` hard
  error). Exports now take a shared read lock; the destructive
  retention writes take an exclusive write lock. Concurrent exports
  remain non-blocking; concurrent collection commits are not gated
  (additive only — no tear). (R110, #10)
- `GET /status.target_count` now matches the number of enabled targets
  surfaced in the response (was using the unfiltered `GetTargets()`
  count, so disabled targets still bumped it). Restores
  INV-SIGNALS-14 agreement with the default export's active-target
  set. (#16)
- `collector.Reload` now returns an `error` and propagates
  `ReconcileEnabledTargets` failures instead of logging-and-swallowing
  them. The reconcile runs **before** the in-memory target swap, so a
  reconcile failure aborts the reload cleanly without mutating
  in-memory state. `POST /reload` returns 500 on failure; the SIGHUP
  path audit-logs `config_reload_rejected` with
  `reason=reconcile_failed` (matching the load/validate-rejected
  pattern). (#16)
- `pg_stats_array_range_v1` no longer disappears silently when its
  per-collector opt-in (`collect_array_range_histograms`, default
  `false`) is off. The collector now appears in `collector_status.json`
  as `status=skipped, reason=config_disabled` — matching the EA-R001
  status-completeness guarantee already provided for
  `version_unsupported` / `extension_missing` / `config_disabled`. (#18)

### Documentation

- Operator-facing sensitivity docs aligned with R075 (revised v2):
  `appendix-b-configuration-schema.md` example, env-var table, and
  the "High-sensitivity collectors" section reflect the default-on /
  opt-out-redact-or-skip semantics.
  `specifications/sensitivity-profiles.md` step 2 distinguishes the
  redact-path (collectors stay eligible, sensitive columns nulled)
  from the skip-path (collector dropped), and notes the `restricted`
  per-target profile remains stricter than the daemon-wide opt-out
  (drops all `HighSensitivity=true` regardless of
  `SensitiveColumns`). (#18)
- Pre-Beta sweep of operator-facing docs to match R075 (revised v2):
  `README.md` "Off-by-default surfaces" rewritten as
  "Operator-controlled sensitivity"; `docs/postgres-role.md`
  "High-sensitivity collectors" section rewritten (default-on, two
  opt-out branches, skip vs redact); Grafana dashboard panel
  description for `arq_signal_high_sensitivity_collectors_enabled`
  updated; `features/arq-signals/traceability.md` R075/R070 rows
  rewritten; spec entries for the skip-path stats collectors
  (`pg_stats_array_range_v1.md`, `pg_statistic_ext_data_v1.md`)
  realigned with the redact-or-skip model. (#20)
- Final spec cleanup: `pg_stats_extended_v1.md` (+ acceptance) and
  `pg_policies_v1.md` Status/Invariants/Configuration/Mitigations
  sections rewritten for the default-on skip-path classification. No
  stale "opt-in / off-by-default" wording remains in operator-facing
  markdown. (#22)
- Snapshot inspection guide and reference example refreshed to the
  current export shape: `metadata.json` now shows `snapshot_count`,
  `ingest_mode`, `run_scope`, `high_sensitivity_collectors_enabled`,
  and `target_identity` (R086/R094); a `collector_status.json`
  example with `cadence`/`freshness` (R107) is included; the file
  table lists `snapshots.ndjson` and `collector_status.json`
  (INV-SIGNALS-11). (#23)

## [0.9.0] - 2026-05-27

### Added

- Broader read-only catalog coverage: additional collectors capture
  user-defined catalog objects and extended statistics so dependent
  tables and queries are fully represented in a snapshot for downstream
  analysis.
- Optional per-collector view in the export ZIP.
- Extra PG 14+ session counters in `pg_stat_database`; role `oid` in
  `login_roles` for stable role-name resolution; `pg_settings` context
  and value bounds.

Still read-only by design — three-layer enforcement, no write
operations, no telemetry, no AI.

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
