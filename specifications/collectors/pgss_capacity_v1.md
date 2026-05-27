# pgss_capacity_v1 — Collector Specification

## Purpose

Capacity-pressure signal for `pg_stat_statements`. Emits two
facts the analyzer needs to know whether the extension is
silently dropping data:

- **`dealloc`** — how many tracked statements PostgreSQL has
  evicted since the last reset because `pg_stat_statements.max`
  was too low. Non-zero means every workload-ranked finding
  downstream is biased toward the queries that survived
  eviction.
- **`tracked_count`** — current row count in
  `pg_stat_statements`. The analyzer compares this to
  `pg_stat_statements.max` (from `pg_settings_v1`) to detect
  "near the cap, eviction imminent" before `dealloc` rises.

Companion to `pgss_reset_check_v1` (different concern —
that collector tracks WHEN the view was reset for delta-
semantics; this one tracks the eviction-pressure window).

## Catalog source

- `pg_stat_statements_info` (extension, PG 14+)
- `pg_stat_statements` (extension, PG ≥ 9.x but only the row
  count is needed; PG 14 floor is set by the info view)

## Query

```
SELECT
    info.dealloc,
    (SELECT count(*) FROM pg_stat_statements) AS tracked_count
FROM pg_stat_statements_info AS info
```

## Output columns

One row.

| Column | Type | Description |
|---|---|---|
| dealloc | bigint | Total statements evicted since stats_reset due to `pg_stat_statements.max` overflow |
| tracked_count | bigint | Current row count in `pg_stat_statements` |

## Scope filter

Single-row view (`pg_stat_statements_info` is single-row by
construction).

## Invariants

- Exactly one row when both extension and view are available.
- Read-only, passes linter.
- `dealloc` is monotonic between resets; the analyzer treats
  any non-zero value as a "dropped data" signal regardless of
  rate.
- `tracked_count` is a point-in-time count.

## Failure Conditions

- FC-01: Extension absent → collector filtered out at
  pgqueries layer via `RequiresExtension: pg_stat_statements`.
- FC-02: Extension present but PG < 14 →
  `pg_stat_statements_info` is unavailable. Filtered out via
  `MinPGVersion: 14`. Older platforms get no eviction signal;
  the analyzer-side rule degrades gracefully.
- FC-03: Permission denied → standard collector error path.
  `pg_stat_statements_info` is readable by any role with
  USAGE on the extension's schema; default deployments are
  typically fine, but locked-down RBAC may block.

## Configuration

- Category: extensions
- Cadence: 1h (Cadence1h)
- Retention: RetentionMedium
- Min PG version: 14
- Requires extension: pg_stat_statements
- Semantics: snapshot (dealloc + tracked_count at sample time)
- Enabled by default: yes

## Sensitivity

Low. `dealloc` and `tracked_count` are aggregate counters; no
query text, no role identity, no row data.

## Downstream use

- **Eviction pressure** —
  `dealloc > 0` is the hard trigger; `tracked_count /
  pg_stat_statements.max >= 0.9` is the soft trigger.
- **Track-top blind spot** — uses
  `tracked_count` as a sanity indicator alongside
  `pg_stat_statements.track` from `pg_settings_v1` and the
  routine inventory.

## Settings dependency

The companion settings `pg_stat_statements.max`,
`pg_stat_statements.track`, `pg_stat_statements.track_utility`,
and `pg_stat_statements.track_planning` are collected by the
existing `pg_settings_v1` collector — no amendment needed there.
The analyzer joins these snapshots by snapshot identity.
