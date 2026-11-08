# Singleton Store Upgrade Lifecycle

## Status

ACTIVE

## Issue

Elevarq/Signals#718 (audit F12 from the code review).

## Purpose

The Helm Deployment set `replicas: 1` but no update strategy, so Kubernetes
used its default `RollingUpdate`. For a collector backed by a single-writer
SQLite store on a ReadWriteOnce claim that is wrong: the rolling update surges a
second pod before terminating the old one, which either mounts the same store
(two writers), stalls when the RWO volume cannot attach to the new pod's node,
or — during a schema migration — lets the old binary keep writing a store the
new binary just migrated. This specification requires the chart to enforce
stop-before-start so exactly one pod owns the store at a time.

## Scope

In scope:

- `deploy/helm/signals/templates/deployment.yaml`: pin
  `strategy.type: Recreate`.
- Runbook note on the resulting brief, bounded downtime and recovery.

Out of scope:

- Multi-replica / sharded collector pools (future v1.1).
- An alternative store-ownership protocol that would make concurrent writers
  safe (not introduced here; mixed old/new writers remain unsupported).

## Interfaces

- Chart output: the rendered Deployment carries `spec.strategy.type: Recreate`.

## Behaviors

### B1 — Chart renders Recreate

Given the default (or any) values, When the chart is rendered, Then the
Deployment's `strategy.type` is `Recreate` and never `RollingUpdate`.

### B2 — One store owner across upgrade

Given a chart upgrade, When the new ReplicaSet rolls out, Then the old pod is
terminated before the replacement pod starts, so at most one pod owns the
SQLite store at any instant (brief downtime by design).

## Rules

- **STO-R001**: The collector Deployment MUST use `strategy.type: Recreate`;
  the chart MUST NOT ship the Kubernetes-default RollingUpdate for this
  single-writer store.
- **STO-R002**: The upgrade contract (stop-before-start, bounded downtime,
  forward-only schema, backup-based rollback) MUST be documented in the
  production runbook so operators expect the brief downtime.

## Invariants

- **INV-STO-01**: At most one collector pod owns the SQLite store at any time;
  the chart's lifecycle never permits a surge pod to co-mount or co-migrate it.

## Failure conditions

| Trigger | System response |
|---------|-----------------|
| Chart rendered with default RollingUpdate | Prevented: the template pins Recreate; the render test fails if it regresses (STO-R001). |

## Constraints

- Deterministic chart render; the test skips gracefully when the `helm` CLI is
  absent. No cluster required.

## NFR overrides

Availability: a chart upgrade incurs a brief, bounded downtime (old pod stops
before the new pod's readiness) — an intentional trade for store-ownership
correctness on the single-replica v1 topology.

## Rollback / recovery

Reverting the strategy restores the prior (unsafe) RollingUpdate default; no
persisted state changes. Operational rollback remains the documented
PVC-snapshot restore, since schema migrations are forward-only.
