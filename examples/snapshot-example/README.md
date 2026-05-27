This directory shows the structure of an Arq Signals snapshot. In practice, snapshots are delivered as ZIP archives. This expanded view is for reference.

## Files

| File | Description |
|---|---|
| `metadata.json` | Collector version, schema version, and timestamp |
| `query_catalog.json` | Registry of all queries that were executed |
| `query_runs.ndjson` | One line per query execution with timing and row counts |
| `query_results.ndjson` | One line per query execution with the actual result payload |

## Schema version

The `schema_version` field in `metadata.json` identifies the snapshot format. The current version is `arq-snapshot.v1`. Consumers should check this field before parsing.
