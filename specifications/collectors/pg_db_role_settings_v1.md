# pg_db_role_settings_v1 — Collector Specification

## Purpose

Enumerate PostgreSQL default configuration overrides stored in
`pg_db_role_setting`. These settings are created by `ALTER DATABASE ...
SET ...`, `ALTER ROLE ... SET ...`, and `ALTER ROLE ... IN DATABASE ...
SET ...`. The analyzer needs this collector to understand the effective
configuration surface for planner advice without assuming that
`pg_settings` from the collector session represents every application
role or database on the cluster.

## Catalog source

- `pg_db_role_setting`
- `pg_database`
- `pg_roles`

## Output columns

| Column | Type | Description |
|---|---|---|
| database_oid | oid | Target database OID, or `0` for role-only settings |
| database_name | name | Target database name, NULL when `database_oid = 0` |
| role_oid | oid | Target role OID, or `0` for database/ALL-role settings |
| role_name | name | Target role name, NULL when `role_oid = 0` |
| setting_scope | text | One of `database`, `role`, `role_in_database`, or `global` |
| setconfig | text[] | Raw array of `"name=value"` default settings |

## Scope filter

- Emits every row in `pg_db_role_setting`.
- Does not filter by the collector's current database or current role.
- Preserves rows with `setdatabase = 0` and/or `setrole = 0`; the
  analyzer interprets the scope from the OIDs and `setting_scope`.

## Invariants

- Deterministic ordering: `ORDER BY setting_scope, database_name NULLS FIRST,
  role_name NULLS FIRST, database_oid, role_oid`.
- Stable output column order.
- Read-only query, passes linter.
- `setconfig` is preserved as the raw `text[]` form; parsing into
  key/value pairs is the analyzer's responsibility.
- Collector output must not depend on the effective settings of the
  collector connection.

## Failure Conditions

- FC-01: Permission denied on `pg_db_role_setting`, `pg_database`, or
  `pg_roles` → standard collector error path.

## Configuration

- Category: server
- Cadence: 24h (CadenceDaily)
- Retention: RetentionMedium
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Low. The collector emits configuration parameter names and values that
affect role/database defaults. It does not read passwords, connection
strings, role password hashes, or user table data.

## Analyzer requirements unblocked

- `planner-config-scope` — distinguish cluster/session-effective GUCs
  from role, database, and role-in-database overrides.
- `advice-drift-detection` — detect whether a recommended role/database
  setting has been applied, changed, or removed.
- `planner-model-inputs` — provide scoped planner GUCs for plan
  prediction per workload role and database.
