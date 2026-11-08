# Audit Event Retention

## Status

ACTIVE

## Issue

Elevarq/Signals#726 (audit F19 from the code review).

## Purpose

The `events` audit log (collection succeeded/failed, pause/resume, config
reloads) is inserted every collection cycle but was never pruned. The retention
cleanup deletes snapshots, query runs, and results, but not events — so a
configured diagnostic retention did not bound the store over the daemon's
lifetime, and at large fleet / high cadence the table is a slow disk-exhaustion
path after diagnostic history has stabilised. This specification gives events
an explicit, separate retention bound so the store cannot grow indefinitely
unnoticed, while preserving recent incident evidence.

## Scope

In scope:

- A dedicated `audit_retention_days` config field (distinct from diagnostic
  retention), default 90 days.
- `DeleteEventsOlderThan` store method (indexed, single-statement/atomic).
- Pruning events in the collector cleanup pass under the retention lock,
  independent of diagnostic retention being enabled.
- Operator documentation of the separate audit retention contract.

Out of scope:

- An external audit archive/export sink (documented as a future option; the
  keep-forever opt-out covers compliance-archival deployments for now).
- Changing diagnostic (snapshot/run/result) retention behaviour.

## Interfaces

- Config: `signals.audit_retention_days` (int, default 90). `0` (or negative)
  = keep events forever (explicit opt-out).
- `DB.DeleteEventsOlderThan(before string) (int64, error)` — prunes `events`
  with `timestamp < before`, returns the count deleted.
- Collector option `WithAuditRetentionDays(days int)`.

## Behaviors

### B1 — Events older than the bound are pruned

Given `audit_retention_days > 0` and events older than that cutoff, When the
cleanup pass runs, Then those events are deleted and the count is logged.

### B2 — Recent incident evidence is preserved

Given events inside the retention window (including exactly at the cutoff
instant), When cleanup runs, Then they are kept — the cutoff is strict (`<`).

### B3 — Audit retention is independent of diagnostic retention

Given diagnostic retention disabled (`retention_days = 0`, no structured
retention) but `audit_retention_days > 0`, When the cleanup pass runs, Then
events are still pruned by their own bound.

### B4 — Keep-forever opt-out

Given `audit_retention_days <= 0`, When cleanup runs, Then no event is pruned
(the pre-#726 behaviour), which an operator chooses deliberately and pairs with
capacity guidance.

### B5 — Consistency with concurrent export

Given an export overlapping cleanup, When events are pruned, Then the prune runs
under the same exclusive retention lock as the diagnostic DELETEs, so the export
sees a consistent pre- or post-cleanup state.

## Rules

- **AER-R001**: `DeleteEventsOlderThan` MUST delete exactly the `events` rows
  with `timestamp < before` (strict), preserving rows at or after the cutoff.
- **AER-R002**: The cleanup pass MUST prune events older than
  `audit_retention_days` when that value is > 0, even when diagnostic retention
  is disabled, under the retention lock.
- **AER-R003**: `audit_retention_days <= 0` MUST keep events forever (explicit
  opt-out); the default MUST be 90 days so growth is bounded by default.

## Invariants

- **INV-AER-01**: With the default config, the `events` table cannot grow
  unbounded over the daemon's lifetime — it is bounded by `audit_retention_days`.
- **INV-AER-02**: Audit retention is a SEPARATE contract from diagnostic
  retention; neither silently governs the other.

## Failure conditions

| Trigger | System response |
|---------|-----------------|
| `DeleteEventsOlderThan` errors | logged (`audit event cleanup failed`); the collection cycle is unaffected; retried next pass. |
| `audit_retention_days <= 0` | events kept forever (documented opt-out). |

## Constraints

- The prune is a single indexed `DELETE` (atomic in SQLite); no new dependency.
  Deterministic; no PostgreSQL, no network.

## NFR overrides

Storage: bounds audit-log growth. An operator opting into keep-forever
(`0`) re-accepts unbounded growth and should provision/monitor accordingly.

## Rollback / recovery

The field defaults to 90 and the prune is additive. Setting `0` restores the
prior keep-everything behaviour. No diagnostic-data impact.
