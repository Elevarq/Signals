# Acceptance — STDD CSI Gate Enforcement (#451)

Spec: `stdd-gate-enforcement.md`. Tests: `tests/signals_stdd_gates_test.go`
(the gates pass on the committed tree and reject injected violations).

```yaml
- id: AC-GATE-01            # normal — gates pass on the committed tree
  ref: [R-GATE-01, R-GATE-02, R-GATE-03, R-GATE-04]
  given: the repository at HEAD
  when: check-traceability.sh, check-spec-status.sh, fingerprint.sh --check run
  then: all exit 0
  test: TestSTDDGates_PassOnCommittedTree

- id: AC-GATE-02            # failure — orphan rule row is rejected
  ref: [R-GATE-01, B1, INV-GATE-01]
  given: a matrix with a COVERED/BEHAVIORAL row that has no Test ID
  when: check-traceability.sh runs against it
  then: exit 1 naming the orphan rule
  test: TestSTDDGates_TraceabilityRejectsOrphan

- id: AC-GATE-03            # failure — a non-ACTIVE shipped spec is rejected
  ref: [R-GATE-02, B3, INV-GATE-02]
  given: a root spec whose Status is DRAFT (not allow-listed)
  when: check-spec-status.sh runs against it
  then: exit 1 naming the spec
  test: TestSTDDGates_SpecStatusRejectsDraft

- id: AC-GATE-04            # failure — knowledge-layer drift is rejected
  ref: [R-GATE-03, B4, INV-GATE-03]
  given: a knowledge-layer tree that does not match .fingerprint
  when: fingerprint.sh --check runs
  then: exit 1 with the regenerate command
  test: TestSTDDGates_FingerprintDetectsDrift

- id: AC-GATE-05            # boundary — PLANNED / DOC rows may omit a Test ID
  ref: [R-GATE-01, B1]
  given: a matrix row marked PLANNED, and one with DOC evidence, each with no Test ID
  when: check-traceability.sh runs
  then: exit 0 (these are legitimate no-test-by-design rows)
  test: TestSTDDGates_TraceabilityAllowsPlannedAndDoc
```
