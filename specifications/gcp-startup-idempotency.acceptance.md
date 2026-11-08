# Acceptance — GCP Startup Reconcile Idempotency (#732)

Spec: `gcp-startup-idempotency.md`. Test:
`tests/signals_gcp_reconcile_test.go` (drives the real reconcile script with a
mock `docker`; skips when `openssl` is absent).

```yaml
- id: AC-GSI-01            # normal/boundary — idempotent across a re-run
  ref: [GSI-R001, GSI-R002, B1, B2, B3, INV-GSI-01]
  given: a first run (no container, no token) then a second run (container exists)
  when: reconcile-collector.sh runs twice
  then: first run docker-runs + mints a token; second run docker-starts (no
        re-run) and reuses the same token
  test: TestGCPReconcile_IdempotentAcrossRerun

- id: AC-GSI-02            # structural — single shared tested script is embedded
  ref: [GSI-R003]
  given: the gcp terraform startup script
  when: it is inspected
  then: it embeds deploy/gcp/scripts/reconcile-collector.sh
  test: TestGCPReconcile_TerraformEmbedsSharedScript
```
