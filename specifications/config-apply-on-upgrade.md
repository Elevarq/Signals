# Apply Config-Only Helm Changes to the Running Collector

## Status

ACTIVE

## Issue

Elevarq/Signals#728 (audit A2 from the code review).

## Purpose

A Helm upgrade that edits only target/connection values updates the ConfigMap
but does not change the pod template, and the daemon has no config-file
watcher. A successful `helm upgrade` could therefore leave the old target
configuration active until an explicit reload or manual rollout — misleading
operators about which databases are actually monitored. This specification
requires the chart to roll the pod deliberately when (and only when) the
ConfigMap content changes, so a config-only upgrade is applied.

## Scope

In scope:

- `deploy/helm/signals/templates/deployment.yaml`: a `checksum/config`
  pod-template annotation derived from the rendered ConfigMap, plus a
  `podAnnotations` passthrough.
- Documentation of the apply behaviour.

Out of scope:

- An in-process config-file watcher / live SIGHUP-on-change (the deliberate
  rollout is the chosen contract; the existing SIGHUP/POST-reload paths remain).
- Multi-replica concerns — the rollout relies on the Recreate strategy (#718)
  for single-store safety.

## Interfaces

- Pod template annotation `checksum/config` = `sha256sum` of the rendered
  `configmap.yaml`.
- `podAnnotations` (map, default `{}`) merged alongside it.

## Behaviors

### B1 — Config change rolls the pod

Given a `helm upgrade` that changes any value rendered into the ConfigMap, When
the Deployment is rendered, Then `checksum/config` changes, producing a new pod
template so Kubernetes rolls the pod and the new configuration becomes active.

### B2 — Non-config change does not roll the pod

Given an upgrade that changes only values NOT in the ConfigMap (e.g. the image
tag), When the Deployment is rendered, Then `checksum/config` is unchanged, so
the collector is not needlessly restarted by this mechanism.

### B3 — Single-store-safe rollout

Given the roll triggered by B1, When Kubernetes applies it, Then the Recreate
strategy (#718) stops the old pod before starting the new one — no surge pod,
no migration race, one store owner.

## Rules

- **CAU-R001**: The pod template MUST carry a `checksum/config` annotation
  derived from the rendered ConfigMap, so a config-only `helm upgrade` changes
  the pod template and rolls the pod.
- **CAU-R002**: The annotation MUST track the ConfigMap content only — an
  unrelated value change (e.g. image tag) MUST NOT change it, so upgrades do not
  cause spurious restarts.

## Invariants

- **INV-CAU-01**: After a successful config-changing `helm upgrade`, the running
  pod reflects the new ConfigMap (no stale target set presented as active).
- **INV-CAU-02**: The config-triggered rollout preserves single-store ownership
  (via Recreate, #718) — it never races migrations or needs an extra pod.

## Failure conditions

| Trigger | System response |
|---------|-----------------|
| ConfigMap changes but pod template unchanged | Prevented by CAU-R001; the render test fails if the annotation is absent/static. |
| Spurious restart on an unrelated value change | Prevented by CAU-R002; the render test fails if the checksum tracks more than the ConfigMap. |

## Constraints

- Deterministic chart render; the test skips when the `helm` CLI is absent.

## NFR overrides

A config-changing upgrade incurs the same brief, bounded Recreate downtime as
any upgrade (#718) — intended for single-store correctness.

## Rollback / recovery

The annotation is additive; reverting restores the prior (no-auto-apply)
behaviour. No persisted state impact.
