# pg_settings_v1 — Collector Specification

## Purpose

Current values of cluster-level GUCs. Every cost-configuration
recommendation references the current setting so advice is phrased
as a delta ("raise from X to Y") rather than an absolute
prescription.

## Catalog source

- `pg_settings`

## Output columns

| Column | Type | Description |
|---|---|---|
| name | text | GUC name |
| setting | text | Current value (canonical text form) |
| unit | text | Unit of measurement (`ms`, `kB`, `8kB`, etc.) |
| category | text | Configuration category |
| source | text | `default`, `configuration file`, `command line`, ... |
| pending_restart | boolean | Change pending a restart |
| context | text | `internal`, `postmaster`, `sighup`, `superuser-backend`, `backend`, `superuser`, `user`. CRITICAL — distinguishes runtime-settable GUCs from restart-required GUCs without a hard-coded analyzer-side allowlist. |
| vartype | text | `bool`, `enum`, `integer`, `real`, `string`. Drives safe value formatting (`on`/`off` vs `1`/`0`). |
| boot_val | text | Compile-time default. Used by detectors to flag "value differs from PostgreSQL default" without hard-coding the upstream defaults. |
| reset_val | text | Value RESET would restore to (typically equals `setting` unless an override is layered on top). |
| min_val | text | Lower bound (numeric / real GUCs). Used to enforce safe sweep ranges. |
| max_val | text | Upper bound. |
| enumvals | text[] | Allowed values for `vartype = 'enum'`. |
| short_desc | text | Operator-facing one-line description shipped by PostgreSQL. |

## Scope filter

All rows emitted. No filter.

## Invariants

- Deterministic ordering: `ORDER BY name`.
- Stable output column order.
- Read-only query, passes linter.
- `setting` is the effective value (not a formula) — analyzer does
  not need to resolve unit conversion at collection time.

## Failure Conditions

- FC-01: On managed platforms (RDS parameter groups, Cloud SQL
  flags, Azure server parameters), `sourcefile` / `sourceline` are
  NULL because the override comes from a platform parameter group
  rather than a file on disk. Those two columns are **excluded**
  from this collector's SELECT for cross-platform safety; the rest
  of the extended set (`context`, `vartype`, `boot_val`,
  `reset_val`, `min_val`, `max_val`, `enumvals`, `short_desc`) is
  readable on every supported managed platform.
- FC-02: Permission denied (rare — `pg_settings` is world-readable)
  → standard collector error path.

## Configuration

- Category: server
- Cadence: 6h (Cadence6h)
- Retention: RetentionLong
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Low. GUC values are visible to any connected role. No credentials
surface here.

## Analyzer requirements unblocked

- `io-cost-calibration` — baseline for the four target GUCs
  (`random_page_cost`, `seq_page_cost`, `effective_io_concurrency`,
  `effective_cache_size`).
- `object-parameter-drift` — cluster defaults for comparison against
  per-table / per-tablespace / per-function overrides.
- Every recommendation detector — grounds advice in current state.

## Remaining gap

The `sourcefile` and `sourceline` columns are not exported. On
self-hosted PostgreSQL they identify the file + line that defined
the override; on managed platforms they are always NULL because
the override layer is the platform's parameter group, not a file.
The downstream analyzer doesn't have a use-case for them today,
and exporting NULL on every managed-platform row would create
operator-facing noise.

If a future analyzer rule needs file-level override provenance for
self-hosted clusters, extend the SELECT in a separate change —
the existing rows degrade gracefully on managed platforms because
`source` already distinguishes `configuration file` from
`override`.
