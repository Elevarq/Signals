# pg_statistic_ext_data_v1 — Collector Specification

Spec version: 1.0
Issue: [Elevarq/Signals#171](https://github.com/Elevarq/Signals/issues/171)
Sibling (metadata): [pg_statistic_ext_v1](pg_statistic_ext_v1.md)

## Purpose

Emit the **sampled** rows from `pg_statistic_ext_data` — the
byte-encoded statistics values themselves (functional
dependencies, multivariate ndistinct, multivariate MCV, expression
stats). These are the values the planner uses to fix correlated-
column selectivity, joint cardinality for `GROUP BY`, etc.

The metadata sibling `pg_statistic_ext_v1` (Signals#131) emits
"which CREATE STATISTICS objects exist". This collector emits
"what those objects contain" — the statistics values themselves, used
in downstream planner-cost analysis.

## Relationship to other stats collectors

| Collector | Source | Scope | Visibility |
|---|---|---|---|
| `pg_stats_v1` | `pg_stats` view | per-column summaries | pg_monitor |
| `pg_stats_extended_v1` | `pg_stats` view (HS) | per-column samples (MCV / hist) | pg_monitor + HS |
| `pg_statistic_ext_v1` | `pg_statistic_ext` catalog | per-object catalog metadata | pg_monitor |
| `pg_statistic_ext_data_v1` | `pg_statistic_ext_data` | per-(object,kind) blobs for `d`/`f`/`e` | owner-only post-PG12 |
| `pg_statistic_ext_data_mcv_v1` | `pg_statistic_ext_data` | per-(object,kind=`m`) blob | owner-only + HighSensitivity |

The MCV blob is split out into its own collector because the
`m`-kind data is the only multivariate-stats kind that carries
**actual sampled column values** — and may therefore contain PII.
The other three kinds (`d` = ndistinct, `f` = functional
dependencies, `e` = expression) encode statistical models, not
sampled values.

## Catalog source

`pg_statistic_ext_data` LEFT-JOINed with `pg_statistic_ext`
(catalog identity) and `pg_class` / `pg_namespace` (target-
relation naming).

`pg_statistic_ext_data` is **owner-only post-PG12** — non-owner
roles see no row even if the parent `pg_statistic_ext` row is
visible. The LEFT JOIN preserves the catalog row and emits NULL
data columns for objects the role can't read. This is the
"privilege-degraded path" the issue calls out (one
per-object availability row, snapshot does not fail).

## Output columns

One row per `(statistics_object, kind)` where kind is one of
`d` / `f` / `e`. Kind `m` is emitted by the sibling collector
`pg_statistic_ext_data_mcv_v1` and not by this one.

| Column | Type | Description |
|---|---|---|
| stat_schema | text | Schema of the statistics object. |
| stat_name | text | `stxname`. |
| table_schema | text | Schema of the target relation. |
| table_name | text | Target relation name. |
| kind | text | One of `d` / `f` / `e`. |
| kind_data | text | The byte-encoded statistics value, cast to text (the planner-consumed form). NULL when the role lacks read access on `pg_statistic_ext_data`. |
| available | bool | TRUE when `kind_data IS NOT NULL`. FALSE = privilege-degraded row (the statistics object exists per the catalog, but its sampled data is owner-only and the collector's role is not the owner). |

## MCV-kind sibling (`pg_statistic_ext_data_mcv_v1`)

Same identity columns; one row per `(statistics_object,
kind='m')` only. Same `kind_data` / `available` columns. Behind
`HighSensitivity=true` (daemon-wide HS floor). Disabled by
default; the operator must explicitly enable
`HighSensitivityEnabled=true` to ship the MCV blob.

The MCV blob carries the actual sampled value tuples from the
target table's covered columns; it MAY contain PII. The HS gate
matches `pg_stats_extended_v1`'s posture for the per-column
MCV / histogram blobs.

## Scope filter

- Target relation schema NOT IN
  (`pg_catalog`, `information_schema`, `pg_toast`)
  AND NOT LIKE `pg_temp_%` / `pg_toast_temp_%`.

## Invariants

- **INV-01** — Deterministic ordering:
  `ORDER BY table_schema, table_name, stat_name, kind`.
- **INV-02** — Read-only query (no writes, no temp tables).
- **INV-03** — Per-object availability row pattern: if the
  parent `pg_statistic_ext` row is visible but the
  `pg_statistic_ext_data` row is not, the collector emits the
  identity columns + `kind_data=NULL` + `available=false` for
  each kind the object declares (per `stxkind`). The snapshot
  MUST NOT fail because some objects are owner-restricted.
- **INV-04** — Default redaction posture: this collector
  (`pg_statistic_ext_data_v1`) NEVER emits the `m` kind. The
  MCV blob is only reachable via the sibling
  `pg_statistic_ext_data_mcv_v1` collector behind the HS gate.

## Failure conditions

- **FC-01** — Permission denied on `pg_statistic_ext` (the
  catalog itself, NOT the `_data` table) → collector error path.
  With `pg_monitor` membership this should not occur.
- **FC-02** — Empty rowset (no objects defined OR all objects
  use only kinds outside this collector's set) → success with
  zero rows.
- **FC-03** — One or more objects owner-restricted on the
  `_data` table → per-object availability rows
  (INV-03), snapshot succeeds.

## Configuration

| Field | `pg_statistic_ext_data_v1` | `pg_statistic_ext_data_mcv_v1` |
|---|---|---|
| Category | schema | schema |
| Cadence | 24h (CadenceDaily) | 24h (CadenceDaily) |
| Retention | RetentionMedium | RetentionShort |
| Min PG version | 14 (matches `SupportedMajors`; PG ≥ 14 carries `stxdexpr` so all three kinds emit uniformly) | 14 (matches `SupportedMajors`) |
| RequiresExtension | none | none |
| Semantics | snapshot | snapshot |
| HighSensitivity | false | **true** |
| Enabled by default | yes | no (HS floor must be on) |

## Sensitivity

`pg_statistic_ext_data_v1` (no MCV): **medium-low**. The byte-
encoded `d` / `f` / `e` blobs are statistical models — they
encode correlation coefficients, functional dependency degrees,
and expression-stats summary stats. They do NOT carry sampled
column values.

`pg_statistic_ext_data_mcv_v1`: **high**. The `m` blob carries
actual most-common-value tuples for the columns the statistics
object covers. Treated identically to `pg_stats_extended_v1`'s
MCV/histogram blob: classified `HighSensitivity = true` on the
**skip-path** (no `SensitiveColumns` declared). Runs by default
(R075 v2: collect-everything default). When an operator opts out via
`signals.high_sensitivity_collectors_enabled: false`, the collector
is dropped from the eligible set and recorded `status=skipped,
reason=config_disabled` rather than having columns redacted (nothing
useful remains after redacting the blob).

`pg_monitor` membership is NOT sufficient. The collecting role
must either own the underlying tables (so `pg_statistic_ext_data`
returns rows for them) or be a superuser. Operators who want
broad coverage and are not running as superuser should grant
ownership of the relevant tables to the Elevarq Signals role, OR
accept the privilege-degraded path (per-object availability rows
with `available=false`).

## Acceptance tests

- **AT-01** — Catalog-drift test (`TestCatalogDriftAcrossPGMajors`
  or equivalent) is green on PG 14..18. The collector's column
  set is identical across majors.
- **AT-02** — Privilege-degraded path test:
  fixture creates a stats object owned by `role_a`; the
  collector runs as `role_b` (no ownership, has `pg_monitor`);
  the snapshot contains rows with `available=false` and
  `kind_data IS NULL` for every kind the object declares.
  Snapshot does NOT error.
- **AT-03** — MCV-redaction default: with
  `HighSensitivityEnabled=false`,
  `pg_statistic_ext_data_mcv_v1` is filtered out of the
  registry's per-target eligibility set (the HS gate). Sibling
  collector `pg_statistic_ext_data_v1` still runs and produces
  zero `m`-kind rows by INV-04.
- **AT-04** — MCV opt-in: with `HighSensitivityEnabled=true`,
  the `_mcv` sibling emits rows where `kind='m'` and
  `kind_data` is non-NULL for objects the role owns.
- **AT-05** — `bash scripts/preflight.sh all` green
  (build / vet / test / security gates).

## Downstream use

- Once these values ship in the snapshot, downstream analysis can
  account for multivariate / extended statistics when reasoning about
  planner cost estimates.
