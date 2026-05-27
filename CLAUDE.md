# AI Development Policy — STDD

This repository follows **STDD — Specification & Test-Driven Development**.

Canonical methodology: https://github.com/fheikens/stdd

Claude Code must follow the workflow defined in this file.

The specification is the source of truth.
Code is disposable. Tests verify behavior.

## STDD Workflow

All development follows this sequence:

1. Specification
2. Acceptance rules
3. Tests derived from the specification
4. Implementation

Implementation must never start before a specification exists.

## Specification Requirements

Every feature specification must define:

- Inputs
- Outputs
- Invariants
- Failure conditions
- Non-functional requirements

Examples of non-functional requirements:

- Performance limits
- Compatibility constraints
- Safety guarantees
- Security requirements

Specifications live in `features/arq-signals/` and follow the
templates in `stdd/templates/`.

## Validation Requirements

All implementations must demonstrate traceability:

```
specification → tests → implementation
```

If code and specification diverge, **the specification is
authoritative**. Fix the code, not the spec — unless the spec itself
is wrong, in which case update the spec first, then the tests, then
the code.

Traceability is tracked in `features/arq-signals/traceability.md`.

## Project Safety Principles

For Arq Signals specifically:

- No write operations on PostgreSQL
- No superuser privileges required
- No hidden telemetry or external network calls
- Safe to run in production environments
- Credentials never stored, exported, or logged

Safety guarantees must never be weakened by any change. If a change
affects the safety model, the relevant specification and tests must be
updated first.

## Issue-first change workflow

Every change to this repository — code, tests, specs, documentation,
configuration, CI workflows, scripts, build files — MUST follow the
**issue → branch → commit → PR** sequence. There are no exceptions
for "small" or "obvious" changes. The single carve-out is genuinely
trivial fixes (typos, whitespace, comment-only edits with no
behaviour change); when in doubt, file the issue.

The four steps:

1. **File the issue first.** Even when the work is small, the
   issue is the durable record (milestone, `effort:*` label,
   priority, acceptance criteria, rationale). Other agents and
   future-you read the issue; no-one reads the PR body in
   isolation. No code, no workflow edits, no config changes start
   before the issue exists and is claimed (`status:claimed`
   label).
2. **Create a branch named after the issue number** —
   `<issue>-<short-slug>` or `feat/<issue>-<slug>` /
   `fix/<issue>-<slug>`. Branch off the latest `origin/main`.
   Never share a branch with another agent — use
   `git worktree add` per session.
3. **Do the changes on the branch.** STDD discipline applies:
   spec → tests → implementation. Keep the change scoped to the
   issue; no drive-by edits. If you discover related work
   mid-change, file a follow-up issue.
4. **Commit + open the PR.** Reference the issue number in the
   PR title and body. Use `closes #N` / `fixes #N` /
   `resolves #N` (not em-dash) so GitHub auto-closes the issue
   on merge.

### Why this matters

The issue-first sequence is not optional process overhead — it is
the practical implementation of the change-management controls
Elevarq aligns to:

- **SOC 2 CC8.1** — change authorization, documentation, approval,
  implementation
- **ISO/IEC 27001:2022 A.8.32** — change management for information
  systems
- **ISO/IEC 27001:2022 A.5.37** — documented operating procedures

A change without an issue is a change without an authorization
record. The issue + PR + signed commit chain is the audit-trail
evidence. Workflow, dependency, and config changes are the
changes auditors scrutinize most — file the issue for those too.

### Coordination

- Parallel-agent coordination via the `status:claimed` label
  and issue-numbered branch names.
- Pre-flight check before starting work: *"is the issue
  claimed? is there an open PR against it?"* If yes, pick a
  different issue or send a message to the running agent.

What this workflow does **not** fix: merge conflicts on hot
shared files (CHANGELOG.md, central indexes). Those are expected
when two well-behaved branches both touch the same file; the
second-to-land PR rebases. If the conflict cost becomes
operational, per-PR changelog fragments (towncrier-style) or
smaller-scope PRs are the mitigations — adopt them when the pain
crosses the cost of the tooling, not before.

The canonical wording for this rule lives in the global
`~/Projects/CLAUDE.md`. This repo extends that standard but
does not weaken it.

## Guardrail — Specification Before Code

If a request asks for code but no specification exists, Claude must
first propose a specification.

Claude must NOT immediately generate implementation code.

Instead, Claude must respond with:

1. Proposed specification (inputs, outputs, invariants, failure
   conditions)
2. Derived rules and acceptance criteria
3. Proposed tests

Only after the specification is confirmed may implementation code be
generated.

This applies to new features, new collectors, behavioral changes, and
safety model modifications. It does not apply to trivial fixes (typos,
formatting) or documentation-only changes.

## Repository Structure

```
features/arq-signals/
  specification.md          # Product requirements
  acceptance-tests.md       # Test cases derived from spec
  traceability.md           # Requirement → test mapping
  appendix-a-api-contract.md
  appendix-b-configuration-schema.md

stdd/templates/
  feature-spec-template.md  # Template for new feature specs
  test-spec-template.md     # Template for derived test cases
```

## Working copy location

The **canonical working copy** of arq-signals is this repository,
checked out at a stable sibling location alongside the other Elevarq
product repos — convention:
`<projects>/arq-signals/` alongside `<projects>/arq/`,
`<projects>/agent/`, `<projects>/pgagroal-container/`.

A copy of this source may also appear at
`<arq-repo>/.cache/repo-split/arq-signals/`. That location is a
**disposable build-input reflection** governed by the arq analyzer's
`workspace-policy.md` spec (WS-R001..WS-R016, Status: ACTIVE). It
may be a symlink to the canonical checkout (preferred — WS-R015),
a secondary clone (WS-R016), or absent — the analyzer's setup
tooling can re-create it at any time.

It is not the source of truth:

- **Never edit, test, or commit inside the cache path.** Any commit
  that lives only in the cache is at risk of deletion by a
  legitimate `rm -rf .cache/` (WS-R013 of the analyzer's workspace
  policy).
- If you notice yourself about to commit inside `.cache/`, stop and
  move the work to the canonical sibling checkout first.

Before any arq-signals action (edit / test / commit / push),
verify the canonical checkout is present:

```bash
test -d <projects>/arq-signals/.git \
  && echo "canonical sibling present — safe to work" \
  || echo "STOP: clone the canonical sibling first"
```

If the canonical sibling is missing:

```bash
cd <projects>
git clone git@github.com:elevarq/arq-signals.git

# Optionally point the analyzer's cache at the canonical checkout:
mkdir -p arq/.cache/repo-split
ln -s $(pwd)/arq-signals arq/.cache/repo-split/arq-signals
```
