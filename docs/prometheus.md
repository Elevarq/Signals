# Prometheus metrics endpoint

Arq Signals can optionally expose a Prometheus-compatible `/metrics`
endpoint that publishes **operational health metrics about the
daemon itself**. This document describes what the endpoint emits,
what it deliberately does not emit, and the recommended way to
scrape it safely.

## Scope: daemon health only

The `/metrics` endpoint is for monitoring the **Arq Signals process**:
collection cycle outcomes, export request outcomes, sqlite
persistence health, the state of the high-sensitivity gate, and
durations. It does **not** publish any of the following:

- collected PostgreSQL data
- SQL text or query identifiers
- query result rows or payloads
- view, materialized view, trigger, or function definitions
- database names, hostnames, usernames, file paths
- raw error message bodies

Label cardinality is bounded by design (R079). The label values that
appear on counters and histograms are:

- `target` — operator-configured target names (a small fixed set per
  deployment).
- `status` — fixed enum: `success`, `partial`, `failed`.
- `reason` — fixed enum drawn from `collector_status.json`:
  `permission_denied`, `timeout`, `execution_error`,
  `config_disabled`, `version_unsupported`, `extension_missing`.
- `error_category` — fixed enum: `invalid_target_id`, `builder_error`,
  `write_error`.

Arbitrary user-supplied or per-row values never reach the metric
output.

## Configuration

Off by default. Enable in `signals.yaml`:

```yaml
signals:
  metrics_enabled: true
  metrics_path: /metrics    # default; can be overridden
```

Or via environment variables:

```
ARQ_SIGNALS_METRICS_ENABLED=true
ARQ_SIGNALS_METRICS_PATH=/metrics
```

`metrics_path` must start with `/` and may not be `/health`,
`/status`, `/collect/now`, or `/export`. Startup aborts on a
mis-configured path so the misconfiguration is visible immediately
rather than silently shadowing an existing API endpoint.

## Authentication

The `/metrics` endpoint is mounted on the same listener as the rest
of the API and inherits the existing bearer-token auth (R011). This
keeps the auth surface uniform — operators do not need to manage a
separate exception for Prometheus.

For Prometheus to scrape an authenticated endpoint, use the standard
`bearer_token_file` directive in your scrape config (see below).

If you prefer unauthenticated scraping, bind the API listener to
loopback or to an internal-only network and rely on network-level
controls. Arq Signals does not relax auth on a per-path basis.

## Scrape configuration

Recommended `prometheus.yml` job for an authenticated endpoint:

```yaml
scrape_configs:
  - job_name: arq-signals
    metrics_path: /metrics
    scheme: http             # use https if the listener is fronted by TLS
    bearer_token_file: /etc/prometheus/secrets/arq-signals-token
    static_configs:
      - targets:
          - arq-signals.internal:8081
        labels:
          deployment: prod
```

For loopback / local-only scraping (no auth):

```yaml
scrape_configs:
  - job_name: arq-signals
    metrics_path: /metrics
    static_configs:
      - targets: ['127.0.0.1:8081']
```

Adjust `scrape_interval` to match `signals.poll_interval`. Scraping
faster than the collection cadence does not produce more data — it
just samples the same counter values more often.

## Security guidance

- **Do not expose the `/metrics` endpoint on a public network** even
  with auth enabled. Bind to loopback or an internal management
  network whenever possible.
- The bearer token used to scrape `/metrics` is the same token that
  protects `/status`, `/collect/now`, and `/export`. Treat it
  accordingly: provision via `bearer_token_file` (a file mode 0600,
  not an env var that may surface in process listings).
- `/metrics` itself never returns sensitive content. The auth
  requirement is mainly to keep counters from public probes; if an
  unauthenticated counter does leak, the exposure is bounded by the
  metric set listed below.

## Metric reference

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `arq_signal_collection_cycles_total` | counter | `target`, `status` | Per-target collection cycles, labelled `success` / `partial` / `failed`. |
| `arq_signal_collection_failures_total` | counter | `target`, `reason` | Per-target hard failures (`connect_error`, `safety_check`, `persistence`, `internal`). |
| `arq_signal_collection_duration_seconds` | histogram | `target`, `status` | Wall-clock duration of each cycle. |
| `arq_signal_collectors_succeeded_total` | counter | `target` | Sum of per-cycle successful collector counts. |
| `arq_signal_collectors_failed_total` | counter | `target`, `reason` | Sum of per-cycle failed collector counts. |
| `arq_signal_collectors_skipped_total` | counter | `target`, `reason` | Sum of per-cycle skipped collector counts. |
| `arq_signal_export_requests_total` | counter | `status` | All export requests, labelled `success` / `failed`. |
| `arq_signal_export_failures_total` | counter | `error_category` | Failed exports keyed by category (matches audit log). |
| `arq_signal_export_duration_seconds` | histogram | `status` | Wall-clock duration of each export. |
| `arq_signal_sqlite_persistence_failures_total` | counter | (none) | `InsertCollectionAtomic` rollbacks (R077). |
| `arq_signal_last_successful_collection_timestamp` | gauge | `target` | Unix seconds of the most recent successful collection per target. |
| `arq_signal_high_sensitivity_collectors_enabled` | gauge | (none) | `1` if the R075 gate is open, `0` otherwise. |

## Suggested alerts

Starting points for an Arq Signals SLO/health alert pack:

- **No successful collection in N intervals** —
  `time() - arq_signal_last_successful_collection_timestamp > 3 * <poll_interval_seconds>`
- **Persistent collection failure** —
  `increase(arq_signal_collection_cycles_total{status="failed"}[1h]) > 5`
- **SQLite persistence health regression** —
  `increase(arq_signal_sqlite_persistence_failures_total[1h]) > 0`
- **Export error rate** —
  `rate(arq_signal_export_failures_total[10m]) > 0`

Tune thresholds to your collection cadence and acceptable signal
delay.
