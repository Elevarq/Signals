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
├── metadata.json          # collector version, schema, unsafe mode
├── snapshots.ndjson       # legacy combined snapshot format
├── query_catalog.json     # registered collectors that were executed
├── query_runs.ndjson      # execution metadata (timing, row counts, errors)
└── query_results.ndjson   # the actual diagnostic data
```

## Inspect metadata

```bash
unzip -p snapshot.zip metadata.json | python3 -m json.tool
```

Example output (safe role):

```json
{
  "schema_version": "arq-snapshot.v1",
  "collector_version": "0.2.0",
  "collected_at": "2026-03-14T21:00:00Z",
  "instance_id": "a1b2c3d4...",
  "unsafe_mode": false
}
```

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

## Inspect query execution metadata

```bash
unzip -p snapshot.zip query_runs.ndjson | python3 -c "
import sys, json
for line in sys.stdin:
    r = json.loads(line)
    status = 'OK' if not r.get('error') else 'ERR'
    print(f\"{r['query_id']:40s} {r['duration_ms']:>5d}ms  {r['row_count']:>5d} rows  {status}\")
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
