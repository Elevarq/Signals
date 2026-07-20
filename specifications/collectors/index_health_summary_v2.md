# index_health_summary_v2 — Collector Specification

## Status

ACTIVE

## Purpose

`index_health_summary_v1` classifies indexes into hygiene findings
(`unused`/`redundant`/`duplicate`/…) from key-column names, four state
booleans, usage, size, and a prefix/peer heuristic. That is enough for a
"consider dropping" report, but **not** enough for a decision-grade
recommendation: a key-column prefix is not proof that an index is safely
removable, and a missing boolean silently coerced to `false` can promote a
heuristic into a `DROP INDEX`.

v2 publishes a **decision-grade** index-health contract so the analyzer
(Elevarq/Analyzer#1596, and the branch-aware lifecycle work in #1597) can
reconstruct the catalog facts v1 discarded, without inventing new PostgreSQL
analysis. It emits explicit safety state, constraint backing, build state, and a
**versioned semantic structure fingerprint** so exact duplicates are proven by
structural equality — not by a key-column prefix.

v1 remains registered and unchanged; v2 is additive.

## Catalog source

`pg_index` ⋈ `pg_class` (index + table) ⋈ `pg_namespace` ⋈ `pg_am`
(access method) ⋈ `pg_stat_user_indexes` (LEFT JOIN, usage) ⋈ `pg_constraint`
(LEFT JOIN on `conindid`, constraint backing) ⋈ `pg_stat_progress_create_index`
(LEFT JOIN on `index_relid`, live build state). The semantic fingerprint is
derived inside PostgreSQL from `pg_get_indexdef` (structure) plus
`pg_index`/`pg_constraint` flags — no raw expression or predicate literals are
emitted.

## Output columns

One deterministic row per non-system index in the connected database.

| Column | Type | Description |
|---|---|---|
| `schemaname` | text | Index schema. |
| `tablename` | text | Owning relation name. |
| `indexname` | text | Index name. |
| `index_oid` | oid | `pg_index.indexrelid` — database-local index identity. |
| `table_oid` | oid | `pg_index.indrelid` — database-local table identity. |
| `size_bytes` | bigint | `pg_relation_size(indexrelid)`. |
| `idx_scan` | bigint | `pg_stat_user_indexes.idx_scan`. **NULL when no stats entry — never coerced to 0.** |
| `idx_tup_read` | bigint | Cumulative tuple-read count. NULL when no stats entry. |
| `idx_tup_fetch` | bigint | Cumulative tuple-fetch count. NULL when no stats entry. |
| `is_valid` | boolean | `pg_index.indisvalid`. |
| `is_ready` | boolean | `pg_index.indisready`. |
| `is_live` | boolean | `pg_index.indislive`. |
| `is_primary` | boolean | `pg_index.indisprimary`. |
| `is_unique` | boolean | `pg_index.indisunique`. |
| `is_exclusion` | boolean | `pg_index.indisexclusion`. |
| `is_immediate` | boolean | `pg_index.indimmediate` (uniqueness enforced immediately vs deferrable). |
| `is_replica_identity` | boolean | `pg_index.indisreplident` — dropping a replica-identity index changes logical-replication row identity. |
| `is_constraint_backed` | boolean | True when a `pg_constraint` row has `conindid = index_oid`. |
| `constraint_type` | text | Controlled: `primary` / `unique` / `exclusion` / `other` when constraint-backed; NULL otherwise. Derived from `pg_constraint.contype`. |
| `build_state` | text | Controlled: `active_build` / `active_reindex` (in `pg_stat_progress_create_index`), else `invalid_residue` (not valid), `not_ready_residue` (valid but not ready), else `ready`. |
| `access_method` | text | `pg_am.amname` (btree/hash/gist/gin/spgist/brin/…). |
| `key_column_count` | int | `pg_index.indnkeyatts`. |
| `include_column_count` | int | `indnatts - indnkeyatts` (INCLUDE columns). |
| `structure_version` | int | Fingerprint-algorithm version (currently `1`). Consumers gate equality on matching versions. |
| `structure_fingerprint` | text | md5 hex of the normalized semantic structure: access method, key/expression columns with opclasses + collations + ordering + NULL placement, INCLUDE columns, uniqueness, NULL-distinct semantics, exclusion operators, and the partial predicate — derived from `pg_get_indexdef` plus the uniqueness/exclusion facts. Name- and table-independent. **An equivalence identifier, not a security digest.** |
| `exact_duplicate_of` | text | Name of the lowest-`index_oid` index on the same `(schemaname, tablename)` whose `(structure_version, structure_fingerprint)` is **identical**. NULL otherwise. The canonical (lowest-OID) index is not tagged. |
| `prefix_candidate_of` | text | **Review candidate, not a removal judgment.** Name of a larger index on the same table whose key columns strictly extend this index's key columns as a left-prefix. NULL otherwise. |
| `prefix_candidate_basis` | text | Controlled: `key_column_left_prefix` when `prefix_candidate_of` is set; NULL otherwise. Names the (heuristic) basis so a consumer never treats it as proven redundancy. |

## Rules

- **R-IHV2-01** (no safety synthesis): `idx_scan`, `idx_tup_read`,
  `idx_tup_fetch` are emitted directly from `pg_stat_user_indexes` and remain
  NULL when the index has no stats entry. No `COALESCE(..., 0)`, no
  `COALESCE(..., false)` on any state boolean.
- **R-IHV2-02** (explicit state): validity, readiness, live, primary, unique,
  exclusion, immediate, and replica-identity are emitted from their `pg_index`
  columns verbatim.
- **R-IHV2-03** (constraint evidence): `is_constraint_backed` /
  `constraint_type` derive from a `pg_constraint` row **that owns this index**
  — `conindid = index_oid` **and** `contype IN ('p','u','x')`. Foreign-key
  (`f`) and other rows whose `conindid` merely *references* this index are
  ignored, so the join yields at most one row per index. `contype` maps
  `p→primary`, `u→unique`, `x→exclusion`; `other` is a reserved defensive
  fallback that the current `p/u/x` filter makes unreachable.
- **R-IHV2-04** (build state): a row present in
  `pg_stat_progress_create_index` for this index is `active_reindex` when its
  `command` begins `RE`, else `active_build`; otherwise `invalid_residue` /
  `not_ready_residue` / `ready` from the validity + readiness flags.
- **R-IHV2-05** (fingerprint proves duplicates): `exact_duplicate_of` is set
  **only** when the full `(structure_version, structure_fingerprint)` matches
  another same-table index — never on key-column equality alone.
- **R-IHV2-06** (prefix is a candidate, not a verdict): `prefix_candidate_of`
  is a review candidate with an explicit `prefix_candidate_basis`; it is never
  labeled redundant or safe-to-drop.
- **R-IHV2-07** (fingerprint derived in-DB): the fingerprint is computed inside
  PostgreSQL from `pg_get_indexdef` + flags; raw expression/predicate literals
  are not emitted as output columns.

## Invariants

- **INV-IHV2-01**: one row per surviving index — no aggregation.
- **INV-IHV2-02**: every `exact_duplicate_of` / `prefix_candidate_of` resolves
  to another row in the same `(schemaname, tablename)` group — no dangling
  pointers.
- **INV-IHV2-03**: `structure_fingerprint` is equal for two indexes iff their
  normalized structure is equal; it is independent of index name and table
  name.
- **INV-IHV2-04**: deterministic output — total `ORDER BY schemaname,
  tablename, indexname`, and peer selection (lowest `index_oid`, then name) is
  stable under catalog/input order changes.
- **INV-IHV2-05**: read-only — a single `SELECT`/`WITH` over `pg_catalog` +
  stats/progress views; passes the linter; excludes system schemas
  (INV-SIGNALS-12).
- **INV-IHV2-06** (v1 compatibility): `index_health_summary_v1` remains
  registered with its documented contract unchanged.

## Scope filter

System schemas excluded: `pg_catalog`, `information_schema`, `pg_toast`,
`pg_temp_%`, `pg_toast_temp_%` (INV-SIGNALS-12).

## Failure Conditions

- **FC-IHV2-01**: an index whose `pg_get_indexdef` is unexpectedly NULL (should
  not occur for a live index) yields a NULL `structure_fingerprint`; such a row
  never matches another (NULL never equals NULL), so it is never reported as a
  duplicate — fail-safe, not fail-open.
- **FC-IHV2-02**: query exceeds `Timeout: 30s` on a pathological catalog (tens
  of thousands of indexes) → standard collector-timeout path. Row count is
  bounded by index count.

## Configuration

- Category: `indexes`
- Cadence: `Cadence6h`
- Retention: `RetentionMedium`
- Timeout: 30 seconds
- Min PG version: none. Every catalog column used (`indislive`,
  `indisexclusion`, `indisreplident`, `indnkeyatts`) and
  `pg_stat_progress_create_index` exist on every Signals-supported major
  (14–18). NULL-distinct semantics (the PG15+ `NULLS NOT DISTINCT` option) are
  captured **implicitly** through `pg_get_indexdef`, which renders that clause
  when present — no version-gated column is read, so PG14 does not error and
  the fingerprint stays correct across majors. Live-validated on PG14 and PG18.
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes
- Sensitivity: `medium` — schema/table/index/column identifiers and structure,
  same surface as `pg_indexes_v1`, `pg_stats_v1`, and v1. No values, no query
  text; the fingerprint is a hash, not literals.

## Analyzer requirements unblocked (Elevarq/Analyzer#1596)

- **Provable duplicate removal**: two indexes with equal
  `(structure_version, structure_fingerprint)` on the same table are true
  duplicates; the follower carries `exact_duplicate_of` — safe to recommend the
  drop of the non-canonical one, with constraint/replica-identity guards
  visible.
- **Safety gating**: `is_constraint_backed`, `constraint_type`,
  `is_replica_identity`, `is_primary`, `is_unique` let the analyzer refuse to
  recommend dropping a constraint-backing or replica-identity index.
- **Build-state awareness**: `active_build` / `active_reindex` vs
  `invalid_residue` distinguishes an in-flight `CONCURRENTLY` build from
  failed-build residue, so a report never proposes dropping an index that is
  actively being built.
- **Prefix as candidate**: `prefix_candidate_of` feeds a review queue, not an
  automated drop.

## Out of scope

- Changing or removing `index_health_summary_v1`.
- Configurable size thresholds / the v1 `health_findings` tag list (v2 emits
  raw decision facts; classification stays analyzer-side).
- Cross-snapshot deltas (analyzer-side).
- Portable (cross-database) fingerprints — the fingerprint is a database-local
  equivalence identifier.
