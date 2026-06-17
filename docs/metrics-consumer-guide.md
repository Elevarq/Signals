# Elevarq Signals — Metrics Consumer Guide

Operational metrics exposed under `signals.metrics_path` (default
`/metrics`) when `signals.metrics_enabled: true`. This guide is the
operator-facing reference for what's published, what labels mean, and
what to alert on.

Spec: ARQ-SIGNALS-R079 (operational metrics endpoint).

## Scrape configuration

The endpoint inherits the daemon's bearer-auth and binds to the
configured `api.listen_addr`. Prometheus scrape config:

```yaml
scrape_configs:
  - job_name: signals
    metrics_path: /metrics
    scheme: http        # or https when the daemon terminates TLS
    static_configs:
      - targets: ['signals.host:8081']
    authorization:
      type: Bearer
      credentials_file: /etc/prometheus/signals.token
```

Operators that scrape from inside the same host typically rely on
network-level controls (loopback binding) rather than the bearer
token. Either is acceptable; INV-SIGNALS-07 (no secrets in metric
labels) holds regardless.

## Metric inventory

Every metric is published from a dedicated registry — none of the
default Go runtime / process metrics are exposed. Names follow
`signals_<concern>_<unit>`.

### Collection cycle

| Metric | Type | Labels | Notes |
|--------|------|--------|-------|
| `signals_collection_cycles_total` | Counter | `target`, `status` | One increment per completed `collectTarget` call. `status` ∈ `{success, partial, failed}`. |
| `signals_collection_failures_total` | Counter | `target`, `reason` | Hard cycle failures by category. `reason` ∈ `{connect_error, version_unsupported, timeout_setup, safety_check, persistence, internal}` (see `metrics.CollectionFailureReasons`). |
| `signals_collection_duration_seconds` | Histogram | `target`, `status` | Per-cycle wall-clock duration. Buckets are exponential from 50ms. |

### Per-collector outcomes

| Metric | Type | Labels | Notes |
|--------|------|--------|-------|
| `signals_collectors_succeeded_total` | Counter | `target` | Sum of per-cycle successful-collector counts. |
| `signals_collectors_failed_total` | Counter | `target`, `reason` | Failed collectors classified by reason (`execution_error`, `timeout`, `permission_denied`, `object_missing`, `savepoint_rollback`). |
| `signals_collectors_skipped_total` | Counter | `target`, `reason` | Skipped collectors by reason (`version_unsupported`, `extension_missing`, `config_disabled`). |
| `signals_eligible_collectors` | Gauge | `target` | **R079 / #79**: number of collectors that would run for this target after every gate is applied (version, extension, sensitivity, profile). Updated at the top of every cycle. |

### Snapshots

| Metric | Type | Labels | Notes |
|--------|------|--------|-------|
| `signals_last_successful_collection_timestamp` | Gauge | `target` | Unix-seconds of the most recent completed cycle. Use `time() - <metric>` for staleness. |

### Export

| Metric | Type | Labels | Notes |
|--------|------|--------|-------|
| `signals_export_requests_total` | Counter | `status` | One increment per `/export` request. `status` ∈ `{ok, failed}`. |
| `signals_export_failures_total` | Counter | `error_category` | Failures by category (`invalid_time_format`, `invalid_target_id`, `invalid_time_range`, `snapshot_not_found`, `conflicting_selectors`, `internal`). |
| `signals_export_duration_seconds` | Histogram | `status` | Per-export wall-clock duration. |

### Daemon state

| Metric | Type | Labels | Notes |
|--------|------|--------|-------|
| `signals_sqlite_persistence_failures_total` | Counter | — | `InsertCollectionAtomic` rollbacks (R077). |
| `signals_high_sensitivity_collectors_enabled` | Gauge | — | 1 if the daemon-wide R075 flag is on, 0 otherwise. |
| `signals_circuit_state` | Gauge | `target`, `state` | **R097**: per-target circuit state. One row per (target, state); the active row has value 1. `state` ∈ `{closed, open, paused}`. |

## Label cardinality

| Label | Source | Cardinality bound |
|-------|--------|-------------------|
| `target` | `signals.targets[].name` from config | Number of configured targets. Operator-bounded. |
| `status` | Closed daemon enum | ≤ 3 values per metric. |
| `reason` | Closed daemon enum | ≤ 5 values per metric. |
| `state` | Circuit-breaker enum (R097) | Exactly 3 values. |
| `error_category` | Closed export error enum | ≤ 6 values. |

**INV-SIGNALS-07** is enforced at every label-write site: no SQL
text, query payload, database name, hostname, username, secret, or
customer data appears in any metric label. Verified by inspection of
every `WithLabelValues` call in `internal/metrics/metrics.go`.

## Recommended alerting rules

The following are starting points — operators tune thresholds based
on their poll interval and fleet shape.

### Coverage / health

```promql
# Coverage drift: a target lost more than 10% of its collectors in
# the last 24h. Catches extension uninstalls, version downgrades,
# accidental profile changes.
(
  signals_eligible_collectors
  / signals_eligible_collectors offset 24h
) < 0.9
```

```promql
# Stale data: no completed cycle in 2x poll_interval (60s here).
time() - signals_last_successful_collection_timestamp > 120

# No metric at all = target never collected since daemon start.
absent(signals_last_successful_collection_timestamp{target="prod-db"})
```

### Cycle health

```promql
# Failure ratio over the last 15m.
sum(rate(signals_collection_cycles_total{status="failed"}[15m])) by (target)
  / sum(rate(signals_collection_cycles_total[15m])) by (target)
  > 0.2

# Latency outlier.
histogram_quantile(0.95,
  rate(signals_collection_duration_seconds_bucket[15m])
) > 30
```

### Operator-controlled state

```promql
# Any target in non-closed circuit state.
sum by (target, state) (signals_circuit_state{state!="closed"} == 1)

# Paused for more than an hour — operator may have forgotten.
signals_circuit_state{state="paused"} == 1
  unless (signals_circuit_state{state="paused"} offset 1h == 1)
```

### Export

```promql
# Export error spike.
sum(rate(signals_export_failures_total[5m])) by (error_category) > 0
```

### Persistence

```promql
# SQLite write failure — should never be > 0.
sum(rate(signals_sqlite_persistence_failures_total[15m])) > 0
```

## What's deliberately NOT exposed

- **SQL text or query payload**. Collector output never enters
  metrics. Operators that need that level of detail use the
  collected NDJSON.
- **Hostnames, dbnames, usernames**. Targets are referenced by their
  operator-assigned `name` only.
- **Credentials / secret references**. Per INV-SIGNALS-07.
- **Per-collector duration histograms**. Cardinality risk is high
  (≈60 collectors × N targets × bucket count). Per-collector
  outcomes are tracked as counters; durations are aggregated at the
  cycle level. If a future workload demands per-collector latency
  tracking, file a follow-up.

## Workbench integration

The Workbench Signals page (Arq-Workbench #53) consumes Signals
state via the Analyzer-mediated import contract, not directly off
`/metrics`. This guide is for **operator** consumption — Prometheus
scrapes, on-call alerting, capacity planning. The analyzer-side
ingest reads `collector_status.json` and `snapshots.ndjson` from
exported ZIPs, not the metrics endpoint.

## Changelog

- 2026-05-12 (#79 review): added `signals_eligible_collectors`
  gauge. Audited all label sites; confirmed bounded cardinality
  and no INV-SIGNALS-07 violations.
