# Catalog version selection: actual major vs compatible baseline

## Status

ACTIVE

## Issue

[Elevarq/Signals#816](https://github.com/Elevarq/Signals/issues/816) —
separate the actual server major (capability gating) from the compatible
SQL baseline (override resolution). Part of the PostgreSQL 19 program;
umbrella
[Elevarq/Analyzer#4201](https://github.com/Elevarq/Analyzer/issues/4201)
owns the shared support-claim policy and cross-repository acceptance
boundary.

Type: behavioral · Version: 0.1

---

## Purpose

The collector discovers the target's **actual** PostgreSQL major
(`discovery.go::Discover` → `MajorVersion`, from
`server_version_num`). For majors above the supported window
(`IsExperimentalMajor`, currently PG 19) it falls back to the highest
supported catalog (PG 18) for SQL, because no PG-19 override set exists
yet.

Before this feature the fallback **clamped a single major value** used
for *both* roles:

1. **Capability gating** — `QueryDef.MinPGVersion` decides whether a
   collector may run at all.
2. **SQL override resolution** — `overrideRegistry[major]` picks the
   version-specific SQL text for a logical collector.

Clamping the actual major (e.g. 19) down to the baseline (18) before
gating meant a `MinPGVersion: 19` collector could **never** run even on
a real PG 19 server — the capability was gated off by the fallback, not
by the server. That is the defect this feature fixes, while preserving
the PG 18 SQL fallback and **without** widening any support claim.

The two roles are now distinct:

- **Actual major** (`FilterParams.PGMajorVersion`) drives every
  `MinPGVersion` capability gate.
- **Catalog baseline** (`FilterParams.CatalogBaselineMajor`, the clamped
  value) drives override/SQL resolution only.

This is a source-confirmed routing correction, not a live-collector
failure, and it does **not** declare PG 19 supported. Full PG 19
certification remains the live-CI work in
[#815](https://github.com/Elevarq/Signals/issues/815) (blocked on
PG 19 GA; final `postgres:19` image does not yet exist as of
2026-11-19).

## Inputs

- `FilterParams.PGMajorVersion int` — the **actual** connected server
  major (never clamped).
- `FilterParams.CatalogBaselineMajor int` — the PG major whose override
  catalog applies. `0` means "same as `PGMajorVersion`" (the pre-#816
  caller shape and the normal in-window case, where actual == baseline).
- `QueryDef.MinPGVersion int` — a collector's minimum server major; `0`
  means "no floor".

## Outputs

- `Filter(FilterParams) []QueryDef` — eligible collectors, each with
  `SQL` resolved against the **baseline** catalog.
- `GatedIDsByReason` / `HighSensitivityIDs` — gating accounting keyed on
  the **actual** major.

## Behaviors

- **Given** a real PG 19 target (`PGMajorVersion=19`,
  `CatalogBaselineMajor=18`) **and** a `MinPGVersion: 19` collector,
  **when** filtering, **then** the collector is **eligible** and its SQL
  resolves against the PG 18 baseline (no PG 19 override exists, so the
  default/PG18 text is used).
- **Given** a PG 18 target, **when** filtering, **then** a
  `MinPGVersion: 19` collector is **gated** under
  `version_unsupported`.
- **Given** any in-window target (PG 14–18, `CatalogBaselineMajor=0` or
  equal to `PGMajorVersion`), **when** filtering, **then** eligibility
  and SQL are **byte-identical** to the pre-#816 behavior.
- **Given** an unknown higher major (e.g. PG 20, experimental fallback
  to baseline 18), **when** filtering, **then** capability gates honor
  the **actual** major (a `MinPGVersion: 19` or `MinPGVersion: 20`
  collector is eligible) while SQL resolves against the PG 18 baseline —
  an explicit, bounded best-effort, never a silent full-support label.

## Rules

- **CVS-R001** — The `MinPGVersion` capability gate MUST evaluate
  against `PGMajorVersion` (the actual server major) in **every** gate
  site: `Filter`, `HighSensitivityIDs`, and `GatedIDsByReason`.
- **CVS-R002** — SQL override resolution (`resolveSQL`) MUST use
  `catalogBaseline()` = `CatalogBaselineMajor` when `> 0`, else
  `PGMajorVersion`.
- **CVS-R003** — `CatalogBaselineMajor == 0` is backward-compatible:
  callers that set only `PGMajorVersion` behave exactly as before, so
  PG 14–18 collection is unchanged.
- **CVS-R004** — The collector MUST pass the **actual** discovered major
  as `PGMajorVersion` and the clamped experimental-fallback major as
  `CatalogBaselineMajor`. The actual server banner
  (`Discovery.ServerVersion`) is never overwritten.
- **CVS-R005** — This feature MUST NOT change `SupportedMajors`,
  `MaxSupportedMajor`, or any support-matrix status. Routing a
  capability by actual major is not a support claim.

## Invariants

- **CVS-INV-01** — A collector gated OFF for a version reason on the
  actual major never has its SQL executed (gating precedes resolution).
- **CVS-INV-02** — Read-only, non-superuser, error-isolation posture is
  unchanged; this feature only reroutes selection, it adds no query,
  cadence, write, or privilege.
- **CVS-INV-03** — No silent full-support: an out-of-window major is
  surfaced as experimental best-effort (the existing startup warning +
  the per-collector `version_unsupported` accounting), never relabeled
  supported.

## Failure conditions

- A server below `MinSupportedMajor` → collection refuses with
  `version_unsupported` (unchanged, precedes this logic).

## Non-functional requirements

- **Compatibility** — additive field on `FilterParams`; zero behavior
  change for in-window majors; downstream export unchanged (the actual
  major already travels in the snapshot `pg_version` banner, from which
  a consumer derives baseline/fallback against the known support
  window).
- **Safety** — no change to the read-only / non-superuser guarantees.

## Acceptance Rules

- **CVS-AT-01** — `catalogBaseline()` returns `PGMajorVersion` when
  `CatalogBaselineMajor == 0`, and `CatalogBaselineMajor` when `> 0`.
  Test: `TestCatalogBaseline_FallsBackToActual`.
- **CVS-AT-02** — `versionGateBlocks` blocks a `MinPGVersion: 19`
  collector for actual majors 14–18 and admits it for 19 and 20;
  `MinPGVersion: 0` is never blocked. Test:
  `TestVersionGateBlocks_KeysOnActualMajor`.
- **CVS-AT-03** — `Filter` with `PGMajorVersion=19, CatalogBaselineMajor=18`
  resolves a collector that has a PG 18 override to the **PG 18**
  override SQL (proving resolution keys on the baseline, not the actual
  major). Test: `TestFilter_ResolvesSQLAgainstBaselineNotActual`.
- **CVS-AT-04** — `Filter` eligibility and resolved SQL for every PG
  14–18 target are identical whether `CatalogBaselineMajor` is `0` or
  equal to `PGMajorVersion`. Test:
  `TestFilter_InWindowUnchangedByBaselineField`.

## Traceability

| Rule | Test |
|---|---|
| CVS-R002, CVS-R003 | `TestCatalogBaseline_FallsBackToActual` |
| CVS-R001 | `TestVersionGateBlocks_KeysOnActualMajor` |
| CVS-R001, CVS-R002 | `TestFilter_ResolvesSQLAgainstBaselineNotActual` |
| CVS-R003 | `TestFilter_InWindowUnchangedByBaselineField` |

## Out of scope

- Live PG 19 CI certification (#815 — blocked on PG 19 GA).
- PG19-only collectors / per-object `stats_reset` projection (#817).
- Persisted per-snapshot baseline provenance column: the actual major
  is already exported via the snapshot `pg_version` banner; adding a
  dedicated collection-time baseline column (CTP-style, #729 pattern) is
  a bounded follow-up if a consumer needs it pre-derived.
