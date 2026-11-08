# Acceptance — Singleton Store Upgrade Lifecycle (#718)

Spec: `singleton-store-upgrade.md`. Test:
`tests/signals_helm_recreate_strategy_test.go` (the rendered Deployment pins
Recreate). The test skips when the `helm` CLI is absent.

```yaml
- id: AC-STO-01            # normal — Deployment renders Recreate
  ref: [STO-R001, B1, INV-STO-01]
  given: the signals chart with default values
  when: it is rendered with helm template
  then: the Deployment has strategy.type Recreate and not RollingUpdate
  test: TestHelm_DeploymentUsesRecreateStrategy
```
