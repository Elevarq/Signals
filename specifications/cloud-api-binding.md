# Cloud API Port Binding

## Status

ACTIVE

## Issue

Elevarq/Signals#719 (audit F13 from the code review).

## Purpose

The Azure and GCP deployment templates run the collector under Docker bridge
networking with `-p 127.0.0.1:8081:8081`, while the daemon bound
`127.0.0.1:8081` INSIDE its container. Published traffic reaches the
container's eth0 interface, not its loopback socket, so the collector can be
alive while host-side status/export/pause verification fails. This
specification requires the daemon to bind the container interface while the
host publication stays restricted to loopback, so authenticated host-side
control reaches the collector without exposing it to other hosts.

The transport mismatch was reproduced during the audit by running the built
image with the templates' loopback port mapping (`/health` succeeded inside the
container, failed on the host's mapped port). The AWS variant uses host
networking and is not affected.

## Scope

In scope:

- `deploy/azure/terraform/main.tf`, `deploy/gcp/terraform/main.tf`,
  `deploy/azure/bicep/main.bicep`: set the daemon `listen_addr` to the container
  interface (`0.0.0.0:8081`) while keeping the Docker host publish restricted to
  `127.0.0.1:8081:8081`.
- Host-side verification instructions in the Azure/GCP READMEs.

Out of scope:

- The AWS host-network variant (already correct).
- Changing the bearer-token auth model or TLS posture.

## Interfaces

- Template config `api.listen_addr` = `0.0.0.0:8081` (container interface).
- Docker run host publish = `-p 127.0.0.1:8081:8081` (loopback-only).

## Behaviors

### B1 — Daemon binds the container interface

Given the Azure/GCP bridge-networking templates, When the config is rendered,
Then `api.listen_addr` is `0.0.0.0:8081` and never `127.0.0.1:8081`, so the host
bridge publish can reach the daemon.

### B2 — Host publication stays loopback-restricted

Given the same templates, When the container is run, Then the host publish is
`127.0.0.1:8081:8081` (loopback) and never binds all host interfaces, so the API
is reachable from the VM host but not from other hosts.

## Rules

- **CAB-R001**: Under Docker bridge networking the daemon MUST bind the
  container interface (`0.0.0.0:8081`), not the container loopback, so the host
  publish reaches it.
- **CAB-R002**: The host publication MUST remain restricted to loopback
  (`127.0.0.1:8081:8081`); it MUST NOT bind all host interfaces. Combined with
  the bearer token this keeps the control plane reachable from the VM host and
  not from other hosts.

## Invariants

- **INV-CAB-01**: No change widens reachability beyond the VM itself — the API
  stays loopback-published and token-authenticated.

## Failure conditions

| Trigger | System response |
|---------|-----------------|
| A bridge template binds `127.0.0.1:8081` inside the container | Prevented by CAB-R001; the consistency test fails. |
| A bridge template publishes `0.0.0.0:8081:8081` on the host | Prevented by CAB-R002; the consistency test fails. |

## Constraints

- Structural/config consistency is CI-enforced deterministically (no live cloud,
  no container required). The full built-image transport reproduction is an
  audit/manual step, not a unit test.

## NFR overrides

None. Restores intended host-side reachability without broadening external
exposure.

## Rollback / recovery

Reverting restores the prior (broken) loopback-inside bind. No persisted state;
IaC-only change.
