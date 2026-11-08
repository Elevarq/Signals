# Acceptance — Collector Shutdown Join (#731)

Spec: `collector-shutdown-join.md`. Tests: `cmd/signals/shutdown_test.go`
(the join primitive and the bounded deadline).

```yaml
- id: AC-SHUT-01            # normal — finished collector joined immediately
  ref: [SHUT-R001, B3, INV-SHUT-01]
  given: the collector goroutine has already returned (done closed)
  when: joinCollector runs
  then: it returns true without waiting
  test: TestJoinCollector_FinishesBeforeDeadline

- id: AC-SHUT-02            # normal — in-flight collector is waited for
  ref: [SHUT-R001, B1, INV-SHUT-01]
  given: the collector finishes shortly after cancellation
  when: joinCollector runs with a generous deadline
  then: it waits and returns true
  test: TestJoinCollector_FinishesJustInTime

- id: AC-SHUT-03            # failure — hung collector honours the deadline
  ref: [SHUT-R002, B4]
  given: the collector never finishes
  when: joinCollector runs with a short deadline
  then: it returns false after the deadline rather than hanging
  test: TestJoinCollector_TimesOutWhenCollectorHangs

- id: AC-SHUT-04            # boundary — the deadline is positive and bounded
  ref: [SHUT-R002]
  given: the configured collectorShutdownTimeout
  when: its value is inspected
  then: it is positive and at most one minute
  test: TestCollectorShutdownTimeout_Bounded
```
