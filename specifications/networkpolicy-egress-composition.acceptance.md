# Acceptance — NetworkPolicy Egress Composition (#727)

Spec: `networkpolicy-egress-composition.md`. Test:
`tests/signals_helm_egress_test.go` (render assertions; skips when the `helm`
CLI is absent).

```yaml
- id: AC-NPE-01            # normal — every configured target port is permitted
  ref: [NPE-R001, B1, INV-NPE-01]
  given: target.port and an additional targets[].port on a different port
  when: the NetworkPolicy is rendered
  then: the egress rule lists both ports
  test: TestHelm_EgressCoversAllTargetPorts

- id: AC-NPE-02            # normal — operator-scoped extra egress is rendered
  ref: [NPE-R002, B2]
  given: a networkPolicy.extraEgress entry (cidr + TCP 443)
  when: the NetworkPolicy is rendered
  then: the entry appears verbatim as an egress rule
  test: TestHelm_EgressExtraEgressPassthrough

- id: AC-NPE-03            # boundary — default-deny preserved, no blanket egress
  ref: [NPE-R003, B3, INV-NPE-01]
  given: no extraEgress (default)
  when: the NetworkPolicy is rendered
  then: no 0.0.0.0/0 egress destination is present
  test: TestHelm_EgressNoBlanketInternetByDefault
```
