# Collector Output-Contract Verification — Behavioral Specification

## Status

ACTIVE

Spec version: 1.2
Status: ACTIVE
Issue: [Elevarq/Signals#314](https://github.com/Elevarq/Signals/issues/314)
Extended by: [Elevarq/Signals#316](https://github.com/Elevarq/Signals/issues/316)
(declared-column completeness across ALL collectors) and
[Elevarq/Signals#320](https://github.com/Elevarq/Signals/issues/320)
(type-fidelity audit of the remaining PostgreSQL type classes)

## Purpose

Collector specs (`specifications/collectors/<id>.md`) declare an output
contract — a table of columns with PostgreSQL types. Until #314, no
test executed a collector against a real PostgreSQL and verified that
its declared output landed, correctly typed, in the exported snapshot
ZIP. The `pgqueries` tests are purely static (registration, read-only
linting, ordering); the SQL is never run. That gap is exactly why
`contype` serializing as the integer `102` instead of the string `"f"`
was invisible for the whole internal-`"char"` class (#312): the
`specification → tests → implementation` chain was missing the
`tests → artifact` link.

This spec defines the missing test layer: an integration harness that,
against a real PostgreSQL carrying representative schema, runs the full
collection, exports a snapshot ZIP via the production export path, reads
the ZIP back, and asserts the per-collector output contract. It locks
the #312 class permanently — the assertions go RED on pre-#313 code and
GREEN after.

Scope (#314/#326) began with the **catalog/schema collectors**
(`Category == "schema"`), with emphasis on the internal-`"char"`
collectors PR #313 touched (`pg_constraints_v1`, `pg_class_storage_v1`,
`pg_functions_v1`, `pg_functions_definitions_v1`, `pg_proc_config_v1`)
**and the remaining schema collectors that alias an internal-`"char"`
column** but were not exercised by the #314 fixtures (#326):
`pg_partitions_v1` (`partition_strategy`), `pg_triggers_v1` /
`pg_triggers_definitions_v1` (`tg_enabled`), and
`fdw_foreign_tables_v1` (`relkind`).

**#316 closes the fast-follow: the per-collector declared-column
assertion (OC-R002 / OC-R008) now covers EVERY registered collector,
not only the schema ones.** For each collector that emits rows against
the seeded live PG, its declared output columns — derived from the
authoritative spec `## Output columns` table(s), never a hand-copied
list — are asserted present in the exported payload. Collectors that
legitimately emit no rows in the test environment (needing concurrent
sessions, in-flight operations, replication, an uninstalled extension,
a privilege the `pg_monitor` role lacks, or a rare catalog object) are
recorded in an explicit **zero-row allowlist** with a per-collector
reason; a collector emitting no rows that is NOT allowlisted fails the
harness, so coverage is honest and a newly-added collector cannot slip
through unclassified (OC-R008). Column-dynamic collectors (`SELECT *`,
version-variant, or scalar) are asserted for row presence only, since a
fixed declared-column list cannot apply.

**#320 completes the family the internal-`"char"` bug (#312) belonged
to.** `"char"` (OID 18) was one member of a class: pgx maps every
PostgreSQL type to a Go type, `queryToMaps` stores whatever that mapping
produced, and `encoding/json` then serializes it — so a type whose JSON
form the Analyzer misreads is a silent contract violation, exactly like
`char`. `char` was fixed centrally (#319); #320 **audits and locks the
remaining classes** by exported JSON shape, cross-checked against the
actual Analyzer consumer:

- **numeric / decimal** (`bloat_ratio`, `dead_pct`, `pct_used`,
  `estimate_drift_pct`) — pgx decodes `numeric` to `pgtype.Numeric`,
  which JSON-marshals as a bare **number**, not an object or string; the
  Analyzer reads it as a number (`toFloat64`/`numberField`).
- **jsonb / json** (`by_backend_type`, `by_wait_event_type`,
  `role_attrs`) — pgx decodes to `map[string]any`, which serializes as a
  JSON **object**, not a base64 `[]byte` string; the Analyzer reads a
  `map[string]any`.
- **arrays** (`enum_labels`, `composite_columns`, `attnums`, `kinds`,
  `column_set`) — pgx decodes `text[]`/`int[]` to `[]any`, which
  serializes as a JSON **array**, not a Postgres array literal string
  (`"{a,b}"`); the Analyzer reads a `[]any`.
- **timestamps** (`postmaster_start_time`, `started_at`, `stats_reset`,
  `last_analyze`) — pgx decodes `timestamptz`/`timestamp`/`date` to
  `time.Time`, which serializes as an **RFC3339 string** the Analyzer
  parses with `time.Parse`.
- **oid** (`relid`, `atttypid`, `index_oid`, `datid`, `table_oid`) — pgx
  decodes `oid` to `uint32`, which serializes as a JSON **number** the
  Analyzer reads via `relidToInt64`; `regclass`/`regproc`/`regnamespace`
  cast to text serialize as **strings** (a name), also as expected.
- **bool** (`relhasindex`, `is_unique`, `security_definer`,
  `is_superuser`, `relispartition`) — pgx decodes `boolean` to `bool`,
  which serializes as a JSON **`true`/`false`**, not the `"t"`/`"f"`
  Postgres text form the Analyzer would silently misread.
- **bytea** — no collector emits a `bytea` column; the class is recorded
  as not-exercised (a `[]byte` would serialize as a base64 string, the
  Analyzer's expected form, but there is nothing to assert).

The measured conclusion (verified against a live PG 14–18): **every one
of these classes already serializes in the Analyzer-expected form** —
the `"char"` class was uniquely broken because pgx's default
`QCharCodec` returns an integer, whereas `numeric`/`jsonb`/`bytea`/
array/`timestamp`/`oid`/`bool` all have correct pgx codecs. #320
therefore adds no production SQL change; it is a **regression lock** —
type-fidelity assertions (OC-R009) that go RED if any of these classes
ever regresses to the wrong JSON shape, plus an explicit coverage map
(OC-R010) of which type×collector combinations are exercised versus
not, so no class can silently drift. One documented nuance the
assertions account for: a `numeric` whose value is `NaN` serializes as
the string `"NaN"` (JSON has no NaN), and the redacted FDW option
columns (`fdw_options`, `server_options`, `foreign_table_options`) are
deliberately transformed from `text[]` to a `map[string]string`
**object** by the FDW redaction post-processor — a contract the
Analyzer's ingestion test pins — so those columns are asserted as
objects, not arrays.

This spec complements `appendix-a-api-contract.md` §"Snapshot
data-integrity invariants (#312)" (INV-SNAP-STATUS-PAYLOAD, INV-CHAR-TEXT)
and §"Collector output-contract verification (#314)"
(INV-OUTPUT-CONTRACT, INV-CHAR-TEXT-VERIFIED, INV-STATUS-PAYLOAD-VERIFIED).

## Interfaces

### Inputs

- A live target PostgreSQL, reachable via `SIGNALS_TEST_PG_DSN`
  (build-tag `integration`; skipped when unset). In CI the target is a
  `services: postgres:` container matrixed across PG 14/15/16/17/18.
- Representative schema seeded into the target before collection:
  a parent table with a primary key, a child table whose foreign key to
  the parent is **unindexed** (so `pg_constraint` emits a `contype='f'`
  row), at least one non-PK index, and columns of varied types; plus a
  **partitioned table**, a user-defined **trigger**, a SQL **function**,
  and an **identity column** so the remaining internal-`"char"` schema
  collectors (`pg_partitions_v1`, `pg_triggers_v1` /
  `pg_triggers_definitions_v1`, `pg_functions_v1` /
  `pg_functions_definitions_v1`, `pg_identity_columns_v1`) each emit a
  row. The seed runs as the non-superuser collector role.
- An optional superuser DSN (`SIGNALS_TEST_PG_SUPERUSER_DSN`) used ONLY
  to provision the FDW capability that a `pg_monitor` role cannot create
  itself (`CREATE EXTENSION postgres_fdw` + a foreign `SERVER` +
  `GRANT USAGE` to the collector role). When absent, the FDW fixture and
  its `fdw_foreign_tables_v1.relkind` assertion are capability-gated and
  skipped (OC-R007); every other assertion still runs.

### Outputs

- No product output. The harness produces a pass/fail verdict from the
  exported snapshot ZIP.

## Behaviors

- **Given** a target seeded with an unindexed FK, **when** the full
  collection runs and a snapshot ZIP is exported, **then** the
  `pg_constraints_v1` payload in `query_results.ndjson` contains a row
  with `contype == "f"` (the string, not a number) for that FK.
- **Given** any catalog/schema collector that produced rows, **when**
  the ZIP is read back, **then** each object in that collector's
  `query_results` payload carries the columns its spec declares.
- **Given** any exported `query_runs.ndjson` row with
  `status="success"` and `row_count=N`, **when** joined to
  `query_results.ndjson` by `run_id`, **then** exactly one payload
  exists whose `payload` array holds exactly `N` objects.
- **Given** an internal-`"char"` column (`contype`, `relkind`,
  `relpersistence`, `provolatile`, `prokind`) present in a payload,
  **when** its JSON value is inspected, **then** it is a
  single-character string, never a JSON number.
- **Given** a target seeded so that `catalog_bloat_v1`/
  `catalog_index_bloat_v1` emit `relkind`, a function emits
  `provolatile`/`volatility`, and an identity column emits
  `attidentity`, **when** the ZIP is read back, **then** each of those
  values is a string (`relkind`/`volatility` a single character;
  `attidentity` the empty string for the non-set case), never a JSON
  number — because the connection boundary decodes OID 18 as text for
  every collector, not just the columns hand-cast with `::text`.
- **Given** a target seeded with a **partitioned table**, a
  user-defined **trigger**, and a SQL **function**, **when** the ZIP is
  read back, **then** `pg_partitions_v1.partition_strategy` (from
  `pg_partitioned_table.partstrat`), `pg_triggers_v1.tg_enabled` /
  `pg_triggers_definitions_v1.tg_enabled` (from `pg_trigger.tgenabled`),
  and `pg_functions_v1.volatility` / `pg_functions_definitions_v1.volatility`
  (from `pg_proc.provolatile`) each decode as a single-character string,
  never a JSON number — extending the OID-18 boundary lock across the
  remaining schema collectors that alias an internal-`"char"` column.
- **Given** the target additionally exposes the FDW capability (a
  `postgres_fdw` foreign server the collector role may create a foreign
  table against), **when** the ZIP is read back, **then**
  `fdw_foreign_tables_v1.relkind` (from `pg_class.relkind`) decodes as
  the single-character string `"f"`, never a JSON number. When the
  environment cannot provide the FDW capability the FDW assertion is
  capability-gated and skipped with a documented reason — the remaining
  schema-collector char assertions still run.
- **Given** the seeded schema (enum + composite type, extended
  statistics, RLS-enabled child, FDW server, and populated tables),
  **when** the ZIP is read back, **then** each audited non-`"char"`
  PostgreSQL type class lands in its Analyzer-expected JSON form:
  `numeric` columns (`bloat_ratio`, `dead_pct`, `pct_used`,
  `estimate_drift_pct`) are JSON **numbers**; `jsonb` columns
  (`by_backend_type`, `by_wait_event_type`, `role_attrs`) are JSON
  **objects**; array columns (`enum_labels`, `composite_columns`,
  `attnums`, `kinds`, `column_set`) are JSON **arrays**; timestamp
  columns (`postmaster_start_time`, `started_at`, `stats_reset`,
  `last_analyze`) are **RFC3339 strings** parseable by `time.Parse`;
  `oid` columns (`relid`, `atttypid`, `index_oid`, `datid`, `table_oid`)
  are JSON **numbers**; and `bool` columns (`relhasindex`, `is_unique`,
  `security_definer`, `is_superuser`, `relispartition`) are JSON
  **`true`/`false`** — never the wrong shape (a `numeric` object, a
  base64 `jsonb` string, a Postgres array literal, a Unix-epoch number,
  an `oid` string, or a `"t"`/`"f"` bool).

## Rules

- **OC-R001 — Collect against live PG.** The harness shall run the full
  collection cycle against a real PostgreSQL target, not a mock, and
  export a snapshot ZIP via the production export path
  (`export.Builder.WriteTo`) — never a hand-built payload.
- **OC-R002 — Declared columns present.** For **each registered
  collector** (not only the catalog/schema ones — #316) whose exported
  payload is non-empty, every column its spec declares shall appear in
  each payload object. The declared column set shall be **derived from
  the authoritative spec** (`specifications/collectors/<id>.md`
  `## Output columns` table(s), or the parent spec for the
  definition-mode / MCV family variants), never hand-copied into the
  test, so it cannot drift from the spec. Column-dynamic collectors
  (`SELECT *`, version-variant, or scalar — e.g. `bgwriter_stats_v1`,
  `pg_version_v1`, the TimescaleDB family) are exempt from the fixed
  column check and asserted for row presence only.
  (INV-OUTPUT-CONTRACT.)
- **OC-R003 — Char-type is text.** Every internal-`"char"` column
  (`contype`, `relkind`, `relpersistence`, `provolatile`, `prokind`)
  present in a payload shall decode as a single-character string, never
  a JSON number. The FK seeded by the harness shall produce a
  `pg_constraints_v1` row with `contype == "f"`. (INV-CHAR-TEXT-VERIFIED.)
- **OC-R004 — Status↔payload joinable.** For every exported
  `query_runs` row with `status="success"` / `row_count=N`, exactly one
  `query_results` payload shall be joinable by `run_id` and contain
  exactly `N` objects. (INV-STATUS-PAYLOAD-VERIFIED.)
- **OC-R005 — Char-type normalized at the connection boundary.** The
  internal PostgreSQL `"char"` type (OID 18) shall be normalized to a
  text string at the collection boundary for ALL collectors and
  queries, present and future — via a pooled-connection type
  registration, not per-column `::text` casts. No OID-18 value shall
  leave a collector as a JSON number, regardless of whether its SQL
  hand-casts the column. In particular the columns #313's per-column
  sweep missed — `relkind` (`catalog_bloat_v1`, `catalog_index_bloat_v1`,
  `catalog_fdw` `fdw_*_v1`), `provolatile`/`volatility`
  (`catalog_schema` functions), and `attidentity` (`catalog_schema`
  identity columns) — shall decode as strings (`""` for the non-set
  `attidentity`, matching `col::text` semantics under the text
  protocol's inability to carry an embedded NUL). This closes the class
  the leaky per-column `::text` approach could not (#319, follow-up to
  #312/#313; root cause of Elevarq/Analyzer#1871).
- **OC-R006 — Char sweep covers the remaining schema collectors.** The
  internal-`"char"` output-column whitelist the harness sweeps shall
  include the aliases the remaining schema collectors expose — not only
  the raw catalog names. Specifically it shall include
  `partition_strategy` (`pg_partitions_v1`, from `partstrat`),
  `tg_enabled` (`pg_triggers_v1` / `pg_triggers_definitions_v1`, from
  `tgenabled`), `volatility` (`pg_functions_v1` /
  `pg_functions_definitions_v1`, the **output alias** of `provolatile`),
  and `attidentity` (`pg_identity_columns_v1`). Because the whitelist
  keys the **exported column name**, the function alias is
  `volatility`, never `provolatile`. The harness shall seed a
  partitioned table, a user trigger, a SQL function, and an identity
  column so each of these collectors emits at least one row, and shall
  assert each aliased char value decodes as a single-character string
  (or the empty string for the non-set `attidentity`), never a JSON
  number. This regression-locks #319 for the whole char family across
  the schema collectors #314 missed (#326).
- **OC-R007 — FDW char is capability-gated.** When the environment
  provides the FDW capability — a `postgres_fdw` foreign server the
  collector role may create a foreign table against — the harness shall
  seed a foreign table so `fdw_foreign_tables_v1` emits a row and shall
  assert its `relkind` decodes as the single-character string `"f"`.
  When the capability is absent (no superuser provisioning of the
  extension/server) the FDW assertion is skipped with a documented
  reason; the OC-R006 schema-collector assertions still run. (#326.)
- **OC-R008 — No silent coverage gap.** Every registered collector
  shall be accounted for by the harness: it either (a) emits rows and
  has its declared columns asserted (OC-R002), (b) is column-dynamic
  and has its row presence asserted, or (c) emits no rows in the test
  environment and is listed in the **zero-row allowlist** with a
  documented reason (contention, in-flight operation, replication,
  uninstalled extension, insufficient privilege, or rare catalog
  object). A collector that emits no rows and is NOT allowlisted shall
  fail the harness. The harness shall emit a coverage report recording
  the asserted count and the enumerated not-exercised allowlist, so
  what is and is not exercised is explicit and auditable. (#316.)
- **OC-R009 — Type classes land in the Analyzer-expected JSON form.**
  For each audited PostgreSQL type class beyond internal-`"char"`, the
  harness shall assert — against the exported ZIP read back through the
  same `encoding/json` path a consumer uses — that a representative
  collector column of that class serializes in the exact JSON shape the
  Analyzer consumer reads:
  - `numeric`/`decimal` → JSON **number** (pgx `pgtype.Numeric`
    marshals as a bare number; a `NaN` numeric marshals as the string
    `"NaN"`, accepted as a documented exception);
  - `jsonb`/`json` → JSON **object** or **array** (pgx decodes to
    `map[string]any` / `[]any`), never a base64 `[]byte` string;
  - array (`text[]`, `int[]`) → JSON **array** (pgx `[]any`), never a
    Postgres array literal string — **except** the FDW option columns
    (`fdw_options`, `server_options`, `mapping_options`,
    `foreign_table_options`) which the redaction post-processor
    deliberately renders as a `map[string]string` **object** (a
    contract the Analyzer ingestion test pins), asserted as objects;
  - `timestamptz`/`timestamp`/`date` → **RFC3339 string** parseable by
    `time.Parse(time.RFC3339, …)` (or `RFC3339Nano`), never a
    Unix-epoch number;
  - `oid` → JSON **number** (pgx `uint32`); `regclass`/`regproc`/
    `regnamespace` cast to text → JSON **string** (a name);
  - `bool` → JSON **`true`/`false`**, never the `"t"`/`"f"` text form.

  Each asserted type×collector combination shall be exercised by a
  seeded, non-null value so the assertion is never vacuous. A value in
  the wrong shape fails the harness. This is a regression lock: it goes
  RED if a collector's type ever regresses to a form the Analyzer
  misreads — the general case of the #312 `contype` bug. (#320.)
- **OC-R010 — No silent type-class gap.** The harness shall carry an
  explicit, auditable coverage map of the audited type classes: for each
  class it records which collector×column combinations are asserted
  (OC-R009) and which are not-exercised, with a reason (the class has no
  collector column — `bytea` — or the column is emitted only by a
  version-gated / capability-gated / zero-row-in-harness collector
  already accounted for by OC-R008). A type class with zero asserted
  combinations and no not-exercised justification fails the harness, so
  a future collector that introduces an unhandled type cannot slip
  through unclassified. The harness logs a per-class coverage report.
  (#320.)

## Invariants

- **INV-01** — The harness reuses the production collect + export code
  paths (`collector.Collector` + `export.Builder`); it never
  reconstructs snapshot bytes independently, so what it asserts is what
  a real consumer receives.
- **INV-02** — The harness is read-only against the target beyond the
  one-time seed of representative schema, honoring the Signals safety
  model (no writes during collection, no superuser required).
- **INV-03** — The char-type assertion (`contype == "f"`) is a
  regression lock: it fails on the pre-#313 uncast SQL and passes on
  the `::text`-cast SQL.
- **INV-04** — The OC-R005 assertions (`relkind`, `provolatile`/
  `volatility`, `attidentity` as strings) are a regression lock on the
  central boundary fix: they fail with the OID-18 `AfterConnect`
  registration removed (columns #313 left uncast decode as numbers) and
  pass with it in place, independent of the redundant per-column
  `::text` casts.
- **INV-05** — The OC-R006 schema-collector char assertions
  (`partition_strategy`, `tg_enabled`, `volatility`) and, when the
  capability is present, the OC-R007 `fdw_foreign_tables_v1.relkind`
  assertion are the same regression lock extended across the remaining
  schema collectors: each aliased internal-`"char"` value decodes as a
  single-character string, never a JSON number, so reverting the OID-18
  `AfterConnect` registration turns them RED. The FDW leg is
  capability-gated — its absence never fails the run, but when the
  capability is present the assertion is mandatory.
- **INV-06** — The FDW-capability provisioning uses a separate,
  optional superuser DSN and touches only a dedicated FDW server + a
  `GRANT USAGE`; collection itself still runs as the non-superuser
  collector role (INV-02 holds — no writes and no superuser during
  collection).
- **INV-07** — The declared-column set the harness asserts (OC-R002)
  is derived from the spec at test time, so it can never silently
  diverge from the spec: renaming or dropping a spec-declared output
  column while leaving the SQL alias unchanged (or vice versa) turns
  the assertion RED. The zero-row allowlist (OC-R008) is exhaustive
  over the not-exercised collectors — a collector may be absent from
  it only if it emits rows.
- **INV-08** — The OC-R009 type-fidelity assertions are read back
  through the same `encoding/json` decode a real consumer uses (the
  export ZIP's `query_results.ndjson`), so what the harness inspects is
  the exact JSON type the Analyzer receives — not a Go value inspected
  before serialization. The assertions hold identically across PG 14–18
  (the pgx→Go→JSON mapping for these classes is version-invariant;
  version-gated collectors that are absent on older majors simply
  contribute no sample and are covered by the OC-R010 not-exercised
  accounting, never a false failure). The type-class coverage map
  (OC-R010) is exhaustive: every audited class either has at least one
  asserted combination or a recorded not-exercised reason.

## Failure conditions

- **FC-01** — A declared column absent from a non-empty payload fails
  OC-R002.
- **FC-02** — An internal-`"char"` column decoding as a JSON number
  (e.g. `contype == 102`) fails OC-R003.
- **FC-03** — A `status="success"`/`row_count=N` run with no joinable
  payload, or a payload whose row count ≠ N, fails OC-R004.
- **FC-04** — An aliased schema-collector internal-`"char"` value
  (`partition_strategy`, `tg_enabled`, `volatility`, or, when the FDW
  capability is present, `fdw_foreign_tables_v1.relkind`) decoding as a
  JSON number, or one of the seeded schema collectors emitting no such
  value at all, fails OC-R006 / OC-R007.
- **FC-05** — A registered collector that emits no rows and is absent
  from the zero-row allowlist fails OC-R008 (unclassified coverage
  gap). A collector whose spec has no `## Output columns` table and no
  column-dynamic classification also fails OC-R002 (underivable
  contract) rather than being silently skipped.
- **FC-06** — An audited type-class column decoding to the wrong JSON
  shape fails OC-R009: a `numeric` as an object or (non-`NaN`) string, a
  `jsonb` as a base64 string, an array as a Postgres literal string, a
  timestamp as a number or an unparseable string, an `oid` as a string,
  or a `bool` as a `"t"`/`"f"` string. A seeded type×collector
  combination that emits no non-null value (a vacuous assertion) also
  fails OC-R009.
- **FC-07** — An audited type class with zero asserted combinations and
  no not-exercised justification fails OC-R010 (unclassified type-class
  gap).

## Constraints

- The test is gated by the `integration` build tag and the
  `SIGNALS_TEST_PG_DSN` env var; it SKIPS locally when unset and RUNS
  in the CI PG-version matrix. It performs no network calls beyond the
  target PostgreSQL connection.

## Non-Functional Requirements

- **NFR-01 — Version coverage.** The harness runs across PG
  14/15/16/17/18 in CI so version-gated column/type differences surface.
- **NFR-02 — Full-collector scope (DELIVERED #316).** The
  declared-column assertion now covers the whole registry, not only the
  catalog/schema collectors. Of the ~100 registered collectors, those
  that emit rows against the seeded live PG have their declared columns
  asserted (or their row presence, for the column-dynamic ones); the
  remainder are enumerated in the zero-row allowlist with reasons
  (OC-R008). The original #314 fast-follow is closed.
- **NFR-03 — Type-class audit locked (DELIVERED #320).** The remaining
  PostgreSQL type classes beyond internal-`"char"` — `numeric`, `jsonb`,
  `bytea`, arrays, timestamps, `oid`/`regclass`, and `bool` — are
  audited by exported JSON shape, cross-checked against the Analyzer
  consumer, and regression-locked (OC-R009), with an explicit
  type-class coverage map (OC-R010). Verified across PG 14–18: no
  class required a production fix (`bytea` has no collector column and
  is recorded not-exercised); the char class remains the only one that
  ever needed the central OID-18 codec (#319). The audit found no real
  mismatch — the type family the #312 `contype` bug belonged to is now
  fully accounted for.
