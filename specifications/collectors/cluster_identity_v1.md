# cluster_identity_v1 — Collector Specification

## Purpose

Single-row fingerprint of the PostgreSQL **cluster** (the running
postmaster), distinct from `server_identity_v1` which fingerprints the
software version and connected database context.

Two same-named databases on different physical clusters cannot be
distinguished by `server_identity_v1` alone — `database_name` and
`version_num` may be identical. `cluster_identity_v1` captures the
network-bound and cluster-level fields needed for end-to-end
disambiguation when an exported snapshot is read outside the
collecting daemon.

Complements (does not replace) `server_identity_v1`.

## Catalog source

Composite — built-in catalog helpers only:

- `inet_server_addr()`
- `inet_server_port()`
- `pg_is_in_recovery()`
- `current_setting('cluster_name')`
- `current_setting('TimeZone')`
- `pg_last_wal_receive_lsn()` (NULL on primary)
- `pg_last_wal_replay_lsn()` (NULL on primary)
- `pg_postmaster_start_time()`

## Output columns

One row.

| Column | Type | Description |
|---|---|---|
| inet_server_addr | inet \| NULL | `inet_server_addr()`. NULL when client connects via unix socket. |
| inet_server_port | int \| NULL | `inet_server_port()`. NULL when client connects via unix socket. |
| is_in_recovery | boolean | `pg_is_in_recovery()`. True on standby / replica. |
| cluster_name | text \| NULL | `current_setting('cluster_name')`. NULL when unset (empty string coalesced to NULL). |
| server_timezone | text | `current_setting('TimeZone')`. Always present. |
| last_wal_receive_lsn | pg_lsn \| NULL | `pg_last_wal_receive_lsn()`. NULL on primary. |
| last_wal_replay_lsn | pg_lsn \| NULL | `pg_last_wal_replay_lsn()`. NULL on primary. |
| postmaster_start_time | timestamptz | `pg_postmaster_start_time()`. Duplicates `server_identity_v1.started_at` so the cluster-identity row is self-contained. |

The composite key `(inet_server_addr, inet_server_port, postmaster_start_time)`
is sufficient for cluster disambiguation in v1. A truly immutable
cluster fingerprint via `pg_control_system().system_identifier` is
out of scope here — it would require `pg_read_all_stats` /
`pg_monitor` membership and the query linter prevents expressing
the standard `has_function_privilege(..., 'EXECUTE')` graceful-fallback
pattern. A separate optional collector can be added later if
operators need the immutable identifier (see § Out of scope).

## Scope filter

Single-row output. No filter.

## Invariants

- Exactly one row per target per sample.
- Read-only — every function is a built-in catalog helper.
- The collector MUST NOT fail when the connection is via unix socket;
  `inet_server_addr` and `inet_server_port` are NULL in that case and
  the collector's `status` remains `success`.
- Empty-string `cluster_name` is coalesced to NULL.
- No password, secret, or credential material appears in any output
  column.
- Passes linter.

## Failure Conditions

- **FC-01**: Connection via unix socket — `inet_server_addr()` and
  `inet_server_port()` return NULL by design. The collector's
  `status` is `success`.
- **FC-02**: Standard collector error path applies to query-level
  faults (timeout, connection drop) — handled by the existing
  collector execution model; not unique to this collector.

## Configuration

- Category: server
- Cadence: 6h (`Cadence6h`)
- Retention: `RetentionLong`
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes
- Sensitivity: low (no credential material; identity fields are
  derivable from any connected client)

## Sensitivity

Low. Every field is derivable by any connected role from built-in
catalog helpers. The collector does not read passwords, role hashes,
connection strings, or user data.

## Analyzer requirements unblocked

- **Cluster disambiguation** — end-to-end identification of the
  collecting cluster, even when two databases share a name across
  servers (e.g. `app` on `db-prod` and `db-staging`).
- **Primary/replica classification** — `is_in_recovery` plus
  `last_wal_receive_lsn` / `last_wal_replay_lsn` lets the analyzer
  distinguish a replica from a primary without inferring it from
  collector activity.
- **Composite cluster key** — `(inet_server_addr, inet_server_port,
  postmaster_start_time)` is the v1 disambiguation key. Stable for
  the lifetime of a postmaster process; changes on restart only via
  `postmaster_start_time`, which the analyzer can treat as a cluster
  re-incarnation event.

## Known constraints

- `pg_last_wal_receive_lsn()` / `pg_last_wal_replay_lsn()` exist as
  named in PG 10+. The pre-PG-10 names (`pg_last_xlog_*`) are not
  supported; arq-signals' min PG version is 10 (R024 / R081).

## Out of scope

- **`pg_control_system().system_identifier`** — the immutable cluster
  fingerprint. Gated by `pg_read_all_stats` / `pg_monitor` membership
  on most PG installs. The query linter (`internal/pgqueries/linter.go`)
  blocks the standard `has_function_privilege(..., 'EXECUTE')`
  graceful-fallback pattern, and including the call unguarded would
  cause the whole collector to fail with `permission_denied` on
  unprivileged roles, costing the operator the other fields too.
  A separate optional collector (e.g. `cluster_system_identifier_v1`)
  can be added in a follow-up if operators need the immutable
  identifier; deferred until there is a concrete need.
- Hostname / FQDN resolution (would require DNS round-trip).
- `pg_controldata` filesystem read (requires filesystem access; not
  available via SQL on managed services anyway).
- Vendor-specific cluster identity (e.g. AWS instance ID, GCP
  instance name) — that's analyzer-side enrichment via
  `extension_inventory_v1` and `login_roles_v1` (see
  `server_identity_v1` § Analyzer requirements unblocked).
