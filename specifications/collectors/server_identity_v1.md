# server_identity_v1 — Collector Specification

## Purpose

Single-row fingerprint of the PostgreSQL instance: version, uptime,
connected database context, current user, and database size.
Foundation for platform detection (hyperscaler vs self-hosted) when
combined with `login_roles_v1` and `extension_inventory_v1`.

## Catalog source

Composite — `version()`, `pg_postmaster_start_time()`,
`pg_database_size()`, `current_database()`, `current_user`,
`current_setting()`.

## Output columns

One row.

| Column | Type | Description |
|---|---|---|
| full_version | text | `version()` verbatim |
| version_string | text | `current_setting('server_version')` |
| version_num | int | `current_setting('server_version_num')` |
| started_at | timestamptz | `pg_postmaster_start_time()` |
| uptime_seconds | double precision | `EXTRACT(EPOCH FROM (now() - pg_postmaster_start_time()))` |
| database_name | text | `current_database()` |
| connected_as | text | `current_user` |
| database_size_bytes | bigint | `pg_database_size(current_database())` |

## Scope filter

Single-row output. No filter.

## Invariants

- Exactly one row per target per sample.
- Read-only — every function is a built-in catalog helper.
- Passes linter.

## Failure Conditions

- FC-01: `pg_database_size()` on the current database can take a
  moment on very large databases — bounded by the 5-second timeout
  declared in the collector.

## Configuration

- Category: server
- Cadence: 6h (Cadence6h)
- Retention: RetentionLong
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Low. Platform identity is derivable by any administrator from
`version()` and is often visible in client tools.

## Analyzer requirements unblocked

- **Platform fingerprinting** — combined with `login_roles_v1`
  (presence of `rds_superuser`, `cloudsqlsuperuser`,
  `azure_pg_admin`, `alloydbsuperuser`) and `extension_inventory_v1`
  (presence of `aurora_stat_utils`, `rds_tools`,
  vendor-specific extensions), the analyzer derives
  `platform ∈ {rds, aurora, cloudsql, alloydb, azure-flex, self-hosted, unknown}`
  and the appropriate `parameter_management_surface`.
- Every recommendation detector — delivery-format selection
  and initial-priors bootstrapping when history is insufficient.

## Known gap

The collector does not directly emit a `platform` field; that is
derived analyzer-side from this collector + `login_roles_v1` +
`extension_inventory_v1`. A future extension could add a
server-side probe for `aurora_version()` / `alloydb_*` functions;
deferred until the analyzer's platform-detection logic lands and
demonstrates whether the extra round-trip is justified.
