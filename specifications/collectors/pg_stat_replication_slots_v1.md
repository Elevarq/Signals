# pg_stat_replication_slots_v1 — Collector Specification

## Status

ACTIVE

## Purpose

Captures the operational health of logical replication slots —
spill counters, stream counters, and total bytes processed — that
the existing `replication_slots_risk_v1` (`pg_replication_slots`)
collector does not surface. For shops running logical replication
(CDC pipelines, blue-green migrations, audit shipping), spill /
stream activity is the leading indicator of slot saturation,
under-sized `logical_decoding_work_mem`, or downstream consumer
back-pressure.

Complements (does not replace) `replication_slots_risk_v1`.

## Catalog source

`pg_stat_replication_slots` (PG 14+ only).

## Output columns

One row per logical replication slot. Empty rowset on PG instances
with no logical slots configured.

| Column | Type | Description |
|---|---|---|
| `slot_name` | text | Slot identifier as configured. |
| `spill_txns` | bigint | Transactions spilled to disk because they exceeded `logical_decoding_work_mem`. |
| `spill_count` | bigint | Number of times decoded transactions were spilled. |
| `spill_bytes` | bigint | Amount of decoded transaction data spilled. |
| `stream_txns` | bigint | Streamed (large-tx in-progress) transaction counter (PG 14+). |
| `stream_count` | bigint | Number of times streamed. |
| `stream_bytes` | bigint | Amount of data streamed. |
| `total_txns` | bigint | Total transactions sent (PG 14+). |
| `total_bytes` | bigint | Total decoded data sent. |
| `stats_reset` | timestamptz | When these counters were last reset. NULL on a fresh cluster. |

## Scope filter

Single SELECT against the system view; no schema scoping needed
(`pg_stat_replication_slots` is in `pg_catalog`).

## Invariants

- Empty rowset on instances with no logical slots — not an error.
- Read-only — single SELECT against a system view, no joins.
- `stats_reset` NULL when the stats subsystem has never been reset
  since cluster boot. Collector emits NULL; analyzer treats as
  "no reset since boot".
- Passes linter.

## Failure Conditions

- **FC-01**: PG version < 14 → collector excluded by `MinPGVersion`
  gate (R081). Emits `status=skipped, reason=version_unsupported`
  in `collector_status.json` via existing EA-R001 framework.

## Configuration

- Category: `replication`
- Cadence: `Cadence5m`
- Retention: `RetentionShort`
- Min PG version: 14
- Requires extension: none
- Semantics: snapshot (cumulative counters; analyzer takes the
  delta between snapshots)
- Enabled by default: yes
- Sensitivity: `low` (slot names are operator-assigned, not
  customer data; counters carry no PII)

## Sensitivity

Low. Slot names are operator-managed identifiers. Spill / stream /
total counters are numeric; no SQL text, no payload, no PII.

## Analyzer requirements unblocked

- **Spill-rate alerting**: cross-snapshot delta on `spill_txns` /
  `spill_count` / `spill_bytes` tells operators when
  `logical_decoding_work_mem` is too small for the workload.
  Actionable tuning recommendation.
- **Slot back-pressure detection**: `total_bytes` growing while
  `stream_bytes` is flat implies upstream is producing but the
  consumer isn't ingesting → slot is backing up.
- **Throughput baselining**: `total_bytes / (now - stats_reset)`
  gives bytes-per-second per slot for capacity planning.

## Known constraints

`pg_stat_replication_slots` view exists from PG 14+ with stable
column shape across PG 14, 15, 16, 17, 18 — single SQL works for
all supported majors. No per-major variants needed (R081 catalog
dispatch isn't triggered).

## Out of scope

- Physical replication slots: those are covered by
  `replication_slots_risk_v1` (`pg_replication_slots`) +
  `replication_status_v1` (`pg_stat_replication`).
- Per-publication / per-subscription accounting: the upstream
  view doesn't expose these dimensions.
