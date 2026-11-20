# Acceptance Tests: PG19 Per-Object stats_reset Projection

## Feature

`specifications/pg19-per-object-stats-reset.md`

## Test Cases

### TC-SR-01: Capability projection resolves on actual PG19

**Rule:** CPJ-R001, SR-R001

**Scenario:** A real PG19 target (above the supported catalog window)
runs the table/index collectors.

**Given:**
- `FilterParams.PGMajorVersion = 19` (actual).
- `FilterParams.CatalogBaselineMajor = 18` (clamped, as the collector
  sets it for an experimental major).

**When:**
- `Filter` resolves the effective SQL for `pg_stat_user_tables_v1` and
  `pg_stat_user_indexes_v1`.

**Then:**
- The resolved SQL selects the real `stats_reset` column from the
  catalog view (not the `NULL::timestamptz` stub).

**Expected Result:** Pass when both resolved queries name `stats_reset`
from the view and contain no `NULL::timestamptz AS stats_reset` stub.

---

### TC-SR-02: Typed-NULL compatibility on PG14-18

**Rule:** SR-R002, FC-01

**Scenario:** A supported-window major runs the collectors.

**Given:**
- `FilterParams.PGMajorVersion` in {14,15,16,17,18}, baseline unset.

**When:**
- `Filter` resolves the effective SQL for both collectors.

**Then:**
- The resolved SQL emits `NULL::timestamptz AS stats_reset` and never
  references a `stats_reset` column from `pg_stat_user_tables` /
  `pg_stat_user_indexes`.

**Expected Result:** Pass when every supported-window major resolves to
the typed-NULL stub for both collectors.

---

### TC-SR-03: Row-shape compatibility — stats_reset is the final column

**Rule:** INV-SR-01, SR-R003

**Scenario:** Column order must be identical across majors with
`stats_reset` always last.

**Given:**
- The resolved SQL for PG18 and for PG19 (via TC-SR-01 / TC-SR-02).

**When:**
- Static inspection of the projected column list.

**Then:**
- The pre-existing columns appear in the same order on both majors.
- `stats_reset` is the last projected column on both majors.

**Expected Result:** Pass when the column prefix matches and
`stats_reset` is terminal.

---

### TC-SR-04: Capability precedence and guards

**Rule:** CPJ-R002, CPJ-R003

**Scenario:** The capability-override registry enforces precedence and
coding-error guards.

**Given:**
- The registered capability projections for PG19.

**When:**
- `HasCapabilityOverride(19, id)` is queried, and a hypothetical
  duplicate / unknown-ID registration is attempted (pure helper, not via
  global init).

**Then:**
- `HasCapabilityOverride(19, "pg_stat_user_tables_v1")` is true and
  `HasCapabilityOverride(18, "pg_stat_user_tables_v1")` is false.
- When both a capability projection (actual major) and a baseline
  override (baseline major) exist for one ID, `Filter` returns the
  capability projection.
- A duplicate `(major, id)` or unknown logical ID registration panics.

**Expected Result:** Pass when precedence and both panics are observed.

---

### TC-SR-05: Live reset identified after counters catch up (COVERED)

**Rule:** SR-R003, INV-SR-01, downstream Analyzer#4315

**Test:** `tests/signals_pg19_stats_reset_live_test.go`
(`TestIntegration_PG19PerObjectStatsResetAfterCatchup`), build tag
`integration`, `SIGNALS_TEST_PG_DSN`-gated, PG19-only. Runs on the
`integration-pg (19)` CI leg.

**Scenario:** On a live PG19 target, reset a relation's per-object
statistics, then generate enough activity that the counters climb back
above their pre-reset values; the reset must still be identifiable from
`stats_reset`, and a no-reset relation's timestamp must survive export.

**Given:**
- A final-GA PG19 stats container in CI with a restricted monitoring
  role.

**When:**
- Two collection cycles straddle a `pg_stat_reset_*` call for one
  relation while another relation is never reset.

**Then:**
- The reset relation's `stats_reset` advances across the two cycles even
  though its counters caught up.
- The no-reset relation's `stats_reset` is stable and survives export.
- A row the role cannot read has no fabricated `stats_reset`.

**Expected Result:** COVERED — the official `postgres:19` image has now
published (GA), so the live fixture is implemented and runs on the
`integration-pg (19)` CI leg. The reset table's `stats_reset` advances
across the two cycles despite the counters catching up; the never-reset
sibling's `stats_reset` stays stable and survives export; the reset is
single-table scoped. The fixture cannot run locally (needs Docker PG19);
live validation is the CI `integration-pg (19)` leg. The amd64 manifest
of the `postgres:19` image may still be propagating, so the first CI run
can transiently fail to pull until amd64 lands.
