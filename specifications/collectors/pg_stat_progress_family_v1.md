# pg_stat_progress_* family — Collector Specification

## Status

ACTIVE

## Purpose

Captures **in-flight PostgreSQL operations** — the per-operation
progress views (`pg_stat_progress_*`) introduced from PG 9.6
onwards and extended through PG 14. Existing collectors capture
state (sizes, stats, plans) and outcomes (counters, recent_at);
none surface "what is happening *right now*". After a snapshot
lands, the analyzer can tell an autovacuum ran or didn't, but not
*why* it has been stuck for six hours.

The progress views close that blind spot at near-zero cost — they
are plain SELECT-able system views, gated only by `MinPGVersion`.

## Scope (one collector per upstream view)

| Collector ID | Upstream view | MinPGVersion |
|---|---|---|
| `pg_stat_progress_vacuum_v1` | `pg_stat_progress_vacuum` | 14 |
| `pg_stat_progress_analyze_v1` | `pg_stat_progress_analyze` | 14 |
| `pg_stat_progress_create_index_v1` | `pg_stat_progress_create_index` | 14 |
| `pg_stat_progress_cluster_v1` | `pg_stat_progress_cluster` | 14 |
| `pg_stat_progress_basebackup_v1` | `pg_stat_progress_basebackup` | 14 |
| `pg_stat_progress_copy_v1` | `pg_stat_progress_copy` | 14 |

`MinPGVersion: 14` is the floor for every collector in this
family. The underlying views landed earlier (9.6 / 12 / 13 / 14
depending on view), but Signals scopes its supported majors at 14+
already; carrying a lower floor would add catalog-dispatch noise
without an addressable consumer.

## Output columns

One row per in-flight operation. Empty rowset when no matching
operation is running on the target cluster.

Each collector projects the upstream view's columns explicitly
(no `SELECT *`). For views whose column shape drifted between
majors the canonical SQL emits the full union with
`NULL::bigint AS <column>` stubs and per-major overrides via
`RegisterOverride` populate the real columns. Consumers see one
stable column list across majors; only the populated subset
differs.

The two views with drift in the v1 supported window:

- **`pg_stat_progress_vacuum`** — PG 17 renamed
  `max_dead_tuples` / `num_dead_tuples` to the byte-denominated
  `max_dead_tuple_bytes` / `dead_tuple_bytes` and added
  `num_dead_item_ids`, `indexes_total`, `indexes_processed`.
  PG 18 added `delay_time` (cumulative seconds spent in
  `vacuum_cost_delay`). PG 17 and PG 18 each have their own
  override.
- **`pg_stat_progress_copy`** — PG 17 added `tuples_skipped`.
  Overrides on PG 17 and PG 18 populate it.

The complete column projections per view are documented inline at
the registration site (`internal/pgqueries/catalog_progress.go`)
to keep the spec from drifting from the code.

## Scope filter

Single SELECT per view; no schema scoping. All `pg_stat_progress_*`
views live in `pg_catalog`.

## Invariants

- **Empty rowset is the success state.** No in-flight operation
  → zero rows → `status = success`. This is the common case.
- **No derived columns.** The collector passes upstream columns
  through unchanged. Cross-snapshot phase / progress comparison is
  analyzer-side (INV-SIGNALS-01).
- **Deterministic ordering.** Each collector includes
  `ORDER BY pid` so successive runs against the same in-flight set
  emit rows in the same order.
- **Read-only.** Single SELECT against a system view, no joins.
- Passes the linter.

## Failure Conditions

- **FC-01**: PG version < 14 → every family member excluded by
  `MinPGVersion` gate (R081). Emits `status=skipped,
  reason=version_unsupported` in `collector_status.json` via
  existing EA-R001 framework.

## Configuration (applies to every family member)

- Category: `progress`
- Cadence: `Cadence5m`
- Retention: `RetentionShort`
- Min PG version: 14
- Requires extension: none
- Semantics: snapshot (point-in-time view of in-flight ops)
- Enabled by default: yes
- Sensitivity: `low` — only operation phases, numeric progress,
  and operator-managed identifiers (pid, datname, relid). No SQL
  text, no payload, no PII.

## Sensitivity

Low. The progress views expose:

- Numeric progress counters (`*_total`, `*_done`, `*_scanned`).
- Phase strings — fixed enum values defined by PG (e.g. `vacuum`
  phases: `initializing`, `scanning heap`, `vacuuming indexes`,
  `vacuuming heap`, `cleaning up indexes`, `truncating heap`,
  `performing final cleanup`).
- OIDs (`relid`, `datid`, `index_relid`) — opaque
  integers. The accompanying `datname` is the database name (already
  exposed via `pg_database`); `relid`'s relation name is **not**
  resolved by the collector.
- pid — the backend OS pid running the operation. Already exposed
  via `pg_stat_activity_v1`.

No SQL text, no payload bytes, no PII.

## Analyzer requirements unblocked

- **Autovacuum starvation**: cross-snapshot `phase` + `heap_blks_scanned`
  delta indicates whether vacuum is making progress or stuck on a
  lock acquisition.
- **Stuck `CREATE INDEX CONCURRENTLY`**: phase stuck at
  `building index: relation scan` for hours typically means a
  long-running transaction is holding back the snapshot.
- **Slow `ANALYZE`**: `sample_blks_scanned` vs `sample_blks_total`
  delta gives ETA.
- **Backup-induced load**: basebackup progress aligns with
  disk-IO spikes from `pg_stat_io_v1`.
- **Bulk-`COPY` visibility**: operators rarely know
  `tuples_processed` of an in-flight import; the analyzer can
  surface it via cross-snapshot delta.

## Known constraints

- **Column drift on PG 17** for `pg_stat_progress_vacuum`. Handled
  via `RegisterOverride(17, "pg_stat_progress_vacuum_v1", ...)` so
  consumers see a stable canonical schema.
- **Empty rowsets dominate.** On a quiet cluster, every family
  member returns zero rows on most cycles — by design. The
  analyzer does not interpret "absent" as "broken".

## Out of scope

- Resolving OIDs to names. The relid / datid columns are passed
  through as integers; downstream consumers join against catalog
  collectors as needed.
- Derived ETA, phase-duration histograms, or progress-rate
  computation. The collector is a passive pass-through; the
  analyzer owns interpretation (INV-SIGNALS-01).
