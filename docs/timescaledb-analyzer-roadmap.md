# TimescaleDB Analyzer roadmap — collector output → future rules

Issue: [#73](https://github.com/Elevarq/Arq-Signals/issues/73)
(collectors; this repo) → follow-up Analyzer rule family TS-R001..TS-R009
(Elevarq Analyzer repo; no Analyzer rules ship from arq-signals).

This page maps each `timescaledb_*_v1` collector output to the
Analyzer rules it is designed to unblock, so the Analyzer slice can
be planned without re-deriving the evidence model. Per
INV-SIGNALS-01 the collectors emit passive evidence only — every
threshold, ratio, and verdict below is Analyzer-side work.

Collector contract: `specifications/collectors/timescaledb_family_v1.md`
(R114/R115). Design rationale: `docs/timescaledb-collectors-design.md`.

## Rule-by-rule evidence map

### TS-R001 — Excessive chunk count

- `timescaledb_chunk_summary_v1.chunk_count` per hypertable
  (authoritative even when the per-chunk rowset truncates).
- `timescaledb_dimensions_v1.time_interval` / `integer_interval`
  (interval vs observed count sanity).
- Context: `pg_settings_v1` (`max_locks_per_transaction`,
  `shared_buffers`) for the "why it hurts" narrative.

### TS-R002 — Chunk interval risk

- `timescaledb_dimensions_v1.time_interval` (configured interval).
- `timescaledb_chunk_summary_v1.oldest_range_start` /
  `newest_range_end` / `chunk_count` (observed data span ÷ count =
  effective interval; mismatch vs configured = drift).
- `timescaledb_chunks_v1.range_start`/`range_end` (per-chunk
  irregularity, newest 5000).
- `timescaledb_hypertable_sizes_v1.total_bytes` (interval vs
  size-per-chunk heuristics — the upstream "25% of memory per chunk"
  guidance).

### TS-R003 — Compression opportunity

- `timescaledb_hypertables_v1.compression_enabled = false` +
  `timescaledb_hypertable_sizes_v1.total_bytes` (large, uncompressed).
- `timescaledb_chunk_summary_v1.newest_range_end` vs now (cold data
  present).
- `timescaledb_extension_v1.license` — suppress the rule on
  `apache` builds (edition cannot compress).

### TS-R004 — Compression backlog

- `timescaledb_compression_stats_v1.total_chunks` vs
  `number_compressed_chunks` (gap = backlog candidate).
- `timescaledb_compression_settings_v1` (compression configured at
  all) + `timescaledb_jobs_v1` rows with
  `proc_name = 'policy_compression'` and their `config`
  (`compress_after`) — distinguishes "no policy" from "policy not
  keeping up".
- `timescaledb_job_stats_v1.last_run_status` / `total_failures` for
  the compression job (backlog caused by failing job → cross-link
  TS-R007).

### TS-R005 — Continuous aggregate refresh lag

- `timescaledb_continuous_aggregates_v1` (cagg inventory,
  `materialized_only`).
- `timescaledb_jobs_v1` rows with
  `proc_name = 'policy_refresh_continuous_aggregate'`:
  `schedule_interval`, `config` (`start_offset` / `end_offset`).
- `timescaledb_job_stats_v1.last_successful_finish` /
  `next_start` / `last_run_status` (observed refresh recency vs
  schedule).
- Known gap: watermark/invalidation depth is not collected (internal
  catalog surface — excluded by design § 3). Lag is inferred from
  job recency, not materialization watermark; revisit only if the
  rule's false-positive rate demands it.

### TS-R006 — Retention policy ineffective

- `timescaledb_jobs_v1` rows with `proc_name = 'policy_retention'`:
  `config` (`drop_after` / `drop_created_before`), `scheduled`,
  `schedule_interval`; absence of such a row on a growing hypertable
  is the "no retention" variant.
- `timescaledb_chunk_summary_v1.oldest_range_start` vs the policy's
  `drop_after` window (data older than the policy promises =
  ineffective).
- `timescaledb_job_stats_v1.total_failures` /
  `last_run_status` for the retention job.

### TS-R007 — Background job failures

- `timescaledb_job_stats_v1.total_failures`, `total_successes`,
  `last_run_status`, `last_successful_finish`, `job_status`
  (visible for ALL jobs regardless of collector privileges).
- `timescaledb_job_errors_v1` (`sqlerrcode`, `start_time`,
  redact-aware `err_message`) — enrichment only: rows are visible
  solely when the collector role has job-owner/db-owner membership
  (FC-TSDB-05). The rule MUST function on `job_stats` alone and
  treat `job_errors` as optional detail.
- `timescaledb_jobs_v1.max_retries` / `retry_period` (flapping vs
  hard-down classification).

### TS-R008 — Hypertable candidate

Evidence comes from existing core collectors, evaluated only when
`timescaledb_extension_v1` confirms the extension is installed:

- `pg_columns_v1` (timestamp/timestamptz columns),
  `pg_indexes_v1` (time-leading btrees), `pg_partitions_v1`
  (manually time-partitioned tables), `largest_relations_v1` +
  `pg_stat_user_tables_v1` (large append-mostly tables: high
  `n_tup_ins`, low `n_tup_upd`/`n_tup_del`).
- `timescaledb_hypertables_v1` (exclusion list — already converted).

### TS-R009 — Time-series index recommendation

- `timescaledb_dimensions_v1.column_name` (time dimension) joined
  against `pg_indexes_v1` / `index_health_summary_v1` on the
  hypertable (missing/duplicate time-leading indexes, unused
  indexes carried into every chunk).
- `timescaledb_compression_settings_v1.segmentby` / `orderby`
  (segmentby columns that deserve — or make redundant — btree
  indexes on the rowstore side).
- `timescaledb_hypertable_sizes_v1.index_bytes` vs `table_bytes`
  (index-heavy hypertables).

## Cross-cutting Analyzer inputs

- **Capability flags** (`timescaledb_extension_v1`): `license`
  gates TS-R003/TS-R004 on Apache builds; `extversion` is the
  provenance for any version-conditional wording;
  `has_columnstore_aliases` selects 2.18+ "columnstore" vs older
  "compression" terminology in findings text.
- **Completeness**: every family member reports through
  `collector_status.json` (EA-R001/EA-R004) — rules must degrade per
  the existing `EvidenceCompleteness` model when members are
  `skipped` (`extension_missing` / `version_unsupported`) or
  `failed` (`object_missing` / `permission_denied`).
- **Truncation**: when
  `sum(timescaledb_chunk_summary_v1.chunk_count)` exceeds the
  `timescaledb_chunks_v1` rowset size, per-chunk evidence is the
  newest 5000 — rules over per-chunk rows must say so rather than
  claim full coverage.
