# STDD CSI Gate Enforcement

## Status

ACTIVE

## Issue

Elevarq/Signals#451 (hardening item from the Sep-2026 internal review,
Elevarq/elevarq-ops#23).

## Purpose

The STDD CSI pipeline in `CLAUDE.md` (Gate 1 traceability, Gate 3 fingerprint)
and the spec lifecycle (`DRAFT → ACTIVE → …`) were documented but **not
enforced**. The traceability matrix was hand-maintained Markdown with no CI
check, there was no fingerprint of the knowledge layer, and shipped specs sat
in `DRAFT`. This specification defines automated gates that make those controls
real so spec/test/lifecycle drift fails the build instead of slipping through
review.

## Scope

In scope:

- `scripts/check-traceability.sh` — matrix-internal consistency (Gate 1).
- `scripts/check-spec-status.sh` — spec-lifecycle enforcement.
- `scripts/fingerprint.sh` + committed `.fingerprint` — knowledge-layer
  integrity (Gate 3).
- Wiring all three into `scripts/preflight.sh` and `.github/workflows/ci.yml`.

Out of scope (explicit non-goals, tracked as follow-up #519):

- **Full source-level bidirectional traceability** — asserting that every
  matrix Test ID resolves to a real test *and* every test maps back to a spec
  ID via a comment convention. The pre-existing matrix has ~42 symbolic Test
  IDs with no verbatim test reference and there is no test→ID convention;
  reconciling them is a separate one-time cleanup. Gate 1 here is the
  matrix-internal direction (a rule claiming coverage must list a test).
- Lifecycle enforcement of the per-collector contracts under
  `specifications/collectors/` — a separate inventory with its own guards.

## Interfaces

- Inputs: `features/signals/traceability.md`, `specifications/*.md` (root),
  the knowledge-layer trees (`specifications/`, `features/signals/`, `tests/`),
  and the committed `.fingerprint`.
- Outputs: process exit status (0 pass / 1 fail) + a human-readable report.
  `fingerprint.sh --write` (re)writes `.fingerprint`.

## Behaviors

### B1 — Traceability: orphan rule fails

Given a matrix row whose Coverage Status is `COVERED` with non-`DOC` Evidence
but an empty Test ID cell, When the traceability gate runs, Then it fails,
naming the rule. A row marked `PLANNED` or carrying `DOC` evidence may have an
empty Test ID.

### B2 — Traceability: duplicate / malformed rule IDs fail

Given two matrix rows with the same Rule ID, or a data row with an empty Rule
ID, When the gate runs, Then it fails.

### B3 — Spec lifecycle: missing or non-ACTIVE status fails

Given a root spec with no `## Status` section, or a shipped spec whose Status is
not `ACTIVE` and is not on the reviewed allow-list, When the spec-lifecycle gate
runs, Then it fails, naming the spec.

### B4 — Fingerprint: knowledge-layer drift fails

Given any change under `specifications/`, `features/signals/`, or `tests/` that
is not reflected in `.fingerprint`, When the fingerprint gate runs, Then it
fails and prints the regenerate command. A matching tree passes.

### B5 — Deterministic fingerprint

Given the same tree, When the fingerprint is computed on any machine (Linux CI
or macOS dev), Then the digest is identical (C-locale sort; `sha256sum` or
`shasum -a 256`).

## Rules

- **R-GATE-01**: The traceability gate MUST fail on a `COVERED`/non-`DOC` row
  with no Test ID (orphan rule), on a duplicate Rule ID, and on an empty Rule
  ID. It MUST allow an empty Test ID only for `PLANNED` rows or `DOC` evidence.
- **R-GATE-02**: The spec-lifecycle gate MUST require every root spec
  (`specifications/*.md`, excluding `*.acceptance.md`) to carry an explicit
  `## Status` section, and MUST fail any shipped spec that is not `ACTIVE`
  unless it is on a reviewed, reasoned allow-list.
- **R-GATE-03**: The fingerprint gate MUST compute one deterministic SHA-256
  over the knowledge layer (`specifications/`, `features/signals/`, `tests/`)
  and fail on any mismatch with the committed `.fingerprint`; `--write` MUST
  regenerate it.
- **R-GATE-04**: All three gates MUST run in `scripts/preflight.sh all` and as
  discrete CI steps, so the local pre-push hook and CI enforce the same checks.

## Invariants

- **INV-GATE-01**: A spec rule can never claim coverage (`COVERED`) in the
  matrix without a test or an explicit no-test-by-design marker.
- **INV-GATE-02**: Every shipped root spec has an explicit, ACTIVE lifecycle
  state; a DRAFT/undeclared shipped spec cannot pass CI.
- **INV-GATE-03**: A spec or test change and its `.fingerprint` update are
  committed together or CI fails — the cryptographic form of STDD's atomic
  commit.

## Failure conditions

| Trigger | System response |
|---------|-----------------|
| Orphan / duplicate / empty Rule ID in the matrix | `check-traceability.sh` exits 1 naming the row (R-GATE-01). |
| Root spec missing `## Status` or non-ACTIVE (not allow-listed) | `check-spec-status.sh` exits 1 naming the spec (R-GATE-02). |
| Knowledge-layer tree ≠ `.fingerprint` | `fingerprint.sh --check` exits 1 with the regenerate command (R-GATE-03). |

## Constraints

- Dependency-free beyond bash, awk, find, sort, and a sha256 tool.
- Deterministic and CI-stable; no network, no AWS, no PostgreSQL.

## NFR overrides

None. These gates strengthen the compliance posture (auditable enforcement of
change controls); they weaken no existing guarantee.

## Rollback / recovery

The gates are additive CI/preflight checks. Reverting the wiring disables them;
the `.fingerprint` file is inert data. No runtime impact.
