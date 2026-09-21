# pg_hba_file_rules_v1 — Collector Specification

## Purpose

Capture the cluster's host-based authentication (HBA) posture — one row
per `pg_hba.conf` rule — so downstream analysis can flag weak host-auth
(e.g. a `trust` or `password` method on a non-local address) by pointing
at *the specific* rule, not just reporting counts. Host-based
authentication was not collected at all before this collector; individual
transport-security GUCs (`ssl`, `ssl_min_protocol_version`,
`ssl_ciphers`) are already carried by `pg_settings_v1` and are therefore
NOT duplicated here (single source of truth). Unblocks Analyzer #1757
(`weak-host-auth`; `tls-not-enforced` reads the ssl GUCs from
`pg_settings_v1`).

## Catalog source

- `pg_catalog.pg_hba_file_rules` — the SQL-queryable HBA view. No
  filesystem access is needed or performed.

## Required grants

By default the view's `SELECT` is owner-only (superuser) and the
underlying `pg_hba_file_rules()` set-returning function's `EXECUTE` is
revoked from `PUBLIC`. Contrary to a common assumption, **neither
`pg_monitor` nor `pg_read_all_settings` grants access** (verified on
PG16). A non-superuser monitoring role therefore needs BOTH grants:

```sql
GRANT SELECT ON pg_catalog.pg_hba_file_rules TO <role>;
GRANT EXECUTE ON FUNCTION pg_catalog.pg_hba_file_rules() TO <role>;
```

Without both, the view raises permission-denied (SQLSTATE 42501) and the
collector degrades gracefully (see Failure Conditions) — so the default
`pg_monitor` deployment records this collector `skipped`, not `failed`,
until the operator adds the grants above.

## Output columns

| Column | Type | Description |
|---|---|---|
| rule_number | integer | Ordinal of the rule within the HBA file; NULL on PG < 16 (column added in PG16) |
| file_name | text | HBA file containing the rule; NULL on PG < 16 (column added in PG16) |
| line_number | integer | Line number of the rule in `file_name` |
| type | text | Connection type (`local`, `host`, `hostssl`, `hostnossl`, ...) |
| database | text[] | Database name(s) the rule matches (raw tokens, e.g. `all`, `replication`) |
| user_name | text[] | Role name(s) the rule matches (raw tokens, e.g. `all`) |
| address | text | Client address / hostname the rule matches, NULL for `local` |
| netmask | text | Netmask for `address`, when expressed separately |
| auth_method | text | Authentication method (`trust`, `scram-sha-256`, `md5`, `peer`, `cert`, ...) |
| options | text[] | Method options as `name=value` tokens |
| error | text | Non-NULL when PostgreSQL could not parse this rule line |

## Scope filter

- Emits every row returned by `pg_hba_file_rules` for the connected
  server. No filtering by database, role, or address.
- Rows with a non-NULL `error` (unparsable lines) are preserved — the
  parse error is itself signal.

## Invariants

- Deterministic ordering: `ORDER BY line_number` on PG ≤ 15, `ORDER BY
  rule_number` on PG ≥ 16.
- Stable output column order across majors: `rule_number` and `file_name`
  are emitted as typed NULL stubs on PG ≤ 15 (the #210 stub pattern) and
  as real columns via a version override on PG ≥ 16, so the column set is
  identical on every supported major.
- Read-only query; passes the collector linter.
- Array columns (`database`, `user_name`, `options`) are preserved as
  their raw `text[]` form; interpretation is the analyzer's job.

## Failure Conditions

- FC-01: The connecting role lacks read access to `pg_hba_file_rules`
  (see Required grants — superuser, or explicit `SELECT` on the view plus
  `EXECUTE` on `pg_hba_file_rules()`; `pg_monitor`/`pg_read_all_settings`
  are NOT sufficient). PostgreSQL returns a hard permission-denied
  (SQLSTATE 42501). Because the collector sets `PrivilegedViewDegrade`,
  this is recorded `status=skipped, reason=privilege_restricted` — an
  expected privilege boundary, not a fault — so the cycle is NOT reported
  partial (the "empty + completeness note" contract). The daemon logs a
  one-shot advisory naming the two grants required. Any non-permission
  error stays a real failure.

## Configuration

- Category: server
- Cadence: 24h (CadenceDaily)
- Retention: RetentionLong
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Low. The collector emits authentication *configuration* — connection
types, methods, matched database/role name tokens, and client
addresses/netmasks. Addresses, role names, and database names in HBA
rules are configuration, not secrets, and the same name class already
appears in `login_roles_v1`. The collector reads no passwords, password
hashes, secret material, or user table data. It is therefore not
`HighSensitivity` and carries no redaction columns.

## Analyzer requirements unblocked

- `weak-host-auth` (Analyzer #1757) — flag a specific non-local rule
  using a weak method (e.g. `trust`/`password` on `0.0.0.0/0`).
- `tls-not-enforced` (Analyzer #1757) — reads the transport-security GUCs
  from `pg_settings_v1`; this collector complements it with the HBA view
  (e.g. `host` vs `hostssl` rules).
