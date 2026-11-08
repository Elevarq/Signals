# NetworkPolicy Egress Composition

## Status

ACTIVE

## Issue

Elevarq/Signals#727 (audit A1 from the code review).

## Purpose

The opt-in restrictive `NetworkPolicy` emitted egress for only the single
`target.port` plus DNS. When the install is configured with additional targets
on different ports, or with cloud-credential auth (`aws_rds_iam`,
`azure_entra`, `gcp_cloudsql_iam`, `secret_store`) or an `s3://` export
destination, those required outbound flows are not permitted — an enforcing CNI
then denies collection, authentication, or delivery under the recommended
restricted posture. This specification requires the policy to permit every
configured target port and to provide an operator-scoped passthrough for the
cloud/export endpoints, without weakening default-deny or adding blanket
internet egress.

## Scope

In scope:

- `deploy/helm/signals/templates/networkpolicy.yaml`: emit a TCP egress port
  rule for the primary `target.port` and every distinct `targets[].port`.
- A `networkPolicy.extraEgress` values passthrough (empty by default) for
  operator-scoped cloud / S3 egress rules.
- Documentation of which integrations require an `extraEgress` entry.

Out of scope:

- Hostname-aware / FQDN egress policy (a basic NetworkPolicy is IP/port only;
  the operator supplies the provider CIDRs). The chart does not infer endpoints.
- Changing ingress rules or the default-OFF posture of the policy.

## Interfaces

- Input: `target.port`, `targets[].port`, `networkPolicy.targetCIDRs`,
  `networkPolicy.kubeDNS`, and the new `networkPolicy.extraEgress` (a list of
  standard NetworkPolicy egress rules; default `[]`).
- Output: the rendered NetworkPolicy's `egress` block.

## Behaviors

### B1 — All configured target ports permitted

Given `target.port` and one or more `targets[].port` values, When the policy is
rendered, Then the egress rule to `targetCIDRs` lists every distinct configured
port (deduplicated).

### B2 — Operator-scoped extra egress is honoured

Given `networkPolicy.extraEgress` entries, When the policy is rendered, Then
each entry appears verbatim as an egress rule (so cloud-credential / S3 flows
can be permitted explicitly).

### B3 — Default-deny preserved

Given no `extraEgress` (the default), When the policy is rendered, Then the
egress destinations are only the operator's `targetCIDRs` and `kubeDNS` — never
a blanket `0.0.0.0/0`.

## Rules

- **NPE-R001**: The NetworkPolicy MUST permit egress to every configured target
  port (primary `target.port` plus each distinct `targets[].port`), not just
  the single primary port.
- **NPE-R002**: The chart MUST provide an operator-scoped `extraEgress`
  passthrough so the cloud-credential and S3-export endpoints can be permitted
  with explicit destination CIDRs + ports.
- **NPE-R003**: The change MUST NOT weaken default-deny or add unrestricted
  internet egress: `extraEgress` is empty by default and the generated policy
  never emits `0.0.0.0/0` on its own.

## Invariants

- **INV-NPE-01**: With `networkPolicy.enabled` true, no egress destination is
  broader than what the operator explicitly configured (`targetCIDRs`,
  `kubeDNS`, `extraEgress`).

## Failure conditions

| Trigger | System response |
|---------|-----------------|
| A configured target port is omitted from egress | Prevented by NPE-R001; the render test fails if it regresses. |
| `extraEgress` configured but not rendered | Prevented by NPE-R002; render test fails. |
| Blanket `0.0.0.0/0` emitted by default | Prevented by NPE-R003; render test fails. |

## Constraints

- Deterministic chart render; the test skips when the `helm` CLI is absent.
- IP/port NetworkPolicy only — no FQDN awareness implied.

## NFR overrides

None. Strengthens the restricted-deployment posture by making the required
flows explicit; it does not broaden the default egress surface.

## Rollback / recovery

`extraEgress` defaults to empty and the multi-port emission is additive;
reverting restores the single-port egress. No runtime/state impact.
