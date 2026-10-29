# Semantic Collector Qualification

## Status

ACTIVE

## Purpose

Make a **semantic-value assertion a required, enforceable step** in the
collector-qualification lifecycle. A collector that merely executed
(`status='success'`), whose output shape reconciled (collected == persisted ==
exported row counts), or whose rows "looked valid" is **not** qualified — it may
still be emitting semantically wrong evidence: the wrong field mapped, the wrong
unit, inverted semantics, an unexpected aggregation, incorrect filtering, a
wrong engine interpretation, or plausible-but-wrong values.

The governing principle is:

> **collector success != collector qualification.**

This specification defines the smallest reusable mechanism that forces a
newly-qualified collector to demonstrate — against controlled database state —
that its emitted *values* are semantically correct, and a gate that a future
collector cannot pass using status-only evidence where semantic qualification is
required.

It does **not** rewrite the existing qualification layers for uniformity. It
layers *on top of* them, reusing the engine-specific semantic verifiers that
already exist (MySQL/MariaDB Replay Tier-1, `VerifyReplayTier1Evidence`), and
names the smallest generic verifier for engines that had only status/shape
checks (SingleStore, ClickHouse, PostgreSQL).

### Scope (POC-minimum vs 2.0)

This spec is the **POC-minimum**: the lifecycle contract + gate + one
**representative** semantically-qualified collector per engine, proving the
mechanism end to end. **Exhaustive per-collector semantic inventory across every
collector of every engine is the 2.0 release-blocker scope** (Elevarq/Signals#624)
and is explicitly out of scope here. The gate must stay honest about this: it
asserts the lifecycle and the representative proof, never a fabricated claim that
every collector is semantically qualified.

## Qualification levels

Qualification is a strictly ordered, explicit level. The levels MUST NOT be
conflated, and a qualification report MUST distinguish them:

| Level | Meaning | Established by |
|-------|---------|----------------|
| `executed` | the collector ran and returned `status='success'` | daemon collection |
| `structurally_validated` | output shape + row-count reconciliation held | `VerifyCadenceSnapshot` / `VerifyExportReconciliation` |
| `semantically_qualified` | emitted **values** matched a contract against controlled DB state | `VerifyReplayTier1Evidence` or `VerifySemanticContract` |

`executed` and `structurally_validated` say nothing about value correctness. Only
`semantically_qualified` may be reported as "semantic contract validated".

## Interfaces

### Inputs

| Input | Type | Constraints |
|-------|------|-------------|
| `QualExport` | parsed production-export payload | `PayloadByQuery[collectorID]` = the exact exported rows, read as a downstream consumer would |
| `SemanticContract` | checked-in per-collector contract | `CollectorID`, `Engine` (in `SupportedEngines`), `Level`, `Fields` (name/kind/unit/required), `ZeroRowLegit`, optional dedicated `Verifier` |
| `SemanticQualRegistry()` | per-engine contract authority | the checked-in inventory of qualified collectors |

### Outputs

| Output | Type | Meaning |
|--------|------|---------|
| `VerifySemanticContract(z, c) error` | error / nil | nil = every required field present with the declared semantic kind; error lists every violation |
| `SemanticContract.EffectiveVerifier()` | `func(QualExport) error` or nil | the dedicated `Verifier`, else a generic verifier built from `Fields`; **nil when a contract has neither** (a lifecycle violation the gate catches) |

## Rules

- **SCQ-R001 (checked-in inventory).** `SemanticQualRegistry()` maps each engine
  to the collectors qualified for it. Each `SemanticContract` records the
  collector's critical fields, each field's semantic **kind** and physical
  **unit**, and whether a zero-row result is a legitimate semantic answer
  (`ZeroRowLegit`). This is the human-readable + machine-checkable inventory.
- **SCQ-R002 (value assertion, not execution).** `VerifySemanticContract`
  asserts the collector's exported **values**: every `Required` field is present
  and non-empty on every row, a `numeric` field parses as a number, an
  `identifier` field is a non-empty untrimmed-clean token. It never passes on the
  strength of `status='success'` or a row count alone.
- **SCQ-R003 (semantic qualification must have teeth).** A contract at level
  `semantically_qualified` MUST have a non-empty `Fields` set **or** a dedicated
  `Verifier`. A `semantically_qualified` contract with neither has no value
  assertion — `EffectiveVerifier()` returns nil — and is a lifecycle violation.
  This is what makes status-only qualification impossible to mislabel.
- **SCQ-R004 (representative proof per engine).** Every engine in
  `SupportedEngines` has at least one `semantically_qualified` collector whose
  `EffectiveVerifier()` passes a representative controlled-state fixture.
- **SCQ-R005 (tamper sensitivity).** A deliberately altered field value — a
  numeric field made non-numeric, a required identifier blanked, a wrong column
  order/type/unit — makes the verifier fail deterministically. A verifier that
  cannot fail on a wrong value is not a qualification.
- **SCQ-R006 (PostgreSQL protection).** PostgreSQL's existing collector output
  stays covered: its representative contract is asserted here, and its full
  producer regression continues to live in the PostgreSQL regression fixtures.
  No PostgreSQL output loses coverage.
- **SCQ-R007 (honesty).** The registry declares only the qualification level each
  collector has actually reached. A collector that is only `executed` or
  `structurally_validated` is declared as such and is never reported as
  semantically qualified. Historical coverage is never fabricated.

## Invariants

- **SCQ-INV-1.** `status='success'` and row-count reconciliation are necessary
  but **not sufficient** for `semantically_qualified`. Semantic qualification is
  a strictly stronger claim.
- **SCQ-INV-2.** The mechanism is additive: the existing
  `VerifyCadenceSnapshot` / `VerifyExportReconciliation` / `VerifyReplayTier1Evidence`
  behaviour is unchanged. PostgreSQL behaviour is unchanged.
- **SCQ-INV-3 (determinism).** Identical input produces identical pass/fail and
  identical violation ordering.

## Failure conditions

| Trigger | Response |
|---------|----------|
| required field absent / empty on a row | error naming collector, row index, field |
| `numeric` field value does not parse as a number | error naming the non-numeric value + its unit |
| zero rows and `ZeroRowLegit=false` | error (an empty result is not a legitimate answer for this collector) |
| `semantically_qualified` contract with no `Fields` and no `Verifier` | `EffectiveVerifier()` nil → gate failure (SCQ-R003) |
| an engine has no `semantically_qualified` representative | gate failure (SCQ-R004) |

## Constraints

- Verification is a **pure** function of already-persisted/exported payload rows;
  it never re-collects and never mutates state.
- The generic verifier asserts field presence, kind, and non-emptiness only — it
  is deliberately not a universal semantic-inference engine. Richer per-collector
  semantics (column order, DDL-faithful types, composite-index ordering) are
  expressed by a dedicated `Verifier` (e.g. `VerifyReplayTier1Evidence`).

## NFR overrides

None. Determinism (SCQ-INV-3) is the governing non-functional requirement.
