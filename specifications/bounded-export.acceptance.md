# Acceptance — Bounded Export Memory and Cancellation (#730)

Spec: `bounded-export.md`. Tests: `internal/api/export_limiter_test.go`
(admission bound) and `internal/export/export_cancel_test.go` (cancellation +
background shim).

```yaml
- id: AC-BEX-01            # normal — admit up to the limit, reject excess
  ref: [BEX-R001, B1, B2, INV-BEX-01]
  given: a limiter with limit 2
  when: three acquires are attempted, then one release and another acquire
  then: the first two admit, the third is rejected, and a freed slot re-admits
  test: TestExportLimiter_AdmitsUpToLimitThenRejects

- id: AC-BEX-02            # boundary — unlimited opt-out
  ref: [BEX-R002, B3]
  given: max_concurrent_exports <= 0
  when: the limiter is created and many acquires are attempted
  then: the limiter is nil and admits every request without panicking
  test: TestExportLimiter_NilIsUnlimited

- id: AC-BEX-03            # failure — cancellation aborts, no partial archive
  ref: [BEX-R003, B4, INV-BEX-02]
  given: a WriteToCtx whose context is already cancelled
  when: the export runs
  then: it returns context.Canceled and writes no archive bytes
  test: TestWriteToCtx_CancelledReturnsError

- id: AC-BEX-04            # normal — background shim unaffected
  ref: [BEX-R003, B5]
  given: a caller using WriteTo (background context)
  when: it runs against a store
  then: it produces a valid non-empty archive
  test: TestWriteTo_BackgroundContextStillWorks
```
