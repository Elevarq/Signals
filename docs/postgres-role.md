# PostgreSQL role for Elevarq Signals

Elevarq Signals connects to monitored PostgreSQL targets with a single role
per target. This document describes the **least-privilege** role
recommended for production and the additional privileges required when
the high-sensitivity collectors (R075) are enabled.

The goal is to give monitoring tooling exactly the visibility it needs
to gather statistics — and nothing more.

> For how the credential for this role is supplied — local password or
> passwordless cloud identity (RDS IAM, Entra, Cloud SQL IAM) or a cloud
> secret store — see
> [database-connections.md](database-connections.md).

## Default role (statistics + diagnostics)

For the default collector pack (system catalogs, `pg_stat_*` views,
extension presence, schema inventory without definition text) the
`pg_monitor` role is sufficient and is the recommended baseline.

```sql
-- Run as a superuser on each target database cluster.
CREATE ROLE arq_signals LOGIN
    PASSWORD '<set-via-secret-store>'
    NOSUPERUSER
    NOCREATEDB
    NOCREATEROLE
    NOREPLICATION
    NOBYPASSRLS;

GRANT pg_monitor TO arq_signals;
```

`pg_monitor` is a built-in PostgreSQL role (since 10) that aggregates
the read-only monitoring sub-roles:

- `pg_read_all_settings` — read all GUC values, including those marked
  as superuser-only.
- `pg_read_all_stats` — read all `pg_stat_*` views regardless of the
  owning role.
- `pg_stat_scan_tables` — execute monitoring functions that may take
  ACCESS SHARE locks.

This grant is **read-only**. It does not allow `INSERT`, `UPDATE`,
`DELETE`, `CREATE`, schema changes, replication, or row-security
bypass. Combined with the runtime safety check that Elevarq Signals
performs on every connection (R005), the role cannot mutate data even
if a query in the catalog were maliciously rewritten.

### What the default pack reads

- `pg_stat_database`, `pg_stat_user_tables`, `pg_stat_user_indexes`,
  `pg_stat_activity`, `pg_stat_replication`, `pg_stat_bgwriter`,
  `pg_stat_io`, `pg_stat_wal_receiver`, `pg_stat_subscription`.
- `pg_settings`, `pg_stats`, `pg_class`, `pg_namespace`, `pg_index`,
  `pg_constraint`, `pg_extension`, `pg_authid`, `pg_database`,
  `pg_tablespace`.
- `pg_stat_statements` if the extension is installed.

None of these emit user data; they emit metadata, counters, and
schema descriptions.

### Connection posture enforced by the daemon

Even with `pg_monitor`, Elevarq Signals applies belt-and-suspenders at
runtime on every connection:

- `SET LOCAL default_transaction_read_only = on`
- `SET LOCAL statement_timeout`, `lock_timeout`,
  `idle_in_transaction_session_timeout` — short, configurable.
- Every connection sets `application_name = arq-signals` in its
  startup parameters (R106). The value is used by the
  `pg_stat_statements_v1` collector to filter out Signals' own
  probe queries so customer workload analysis is not polluted by
  monitoring traffic. Operators can identify Signals sessions in
  `pg_stat_activity` by this application name.
- The role is verified to be `NOSUPERUSER`, non-replication,
  non-bypassrls, and not a member of `pg_write_all_data` before any
  collector query is executed. Any failure aborts collection
  (R006/R023).

## High-sensitivity collectors (R075, default-on)

The high-sensitivity pack runs **by default** (R075 revised 2026-05:
collect-everything default). Two groups of collectors are flagged
high-sensitivity:

1. **SQL definition text** authored by the application owners
   (whole-row-sensitive — opt-out skips them entirely):
   - `pg_views_definitions_v1` — `pg_get_viewdef(oid)`
   - `pg_matviews_definitions_v1` — `pg_get_viewdef(oid)`
   - `pg_triggers_definitions_v1` — `pg_get_triggerdef(oid)`
   - `pg_functions_definitions_v1` — `pg_get_functiondef(oid)` (PG 11+)
2. **Live `pg_stat_activity` query text** (mixed sensitive /
   non-sensitive columns — opt-out keeps the collectors running and
   nulls only the listed `SensitiveColumns`):
   - `long_running_txns_v1` (`query_snippet`)
   - `blocking_locks_v1` (`blocked_query`, `blocking_query`)
   - `idle_in_txn_offenders_v1` (`query_snippet`)
   - `wraparound_blockers_v1` (`query_snippet`)

Operators who prefer privacy over diagnostic richness opt **out** with:

```yaml
signals:
  high_sensitivity_collectors_enabled: false
```

(or `ARQ_SIGNALS_HIGH_SENSITIVITY_COLLECTORS_ENABLED=false`). Group 1
collectors then skip entirely and appear in `collector_status.json` as
`status=skipped, reason=config_disabled`; group 2 collectors keep
running with their listed `SensitiveColumns` set to `NULL` in
persisted rows.

These collectors do **not** read user data, but the captured
text may contain:

- Application-authored SQL logic (a form of intellectual property).
- Inline constants or comments referencing internal terminology,
  ticket IDs, or table/column naming that some organisations consider
  sensitive.

`pg_monitor` is sufficient to read most definition text in a typical
deployment because the monitoring role does not need to *own* the
objects to call `pg_get_viewdef` and friends — but read access to the
underlying objects does need to be granted, either directly or
transitively. If your deployment locks down individual schemas with
`REVOKE ALL`, you may need to grant USAGE on the relevant schemas:

```sql
-- Optional: only required if specific schemas are locked down and
-- you need definition coverage for them.
GRANT USAGE ON SCHEMA <schema> TO arq_signals;
```

**There is no need to grant write access, ownership, or membership in
`pg_write_all_data` for the high-sensitivity pack.** If a deployment
appears to require those grants, the catalog query is wrong — open
an issue, do not weaken the role.

### Compliance considerations for the high-sensitivity pack

Operators evaluating SOC 2 / ISO 27001 readiness should treat the
high-sensitivity pack as data classification "Internal" or higher.
Specifically:

- The pack runs **by default** (R075 revised 2026-05:
  collect-everything default). Opting **out** is an explicit operator
  decision; the effective state is recorded in the export metadata
  under `high_sensitivity_collectors_enabled` (R078) so an auditor can
  tell at a glance whether sensitive data may be present in the
  artifact.
- When the operator has opted out:
  - Group 1 collectors (SQL definition text) are skipped — each
    appears in `collector_status.json` with `status=skipped` /
    `reason=config_disabled` for the audit trail.
  - Group 2 collectors (live `pg_stat_activity` query text) still run
    but emit `NULL` in their declared `SensitiveColumns`; the
    non-sensitive columns survive.
- Exports produced with the gate open will contain SQL definitions
  and live query text. Treat the resulting ZIP at the same
  classification level as the underlying schema definitions.

## TimescaleDB targets (R114)

The TimescaleDB collector family needs **no additional grants** on
top of the default role above. TimescaleDB ships its
`timescaledb_information` views with `GRANT SELECT TO PUBLIC` and no
row filtering, and the two helper functions the family calls
(`hypertable_approximate_detailed_size()`,
`hypertable_compression_stats()`) perform no table ACL checks — any
LOGIN role can read the full hypertable/chunk/compression/job
metadata surface.

The single exception is `timescaledb_information.job_errors` (and
`job_history`, which Elevarq Signals does not collect): these are
security-barrier views that show a row only when the connecting role
is a member of the **job owner's** role or the **database owner's**
role. With the standard `arq_signals` role,
`timescaledb_job_errors_v1` therefore returns **zero rows with
`status=success` — this is the expected least-privilege state, not a
failure.** Job failure *counters* remain fully visible to any role
via `timescaledb_job_stats_v1` (`total_failures`,
`last_run_status`).

Operators who want fleet-wide per-failure error detail (SQLSTATE +
message per failed run) can opt in by granting the collector role
membership in the database-owner role:

```sql
-- Optional. Widens job_errors visibility to all jobs in the database.
GRANT <database_owner_role> TO arq_signals;
```

Weigh this against least-privilege policy: database-owner membership
grants more than `job_errors` visibility. Most deployments should
skip it and rely on `job_stats` counters.

One operational caveat: the family calls the two helper functions
unqualified, so the TimescaleDB extension's API schema (default:
`public`) must be on the collector role's `search_path`. If the
extension was installed into a custom schema that the role does not
resolve, those two collectors fail with the structured
`reason=object_missing` (R115) while the rest of the family — and
the snapshot — continue normally.

## Verifying the role

After creating the role, verify the posture from inside `psql`:

```sql
SET ROLE arq_signals;

-- Should succeed:
SELECT count(*) FROM pg_stat_database;
SELECT count(*) FROM pg_stat_activity;

-- Should ALL fail with permission denied:
CREATE TABLE _arq_role_check (id int);
INSERT INTO pg_class VALUES (NULL);
SELECT pg_terminate_backend(1);

-- Should report 'on' in production:
SHOW default_transaction_read_only;
```

The first two queries demonstrate read access to monitoring views.
The next three demonstrate that the role cannot mutate state.

## Rotation

Elevarq Signals re-resolves the password on every new pool connection
(see `BeforeConnect` in the collector). Rotate the password by
updating the secret store; the next connection picks up the new
value. The daemon does not need to be restarted.

## Auditing the role

To verify what your monitoring role can actually do, run:

```sql
SELECT rolname, rolsuper, rolinherit, rolcreaterole,
       rolcreatedb, rolcanlogin, rolreplication, rolbypassrls,
       (SELECT array_agg(b.rolname)
          FROM pg_auth_members m
          JOIN pg_roles b ON m.roleid = b.oid
         WHERE m.member = r.oid) AS memberships
  FROM pg_roles r
 WHERE rolname = 'arq_signals';
```

Expected:
- `rolsuper`, `rolcreaterole`, `rolcreatedb`, `rolreplication`,
  `rolbypassrls` are all `f`.
- `memberships` includes `pg_monitor` and nothing that grants write
  access (`pg_write_all_data` is explicitly forbidden by R023).
