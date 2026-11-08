# Acceptance — Audit Event Retention (#726)

Spec: `audit-event-retention.md`. Tests:
`internal/db/audit_retention_test.go` (store-level prune + strict cutoff) and
`internal/collector/audit_retention_cleanup_test.go` (cleanup pass prunes by its
own bound; keep-forever opt-out).

```yaml
- id: AC-AER-01            # normal — old events pruned, recent kept
  ref: [AER-R001, B1, B2]
  given: events older than a cutoff and events inside the window
  when: DeleteEventsOlderThan runs with that cutoff
  then: older events are deleted; recent events are preserved
  test: TestDeleteEventsOlderThan_PrunesOldKeepsRecent

- id: AC-AER-02            # boundary — cutoff is strict
  ref: [AER-R001, B2]
  given: one event exactly at the cutoff and one strictly before it
  when: DeleteEventsOlderThan runs
  then: only the strictly-older event is deleted; the at-cutoff event stays
  test: TestDeleteEventsOlderThan_CutoffIsStrict

- id: AC-AER-03            # normal — cleanup prunes by audit bound (diag off)
  ref: [AER-R002, B1, B3, INV-AER-01, INV-AER-02]
  given: diagnostic retention disabled but audit_retention_days = 90, with an
         old (>90d) and a recent event
  when: the collector cleanup pass runs
  then: the old event is pruned and the recent one kept
  test: TestCleanup_PrunesAuditEventsByOwnBound

- id: AC-AER-04            # boundary — keep-forever opt-out
  ref: [AER-R003, B4]
  given: audit_retention_days = 0 with a very old event
  when: the cleanup pass runs
  then: no event is pruned
  test: TestCleanup_AuditRetentionZeroKeepsEvents
```
