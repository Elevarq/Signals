# Archive

Historical documentation preserved for audit trail. Content here is
**not** maintained — these documents describe work, decisions, and
state at the moment they were written. For current operator-facing
documentation, see [`docs/`](../) (one level up).

## Why these files exist

Several SOC 2 / ISO 27001-relevant evidence categories require
continuity of pre-decision and post-decision documentation:

- **Pre-release readiness**: go/no-go reports, publication checklists
- **Audits**: documentation audit, credential-handling review,
  STDD-sufficiency audit
- **Remediation trails**: current-state findings paired with their
  implementation reports
- **Historical release notes**: superseded by [`CHANGELOG.md`](../../CHANGELOG.md)
- **Planning artifacts**: pack-2 survival plan, dynamic capture
  plans — work that has since shipped

Deleting them would erode the audit story. Keeping them in
[`docs/`](../) clutters the operator-facing surface. Archiving them
here preserves both.

## Index

### Release-era artifacts (v0.1.0 / v0.2.0)

- [`release-notes-v0.1.0.md`](release-notes-v0.1.0.md)
- [`release-notes-v0.2.0.md`](release-notes-v0.2.0.md)
- [`pre-publication-go-no-go.md`](pre-publication-go-no-go.md)
- [`publication-check.md`](publication-check.md)
- [`launch-announcement.md`](launch-announcement.md)
- [`github-repo-metadata.md`](github-repo-metadata.md)
- [`v0.2.0-smoke-test-report.md`](v0.2.0-smoke-test-report.md)

### Audits

- [`documentation-audit.md`](documentation-audit.md)
- [`credential-handling-review.md`](credential-handling-review.md)
- [`stdd-sufficiency-audit.md`](stdd-sufficiency-audit.md)
- [`stdd-coverage-closure-report.md`](stdd-coverage-closure-report.md)
- [`stdd-reconstruction-remediation-report.md`](stdd-reconstruction-remediation-report.md)

### Remediation trails

- [`remediation-current-state.md`](remediation-current-state.md)
- [`remediation-implementation-report.md`](remediation-implementation-report.md)
- [`safety-hardening-current-state.md`](safety-hardening-current-state.md)
- [`runtime-safety-implementation-report.md`](runtime-safety-implementation-report.md)

### Planning artifacts (shipped)

- [`diagnostic-pack-2-survival-plan.md`](diagnostic-pack-2-survival-plan.md)
- [`pg-stat-statements-dynamic-capture-plan.md`](pg-stat-statements-dynamic-capture-plan.md)
- [`pg-stat-statements-dynamic-capture-report.md`](pg-stat-statements-dynamic-capture-report.md)

## When to add to this archive

Move a document here when:

1. It describes a **completed** activity (an audit, a release, a
   remediation pass). The activity is the deliverable; the
   document is the receipt.
2. The information has been **superseded** by a current document
   (e.g. release notes superseded by `CHANGELOG.md`).
3. It was a **planning artifact** and the work has shipped.

Documents that describe the **current** state of the system stay in
[`docs/`](../) and get updated as the system changes. Examples:
`runtime-safety-model.md`, `audit-model.md`, `control-plane.md`.

## When to delete from this archive

Rarely. If a document contains incorrect information that could
mislead an auditor reading the trail, replace it with a corrected
version and keep both — the original with a `## SUPERSEDED` banner
at the top, and the corrected file alongside. Deletion erases the
audit trail.
