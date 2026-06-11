# Snapshot Inspection Guide

This guide explains how to inspect an Arq Signals export snapshot.

## Export a snapshot

```bash
curl -o snapshot.zip http://localhost:8081/export \
  -H "Authorization: Bearer $ARQ_SIGNALS_API_TOKEN"
```

## What's inside

```bash
unzip -l snapshot.zip
```

```
snapshot.zip
├── metadata.json          # schema, identity, scope markers, sensitivity state
├── collector_status.json  # per-collector outcome + cadence/freshness (R107)
├── snapshots.ndjson       # snapshot rows in scope (with per-snapshot identity, R094)
├── query_catalog.json     # registered collectors known to the local store
├── query_runs.ndjson      # execution metadata (timing, row counts, status, reason)
└── query_results.ndjson   # one line per SUCCESSFUL run with its payload
```

## Inspect metadata

```bash
unzip -p snapshot.zip metadata.json | python3 -m json.tool
```

Example output (safe role, single-target export):

```json
{
  "schema_version": "arq-snapshot.v1",
  "collector_status_schema_version": "1",
  "instance_id": "a1b2c3d4...",
  "arq_signals_version": "0.10.0-beta.5",
  "collector_commit": "abcdef12",
  "generated_at": "2026-05-28T10:30:00Z",
  "collected_at": "2026-05-28T10:30:00Z",
  "unsafe_mode": false,
  "high_sensitivity_collectors_enabled": true,
  "snapshot_count": 2,
  "ingest_mode": "analyze",
  "run_scope": "latest-per-collector",
  "target_name": "my-database",
  "target_identity": {
    "host": "db.internal", "port": 5432,
    "dbname": "myapp", "username": "arq_signals"
  }
}
```

### Scope markers (R086)

- `snapshot_count` — number of distinct snapshots in the ZIP.
- `ingest_mode` — `"analyze"` (operator export) or `"history_only"`
  (R087 backlog replay).
- `run_scope` — `"latest-per-collector"` (R084 default; per-run
  `collected_at` may differ) or `"snapshot"` (selector scopes). When
  absent or unrecognised, treat as `"snapshot"`.

### Sensitivity state (R075 revised)

`high_sensitivity_collectors_enabled` reflects the effective gate.
When `false`, the opt-out behaves per collector:

- **Redact-path** (live `pg_stat_activity` collectors): collector
  still ran; declared sensitive columns are `null` in
  `query_results.ndjson`.
- **Skip-path** (DDL definitions, sampled-value stats, RLS policies,
  rewrite rules): collector dropped; appears in
  `collector_status.json` with `status=skipped, reason=config_disabled`.

### Understanding unsafe_mode

| Value | Meaning |
|-------|---------|
| `false` | Collected with a safe monitoring role (recommended) |
| `true` | Collected with unsafe role override enabled |

When `unsafe_mode` is `true`, an `unsafe_reasons` array lists the
specific role attributes that were bypassed:

```json
{
  "unsafe_mode": true,
  "unsafe_reasons": [
    "role \"postgres\" has superuser attribute (rolsuper=true)"
  ]
}
```

## Inspect query results

```bash
# Pretty-print the first line of query results
unzip -p snapshot.zip query_results.ndjson | head -1 | python3 -m json.tool
```

Each line is a JSON object containing:

```json
{
  "run_id": "01KKQ...",
  "payload": [
    {"name": "max_connections", "setting": "100", "unit": "", "source": "configuration file"},
    {"name": "shared_buffers", "setting": "16384", "unit": "8kB", "source": "configuration file"}
  ]
}
```

## Inspect collector status (R072 + R107)

`collector_status.json` is the per-collector outcome summary — it
exists in every export (INV-SIGNALS-11) and is the most direct way to
spot coverage gaps:

```bash
unzip -p snapshot.zip collector_status.json | python3 -c "
import sys, json
data = json.load(sys.stdin)
for c in data['collectors']:
    age = c.get('collected_at') or '-'
    fresh = c.get('freshness') or '-'
    cad = c.get('cadence') or '-'
    print(f\"{c['id']:40s} {c['status']:>9s}  cadence={cad:<4s}  fresh={fresh:<10s}  {age}\")
"
```

Statuses you can see:

| Status / Reason | Meaning |
|---|---|
| `success` | Ran cleanly, payload in `query_results.ndjson`. |
| `failed` / `permission_denied` etc. | Attempted but errored — see `error` in `query_runs.ndjson`. |
| `skipped` / `version_unsupported` | PG major too old. |
| `skipped` / `extension_missing` | Required extension not installed. |
| `skipped` / `config_disabled` | High-sensitivity opt-out, per-target profile exclude, or per-collector opt-in off. |
| `skipped` / `budget_exhausted` | Was due but the cycle's time budget elapsed before it ran (R108). |

Freshness (R107) only applies to the R084 default scope:

| `freshness` | Meaning |
|---|---|
| `fresh` | Latest run within 2× cadence — current. |
| `stale` | Latest run older than 2× cadence — at least one full cycle missed. |
| `never_run` | Eligible but no run in scope (target-scoped exports only). |
| `""` | Not applicable (skipped/gated entries; or selector-scope export). |

## Inspect query execution metadata

```bash
unzip -p snapshot.zip query_runs.ndjson | python3 -c "
import sys, json
for line in sys.stdin:
    r = json.loads(line)
    status = r.get('status') or ('failed' if r.get('error') else 'success')
    detail = r.get('reason') or r.get('error') or ''
    print(f\"{r['query_id']:40s} {r['duration_ms']:>5d}ms  {r['row_count']:>5d} rows  {status:>9s}  {detail}\")
"
```

## pg_stat_statements across PostgreSQL versions

The `pg_stat_statements_v1` collector uses `SELECT *`, which means
the column names in the payload may differ between PostgreSQL versions:

| Column | PG 14–16 | PG 17+ |
|--------|----------|--------|
| Block read time | `blk_read_time` | `shared_blk_read_time` |
| Block write time | `blk_write_time` | `shared_blk_write_time` |

New columns added in future PostgreSQL versions will appear
automatically. This is intentional — Arq Signals captures the actual
returned fields for version-sensitive views.

## Inspect the query catalog

```bash
unzip -p snapshot.zip query_catalog.json | python3 -m json.tool | head -20
```

This shows which collectors were registered and executed.

## Reference

A static example snapshot is available at
[`examples/snapshot-example/`](../snapshot-example/) for offline
reference without running the collector.
