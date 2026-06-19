# TimescaleDB demo fixture

A deterministic TimescaleDB sample workload that produces non-trivial
output from **every** `timescaledb_*_v1` collector (R114). Built for
Tiger Data demo work and as a reusable development fixture.
Issue: [#76](https://github.com/Elevarq/Arq-Signals/issues/76).

Collector contract: `specifications/collectors/timescaledb_family_v1.md`.
Analyzer rule mapping: `docs/timescaledb-analyzer-roadmap.md`
(TS-R001..TS-R009, [Elevarq/Arq#1204](https://github.com/Elevarq/Arq/issues/1204)).

## Run it

```bash
docker compose -f examples/timescaledb-demo/docker-compose.yml up -d --build

# Wait for the first collection cycle (or trigger one):
curl -X POST http://localhost:8082/collect/now \
  -H "Authorization: Bearer dev-local-only-replace-in-prod-32chars"

# Export the snapshot:
curl -o tsdemo-snapshot.zip http://localhost:8082/export \
  -H "Authorization: Bearer dev-local-only-replace-in-prod-32chars"
```

The TimescaleDB instance itself is on `localhost:5433`
(db `tsdemo`). An optional PostgreSQL 18 variant (database only) is
behind the `pg18` profile on `localhost:5435`:

```bash
docker compose -f examples/timescaledb-demo/docker-compose.yml --profile pg18 up -d
```

Everything is anchored to `date_trunc('day', now())` at seed time, so
chunk counts and the compressed/uncompressed split are stable for any
run on a given day. The deliberately failing job means
`total_failures` grows over time — that is the fixture working, not a
bug.

## Role variants

| Role | Password | Behavior |
|---|---|---|
| `signals` | `monitor_pass` | Least-privilege (LOGIN + `pg_monitor`). Full hypertable/chunk/compression/jobs surface; **zero rows** in `timescaledb_job_errors_v1` — the documented partial-by-design state (`docs/postgres-role.md` § TimescaleDB targets). |
| `signals_monitor_owner` | `monitor_owner_pass` | Same + membership in `tsdemo_owner` (the demo database-owner role) → sees the failing job's rows in `job_errors`. |

The compose file wires `signals` to `signals`. To demo the
owner variant, point a second target (or `psql`) at the same database
as `signals_monitor_owner` and compare `timescaledb_job_errors_v1`.

## What the fixture produces, collector by collector

| Fixture element | Collector(s) | Demo-worthy output | Future Analyzer rule |
|---|---|---|---|
| TimescaleDB 2.27.2, Community edition, default telemetry | `timescaledb_extension_v1` | `extversion`, `license=timescale`, capability flags (all probes true on 2.27) | edition gating for TS-R003/TS-R004 |
| `metrics` hypertable (timestamptz + 4-partition space dimension) | `timescaledb_hypertables_v1`, `timescaledb_dimensions_v1` | 2 dimensions incl. a `Space` row; `compression_enabled=true`; `primary_dimension` (2.20+ column via dynamic capture) | TS-R002 chunk interval risk |
| 46 daily `metrics` chunks + 10 integer-dimension `events` chunks | `timescaledb_chunks_v1`, `timescaledb_chunk_summary_v1` | mixed `is_compressed`; `range_start/range_end` AND `range_*_integer` populated; summary counts vs capped detail | TS-R001 excessive chunk count |
| `events` hypertable (bigint dimension) | same | integer-dimension coverage — exercises the newest-created-first cap ordering | TS-R001/TS-R002 |
| Compression: `segmentby device_id, orderby time DESC`; chunks > 14 days compressed; policy at 14 days | `timescaledb_compression_settings_v1`, `timescaledb_compression_stats_v1` | settings row; ~32/46 compressed; non-NULL before/after bytes (real ratio) | TS-R003 opportunity, TS-R004 backlog |
| `metrics_daily` continuous aggregate, hourly refresh policy, refreshed once at seed | `timescaledb_continuous_aggregates_v1` | cagg row with materialization hypertable; `view_definition` (redacted when the R075 opt-out is active) | TS-R005 refresh lag |
| Retention policy (`drop_after => 365 days`) | `timescaledb_jobs_v1` (`proc_name='policy_retention'`, `config`) | policy job with schedule + config JSONB | TS-R006 retention ineffective |
| All policy + system jobs | `timescaledb_jobs_v1`, `timescaledb_job_stats_v1` | 6+ jobs (telemetry, log retention, compression, retention, cagg refresh, demo job) with run counters | TS-R007 job failures |
| `demo_failing_job` (raises every minute, no retries) | `timescaledb_job_stats_v1`, `timescaledb_job_errors_v1` | `total_failures` > 0 visible to ANY role; per-failure rows only for `signals_monitor_owner` | TS-R007 |
| `legacy_measurements` (plain, append-only, time-keyed, 52k rows, time-leading btree) | core collectors (`pg_columns_v1`, `pg_indexes_v1`, `largest_relations_v1`, `pg_stat_user_tables_v1`) | the "should be a hypertable" exhibit | TS-R008 hypertable candidate |
| time-leading index + segmentby settings + size split | `timescaledb_hypertable_sizes_v1` + core index collectors | approximate table/index/toast/total bytes per hypertable | TS-R009 index recommendation |

## Resetting

The seed runs only on first initialization. To reset:

```bash
docker compose -f examples/timescaledb-demo/docker-compose.yml down -v
docker compose -f examples/timescaledb-demo/docker-compose.yml up -d --build
```

## Caveats

- Demo/dev only: placeholder passwords and a dev API token. Never
  reuse any of it outside a local environment.
- The failing job writes one `job_errors` row per minute; the
  built-in "Job History Log Retention Policy" prunes monthly, so a
  long-lived demo instance accumulates at a bounded rate (and
  `timescaledb_job_errors_v1` is capped at 1000 newest rows anyway).
- On the Apache-2 (`-oss`) image this seed would fail at the first
  TSL feature (`timescaledb.compress`); edition demos should use the
  plain verification flow from the #75 test matrix instead.
