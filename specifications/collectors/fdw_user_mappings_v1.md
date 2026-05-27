# fdw_user_mappings_v1 — Collector Specification

## Purpose

User-mapping inventory. Tells the analyzer which local roles can
talk to which foreign servers, with REDACTED option arrays — never
cleartext passwords or tokens.

## Catalog source

- `pg_user_mappings` (the public **view**, not the
  superuser-restricted `pg_user_mapping` table). The view exposes
  all mappings to all roles by default, and hides `umoptions` for
  roles that lack the privilege to see another user's option set —
  matching the documented graceful-degradation path (FC-02).
- The view already carries `srvname` and `usename`; we resolve
  `umuser=0` → `'PUBLIC'` in the SQL. `pg_get_userbyid(umuser)` is
  used as a fallback for the rare case where the view's `usename`
  is NULL.

## Output columns

| Column | Type | Description |
|---|---|---|
| server_name | text | `pg_foreign_server.srvname` |
| local_user_name | text | local role name from `pg_get_userbyid`; `'PUBLIC'` for unscoped mappings |
| mapping_options | text[] | raw `key=value` option array (`umoptions`); ALWAYS run through the redactor before persistence (REDACT-R003) |

## Scope filter

- All mappings included.

## Invariants

- **CRITICAL**: `mapping_options` typically contains `password=…`.
  Persisting cleartext is a hard violation of the safety model.
  Redaction is enforced at the collector pipeline (post-`queryToMaps`
  step) and tested in `TestFDWUserMappings_RedactsPasswordAndSecrets`.
- Local user OID is **never** emitted — only the resolved role name.
- Read-only. No remote connection.
- Deterministic ordering: `ORDER BY srvname, local_user_name`.

## Failure conditions

- FC-01: No mappings → zero rows.
- FC-02: Permission denied → standard error path.
  - Note: `pg_user_mapping` visibility is restricted to the
    superuser AND any non-superuser whose mapping is being read for
    their own role. `pg_monitor` does not by itself confer access
    to other users' mappings. Mappings the collecting role cannot
    see are silently absent — the collector_status row succeeds
    with whatever rows the role can see, mirroring how
    `pg_user_mappings` (the public view) behaves.

## Redaction

- **REDACT-R003**: Every entry in `mapping_options` whose key
  matches the FDW redactor's sensitive-pattern set
  (`internal/pgqueries/fdw_redact.go::fdwRedactPatterns`) MUST emit
  `<redacted>` for its value. Non-sensitive option keys (e.g.
  `user`, `username` — the *remote* role name) round-trip
  unchanged.
- The cleartext value never appears on disk, in NDJSON payloads, in
  the snapshot ZIP, or in collector logs.

## Configuration

- Category: `schema`
- Cadence: `CadenceDaily`
- Retention: `RetentionLong`
- Timeout: 15 s
- HighSensitivity: **false** — but only because the option values
  are redacted before persistence. Without redaction, this would be
  the most sensitive collector in the registry.

## PostgreSQL version compatibility

- PG 14–18: same `pg_user_mapping` shape; default SQL works.
- PG 19: default until divergence observed.

## Acceptance criteria

| AC | What | Test |
|----|------|------|
| FDW-U-AC1 | Registry contains `fdw_user_mappings_v1`. | `TestFDWUserMappings_Registered` |
| FDW-U-AC2 | SQL compiles cleanly. | build / vet |
| FDW-U-AC3 | A mapping `(host_user → remote_server) OPTIONS (user 'remote', password 'hunter2')` produces a row with `mapping_options` containing `user=remote` (unchanged) and `password=<redacted>`. The literal `'hunter2'` appears in NEITHER the row nor any log line. | Live pgtest / smoke + redactor unit tests |
| FDW-U-AC4 | A `PUBLIC` mapping emits `local_user_name='PUBLIC'`. | Live pgtest / smoke |
| FDW-U-AC5 | No-mappings database → zero rows. | Live pgtest / smoke |

Spec status: ACTIVE.
