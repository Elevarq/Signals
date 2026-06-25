# Owner-Only Privilege Degradation — Behavioral Specification

Spec version: 1.0
Status: ACTIVE
Issue: [Elevarq/Signals#200](https://github.com/Elevarq/Signals/issues/200)
Promoted DRAFT → ACTIVE 2026-06-25 after review (#200, PR #207).

## Purpose

Some collectors read a PostgreSQL system catalog whose `PUBLIC` `SELECT`
privilege is revoked — specifically `pg_statistic_ext_data` (the same
posture as `pg_statistic`). A least-privilege monitoring role
(`pg_monitor` / `pg_read_all_stats`) therefore receives a hard
permission-denied error (SQLSTATE `42501`) on the *relation* — the
collector's `LEFT JOIN` does **not** rescue it. Direct reads require
superuser or an explicit `GRANT SELECT` on the catalog; `pg_monitor` /
`pg_read_all_stats` does not grant it (confirmed by the live AWS smoke,
where the monitoring role saw `42501`).

This is an **expected privilege boundary**, not a fault. Before this
spec the daemon recorded such a run as `status=failed,
reason=permission_denied`, degraded the cycle to `partial`, and logged a
"grant pg_monitor" advisory **every poll cycle** — advice that is both
incorrect (the missing privilege is ownership, not `pg_monitor`) and
noisy. This spec defines the corrected behavior: degrade the affected
collectors to `skipped`, and emit any operator advisory once.

This corrects [`pg_statistic_ext_data_v1.md`](collectors/pg_statistic_ext_data_v1.md)
FC-03 / AT-02, which previously assumed the `LEFT JOIN` yields
`available=false` rows for a non-owner `pg_monitor` role. It does not —
the relation is not selectable at all without ownership.

## Inputs

- A collector `QueryDef` carrying the boolean `OwnerOnlyDegrade` flag
  (`internal/pgqueries`). Set on `pg_statistic_ext_data_v1` and
  `pg_statistic_ext_data_mcv_v1`.
- The error returned by executing that collector's SQL against a target,
  classified as permission-denied when it carries SQLSTATE `42501`
  (`isPermissionDenied`).
- The `(target, collector)` identity of the run, for advisory dedup.

## Outputs

- A persisted `query_runs` row whose `status` / `reason` reflect the
  outcome (`internal/collector`), surfaced through
  `collector_status.json`.
- At most one operator log advisory per `(target, collector, kind)` for
  the daemon's lifetime.

## Rules

- **R116 — Owner-only degrade.** When a collector with
  `OwnerOnlyDegrade = true` returns a permission-denied (`42501`) error,
  the run is recorded `status=skipped, reason=privilege_owner_only`.
  A permission-denied error on any collector **without** the flag, and
  any non-permission error on a flagged collector, remain
  `status=failed`.
- **R117 — Advisory dedup.** A per-cycle operator advisory for a
  persistent condition (owner-only skip, or a genuine permission_denied
  failure) is logged at most once per `(target, collector, kind)` for the
  daemon's lifetime, not on every poll.

## Invariants

- **INV-01** — A `privilege_owner_only` outcome is a *skip*, never a
  *failure*: `status=skipped`, `Attempted=false` in the reconstructed
  `CollectorStatus`.
- **INV-02** — `privilege_owner_only` is distinct from
  `budget_exhausted`; only `budget_exhausted` skips (and failures) make a
  cycle `partial`. An owner-only skip never marks the cycle `partial`.
- **INV-03** — The degrade is collector-scoped: it applies only to
  collectors that opt in via `OwnerOnlyDegrade`. Exactly the two
  `pg_statistic_ext_data` collectors carry the flag.
- **INV-04** — The behavior is read-only and credential-free: it changes
  only run classification and log cadence, never query SQL, connection
  posture, or persisted payloads.

## Failure conditions

- **FC-01** — Owner-only collector, `42501` → `skipped` /
  `privilege_owner_only`; cycle not `partial` on this account; advisory
  logged once per `(target, collector)`.
- **FC-02** — Owner-only collector, non-permission error (timeout,
  connection reset, `42P01`, …) → `failed` with the classified reason
  (unchanged behavior).
- **FC-03** — Non-owner-only collector, `42501` → `failed` /
  `permission_denied` (unchanged); the standard "grant pg_monitor"
  advisory still applies, but deduped per R117.
- **FC-04** — Superuser (or a role explicitly granted `SELECT` on
  `pg_statistic_ext_data`) → the collector succeeds and the `available`
  column reports per-object blob presence as normal; the degrade path is
  not taken.

## Non-Functional Requirements

- **NFR-01** — Dedup state is per daemon process, guarded for concurrent
  multi-target cycles; it adds O(distinct conditions) memory, bounded by
  `targets × owner-only-collectors`.
- **NFR-02** — No change to export schema or `collector_status.json`
  shape; `privilege_owner_only` is a new `reason` value within the
  existing `skipped` status, consumed by downstream completeness models
  the same way as other skip reasons.

## Acceptance Rules

- **AT-01** — `classifyQueryFailure(true, <42501>)` →
  `("skipped", "privilege_owner_only")`. (TC-OOPD-01)
- **AT-02** — `classifyQueryFailure(true, <non-permission>)` →
  `("failed", …)`. (TC-OOPD-02)
- **AT-03** — `classifyQueryFailure(false, <42501>)` →
  `("failed", "permission_denied")`. (TC-OOPD-03)
- **AT-04** — `reasonPrivilegeOwnerOnly != reasonBudgetExhausted`, and a
  persisted `skipped/privilege_owner_only` run reconstructs as a skip
  with `Attempted=false`. (TC-OOPD-04)
- **AT-05** — `warnOnce` returns true exactly once per
  `(target, collector, kind)`. (TC-OOPD-05)
- **AT-06** — Exactly `pg_statistic_ext_data_v1` and
  `pg_statistic_ext_data_mcv_v1` carry `OwnerOnlyDegrade`; an ordinary
  collector does not. (TC-OOPD-06, TC-OOPD-07, TC-OOPD-08)

## Safety Impact

- [x] Read-only enforcement preserved — no query, write, or connection
      change; only run classification and log cadence change.
- [x] No credential handling change.
- [x] No new external surface — `privilege_owner_only` is a new value of
      the existing `reason` field in `collector_status.json` and the
      existing skip-reason metric label, not a new endpoint, config key,
      or export schema (NFR-02).

The degrade makes the daemon *more* honest about least-privilege roles:
an expected privilege boundary is reported as an explicit, named skip
rather than a misleading failure with incorrect remediation advice.
