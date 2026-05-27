# Operational readiness — health, diagnostics, support

Tracking: [#138](https://github.com/Elevarq/Arq-Signals/issues/138).

This page documents the runtime observability surface Arq Signals
exposes, the closed taxonomy operators use to triage state, and
the support-bundle guidance for handing evidence to Elevarq
without leaking sensitive data. Suitable to cite alongside
[`docs/security/access-control.md`](../security/access-control.md)
for SOC 2 / ISO 27001 evidence on **operational monitoring** and
**incident response**.

## Closed runtime-state taxonomy

A running Signals daemon is always in exactly ONE of the
following states. Each state has a deterministic operator-action
recommendation.

| State | Meaning | Operator action |
|---|---|---|
| **healthy** | Last collection succeeded for every enabled target within the configured `poll_interval`. Storage writable. API serving. | None. |
| **degraded** | Daemon serving, but ≥ 1 enabled target is failing collection (transient). Examples: temporary connection refused, query timeout, single-collector error. Cleared automatically on next successful poll. | Inspect `/status`; check target reachability. |
| **misconfigured** | Startup-time configuration validation failed. Examples: weak API token in `env=prod`, unreachable identity directory, invalid retention class. Daemon exits at startup; no API. | Fix the closed `reason_code` (see § Failure categories) and restart. Pre-flight via `arqctl doctor` before re-deploy. |
| **blocked** | Daemon serving, but no collection happening because a hard precondition isn't met. Examples: `pg_monitor` role missing → role_safe check failed; circuit-breaker open for every target. | Resolve the precondition; daemon detects and resumes. |

The state is exposed two ways:

- **`GET /health`** — minimal binary (`200 OK` for healthy /
  degraded, `503` for misconfigured / blocked). Suitable for
  Kubernetes `livenessProbe`. Carries no diagnostic content.
- **`GET /status`** (auth-required) — closed-payload JSON with
  the current state, per-target collection status, last error
  per target, circuit-breaker state, and freshness counters.
  Never exposes credential source paths beyond their type
  (`file` / `env` / `pgpass` / `none`).

## Failure categories (closed enum)

Every collection or startup failure resolves to ONE of these
closed `reason_code` values. The taxonomy is the **v1.0 contract**
— operator-facing tooling (`/status`, `arqctl doctor --json`,
metric labels, support-bundle templates) aligns to this list.
The Go-side `internal/metrics` and `internal/api` packages emit
the codes as their failure dimension; future runtime additions
that don't fit MUST extend this list via a deliberate spec
amendment rather than introducing a new ad-hoc string.

| Code | When it fires | What it means |
|---|---|---|
| `target_unreachable` | TCP-level connection refused / timeout. | Network, firewall, or target service down. |
| `target_tls_invalid` | Cert verification failed (`verify-full` / `verify-ca`). | Server certificate not trusted by configured root. |
| `auth_failed` | Postgres rejected credentials. | Wrong password, role doesn't exist, or `pg_hba.conf` rejects. |
| `role_insufficient` | Postgres accepted credentials but lacked permission for required catalogs (`pg_stat_*`). | Missing `pg_monitor` (or equivalent) grant. |
| `collector_pg_version_unsupported` | Catalog probe found PG major outside `SupportedMajors`. | PG 12/13 or experimental PG 19+ without the experimental opt-in. |
| `collector_extension_missing` | Required extension (e.g. `pg_stat_statements` for the workload collectors) is not installed. | Operator decision: install the extension or accept reduced coverage. |
| `collector_query_timeout` | Collector query exceeded `query_timeout`. | Target under load; consider raising the timeout for the affected target. |
| `collector_circuit_open` | Per-target circuit-breaker opened after `signals.circuit.fail_threshold` consecutive failures. | Cooldown active for `signals.circuit.open_cooldown`; daemon retries automatically. |
| `storage_write_failed` | SQLite write failed. Disk full, permission, FS unmounted. | Restore writeability; daemon resumes. |
| `storage_busy` | SQLite `SQLITE_BUSY` after the configured retry budget. | Concurrent reader holding a write lock too long; usually transient. |
| `config_invalid` | `ValidateStrict` returned a hard error at startup. | Daemon refuses to start; closed reason names the offending field. |
| `unknown` | Anything not above. | File a bug — the closed taxonomy is supposed to cover every observed failure. |

The `unknown` bucket is a deliberate honesty signal: if it fires,
the taxonomy is incomplete and the operator should report the
output of `arqctl doctor --json` + the daemon log line carrying
the bucket.

## Prometheus metrics

When `signals.metrics_enabled: true`, the daemon publishes the
metric set documented in
[`metrics-consumer-guide.md`](../metrics-consumer-guide.md).
Highlights for triage:

| Metric | What it tells you |
|---|---|
| `arq_signals_collection_total{target,collector,outcome}` | Per-target / per-collector counter. `outcome` is the closed enum above. |
| `arq_signals_collection_duration_seconds` | Histogram. Long-tail latency signals query_timeout pressure. |
| `arq_signals_circuit_state{target,state}` | Gauge with the closed enum (`closed` / `open` / `half_open`). |
| `arq_signals_last_snapshot_age_seconds{target}` | Gauge. Compare against `poll_interval`; consistently > 1.5 × signals a problem. |

INV-SIGNALS-07 holds: **no labels carry credential bytes,
hostnames-with-passwords, or workload data**. Labels are
operator-declared identifiers only (target names, collector ids,
the closed `outcome` enum).

## Operator preflight

Before deploying a new config:

```sh
arqctl doctor --config config.yaml
```

Runs the closed read-only checks:

- `config_valid` (passes YAML through `ValidateStrict`)
- `target_reachable` (TCP-level)
- `role_safe` (refuses superuser by default;
  `ARQ_SIGNALS_ALLOW_UNSAFE_ROLE=1` for evaluation)
- `collector_prerequisites` (extension availability;
  high-sensitivity-collector gating)
- `snapshot_freshness` (existing daemon, if any)
- `store_writable`

`--json` for automation. Exit code is non-zero when any check
fails.

## Support bundle

When Elevarq support asks for evidence, the goal is **bounded
data, redaction-by-default**, not "send me everything in /var/log".

The recommended package:

| Artefact | What it shows | Redaction guarantees |
|---|---|---|
| `arqctl doctor --json` output | Current target reachability + role + collector prerequisites + storage state. Last failure reason per target. | Closed JSON shape; never includes passwords / pgpass content. |
| `/status` JSON snapshot | Daemon-side view of all targets. Last error per target. Circuit-breaker state. Last snapshot age. | Same — closed shape; credential source types only. |
| Daemon stderr / journald excerpt for the troubled window | The slog lines around the issue. | The daemon's logger is set up to never emit secrets at any level; the API-token fingerprint is the only token-derived value that appears (12 chars of SHA-256). |
| `arqctl export` snapshot for ONE failing target | The collected catalog rows for the period of interest. | Sensitivity-profile redaction (see [`specifications/sensitivity-profiles.md`](../../specifications/sensitivity-profiles.md)) applies. PII MUST NOT appear in collected rows; producer-side discipline is the load-bearing rule. |

**Forbidden**:

- Raw daemon logs without timestamp bounding — they often span
  multiple targets and surface concurrent activity the issue at
  hand is irrelevant to.
- Files under `/var/lib/arq` (the identity directory) —
  `install_secret` is private to the process. Send the
  fingerprint from the daemon's startup log instead.
- Copies of `config.yaml` carrying inline credentials — redact
  the credential source values to their kind (`PG_PASSWORD`,
  `/run/secrets/pg.password`, etc.) before sending. The daemon
  itself never emits the inline contents.

## Test surface

The behavior documented above is pinned by:

| Control | Test file |
|---|---|
| `/health` returns binary state, never includes diagnostics | `internal/api/health_test.go` (existing) |
| `/status` payload never carries credential source values | `tests/signals_target_identity_test.go` |
| Closed failure-category enum constants exist + each value's wire string is the documented one | `tests/signals_failure_taxonomy_test.go` (new — see this PR) |
| `arqctl doctor` reports closed check ids + closed reasons | `internal/doctor/doctor_test.go` |
| Sensitivity profile redaction holds on snapshot export | `tests/signals_sensitivity_profiles_test.go` |

## Threat model

In scope:

- Operator misconfiguration → `misconfigured` state with closed
  reason at startup; `arqctl doctor` runnable to repair.
- Transient target failure → `degraded` with closed
  `reason_code`; circuit-breaker cuts noise once threshold hit.
- Support evidence leakage → bounded surface + sensitivity-
  profile redaction.

Out of scope (separate controls):

- Alerting orchestration (Prometheus / Grafana / PagerDuty) —
  consumer's responsibility; metric set documented separately.
- Long-term log retention — operator's logging stack handles it.
- Network-level monitoring (TLS handshake metrics) — not
  exposed by Signals; consumer's NetworkPolicy / service mesh.
