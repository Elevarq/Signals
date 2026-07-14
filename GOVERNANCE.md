# Governance

How Elevarq Signals is governed: who maintains it, how decisions are
made, who holds release and security authority, and — most importantly
for an infrastructure collector you may depend on — what happens to the
project if ownership, staffing, or company strategy changes.

This policy is designed for auditability and supports Elevarq's
compliance-readiness posture (SOC 2 / ISO 27001 alignment). It reflects
the project's **actual** structure today, not an aspirational one.

## Model at a glance

Elevarq Signals is a **company-led, single-maintainer** open-source
project, published by **Elevarq** (Scantr LLC d/b/a Elevarq) under the
[BSD-3-Clause license](LICENSE). "Company-led" means final authority on
direction, releases, and security response rests with the company through
its maintainer. "Single-maintainer" is the honest current state — the
project does not yet have a maintainer team, and the continuity provisions
below exist precisely for that reason.

## Roles

| Role | Holds | Currently |
|------|-------|-----------|
| **Lead maintainer** | Final say on architecture, roadmap, releases, and security response. Merges changes; owns [`CODEOWNERS`](CODEOWNERS). | Frank Heikens ([@fheikens](https://github.com/fheikens)) |
| **Maintainer** | Reviews and merges within an area; no team members hold this role yet. | *(none yet)* |
| **Contributor** | Anyone who opens an issue or PR. | The community |

The roles are defined now so that adding people later is a matter of
naming them in the table and in `CODEOWNERS`, not of inventing process
under pressure.

## Decision making

Architectural and strategic decisions are made by the lead maintainer on
behalf of Elevarq. Community input is welcome and expected via GitHub
issues and pull requests, and material decisions are recorded in the
issue that authorizes the change (see the issue-first workflow in
[CLAUDE.md](CLAUDE.md)).

- **Routine changes** follow the issue → branch → PR → review → merge
  path in [CONTRIBUTING.md](CONTRIBUTING.md).
- **Disagreements** are resolved in the open on the issue or PR; the lead
  maintainer is the final escalation point.
- **Scope boundary** (below) is not negotiable per-PR — it is a
  product-line decision.

## Admitting and removing maintainers

The project is single-maintainer today. The path to a second maintainer,
recorded now so it is ready when it is needed:

- **Admission.** A contributor with a sustained track record of reviewed,
  merged, high-quality contributions and demonstrated judgement about the
  scope boundary and the safety model may be invited by the lead
  maintainer. Admission is recorded by adding the person to the **Roles**
  table and to `CODEOWNERS` in a normal PR.
- **Inactivity.** A maintainer inactive for **six consecutive months**
  without prior arrangement may be moved to emeritus (removed from
  `CODEOWNERS`, retained with thanks in the history). This protects the
  project from stale merge rights.
- **Removal.** A maintainer may step down at any time, or be removed by
  the company for a serious breach of this policy or the code of conduct.
  Either is recorded in a PR editing the Roles table and `CODEOWNERS`.

## Release authority

Releases are cut only through the automated, tag-driven CI/CD pipeline —
never by hand — per the Release Protocol in [CLAUDE.md](CLAUDE.md). The
lead maintainer authorizes a release by pushing the version tag after the
release gates pass. Authority to publish a release is equivalent to write
access to the repository and the release pipeline, held today by the lead
maintainer alone.

## Security-response authority

Vulnerability handling — private reporting, coordinated disclosure,
acknowledgement and fix targets, and supported versions — is defined in
[SECURITY.md](SECURITY.md) and is not restated here. Authority to triage
and disclose a security issue, and to cut an out-of-band security release,
rests with the lead maintainer. The private reporting channels
(GitHub Private Vulnerability Reporting and `security@elevarq.com`) are
company-controlled and survive a change of maintainer.

## Continuity and succession

This is the section that matters if you are deciding whether to depend on
Signals. The project's continuity does **not** hinge on any single person
or on the company's continued existence:

- **The permissive license is the backstop.** BSD-3-Clause grants everyone
  a perpetual, irrevocable right to use, modify, and redistribute the
  code. If Elevarq stops maintaining Signals for any reason, the community
  can fork the last released state and continue — no permission, key, or
  company cooperation required.
- **Everything needed to fork is public.** Source, specifications, tests,
  build pipeline, SBOMs, and signed release provenance are all in this
  repository and its releases. There is no private build step, no hidden
  dependency, and no secret required to build a working artifact from a
  clean checkout.
- **Reproducible, verifiable builds.** Releases are built by CI from a
  committed state, with SBOM and signed provenance (see
  [release-verification.md](docs/release-verification.md)), so a fork can
  prove it is building the same thing.

Company intent on an ownership or strategy change, in order of preference:

1. **Transfer stewardship** to a capable successor maintainer or steward
   organisation, announced before the transfer.
2. Failing that, **archive** the repository in a read-only state with a
   notice that points to the recommended community fork, if one exists.
3. In all cases, the license guarantee in the first bullet continues to
   apply to every released version.

> **Owner to confirm before this policy is marked final:** whether a
> specific successor maintainer or steward organisation is designated in
> advance. Absent a named successor, the license-plus-public-history
> guarantee above stands on its own and is the honest baseline. (The
> archival notice itself is written at the time of archival — GitHub's
> archived state is already read-only and self-describing — so no wording
> is pre-committed here.)

## Communication

Material governance changes — a new or departing maintainer, a change of
ownership, a transfer or archival — are communicated through:

- a merged PR editing this file and `CODEOWNERS` (the durable, auditable
  record); and
- the [CHANGELOG](CHANGELOG.md) and GitHub release notes for anything that
  affects how users consume the project.

Security communications follow [SECURITY.md](SECURITY.md) instead.

## Scope boundary

Elevarq Signals is strictly a diagnostic signal collector. It does not
perform analysis, scoring, or AI-powered interpretation. Contributions
that cross this boundary are redirected to the appropriate downstream
repository. This boundary is a product-line decision, not a per-PR one.

## Code of conduct

We expect all participants to be respectful and constructive. Harassment,
discrimination, and abusive behaviour are not tolerated. Conduct concerns
may be raised privately at `security@elevarq.com` (the monitored company
channel) and are handled by the lead maintainer.
