# PG19 Per-Object stats_reset Projection — Specification

## Status

ACTIVE

Feature: PG19 per-object reset-identity projection for table/index collectors
Version: 1.0
Type: behavioral
Issue: Elevarq/Signals#817 (umbrella Elevarq/Analyzer#4201)
Downstream consumer: Elevarq/Analyzer#4315 (temporal-reset invalidation)

---

## Purpose

PostgreSQL 19 adds a per-object `stats_reset` column to
`pg_stat_all_tables` / `pg_stat_user_tables` and
`pg_stat_all_indexes` / `pg_stat_user_indexes` (PG19 release notes,
commit `a5b543258`; "Add `stats_reset` column to system views
`pg_stat_all_tables`, `pg_stat_all_indexes`, and
`pg_statio_all_sequences`"). Before PG19 these per-object views did not
expose any reset timestamp, so a counter reset followed by enough new
activity could evade a simple decreasing-counter check and be reported
as a false usage/churn/rate trend.

This specification adds the **per-object reset identity** to the two
existing logical collectors `pg_stat_user_tables_v1` and
`pg_stat_user_indexes_v1` so a downstream consumer (Analyzer #4315) can
invalidate a usage/churn/rate interval that straddles a reset rather
than report a false trend. It does so WITHOUT adding a new collector, a
new object scan, or a new query cadence: one nullable scalar per
existing row.

The projection is a PostgreSQL-19 capability, gated on the **actual**
connected server major (`FilterParams.PGMajorVersion`, post-#816), not
on the clamped catalog baseline. This matters because PG19 is above the
supported catalog window: on a real PG19 target #816 clamps
`CatalogBaselineMajor` to 18 so default SQL resolves against the PG18
baseline, while `PGMajorVersion` remains 19 and drives capability
gating. A per-object `stats_reset` that only exists on PG19 therefore
cannot use the baseline-keyed override path (`RegisterOverride`, which
`resolveSQL` resolves against the baseline); it uses a new
actual-major-keyed capability-projection path (CPJ-R001..R003 below).

Adding full PG19 support (moving `MaxSupportedMajor`) is explicitly out
of scope and remains gated on live PG19 CI; this specification adds a
single capability-gated evidence column, not a support claim.

## Interfaces

### Inputs

| Input | Type | Constraint |
|---|---|---|
| `FilterParams.PGMajorVersion` | int | Actual connected server major (post-#816). Drives the capability projection. |
| `FilterParams.CatalogBaselineMajor` | int | Clamped baseline (post-#816). Unchanged; drives `resolveSQL` baseline resolution only. |

### Outputs

The two affected collectors gain one trailing output column:

| Column | Type | Description |
|---|---|---|
| `stats_reset` | timestamptz | Per-object time at which this relation's statistics were last reset. NULL on PG14-18 (column absent in the catalog) and whenever the server reports it NULL (never reset). |

Existing columns, their names, their order, and the row set (ordering,
scope filter) are unchanged. `stats_reset` is appended as the final
column so the pre-existing identity and column order are preserved.

## Rules

- **CPJ-R001 (capability-projection path).** A collector MAY register a
  capability projection keyed on an **actual** PG major via
  `RegisterCapabilityOverride(major, id, sql)`. `Filter` resolves the
  effective SQL as: capability projection for `PGMajorVersion` if one
  exists, else the baseline override for `catalogBaseline()` (#816),
  else the default SQL. The capability projection keys on the actual
  server major so a PG19-only column is selected on a real PG19 target
  even though its catalog baseline is clamped to 18.

- **CPJ-R002 (capability precedence).** When both a capability
  projection (for the actual major) and a baseline override (for the
  clamped baseline) exist for the same logical ID, the capability
  projection wins. This is the only case where actual-major resolution
  overrides baseline resolution; absent a capability projection,
  resolution is byte-identical to pre-#817 behaviour.

- **CPJ-R003 (lint + duplicate guards).** `RegisterCapabilityOverride`
  lint-checks its SQL exactly like `Register`/`RegisterOverride`,
  panics on an unknown logical ID, and panics on a duplicate
  `(major, id)` pair — the same coding-error guards as the baseline
  override registry.

- **SR-R001 (PG19 projection).** On an actual PG19 target,
  `pg_stat_user_tables_v1` selects the real `stats_reset` column from
  `pg_stat_user_tables`, and `pg_stat_user_indexes_v1` selects the real
  `stats_reset` column from `pg_stat_user_indexes`. Both appended last.

- **SR-R002 (PG14-18 typed NULL).** On PG14-18 the default SQL emits
  `NULL::timestamptz AS stats_reset` for both collectors, so the output
  column set is identical across majors; PG14-18 never reference the
  absent catalog column.

- **SR-R003 (unknown vs value).** A NULL `stats_reset` is an explicit
  unknown: PG14-18 (column absent) and PG19 "never reset" both surface
  as NULL. A non-NULL value is the server-reported per-object reset
  time, carried through export unchanged.

## Invariants

- **INV-SR-01 (row-shape compatibility).** The pre-existing columns,
  their order, and the row set (ordering, scope) of both collectors are
  unchanged on every major. `stats_reset` is strictly additive and
  always the final column. An older Analyzer build that ignores unknown
  trailing columns continues to ingest.

- **INV-SR-02 (no new work).** No additional object scan, query, or
  cadence is introduced: the column is a projection on the existing
  `FROM pg_stat_user_tables` / `pg_stat_user_indexes` select. Read-only
  `pg_catalog` selection; passes the linter.

- **INV-SR-03 (safety preserved).** No writes, no superuser, no new
  network calls. The column is read under the same
  normal-monitoring-visibility role as the rest of the row;
  permission-masked rows stay unavailable exactly as before (no
  elevation is requested to read `stats_reset`).

## Failure Conditions

- **FC-01 (PG < 19).** The catalog column is absent. Handled at the SQL
  layer by the typed-NULL default (SR-R002), not at runtime: PG14-18 SQL
  never names `stats_reset`, so there is no missing-column error. The
  collector keeps its existing `MinPGVersion: 10` and stays eligible on
  every supported major.

- **FC-02 (permission denied / masked rows).** Unchanged from the base
  collectors: a row the role cannot see is not emitted, and its
  `stats_reset` is therefore unavailable rather than a fabricated value.
  No new privilege is requested.

## Constraints

- One scalar (`timestamptz`) added per existing row. Payload delta is
  bounded by (row count) x (one nullable timestamp); no extra rows, no
  extra round-trips. On a large catalog the delta is linear in the
  existing table/index row count already emitted by these collectors.

## Live-fixture dependency (blocked)

The live acceptance (TC-SR-05: "identify a reset even after counters
catch up") requires a final-GA PG19 stats container in CI. As of
2026-11-19 the official `postgres:19` image is not yet published (Docker
Hub `library/postgres` latest 19.x is `19rc2`; no `19`/`19.0`), so the
live reset/catch-up and masked-visibility fixtures cannot run yet. The
catalog/unit layer (CPJ-R001..R003, SR-R001..R003, INV-SR-01..03) is
verified now; the live fixtures are recorded as blocked, never reported
as passing.

## NFR overrides

None. Inherits the repository safety model (no writes, no superuser, no
hidden telemetry, no external network calls).
