# Acceptance — Apply Config-Only Helm Changes (#728)

Spec: `config-apply-on-upgrade.md`. Test:
`tests/signals_helm_config_checksum_test.go` (render assertions; skips when the
`helm` CLI is absent).

```yaml
- id: AC-CAU-01            # normal — config change rolls the pod
  ref: [CAU-R001, B1, INV-CAU-01]
  given: a helm upgrade that changes a target value
  when: the Deployment is rendered before and after
  then: the checksum/config annotation changes
  test: TestHelm_ConfigChangeRollsPod

- id: AC-CAU-02            # boundary — non-config change does not roll the pod
  ref: [CAU-R002, B2]
  given: an upgrade that changes only the image tag
  when: the Deployment is rendered before and after
  then: the checksum/config annotation is unchanged
  test: TestHelm_NonConfigChangeDoesNotRollPod
```
