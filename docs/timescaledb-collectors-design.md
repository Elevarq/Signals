# Design note — TimescaleDB / Tiger Data collector family

Issue: [#73](https://github.com/Elevarq/Arq-Signals/issues/73)
Status: IMPLEMENTED (`internal/pgqueries/catalog_timescaledb.go`;
spec `specifications/collectors/timescaledb_family_v1.md` is ACTIVE)
Spec: `specifications/collectors/timescaledb_family_v1.md` (R114),
`features/arq-signals/specification.md` § TimescaleDB collector family
(R114) and § Extension-version gating (R115)

This note records the research findings and the design decisions for
adding TimescaleDB detection and metadata collection to Elevarq Signals.
All version facts below were verified against the TimescaleDB source
(`sql/views.sql`, `sql/pre_install/tables.sql`, `src/guc.c` at release
tags), the GitHub changelog, and the Tiger Data documentation
(docs.tigerdata.com) as of 2026-06-10. TimescaleDB is a Tiger Data
product; docs.timescale.com redirects there.

## 1. Supported version combinations

Latest TimescaleDB release: **2.27.2** (2026-06-02).

| TimescaleDB | PostgreSQL majors | Elevarq Signals overlap (PG 14–18) |
|---|---|---|
| 2.23 – 2.27 | 15–18 | 15–18 |
| 2.20 – 2.22 | 15–17 | 15–17 |
| 2.17 – 2.19 | 14–17 | 14–17 (2.19 is the last line with PG 14) |
| 2.14 – 2.16 | 13–16 (varies) | 14–16 |
| ≤ 2.13 | ≤ 16 | legacy; multi-node era |

PG 15 support is announced to end with the TimescaleDB release after
2.27 (June 2026). Tiger Data publishes no per-version EOL dates;
support is expressed through the PG window.

**Elevarq Signals support tiers (proposed):**

- **Supported (tested in CI):** TimescaleDB 2.17 → 2.27 on PG 14–18,
  per the intersection above.
- **Best-effort:** 2.14 → 2.16. All views/functions the family uses
  exist from 2.14 onward; we do not test these lines.
- **Detection-only:** < 2.14. The detection collector
  (`timescaledb_extension_v1`) still runs (it reads only
  `pg_extension` / `pg_settings` / `to_regclass` probes); every other
  family collector is gated off with `reason=version_unsupported`
  (R115). Rationale: 2.13 and earlier are EOL, multi-node-era, and
  predate `hypertable_compression_settings` and the approximate-size
  functions.

**Editions.** Community (TSL, `timescaledb.license = 'timescale'`,
the default) vs Apache-2 (`'apache'`, the `-oss` Docker tags).
Compression/columnstore, continuous aggregates, retention policies,
and the job framework are TSL-only *features*, but the
`timescaledb_information` views and stats functions exist in both
editions — on Apache builds they simply return empty/zero results
because the features cannot be enabled. The detection collector
records the license GUC so the Analyzer can distinguish "no
compression configured" from "edition cannot compress".

## 2. Source selection: documented views only, functions where cheap

**Ground rules derived from research:**

1. `timescaledb_information.*` is the documented, stable interface
   and is `GRANT SELECT TO PUBLIC` — every view is fully visible to
   an unprivileged role with **no row filtering**, except
   `job_errors` / `job_history` (security-barrier views filtered to
   job-owner / database-owner role membership).
2. `_timescaledb_catalog.*` / `_timescaledb_internal.*` are
   PUBLIC-readable but **internal and unstable** (observed churn:
   `bgw_job` moved schemas in 2.25.0; `chunk_constraint` removed on
   main; `job_errors` table replaced in 2.15.0). The family does
   **not** query internal schemas in v1. If a future field is only
   available there, it must be marked best-effort in its spec.
3. `timescaledb_experimental.policies` is deprecated (2.24.0 release
   notes announce removal) — **not used**. Policy data comes from
   `timescaledb_information.jobs` (`proc_name` +
   `config` JSONB carry `drop_after`, `compress_after`, refresh
   windows, etc.).
4. Exact size functions (`hypertable_size`,
   `hypertable_detailed_size`, `chunks_detailed_size`) take an
   `AccessShareLock` per chunk per call — O(chunks) locking that can
   block behind compression/retention jobs and pressure
   `max_locks_per_transaction`. **Not used.** Instead:
   `hypertable_approximate_detailed_size()` (introduced 2.14.0
   precisely for monitoring; smgr-cache priced) and
   `hypertable_compression_stats()` (pure catalog read of recorded
   compression sizes).
5. Compression-settings views: the pre-rename names
   (`hypertable_compression_settings`, `chunk_compression_settings`,
   introduced 2.14.0) and the 2.18.0 columnstore aliases
   (`*_columnstore_settings`) are **identical**; the old names still
   exist in 2.27.2 with removal announced but unscheduled. The family
   queries the **old names** (valid across the whole 2.14→2.27
   window) and records the presence of the aliases as a capability
   flag, so the eventual cut-over is a one-line SQL swap driven by
   detection, not a guess.
6. Column drift inside views is real
   (`hypertables.primary_dimension{,_type}` added 2.20.0;
   `continuous_aggregates.finalized` removed 2.25.0; settings views
   gained `index` in 2.22.0; `job_stats` switched INNER→LEFT JOIN in
   2.23.0). The family uses **dynamic column capture (`SELECT *`,
   R037 precedent: `pg_stat_statements_v1`)** so rows carry whatever
   the connected version exposes, with NDJSON preserving all columns.
   Consumers must treat per-version columns as optional.

## 3. Collector family (category `timescaledb`)

All collectors: `RequiresExtension: "timescaledb"` (clean skip on
plain PostgreSQL via the existing EA-R001 channel,
`reason=extension_missing`), `RequiresExtensionMinVersion: "2.14"`
(R115) except the detection collector, `MinPGVersion: 14`,
`ResultRowset`, read-only SELECT/WITH passing the registration linter.

| ID | Source | Cadence | Issue category |
|---|---|---|---|
| `timescaledb_extension_v1` | `pg_extension` + `pg_settings` (license, telemetry) + `to_regclass`/`to_regnamespace` capability probes | 6h | A, I, capabilities |
| `timescaledb_hypertables_v1` | `timescaledb_information.hypertables` (`SELECT *`) | 6h | B |
| `timescaledb_dimensions_v1` | `timescaledb_information.dimensions` | 24h | B |
| `timescaledb_chunks_v1` | `timescaledb_information.chunks`, newest-created-first, **LIMIT 5000** | 6h | C |
| `timescaledb_chunk_summary_v1` | aggregate over the chunks view: per hypertable — chunk count, compressed count, min/max range, min/max creation time | 6h | C, H |
| `timescaledb_hypertable_sizes_v1` | hypertables × LATERAL `hypertable_approximate_detailed_size()` | 1h | H |
| `timescaledb_compression_settings_v1` | `timescaledb_information.hypertable_compression_settings` | 24h | D |
| `timescaledb_compression_stats_v1` | hypertables × LATERAL `hypertable_compression_stats()` | 1h | D, H |
| `timescaledb_continuous_aggregates_v1` | `timescaledb_information.continuous_aggregates` | 6h | E |
| `timescaledb_jobs_v1` | `timescaledb_information.jobs` | 1h | F, G (policies incl. retention/compression/refresh) |
| `timescaledb_job_stats_v1` | `timescaledb_information.job_stats` | 15m | F, G |
| `timescaledb_job_errors_v1` | `timescaledb_information.job_errors` | 1h | G, J |

**Not collected, and why:**

- `timescaledb.*` GUCs (issue category I): already collected by the
  existing `pg_settings_v1` — all `timescaledb.*` GUCs appear in
  `pg_settings` for any role except the four
  `GUC_SUPERUSER_ONLY` feature flags (`enable_hypertable_create`
  etc.), which `pg_monitor` (via `pg_read_all_settings`, our
  recommended baseline role) can also read. No new collector.
- `timescaledb_information.job_history` (2.15+): redundant with
  `jobs` + `job_stats` + `job_errors`, owner-filtered for
  least-privilege roles, and pruned to 1 month by a built-in job.
- `timescaledb_experimental.policies`: deprecated upstream (see § 2).
- `data_nodes` / distributed hypertables: multi-node was removed in
  2.14.0; the supported window has no multi-node. `node_name` output
  columns from stats functions are permanent NULLs and are simply
  carried through by dynamic capture.
- Exact per-chunk sizes (`chunks_detailed_size`): O(chunks) locking
  cost (see § 2 rule 4). Chunk-level `is_compressed` plus
  hypertable-level before/after compression bytes cover the Analyzer
  rule family (TS-R001..R009) without it. Revisit only with a
  concrete Analyzer need.

**Sensitivity.** Two collectors carry application-authored or
potentially data-bearing text and follow the R075 redact path:

- `timescaledb_continuous_aggregates_v1` →
  `SensitiveColumns: ["view_definition"]` (cagg defining SELECT —
  same class as `pg_views_definitions_v1`).
- `timescaledb_job_errors_v1` → `SensitiveColumns: ["err_message"]`
  (error text can embed data values).

Everything else is structural metadata (names, intervals, counters,
sizes) — low sensitivity, consistent with the
"no raw query text" privacy posture.

## 4. Detection and capability flags

`timescaledb_extension_v1` emits exactly one row:

| Column | Source | Meaning |
|---|---|---|
| `extversion` | `pg_extension` | version provenance for the whole family snapshot |
| `extension_schema` | `extnamespace::regnamespace` | where the API functions live (default `public`; the extension is non-relocatable) |
| `license` | `current_setting('timescaledb.license', true)` | `timescale` (TSL) / `apache` |
| `telemetry_level` | `current_setting('timescaledb.telemetry_level', true)` | `off` / `basic` |
| `has_information_views` | `to_regclass` probe | sanity flag |
| `has_job_history` | `to_regclass('timescaledb_information.job_history')` | true ⇒ 2.15+ |
| `has_columnstore_aliases` | `to_regclass('…hypertable_columnstore_settings')` | true ⇒ 2.18+ (rename landed) |
| `has_experimental_policies` | `to_regclass('timescaledb_experimental.policies')` | upstream-removal tripwire |
| `has_functions_schema` | `to_regnamespace('_timescaledb_functions')` | true ⇒ 2.11+ |
| `bgw_job_in_catalog` | `to_regclass('_timescaledb_catalog.bgw_job')` | true ⇒ 2.25+ |

These are the **capability flags** the Analyzer keys on, and they are
feature-detected (existence probes), not version-table lookups —
`extversion` is provenance, not a dispatch key. PostgreSQL version is
already in snapshot `metadata.json` (`pg_version`) and
`collector_status.json` carries per-collector run status, so each
family result is attributable to (PG version, TS version,
capabilities) collected in the same cycle.

## 5. Gating and fallback behavior

| Situation | Behavior | Channel |
|---|---|---|
| Plain PostgreSQL, no TimescaleDB | whole family ineligible | `skipped` / `extension_missing` (existing EA-R001; discovery already enumerates `pg_extension` per cycle) |
| TimescaleDB < 2.14 | all except `timescaledb_extension_v1` ineligible | `skipped` / `version_unsupported` via new extension-version gate (R115) |
| View/function missing despite gates (e.g. future upstream removal, relocated extension schema breaking unqualified function calls) | query fails inside its savepoint (R038); snapshot continues | `failed` / `object_missing` (new error classification for SQLSTATE 42P01/42883, R115) |
| Permission denied (not expected for the family — see § 6) | query fails inside its savepoint | `failed` / `permission_denied` (existing) |
| `job_errors` visible-but-empty for a least-privilege role | **normal partial-by-design state**: zero rows, `status=success` | documented in the permissions doc; Analyzer must not read "no rows" as "no failures" — cross-check `job_stats.total_failures` (visible for all jobs) |
| Apache edition | all collectors run; compression/cagg/job rowsets are empty or zero | `license=apache` capability flag disambiguates |
| TimescaleDB ≥ supported window (e.g. future 2.28+) | dynamic capture carries new columns; existence probes flag new surfaces; collectors keep running | best-effort forward compatibility, same posture as PG 19 |

The snapshot never fails because of this family: extension gating
happens before any SQL runs, and every executed query is isolated in
its own savepoint (R038) with structured status (R072).

**R115 — extension-version gating (small framework extension).**
Discovery (`internal/pgqueries/discovery.go`) starts capturing
`extversion` alongside `extname`; `QueryDef` gains an optional
`RequiresExtensionMinVersion` (dotted-numeric compare, applied only
when `RequiresExtension` is set); `GatedIDsByReason` reports failures
of this gate under the existing `version_unsupported` reason so
`collector_status.json`, doctor C5, and the Analyzer completeness
model work unchanged. Defense-in-depth: the run-error classifier
learns SQLSTATE 42P01/42883 → `object_missing` so an unexpectedly
missing relation/function is a structured warning, not an opaque
`execution_error`.

## 6. Least-privilege posture

Verified from extension source (grants in `sql/pre_install/tables.sql`
and view predicates in `sql/views.sql`):

- `timescaledb_information` and `timescaledb_experimental` schemas:
  `GRANT SELECT … TO PUBLIC`. No `pg_monitor` integration exists in
  the extension; none is needed.
- The approximate-size and compression-stats functions perform **no
  ACL check** (they read PUBLIC-readable catalogs and call
  smgr/`pg_total_relation_size`-class primitives) — no table SELECT
  or ownership required.
- The **only** privilege-shaped surface is `job_errors` /
  `job_history`: rows are visible only to members of the job-owner or
  database-owner role. The standard `arq_signals` role (LOGIN +
  `pg_monitor`, per `docs/postgres-role.md`) therefore sees zero
  rows. Operators who want fleet-wide job *error detail* can
  optionally grant the collector role membership in the database
  owner role; `job_stats` failure counters are visible without it.
  (Upstream fixed an information leak in the other direction in
  2.27.x — `job_errors` showed failed jobs to non-owners before the
  fix — so "zero rows" is the *correct* least-privilege behavior on
  current versions.)
- No superuser anywhere; the existing role-safety hard-stop (R018)
  is unchanged.

**Read-only / do-not-call.** The family is SELECT-only and the
registration linter independently blocks every mutating TimescaleDB
API (`add_job`, `alter_job`, `run_job`, `compress_chunk`,
`convert_to_columnstore`, `drop_chunks`, `refresh_continuous_aggregate`,
`timescaledb_pre_restore` — which would stop all background workers —
etc.) via the SELECT/WITH-only rule and the keyword denylist; session
`default_transaction_read_only=on` and per-query READ ONLY
transactions (R013/R017/R021) back-stop it. `get_telemetry_report()`
is read-only but expensive (scans stats for all relations) and is
**not** called.

## 7. Snapshot shape and size bounds

- **Namespace:** query IDs `timescaledb_*_v1`, `Category:
  "timescaledb"`. Results land in the standard
  `query_results.ndjson` keyed by query ID — same contract as every
  other collector (INV-SIGNALS-02/03); no parallel schema tree.
- **Stable identifiers:** hypertables/chunks/caggs are identified by
  schema + name as emitted by the information views (the views do not
  expose OIDs; `format('%I.%I', …)::regclass` is used only as a
  function argument, never as a stored identifier).
- **Bounded size:** two sources have unbounded cardinality. The
  chunks view (a busy fleet can have 10⁵+ chunks):
  `timescaledb_chunks_v1` caps at **5000 rows, newest creation time
  first** (uniform across time- and integer-dimension hypertables);
  `timescaledb_chunk_summary_v1` stays complete (one row per
  hypertable) and carries the true counts, so truncation is always
  detectable (`sum(chunk_count) > rows in timescaledb_chunks_v1`).
  And `job_errors`, which is per-execution (a crash-looping job
  outruns the monthly retention job): `timescaledb_job_errors_v1`
  caps at **1000 rows newest-first**. Everything else is naturally
  bounded (per-hypertable, per-job, per-dimension rows).
- **No raw query text** beyond the two redact-path columns in § 3.

## 8. Integration-test matrix

Image facts verified on Docker Hub (June 2026): database images
remain under the `timescale/` namespace; `-oss` tags are Apache-2
builds; `timescale/timescaledb` (Alpine) carries
`2.27.x-pg15…pg18`; PG 14 tags end at the 2.19.x line;
`timescale/timescaledb-ha` (Ubuntu) carries `pg15…pg18` with
`pgX-tsY` pinning.

| Scenario | Image | Asserts |
|---|---|---|
| Plain PostgreSQL | `postgres:18` (existing harness) | family gated `extension_missing`; snapshot succeeds (acceptance TC-TSDB-01) |
| Empty TimescaleDB | `timescale/timescaledb:2.27.2-pg17` | detection row present; inventory rowsets empty; all `success` (TC-TSDB-02) |
| Hypertable + dimensions | same image + fixtures | TC-TSDB-03/04 |
| Chunks + summary + cap | same | TC-TSDB-05/15 |
| Compression settings/stats | same | TC-TSDB-06 |
| Continuous aggregate (+ refresh policy) | same | TC-TSDB-07 |
| Retention policy | same | TC-TSDB-08 |
| Job stats / job errors least-privilege | same, connecting as the `arq_signals` role | TC-TSDB-09/10 |
| Apache edition | `timescale/timescaledb:2.27.2-pg17-oss` | `license=apache`; graceful empty TSL surfaces (TC-TSDB-11) |
| Oldest supported combo | `timescale/timescaledb:2.19.3-pg14` | pre-2.20 column drift absorbed by dynamic capture (TC-TSDB-03 variant) |
| PG 18 lane (optional) | `timescale/timescaledb-ha:pg18-ts2.27` | smoke |

Harness: build-tag `integration` tests gated on `ARQ_TEST_TSDB_DSN`
(TimescaleDB target) alongside the existing `ARQ_TEST_PG_DSN`
pattern; CI workflow wiring is implementation scope.

## 9. Out of scope (this issue)

- Analyzer rules TS-R001…TS-R009 (follow-up issue after
  implementation, per #73).
- Insight/LoRA work.
- Multi-node/distributed metadata (removed upstream in 2.14).
- `job_history`, `timescaledb_experimental.policies`, internal
  catalog reads, exact per-chunk size functions (rationale in § 3).
- Tiered storage / OSM chunk metadata (cloud-only surface; no
  self-hosted introspection contract).

## 10. Source index

- Compatibility matrix: tigerdata.com/docs/deploy/self-hosted/upgrades/upgrade-pg
- Editions + license GUC: tigerdata.com/docs/get-started/choose-your-path/timescaledb-editions
- Informational views reference: tigerdata.com/docs/api/latest/informational-views/
- Size/stats functions: tigerdata.com/docs/api/latest/hypertable/ and …/compression/, …/hypercore/
- GUC reference: tigerdata.com/docs/api/latest/configuration/gucs
- Extension source ground truth: github.com/timescale/timescaledb —
  `sql/views.sql`, `sql/pre_install/tables.sql`, `sql/size_utils.sql`,
  `src/guc.c`, `CHANGELOG.md` (verified at tags 2.14.x → 2.27.2)
- Job-views privilege model: timescale/timescaledb#5217 (+ PR #5218),
  2.27.x `job_errors` leak fix
- Docker images: hub.docker.com/r/timescale/timescaledb,
  …/timescaledb-ha
