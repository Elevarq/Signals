# login_roles_v1 — Collector Specification

## Purpose

Enumerate roles that can log in, together with attributes that
affect authorization posture: superuser, DB/role creation,
replication, RLS bypass, connection limit, password validity.
Feeds security-posture reporting and — importantly — platform
fingerprinting (presence of `rds_superuser`, `cloudsqlsuperuser`,
`azure_pg_admin`, `alloydbsuperuser`).

## Catalog source

- `pg_roles` (view over `pg_authid` that redacts password hash)

## Output columns

| Column | Type | Description |
|---|---|---|
| oid | oid | Role OID — stable join key for resolving `pg_stat_statements.userid` to a role name |
| rolname | text | Role name |
| rolsuper | boolean | Superuser |
| rolcreatedb | boolean | Can create databases |
| rolcreaterole | boolean | Can create roles |
| rolreplication | boolean | Can initiate streaming replication |
| rolbypassrls | boolean | Bypasses row-level security |
| rolconnlimit | int | Per-role connection limit (-1 = unlimited) |
| rolvaliduntil | timestamptz | Password validity expiry (NULL = never) |

## Scope filter

`WHERE rolcanlogin = true` — non-login roles (groups) excluded.

## Invariants

- Deterministic ordering: `ORDER BY rolsuper DESC, rolname`
  (superusers first, then alphabetical).
- Stable output column order.
- Read-only, passes linter.
- Password hash never surfaces (`pg_roles` view does not expose it).

## Failure Conditions

- FC-01: Permission denied on `pg_roles` (unusual — world-readable
  by default) → standard collector error path.

## Configuration

- Category: security
- Cadence: 6h (Cadence6h)
- Retention: RetentionLong
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Low. No passwords. Role names are visible to every connected role
by default.

## Analyzer requirements unblocked

- **Platform fingerprint** — combined with `server_identity_v1` and
  `extension_inventory_v1`, detects hyperscaler platform via
  presence of vendor-specific admin roles.
- Security-posture reporting (superuser inventory, expired
  passwords, dangerous attributes on login roles).
