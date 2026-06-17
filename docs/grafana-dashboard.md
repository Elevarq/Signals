# Grafana dashboard for Elevarq Signal operational health

This document accompanies the local-only example stack at
`examples/observability/`. The stack runs Elevarq Signal, a PostgreSQL
target, Prometheus, and Grafana side-by-side and pre-loads a
dashboard that surfaces Elevarq Signal's `/metrics` data.

## What this dashboard is — and isn't

This dashboard monitors **the Elevarq Signal daemon's operational
health**. It tells you whether collection cycles are running, how
long they take, whether the SQLite store is healthy, and whether
exports are succeeding.

**It does not monitor PostgreSQL itself.** The collected PostgreSQL
diagnostic data lives in Elevarq Signal's local SQLite store and the
exported snapshot ZIP — not in Prometheus. Elevarq Signal deliberately
does not push collected metrics into Prometheus (see
[`docs/prometheus.md`](./prometheus.md) for the scope statement and
the list of things `/metrics` does not emit).

If you want PostgreSQL-side metrics in Grafana, run a separate
exporter such as `postgres_exporter`. Elevarq Signal and that exporter
have different jobs.

## Scope

- **Local / development convenience only.** The example stack uses
  hard-coded credentials (`dev-local-only-replace-in-prod-32chars`, `admin/admin`), exposes
  Grafana on `0.0.0.0:3000`, and runs Prometheus without TLS or
  retention tuning. Do not adapt this verbatim for production.
- **No code changes to Elevarq Signal** are required to use this stack —
  it consumes the existing `/metrics` endpoint described in
  [`docs/prometheus.md`](./prometheus.md).
- **No production claims.** This is a starting point you can iterate
  on; it is not a reference deployment.

## Local test instructions

Prerequisites: Docker (with the Compose plugin) and a free TCP port
range for `8081`, `9090`, and `3000`.

```bash
# From the repo root.
docker compose -f examples/observability/docker-compose.yml up --build
```

The first run builds the Elevarq Signal image and pulls Prometheus +
Grafana + PostgreSQL. Once everything is up:

| Service       | URL                       | Credentials               |
|---------------|---------------------------|---------------------------|
| Elevarq Signal    | http://localhost:8081     | bearer `dev-local-only-replace-in-prod-32chars`        |
| Prometheus    | http://localhost:9090     | none (local, dev-only)    |
| Grafana       | http://localhost:3000     | `admin` / `admin`         |

Trigger a collection so the metrics start moving:

```bash
curl -s -X POST http://localhost:8081/collect/now \
  -H "Authorization: Bearer dev-local-only-replace-in-prod-32chars"
```

Useful sanity checks:

- Prometheus targets:  http://localhost:9090/targets — the
  `arq-signals` job should be `UP`.
- Raw metrics:         `curl -s -H "Authorization: Bearer dev-local-only-replace-in-prod-32chars" \
                        http://localhost:8081/metrics | head -30`
- Dashboard URL:       http://localhost:3000/dashboards →
  *Elevarq Signal / Elevarq Signal Operational Health*

Tear down (volumes preserved):

```bash
docker compose -f examples/observability/docker-compose.yml down
```

Tear down and remove all data:

```bash
docker compose -f examples/observability/docker-compose.yml down -v
```

## Panels at a glance

The dashboard has eight functional panels plus one explanatory
text panel. Every query maps to a metric defined in
[`docs/prometheus.md`](./prometheus.md).

| Panel | Metric(s) | What it answers |
|---|---|---|
| **High-sensitivity collectors enabled (R075)** | `arq_signal_high_sensitivity_collectors_enabled` | Effective state of the daemon-wide gate. Default `1` (collect-everything default, R075 revised 2026-05); `0` means the operator opted out (redact-path collectors null their `SensitiveColumns`; skip-path collectors are dropped). |
| **Time since last successful collection (per target)** | `time() - arq_signal_last_successful_collection_timestamp` | Are any targets going stale? |
| **Collection cycles by status (rate / 5m)** | `arq_signal_collection_cycles_total{status}` | How are per-target cycles trending — success / partial / failed? |
| **Collection duration (p50 / p95 / p99)** | `arq_signal_collection_duration_seconds_bucket` | Cycle latency profile per target. |
| **Collector outcomes by status (rate / 5m)** | `arq_signal_collectors_{succeeded,failed,skipped}_total` | Aggregate per-cycle collector counts. |
| **Collector failed/skipped by reason (rate / 5m)** | `arq_signal_collectors_failed_total{reason}` + `arq_signal_collectors_skipped_total{reason}` | Why are collectors failing or being skipped (`permission_denied`, `timeout`, `config_disabled`, `version_unsupported`, `extension_missing`, …)? |
| **Export requests by status (rate / 5m)** | `arq_signal_export_requests_total{status}` | Is `/export` healthy? |
| **Export failures by error_category (rate / 5m)** | `arq_signal_export_failures_total{error_category}` | Why is `/export` failing? |
| **SQLite persistence failures (R077, last 24h)** | `arq_signal_sqlite_persistence_failures_total` | Has the atomic-cycle commit ever rolled back recently? |
| **Collection failures by reason (last 1h)** | `arq_signal_collection_failures_total{reason}` | Bounded reason breakdown for hard target failures. |

### Note on `collect_now_dropped`

`POST /collect/now` requests that get queued but never run (channel
full at the API handler, or cycle overlap detected in the collector
goroutine) are emitted as the audit event
`audit_event=collect_now_dropped` with `reason_category=previous_request_pending`
or `cycle_overlap_skipped`. **There is no Prometheus counter for
this signal in the current Elevarq Signal build.**

The dashboard surfaces this gap as a Markdown panel rather than
inventing a panel that would always read zero. Operators who need a
metric for drop rate can:

- grep the daemon log for `audit_event=collect_now_dropped` and feed
  the count into a log-derived metric (e.g. via Loki + recording
  rule, or `mtail`); or
- file a request to add a counter such as
  `arq_signal_collect_now_dropped_total{reason_category}` to the
  Elevarq Signal metrics package — the audit-event values map cleanly
  onto a bounded label set.

## Updating the dashboard

Grafana provisioning is read-only for this dashboard via the
`provisioning/dashboards/arq-signals.yml` file. Edits made in the UI
persist for the session but are reset on next provisioning sync.

To make a change durable, edit
`examples/observability/grafana/dashboards/arq-signal-operational-health.json`
and re-run `docker compose up`. (Grafana's provisioner re-reads the
file every 30 seconds, so a restart is usually not required.)

## Bearer token in the scrape config

Prometheus authenticates to Elevarq Signal using the standard
`authorization` block with `credentials_file:` pointing at
`/etc/prometheus/secrets/arq-signals-token`. The token file is
mounted from `prometheus/arq-signals-token` and is `dev-local-only-replace-in-prod-32chars` in
this example.

For real deployments, see the production guidance in
[`docs/prometheus.md`](./prometheus.md):

- Mount the token file at mode `0600`, owned by the Prometheus user.
- Bind the Elevarq Signal listener to an internal network — never to a
  public address.
- Rotate the token via the file source so neither side has to be
  restarted (Elevarq Signal re-reads the API token on every request when
  configured via `ARQ_SIGNALS_API_TOKEN_FILE`).
