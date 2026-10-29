# Acceptance Tests: semantic-collector-qualification

## Feature

`specifications/semantic-collector-qualification.md` — a semantic-value
assertion is a required, enforceable step in the collector-qualification
lifecycle; `status='success'` and row-count reconciliation are never sufficient
for `semantically_qualified`.

## Test Cases

### TC-SCQ-01: representative proof per engine (normal)

**Rule:** SCQ-R004, SCQ-R001

**Given:** `SemanticQualRegistry()` and a representative controlled-state fixture
per engine (MySQL/MariaDB Replay Tier-1, SingleStore table statistics, ClickHouse
parts, PostgreSQL bloat estimate).

**When:** each engine's `semantically_qualified` contract's `EffectiveVerifier()`
runs against its fixture.

**Then:** every engine in `SupportedEngines` has at least one
`semantically_qualified` contract and its verifier returns nil.

**Expected Result:** Pass only when all five engines carry a passing
representative semantic assertion.

---

### TC-SCQ-02: semantic qualification must have teeth (boundary)

**Rule:** SCQ-R003

**Given:** every `semantically_qualified` contract in the registry.

**When:** `EffectiveVerifier()` is resolved for each.

**Then:** it is non-nil for all of them — each has a non-empty `Fields` set or a
dedicated `Verifier`.

**Expected Result:** Pass only when no `semantically_qualified` contract relies on
status-only evidence.

---

### TC-SCQ-03: a tampered value fails deterministically (failure)

**Rule:** SCQ-R005, SCQ-R002

**Given:** each engine's passing representative fixture.

**When:** one critical value is altered — a numeric field made non-numeric or a
required identifier blanked — and the verifier runs again.

**Then:** the verifier returns a non-nil error for every engine.

**Expected Result:** Pass only when every representative verifier is proven to
fail on a wrong value.

---

### TC-SCQ-04: execution without values is not qualification (invalid)

**Rule:** SCQ-R002, SCQ-INV-1

**Given:** a `QualExport` whose collector ran (`status='success'`, rows present)
but carries a required numeric field with a non-numeric value.

**When:** `VerifySemanticContract` runs.

**Then:** it returns an error — a successful, shape-valid collection is rejected
on value grounds.

**Expected Result:** Pass only when status-success-but-wrong-value is rejected.

---

### TC-SCQ-05: levels are distinct and honestly reported (normal)

**Rule:** SCQ-R007, SCQ-INV-1

**Given:** the `CollectorQualLevel` values and the registry.

**When:** levels are rendered for reporting.

**Then:** `executed`, `structurally_validated`, and `semantically_qualified` are
distinct strings, and registry entries report their declared level — none
reported as semantically qualified without a value assertion.

**Expected Result:** Pass only when the "collector executed" vs "semantic
contract validated" distinction is explicit.

---

### TC-SCQ-06: zero rows honor declared legitimacy (boundary)

**Rule:** SCQ-R002

**Given:** a contract with `ZeroRowLegit=false` and an empty payload.

**When:** `VerifySemanticContract` runs.

**Then:** it returns an error (an empty result is not a legitimate semantic answer
for that collector); the same empty payload under `ZeroRowLegit=true` passes.

**Expected Result:** Pass only when zero-row legitimacy is enforced per contract.
