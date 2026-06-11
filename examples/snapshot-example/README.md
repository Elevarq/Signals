This directory shows the structure of an Arq Signals export ZIP. In
practice, exports are delivered as ZIP archives by `arqctl export` and
the daemon's `GET /export` endpoint; this expanded view is for
reference. The values reflect the v0.10.0-beta.5 export shape.

## Files

| File | Description |
|---|---|
| `metadata.json` | Schema version, instance identity, build identity, scope markers (`snapshot_count`, `ingest_mode`, `run_scope`), effective high-sensitivity state, and (for single-target exports) `target_name` + `target_identity`. |
| `collector_status.json` | Per-collector execution outcome for the scoped runs — `status`, `reason`, `row_count`, `duration_ms`, `collected_at`, and the R107 freshness fields (`cadence`, `freshness`). Always present in every export (INV-SIGNALS-11). |
| `snapshots.ndjson` | One line per snapshot row in scope. Carries `target_identity` per row in multi-target exports (R094); single-target exports also embed it at the top level of `metadata.json`. |
| `query_catalog.json` | Registry of all collectors that have ever produced a run in the local store. |
| `query_runs.ndjson` | One line per `query_run` in scope: timing, row counts, errors, and the explicit `status` / `reason` fields (R072). |
| `query_results.ndjson` | One line per successful run with its payload. Skipped and failed runs legitimately have no payload row (their `status` + `reason` in `query_runs.ndjson` explains the absence). |

## Schema versions

- `metadata.schema_version` identifies the snapshot wire format
  (`arq-snapshot.v1`).
- `metadata.collector_status_schema_version` identifies the
  `collector_status.json` shape (`"1"`).

Consumers should check both before parsing.

## Scope markers (R086)

- `snapshot_count` — number of distinct `snapshots` rows packaged.
- `ingest_mode` — `"analyze"` (operator-driven export) or
  `"history_only"` (R087 backlog replay).
- `run_scope` — `"latest-per-collector"` (R084 default; runs may
  carry differing `collected_at` values) or `"snapshot"` (selector
  scopes: `--all`, `--snapshot-id`, `--since/--until`). A consumer
  that does not recognise the value, or finds it absent, MUST treat
  the export as `"snapshot"`.

## Sensitivity (R075 revised)

`metadata.high_sensitivity_collectors_enabled` reflects the effective
state. When `true` (default), high-sensitivity collectors run and
their full output is persisted. When `false`, the opt-out behaves per
collector:

- **Redact-path** collectors (live `pg_stat_activity` query text) still
  run; their declared sensitive columns appear as `null` in
  `query_results.ndjson`.
- **Skip-path** collectors (DDL definitions, sampled-value stats,
  RLS policies, rewrite rules) are dropped and appear in
  `collector_status.json` with `status=skipped, reason=config_disabled`.

See the inspection guide at
[`../snapshot-inspection/`](../snapshot-inspection/) for end-to-end
examples of reading these files.
