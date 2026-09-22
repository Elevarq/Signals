# pg_statistic_ext_data_v1 — Collector Specification

Spec version: 1.1
Issue: [Elevarq/Signals#171](https://github.com/Elevarq/Signals/issues/171),
[Elevarq/Signals#433](https://github.com/Elevarq/Signals/issues/433)
(binary-safe `kind_data_binary` / `kind_data_encoding` columns)
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

`pg_statistic_ext_data` has **PUBLIC SELECT revoked** (the same
posture as `pg_statistic`). A least-privilege monitoring role
(`pg_monitor` / `pg_read_all_stats`) cannot read it: the query
fails with SQLSTATE 42501 and the `LEFT JOIN` does NOT rescue it —
the collector is recorded `status=skipped,
reason=privilege_owner_only` (#200; see
../owner_only_privilege_degradation.md). Under a role explicitly
granted `SELECT` on the catalog (no superuser required) the
`LEFT JOIN`
preserves the parent `pg_statistic_ext` row with NULL data columns
for any object whose statistics have not been computed yet,
yielding an `available=false` row rather than dropping it.

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
| kind_data | text | The human-readable text form of the statistics value (`::text`), retained for inspection. This is **not** a lossless replay format: `pg_ndistinct` and `pg_dependencies` reject text input and `pg_ndistinct_out` is not a complete serialization (#433). NULL when the object has no computed data for this kind yet (privileged read path). |
| kind_data_binary | text | Lossless, replay-safe encoding of the stored value: the **hex** encoding of PostgreSQL's binary *send* output for the value's type — `pg_ndistinct_send` for `d`, `pg_dependencies_send` for `f`, `array_send` over `pg_statistic[]` for `e` (`pg_mcv_list_send` for `m` in the MCV sibling). The Analyzer replay path reconstructs the varlena via the matching *recv* function (the send/recv pair is PostgreSQL's binary-I/O inverse, as used by `COPY … WITH BINARY`). Hex (not base64) so the payload carries no embedded newlines. NULL exactly when `kind_data` is NULL (the strict send functions map a NULL input to a NULL output). |
| kind_data_encoding | text | Codec+version tag telling a consumer how to decode `kind_data_binary`, of the form `v1:<send_fn>:hex` (`v1:pg_ndistinct_send:hex`, `v1:pg_dependencies_send:hex`, `v1:array_send:hex`; `v1:pg_mcv_list_send:hex` in the MCV sibling). A consumer MUST check this token before decoding so the binary form is never confused with the human-readable `kind_data`. The send wire format is PostgreSQL-major-specific; the snapshot's captured server version governs which major a value can be replayed into. NULL exactly when `kind_data_binary` is NULL. |
| available | bool | TRUE when `kind_data IS NOT NULL`. FALSE = the statistics object exists per the catalog but has no computed data for this kind (e.g. not yet `ANALYZE`d); only observable under a role explicitly granted `SELECT` on `pg_statistic_ext_data` (no superuser required). A role without that access does not produce these rows — the collector is skipped (#200). |

## MCV-kind sibling (`pg_statistic_ext_data_mcv_v1`)

Same identity columns; one row per `(statistics_object,
kind='m')` only. Same `kind_data` / `kind_data_binary` /
`kind_data_encoding` / `available` columns, where the binary form
is `encode(pg_mcv_list_send(stxdmcv), 'hex')` and the encoding tag
is `v1:pg_mcv_list_send:hex`. Behind `HighSensitivity=true`
(daemon-wide HS floor). Disabled by default; the operator must
explicitly enable `HighSensitivityEnabled=true` to ship the MCV
blob.

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
- **INV-03** — Per-object availability row pattern (privileged
  read path): under a role explicitly granted `SELECT` on
  `pg_statistic_ext_data`, if the parent `pg_statistic_ext`
  row is visible but no computed `_data` row exists for an object,
  the collector emits the identity columns + `kind_data=NULL` +
  `available=false` for each declared kind (per `stxkind`) rather
  than dropping the object. A role WITHOUT read access on the
  catalog does not reach this path: the query fails with 42501 and
  the collector is recorded skipped (#200), not a per-object
  availability row.
- **INV-04** — Default redaction posture: this collector
  (`pg_statistic_ext_data_v1`) NEVER emits the `m` kind. The
  MCV blob is only reachable via the sibling
  `pg_statistic_ext_data_mcv_v1` collector behind the HS gate.
- **INV-05** — Lossless binary form (#433): for every emitted
  `(object, kind)` row whose value is computed
  (`available = true`), `kind_data_binary` is the hex encoding of
  the value type's binary *send* output and `kind_data_encoding`
  is the matching `v1:<send_fn>:hex` token; the pair round-trips
  through the type's *recv* function. The `kind_data` text column
  is retained unchanged as the inspection form and is NOT the
  replay source. `kind_data_binary` and `kind_data_encoding` are
  both NULL exactly when `kind_data` is NULL — the three
  value-bearing columns share one availability predicate, so a row
  never carries a binary form without a text form or vice versa.
- **INV-06** — Encoding self-describes the codec, not the value:
  `kind_data_encoding` is a fixed per-kind constant
  (`v1:pg_ndistinct_send:hex` for `d`, `v1:pg_dependencies_send:hex`
  for `f`, `v1:array_send:hex` for `e`, `v1:pg_mcv_list_send:hex`
  for `m`); it does not vary with the value's contents. The binary
  wire format is PostgreSQL-major-specific, so replay is only valid
  into a server of the same major, resolved from the snapshot's
  captured server version — not from this field.

## Failure conditions

- **FC-01** — Permission denied on `pg_statistic_ext` (the
  catalog itself, NOT the `_data` table) → collector error path.
  With `pg_monitor` membership this should not occur.
- **FC-02** — Empty rowset (no objects defined OR all objects
  use only kinds outside this collector's set) → success with
  zero rows.
- **FC-03** — Role lacks privilege to read `pg_statistic_ext_data`
  (e.g. a `pg_monitor` role): PUBLIC SELECT is revoked on the
  catalog, so the query fails with SQLSTATE 42501 — the `LEFT JOIN`
  does NOT degrade to `available=false` rows. This is an expected
  privilege boundary: the collector is recorded `status=skipped,
  reason=privilege_owner_only` (NOT failed) and the cycle is not
  marked partial (#200; see
  ../owner_only_privilege_degradation.md). Only a role explicitly
  granted `SELECT` on the catalog (no superuser required) reads the
  blobs and reports per-object presence via the `available` column
  (INV-03).

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
column values. The `kind_data_binary` form carries exactly the
same information as the existing `kind_data` text form (it is the
same stored value under a different, lossless transport), so
adding it does NOT change the sensitivity classification or the
privilege / HS posture of either collector — the `m` blob remains
gated to the HS sibling, `d` / `f` / `e` remain medium-low.

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

`pg_monitor` membership is NOT sufficient: `pg_statistic_ext_data`
has PUBLIC SELECT revoked, so a `pg_monitor` / `pg_read_all_stats`
role gets SQLSTATE 42501 and the collector is recorded
`status=skipped, reason=privilege_owner_only` (#200). Reading the
blobs requires an explicit `GRANT SELECT ON pg_statistic_ext_data`
to the monitoring role — no superuser required. Operators running a
least-privilege role need take no action — the skip is expected and
does not mark the cycle partial.

## Acceptance tests

- **AT-01** — Catalog-drift test (`TestCatalogDriftAcrossPGMajors`
  or equivalent) is green on PG 14..18. The collector's column
  set is identical across majors.
- **AT-02** — Privilege-degraded path: a least-privilege role
  (`pg_monitor`, no `SELECT` on `pg_statistic_ext_data`) hits
  SQLSTATE 42501; the collector is recorded `status=skipped,
  reason=privilege_owner_only` and the cycle is not partial
  (owner-only degrade, #200). Covered by
  ../owner_only_privilege_degradation.acceptance.md
  (TC-OOPD-01..08).
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
- **AT-06** — Binary-safe projection (#433): both collectors
  project `kind_data_binary` and `kind_data_encoding` alongside the
  existing `kind_data` / `available` columns. For each supported
  kind the binary form is `encode(<send_fn>(<col>), 'hex')` and the
  encoding token is the fixed `v1:<send_fn>:hex` constant; on a
  computed value the pair is non-NULL and on an uncomputed value
  (`available=false`) both are NULL. Verified structurally by the
  registered-SQL projection tests and, on the live privileged read
  path, by the output-contract harness.

## Downstream use

- Once these values ship in the snapshot, downstream analysis can
  account for multivariate / extended statistics when reasoning about
  planner cost estimates.
- Analyzer replay (Elevarq/Analyzer#3045) restores the stored
  extended-statistics values into a replica catalog by decoding
  `kind_data_binary` (`decode(kind_data_binary, 'hex')`) and feeding
  the bytes to the type's *recv* function, into a server matching the
  snapshot's PostgreSQL major. The `kind_data` text column is for
  human inspection only and is not the replay source — the `d` and
  `f` kinds cannot be reconstructed from text at all.
