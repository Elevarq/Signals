# Security Policy

[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/13020/badge)](https://www.bestpractices.dev/projects/13020)

## Reporting a vulnerability

If you find a vulnerability in Elevarq Signals, please report it privately.
Do **not** open a public GitHub issue.

**Preferred channel — GitHub Private Vulnerability Reporting:**
[github.com/Elevarq/signals/security/advisories/new](https://github.com/Elevarq/signals/security/advisories/new).
The thread is end-to-end encrypted to the project maintainers, and we
can collaborate on a fix and a coordinated disclosure inside the
advisory.

**Backup channel — email:** `security@elevarq.com`. Useful when GitHub
PVR is not an option for you (e.g. you don't have a GitHub account
or your policy forbids signing in).

Please include:

- A description of the vulnerability and your evaluation of its
  severity.
- Reproduction steps or a minimal proof-of-concept (if available).
- The version(s) you tested against (image tag, chart version, or
  commit SHA).
- Whether you intend to disclose publicly and your preferred timeline.

We aim to **acknowledge receipt within two business days** and to
**publish a fix or a documented mitigation within ten business days**
of acknowledgement for High/Critical issues. These are best-effort
targets for a small maintainer team; we will keep you informed inside
the advisory thread if a finding needs longer. For Low/Medium issues
the cadence is the next routine release.

## Supported versions

Elevarq Signals is **pre-1.0 / Beta**. Only the **latest tagged release on
the active line** receives security fixes:

| Version line | Status |
|---|---|
| `v0.10.0-beta.x` | **Active** — receives security fixes |
| `v0.9.x` | End-of-life (superseded by the Beta line) |
| `v0.3.x` and older | Unsupported |

Once `v0.10.0` (stable) ships, the latest stable release on the most
recent minor will be the supported line; earlier minors will receive
only Critical fixes for one minor cycle.

## Security model

### Read-only PostgreSQL access

Elevarq Signals enforces read-only access at three independent layers:

1. **Static SQL lint at registration.** Every catalog query is
   validated at process startup; any DDL, DML, or dangerous-function
   reference aborts startup before a single connection is opened.
2. **Session level.** Every monitoring session is configured with
   `default_transaction_read_only=on`.
3. **Per-query.** Every collector query runs inside a
   `BEGIN ... READ ONLY` transaction.

The connecting role is also verified to be `NOSUPERUSER`,
non-replication, non-bypassrls, and not a member of
`pg_write_all_data` before any collector query runs; a failure aborts
the cycle.

### Credentials

- Passwords are read from file or environment at connection time.
- Passwords are not cached beyond a single connection attempt and are
  not written to the local SQLite store.
- DSNs and passwords never appear in exports, logs, the metrics
  endpoint, or audit events (a central audit-attribute denylist
  scrubs secret-shaped keys before any slog record is emitted).
- Password rotation is supported (re-read on every new connection).

### Network

- The HTTP API binds to `127.0.0.1:8081` by default.
- Elevarq Signals makes **no outbound network connections** other than to
  the configured PostgreSQL targets. No telemetry, no AI providers,
  no analytics, no auto-update.
- The optional Prometheus `/metrics` endpoint is **off by default**.

### Sensitivity policy and what an export contains (R075 v2)

The default export carries **operational diagnostic data**. By design,
a default install collects everything that is useful for downstream
analysis, including some classes of application-authored text. This
is a deliberate choice: collection is fully **local** (no data leaves
the operator's environment), the export ZIP self-identifies the
effective sensitivity state, and operators who prefer privacy over
diagnostic richness have a single config flag to opt out.

A default-install export can contain:

- Live `pg_stat_activity` statement text (left-truncated, 200 chars):
  `long_running_txns_v1`, `blocking_locks_v1`,
  `idle_in_txn_offenders_v1`, `wraparound_blockers_v1`.
- Application-authored SQL definitions: `pg_views_definitions_v1`,
  `pg_matviews_definitions_v1`, `pg_triggers_definitions_v1`,
  `pg_functions_definitions_v1`.
- Sampled-value statistics: `pg_stats_extended_v1`,
  `pg_statistic_ext_data_mcv_v1`; optionally
  `pg_stats_array_range_v1` when the per-collector flag is set.
- RLS policy bodies: `pg_policies_v1`.
- Rewrite-rule bodies: `pg_rules_v1`.

These are all classified `HighSensitivity = true` in the registry.
**The privacy opt-out** is one config flag:

```yaml
signals:
  high_sensitivity_collectors_enabled: false
```

(or `SIGNALS_HIGH_SENSITIVITY_COLLECTORS_ENABLED=false`). When set
to `false`, the opt-out branches **per collector** based on whether
the row carries non-sensitive diagnostic columns:

- **Redact path** (live `pg_stat_activity` collectors): the collector
  still runs; its declared sensitive columns (`query_snippet`,
  `blocked_query`, `blocking_query`) are set to `NULL` in persisted
  output. Non-sensitive columns (`pid`, `wait_event`,
  `txn_age_seconds`, `waiting_seconds`, …) survive, so
  blocking-lock-chain shape and idle-in-txn / long-running-tx
  visibility remain.
- **Skip path** (collectors whose row is itself the sensitive
  payload — DDL definitions, sampled-value stats, RLS policies,
  rewrite rules): the collector is dropped; it appears in
  `collector_status.json` as `status=skipped, reason=config_disabled`.

The effective state is recorded in
`metadata.json.high_sensitivity_collectors_enabled` so a consumer or
auditor can tell at a glance whether sensitive data may be present in
the artifact — without parsing the body.

The toggle is a local operator control over data sensitivity. It is
not an exfiltration boundary; Elevarq Signals does not transmit data
outside the operator's environment regardless of this setting.

### What is never persisted or exported

- Passwords / DSNs / API tokens (filtered out of audit events;
  redacted in any error message that quotes a connection string;
  scrubbed from FDW option values via dedicated redaction).
- SSL configuration material.
- Connection identity beyond `host` / `port` / `dbname` / `username`
  in the export's identity block — no `sslmode`, no secret reference.
- Replication topology credentials.

## Supply chain & artifact integrity

Every release ships verifiable artefacts:

- **Container image**: multi-arch (`linux/amd64`, `linux/arm64`) at
  `ghcr.io/elevarq/signals`. Cosign-signed (keyless, GitHub OIDC,
  Sigstore Fulcio). SBOM attached both as an OCI attestation
  (`sbom: true` in BuildKit) and as a re-attested
  `cosign attest --type spdxjson` predicate. SLSA build provenance
  (`provenance: mode=max`) attached as a verifiable attestation.
- **Helm chart**: `oci://ghcr.io/elevarq/charts/signals`,
  cosign-signed under the same trust root as the container image.
- **Release assets**: per-platform binaries, `sbom.spdx.json`, and a
  `SHA256SUMS` file pinned on the GitHub Release page; the Release is
  flagged `prerelease` while we are on the Beta line.

Reproducibility / supply-chain controls:

- Every `uses:` line in `.github/workflows/*.yml` is pinned to a
  40-char commit SHA, with the human-readable version in a trailing
  comment. Every `FROM` in `Dockerfile` is pinned to
  `<image>:<tag>@sha256:<digest>`.
- Dependabot watches `gomod` and `github-actions` weekly and
  refreshes both the SHA and the trailing version comment in
  lockstep.
- Branch protection on `main`: required status checks (`test`,
  `security-scan`, `Analyze (go)`, `Analyze (actions)`, `CodeQL`),
  strict (branch must be up-to-date), linear history, no force
  pushes, no deletions.

### Verifying a release

Command-line verification of the image and the chart (replace the
version):

```bash
cosign verify ghcr.io/elevarq/signals:<VERSION> \
  --certificate-identity-regexp='https://github.com/Elevarq/(Arq-Signals|signals)/.github/workflows/release.yml@' \
  --certificate-oidc-issuer='https://token.actions.githubusercontent.com'

cosign verify ghcr.io/elevarq/charts/signals:<VERSION> \
  --certificate-identity-regexp='https://github.com/Elevarq/(Arq-Signals|signals)/.github/workflows/release.yml@' \
  --certificate-oidc-issuer='https://token.actions.githubusercontent.com'
```

Full step-by-step verification (manifest digest, SBOM attestation,
SLSA provenance, Trivy on the published image) lives in
[`docs/release-verification.md`](./docs/release-verification.md).

## What enforces this in CI

Push- and PR-time gates (`.github/workflows/ci.yml` + the local
`scripts/preflight.sh`): `gofmt`, `go vet`, full test suite,
`golangci-lint`, `gitleaks` (secrets), `govulncheck`, Trivy
filesystem + config scans, `semgrep`, `osv-scanner`, `kube-linter` +
`conftest` on the rendered Helm chart, and `hadolint` on the
Dockerfile.

Scheduled evidence (`.github/workflows/nightly-security.yml`,
07:23 UTC daily): full-history Gitleaks, source-level SBOM via syft,
source-level vuln scan via grype, OpenSSF Scorecard. Findings land in
the Security tab as SARIF.

## Container hardening

When deployed via the published container:

- Runs as a non-root user (`UID 10001`).
- Minimal Alpine base, pinned by digest. No shell or compilers in the
  runtime image beyond what BusyBox / Alpine provides.
- The Helm chart's PodSecurityContext sets
  `automountServiceAccountToken=false`, no hostNetwork / hostPID /
  hostIPC, `runAsNonRoot: true`, drops all capabilities, and forbids
  privilege escalation. Conftest policies in `policy/security.rego`
  enforce these at chart-render time.
