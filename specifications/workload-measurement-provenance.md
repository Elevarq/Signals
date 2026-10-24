# Workload Measurement and Provenance Model (POC)

## Status

ACTIVE

## Purpose

Define how Signals captures workload evidence **losslessly** and with full
**provenance**, across engines (Elevarq/Signals #533, a #524 architecture
concern). The principle: collectors emit **raw values as observed at collection
time** — never a lossy pre-aggregation or a pre-computed delta — and every
result is tagged with enough provenance to attribute it to an engine, node,
collector, and moment. SingleStore is the worked example; the model is
engine-neutral and matches the existing PostgreSQL `delta-semantics` rule
(raw cumulative counters).

## Model

- **Lossless measurement:** workload collectors read cumulative counters and
  emit them **raw** (e.g. SingleStore `MV_PLANCACHE.COMMITS`,
  `EXECUTION_TIME`, `ROWCOUNT`, `CPU_TIME`; `MV_ACTIVITIES.CPU_TIME_MS`,
  `ELAPSED_TIME_MS`, `RUN_COUNT`). Deltas/rates are derived **downstream** from
  two snapshots, never computed in the collector — so no information is lost.
- **Provenance, per layer:**
  - *Snapshot:* `engine`, `engine_version`, `collected_at`, `target`
    (`metadata.json`).
  - *Collector result:* `collector_id`, `category` (one NDJSON object per
    collector).
  - *Row:* the source node where the view exposes it (`NODE_ID`), so
    distributed evidence is attributable to a node.

## Rules

| ID | Rule |
|----|------|
| **WMP-R001** | A workload collector emits raw cumulative counters; its SQL contains no delta computation (no `LAG`, no self-join on a prior sample, no `SUM/GROUP BY` that collapses rows lossily). |
| **WMP-R002** | Every collector result carries `collector_id` + `category`; a row from a node-scoped view carries its `NODE_ID`. |
| **WMP-R003** | The snapshot carries engine/version/collected_at/target provenance so any result can be attributed. |

## Invariants

| ID | Invariant |
|----|-----------|
| **INV-WMP-01** | No workload collector pre-aggregates or pre-deltas in-collector — raw counters only (lossless). |
| **INV-WMP-02** | Engine identity + collection time are always present in the snapshot envelope (attribution is never ambiguous). |

## Acceptance

See `workload-measurement-provenance.acceptance.md` — asserts the workload
collectors (`plancache`, `activities`) select raw cumulative counters and that
their SQL carries no delta/aggregation keyword, and that provenance fields are
present on the assembled snapshot.

## Non-functional requirements

- Correctness: downstream can always reconstruct rates from raw counters;
  reverse is impossible, hence raw is mandatory.
- Engine-neutral: the same rule governs PostgreSQL (`delta-semantics.md`) and
  SingleStore.

## Traceability

- Elevarq/Signals #533 (workload measurement + provenance), #524 architecture.
  Worked example: `singlestore-collectors.md`. Sibling: `delta-semantics.md`
  (PostgreSQL).
