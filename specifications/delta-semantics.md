# Delta Semantics — Cross-Cutting Specification

Feature: Delta Semantics for Cumulative Counters
Version: 1.0
Type: behavioral
Status: DRAFT

---

## Purpose

Many PostgreSQL statistics views expose *cumulative* counters since the
last stats reset (e.g. `pg_stat_database.blks_read`,
`pg_stat_io.reads`, `pg_stat_statements.total_exec_time`). Analyses
that need rates, throughput, or activity-over-a-window require
differences between samples, not absolute values.

This specification defines how collectors emit cumulative data and
where the delta is computed.

## Rule

- Collectors emit **raw cumulative values** at collection time.
- The analyzer computes deltas across consecutive samples within the
  evaluation window.
- Signals maintains no inter-run state beyond the existing cadence
  planner's `last_run` table.

## Rationale

- Keeps the collector simple and stateless per query.
- Makes the snapshot self-describing: any downstream consumer can
  reconstruct rates without trusting Signals' internal bookkeeping.
- Preserves the separation-of-concerns principle: Signals collects,
  Arq analyzes.

## Requirements

- DS-R001: Every collector that reads a cumulative counter view MUST
  emit the raw counter value(s) and the `stats_reset` timestamp of
  the underlying view, where available.
- DS-R002: A collector MUST NOT subtract previous values inside its
  SQL. No `LAG()`, no `JOIN` to a previous-sample table.
- DS-R003: A collector spec that uses this model MUST declare
  `Semantics: cumulative` in its Configuration section and reference
  this specification.
- DS-R004: When a `stats_reset` is observed to move forward between
  samples, the analyzer MUST treat the delta as invalid for that
  pair and skip it (a reset truncates the counter to zero).
- DS-R005: When the underlying view does not expose `stats_reset`,
  the collector SHOULD emit the `collected_at` timestamp
  alongside each row so the analyzer can reason about sample gaps.

## Invariants

- INV-DS-01: The collector output for a cumulative counter is
  reproducible for a given server state at a given instant — no
  hidden state enters the row.
- INV-DS-02: Monotonicity within a session: between two samples
  without a reset, the cumulative counter is non-decreasing. A
  decrease indicates a counter reset, wraparound, or a server
  restart.

## Failure Conditions

- FC-DS-01: Counter decrease without `stats_reset` change → analyzer
  emits a coverage note ("counter wraparound or restart detected")
  and discards the delta.
- FC-DS-02: Sample gap exceeds the collector's cadence by more than
  2× (e.g. missed collection cycles) → analyzer may still compute
  a delta but must record the sample interval in the derived
  evidence so downstream consumers can weight accordingly.

## Non-Functional Requirements

- NFR-DS-01: No measurable overhead on the target database beyond
  the cost of the underlying `SELECT`. No temp tables, no server-side
  materialization of deltas.
- NFR-DS-02: Determinism. Given the same two raw samples, the
  analyzer must compute the same delta on every run.

## Acceptance Rules

- Every collector spec that cites this cross-cutter carries a
  `Semantics: cumulative (see delta-semantics.md)` line in its
  Configuration section.
- The analyzer's ingestion layer has an explicit
  `ComputeCumulativeDelta` path that handles FC-DS-01 and FC-DS-02.
- A traceability test verifies that every collector declared with
  `Semantics: cumulative` is wired into the analyzer's delta path.
