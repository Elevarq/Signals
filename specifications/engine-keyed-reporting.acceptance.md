# Acceptance Tests: engine-keyed-reporting

## Feature

`specifications/engine-keyed-reporting.md` — the export and the SingleStore
session are engine-keyed through a single `catalogFor(engine)` seam; no foreign
engine's catalog leaks into an export; PostgreSQL output is byte-identical.

## Test Cases

### TC-MKR-01: SingleStore export — no PostgreSQL collector in collector_status.json (normal)

**Rule:** MKR-R001, INV-MKR-01

**Given:** a store holding one SingleStore snapshot (`Engine == singlestore`)
with query_runs for a subset of `singlestore.Catalog()` collector ids, under a
target.

**When:** the default-scope export (TargetID set → `latest-per-collector`) is
built and `collector_status.json` is read back.

**Then:** every collector id present is a SingleStore collector id; no
PostgreSQL collector id appears, including as a `never_run` entry.

**Expected Result:** Pass when collector_status.json is SingleStore-only.

---

### TC-MKR-02: SingleStore export — query_catalog.json excludes PostgreSQL rows (normal)

**Rule:** MKR-R002, INV-MKR-01

**Given:** a mixed store whose `query_catalog` table holds both a PostgreSQL
collector row and a SingleStore collector row, and a SingleStore snapshot/target.

**When:** the SingleStore export is built and `query_catalog.json` is read back.

**Then:** only the SingleStore collector row is present; the PostgreSQL row is
excluded.

**Expected Result:** Pass when query_catalog.json is SingleStore-only.

---

### TC-MKR-03: PostgreSQL export — collector_status.json never_run set unchanged (invariant)

**Rule:** MKR-R001, INV-MKR-02

**Given:** a PostgreSQL snapshot/target with query_runs for a subset of
`pgqueries.All()`.

**When:** the default-scope export is built.

**Then:** every `pgqueries.All()` collector id appears (those with runs with
their outcome, the rest as `never_run`); the id set equals the pre-change set.

**Expected Result:** Pass when no PostgreSQL collector is dropped by the seam.

---

### TC-MKR-04: PostgreSQL export — query_catalog.json retains all stored rows (invariant)

**Rule:** MKR-R002, INV-MKR-02

**Given:** a PostgreSQL store whose `query_catalog` holds `pgqueries.All()`.

**When:** the PostgreSQL export's `query_catalog.json` is read back.

**Then:** every stored PostgreSQL row is present (the engine filter is a no-op).

**Expected Result:** Pass when the PostgreSQL catalog is byte-identical.

---

### TC-MKR-05: catalogFor is total — unknown engine yields empty catalog (boundary)

**Rule:** INV-MKR-03

**Given:** engine strings `postgresql`, `""`, `singlestore`, and `mysql`.

**When:** `catalogFor` is called for each.

**Then:** `postgresql` and `""` return the `pgqueries` catalog; `singlestore`
returns the `singlestore.Catalog()` ids; `mysql` (no backend) returns an empty
slice — never a PostgreSQL fallback.

**Expected Result:** Pass when the seam never falls back to PostgreSQL.

---

### TC-MKR-06: cycleStatus — zero-success attempted cycle is failed (failure)

**Rule:** MKR-R005

**Given:** cycle outcome counts.

**When:** `cycleStatus(nil, success, failed, budget)` is evaluated for:
all-failed (success=0, failed>0), all-skipped-budget (success=0, budget>0),
mixed (success>0, failed>0), clean (success>0, failed=0), nothing-due
(success=0, failed=0, budget=0), and err!=nil.

**Then:** all-failed → `failed`; all-skipped-budget → `failed`; mixed →
`partial`; clean → `success`; nothing-due → `success`; err → `failed`.

**Expected Result:** Pass when zero-success-with-attempts is failed and
nothing-due stays success.

---

### TC-MKR-07: SingleStore mapping — measured duration recorded (normal)

**Rule:** MKR-R006

**Given:** SingleStore collection results carrying a measured `DurationMS` per
collector.

**When:** `mapSingleStoreCollection` builds the query_runs.

**Then:** each run's `DurationMS` equals the result's measured value (not a
hardcoded 0).

**Expected Result:** Pass when the measured duration propagates to the run.

---

### TC-MKR-08: SingleStore Collect — duration measured via injected clock (boundary)

**Rule:** MKR-R006

**Given:** a `singlestore.Collect` run with an injected clock that advances a
fixed delta across each collector's statement.

**When:** `Collect` runs the catalog.

**Then:** each successful `Result.DurationMS` equals the injected delta (proving
a real measurement, deterministically).

**Expected Result:** Pass when the measured value is the injected delta.

---

### TC-MKR-09: SingleStore mapping — elevated permission-denied is skipped, not failed (failure)

**Rule:** MKR-R007

**Given:** a result for an elevated collector (`ElevatedPrivilege != ""`) whose
`Err` is a permission-denied error, and a result for a non-elevated collector
with the same error.

**When:** `mapSingleStoreCollection` maps them.

**Then:** the elevated collector's run is `skipped` with reason
`permission_denied`; the non-elevated collector's run is `failed`.

**Expected Result:** Pass when only the elevated collector degrades to skipped.

---

### TC-MKR-10: SingleStore outcome counts — success/failed/skipped, not len(results) (normal)

**Rule:** MKR-R004

**Given:** a set of mapped SingleStore runs with mixed outcomes.

**When:** the per-outcome counts used by the "snapshot collected" log are
computed.

**Then:** `collectors_success`, `collectors_failed`, `collectors_skipped` match
the actual outcome tallies and sum to the run count (never conflated into one
`collectors` total).

**Expected Result:** Pass when counts are per-outcome.

---

### TC-MKR-11: SingleStore failure log carries collector identity (normal)

**Rule:** MKR-R003

**Given:** mapped SingleStore runs including ≥1 failed collector.

**When:** the session's per-collector failure log attributes are built.

**Then:** each failed collector contributes a log record naming its collector id
and its reason/error.

**Expected Result:** Pass when every failed collector is logged by identity.

---

### TC-MKR-12: SingleStore export — metadata.json carries engine_version; PostgreSQL does not (boundary)

**Rule:** MKR-R008, INV-MKR-02

**Given:** a SingleStore snapshot carrying a server version, and a PostgreSQL
snapshot.

**When:** each export's `metadata.json` is read back.

**Then:** the SingleStore `metadata.json` has `engine_version` equal to the
snapshot's version; the PostgreSQL `metadata.json` has no `engine_version` key.

**Expected Result:** Pass when the engine-neutral version appears only for
non-PostgreSQL and PostgreSQL stays byte-identical.
