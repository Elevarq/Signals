# Engine-Keyed Reporting Seam

## Status

ACTIVE

## Purpose

Make the Signals reporting layer (the export builder and the SingleStore
collection session) **engine-keyed**, so an export and its logs describe the
engine that was actually collected — never a foreign engine's catalog.

Before this spec, the export builder enumerated the **PostgreSQL** collector
catalog (`internal/pgqueries`) unconditionally:

- `applyFreshness` looked collectors up and listed `never_run` entries from
  `pgqueries.ByID` / `pgqueries.All()` regardless of the export's engine; and
- `query_catalog.json` was the whole `query_catalog` table, which in a mixed
  (PostgreSQL + SingleStore) store carries both engines' collectors.

So a **SingleStore** export leaked every PostgreSQL collector into
`collector_status.json` (as `never_run`) and into `query_catalog.json`. This is
the area the original POC defect lived (#534, #578). The fix is a **single
engine-keyed seam** — `catalogFor(engine)` — that every reporting enumeration
goes through, plus correctness fixes to the SingleStore session's per-collector
outcome, duration, failure logging, and the cycle-outcome classification that
both engines share.

PostgreSQL remains the control: for a PostgreSQL export the seam returns exactly
the `pgqueries` catalog, so PostgreSQL output is **byte-identical** to the
pre-change output for the same store state.

This owns the reporting-seam behavioural invariant of the SingleStore rebuild
(epic #524): defects #534, #578, #579, #580, #581, #582, and the testable slice
of #531.

## Scope

- **`catalogFor(engine string) []reportCollector`** — a single seam in
  `internal/export` mapping an engine to its reporting catalog
  (collector id + cadence). `postgresql` (and empty) → the `pgqueries` registry;
  `singlestore` → `singlestore.Catalog()`; any other engine → empty.
- The export's `collector_status.json` freshness/`never_run` enrichment and
  `query_catalog.json` enumeration, both driven by `catalogFor(engineOf(scope))`.
- The SingleStore session's result→run mapping (`mapSingleStoreCollection`):
  measured duration, elevated-privilege skip classification.
- The SingleStore session's "snapshot collected" log + per-collector failure
  logging.
- The shared cycle-outcome classifier `cycleStatus`.
- `metadata.json`'s engine-neutral version field for non-PostgreSQL engines.

Out of scope (deferred, would rewrite valid PostgreSQL archives): renaming the
`pg_version` storage column / the `pg_version` field in `query_runs.ndjson` and
`snapshots.ndjson`. #531 is resolved here only to the extent that is testable
without changing PostgreSQL output — see MKR-R008.

## Interfaces

- **`engineOf(scope *exportScope) string`** — the engine an export describes:
  the scoped snapshot's `Engine`, defaulting empty/legacy to `postgresql`
  (identical rule to the existing `metadata.json` engine field).
- **`reportCollector`** — the engine-neutral reporting view of one collector:
  `ID string`, `Cadence time.Duration` (0 when the engine has no cadence
  concept), `CadenceString string` (the display form, "" when none).
- **`catalogFor(engine string) []reportCollector`** — as above. Pure; no I/O.
- **`singlestore.Result`** gains `DurationMS int` and `ElevatedPrivilege string`
  (both additive; existing fields unchanged).
- **`cycleStatus(err error, success, failed, budgetExhausted int) string`** —
  gains the `success` count (was `cycleStatus(err, failed, budgetExhausted)`).

## Behaviors

- **Given** a SingleStore export (scoped snapshot `Engine == singlestore`),
  **when** the ZIP is built, **then** every collector id in
  `collector_status.json` and every row in `query_catalog.json` is a SingleStore
  collector; no PostgreSQL collector appears, as `never_run` or otherwise.
- **Given** a PostgreSQL export for a given store state, **when** the ZIP is
  built, **then** `collector_status.json` and `query_catalog.json` are identical
  to the pre-change output (every `pgqueries` collector still enumerated).
- **Given** a SingleStore cycle where every collector failed (or every collector
  was skipped), **when** the cycle completes, **then** the cycle outcome is
  `failed`.
- **Given** a SingleStore collector that declares an elevated privilege and
  fails with a permission-denied error, **when** results are mapped, **then**
  its run is `skipped` (reason `permission_denied`), not `failed`.

## Rules

- **MKR-R001** — Export `collector_status.json` freshness and `never_run`
  enrichment enumerate the catalog of the **export's engine** via
  `catalogFor(engineOf(scope))`, never `pgqueries` unconditionally. A
  non-PostgreSQL export never gains a `never_run` entry for a collector of any
  other engine. (#534, #578)
- **MKR-R002** — Export `query_catalog.json` contains only collectors whose id
  is in `catalogFor(engineOf(scope))`; catalog rows for other engines
  (present in a mixed store) are excluded. For a PostgreSQL export every stored
  PostgreSQL catalog row is retained (the filter is a no-op, since the stored
  PostgreSQL catalog is exactly `pgqueries.All()`). (#534)
- **MKR-R003** — The SingleStore session logs each failed collector with its
  collector identity (the collector id), not only an aggregate count. (#581)
- **MKR-R004** — The SingleStore "snapshot collected" log line reports
  `collectors_success` / `collectors_failed` / `collectors_skipped` counts
  derived from the persisted runs, not `len(results)`. (#582)
- **MKR-R005** — A collection cycle that attempted at least one collector and
  recorded **zero** successful collectors is classified `failed`. A cycle that
  attempted no collectors (nothing due) is unaffected and stays `success`. A
  cycle with ≥1 success and ≥1 failed/budget-skipped collector stays `partial`.
  This applies to both engines through the shared `cycleStatus`. (#580)
- **MKR-R006** — Each SingleStore collector run carries a **measured**
  `duration_ms` (wall-clock around the collector's statement), not a hardcoded
  0. (#579)
- **MKR-R007** — A SingleStore collector that declares an `ElevatedPrivilege`
  and fails with a permission-denied error is recorded with status `skipped`
  and reason `permission_denied`, not `failed`. A non-elevated collector's
  permission error remains `failed`.
- **MKR-R008** — For a non-PostgreSQL export, `metadata.json` carries an
  `engine_version` field sourced from the scoped snapshot's collected server
  version. A PostgreSQL export does **not** gain `engine_version` and its
  `pg_version` carrier is unchanged (byte-identical). (#531, testable slice)

## Invariants

- **INV-MKR-01** — No export's `collector_status.json` or `query_catalog.json`
  ever contains a collector that does not belong to the export's engine.
- **INV-MKR-02** — PostgreSQL export output is byte-identical to the pre-change
  output for the same store state (guarded by the M0 PostgreSQL CI matrix plus
  the collector_status / query_catalog golden here).
- **INV-MKR-03** — `catalogFor` is total: an unrecognised engine yields an empty
  catalog (no panic, no PostgreSQL fallback), so a future engine can never
  silently inherit PostgreSQL's catalog.

## Failure conditions

- A permission-denied error on a non-elevated collector → `failed` (MKR-R007
  does not mask genuine faults).
- A SingleStore collector that errors for any non-permission reason → `failed`
  (status/reason preserved for the export and the failure log).
- `catalogFor` on an unknown engine → empty slice (INV-MKR-03); the export still
  writes a well-formed (possibly empty) `query_catalog.json` and omits
  `never_run` enrichment.

## Constraints

- The seam lives in `internal/export`; it may import `internal/pgqueries` and
  `internal/singlestore` (no import cycle — neither imports `export`).
- No change to transport, identity, session selection, or dispatch (those are
  M1/M2 and engine-neutral). The only session edit is the result→run mapping and
  logging of the already-existing SingleStore session.

## NFR overrides

None beyond the project safety baseline (read-only collection, no secrets in
output). `metadata.json`'s `engine_version` carries only the server version
string already stored on the snapshot — no credential or connection secret.

## Traceability

Acceptance cases: `specifications/engine-keyed-reporting.acceptance.md`.
Matrix rows: `features/signals/traceability.md` (MKR-R001..R008, INV-MKR-01..03).
