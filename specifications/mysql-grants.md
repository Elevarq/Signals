# MySQL Monitoring Grants

## Status

ACTIVE

## Purpose

Define the least-privilege, read-only MySQL role Signals uses to run the MySQL
collector catalog (`specifications/mysql-collectors.md`, 16 collectors), and
characterize the cross-user / sensitive evidence each grant exposes
(Elevarq/Signals #545, audit finding F2). Source-permission-first: the database
grant is the primary collection boundary; Signals-side redaction is defense in
depth, not a substitute. PostgreSQL behavior is unchanged.

## Empirical findings (live MySQL 8.4 Community Server)

| Grant | Collectors enabled |
|-------|--------------------|
| `GRANT SELECT ON *.*` | identity, global_variables, global_status, innodb_metrics, table_statistics, engines, schemata, replication_status, statement_digests, innodb_transactions, data_locks, data_lock_waits, metadata_locks, replica_applier_status, group_replication_members — **but** `information_schema.PROCESSLIST` / `INNODB_TRX` show only the monitoring session's own rows |
| `+ GRANT PROCESS ON *.*` | the above with `mysql_processlist_v1` and `mysql_innodb_transactions_v1` surfacing **all** sessions/transactions (operational visibility) |

The full 16-collector catalog runs green under `SELECT, PROCESS` (the
`integration-mysql` live qualification: all collectors `status=success`).
`REPLICATION CLIENT` is **not** required; the replication collectors read
`performance_schema` views a `SELECT`-granted role can read (zero rows on a
standalone server = a legitimate empty result).

## Decision

- **Baseline monitoring role = `GRANT SELECT, PROCESS ON *.*`** — read-only,
  covers the entire MySQL catalog. `PROCESS` is the one privilege beyond `SELECT`;
  it is a read-only monitoring privilege (no data mutation).
- **No elevated/optional grant** is needed (unlike SingleStore's `CLUSTER`). A
  standalone server returns empty replication/Group-Replication results.
- The role has no schema write privilege and never mutates the target (Signals
  safety baseline).

## Cross-user / sensitive exposure (what each grant makes visible)

`PROCESS` is the privilege that broadens visibility from the collector's own
session to **every** session/account on the server. With `PROCESS`, these
collectors can surface other users' SQL and locked data:

| Collector | Sensitive field | What it exposes | Default handling |
|-----------|-----------------|-----------------|------------------|
| `mysql_processlist_v1` | `INFO` | the live SQL text of every session | **HighSensitivity → redacted** |
| `mysql_innodb_transactions_v1` | `trx_query` | the live SQL of every active transaction | **HighSensitivity → redacted** |
| `mysql_statement_digests_v1` | `DIGEST_TEXT` | the normalized sample SQL per digest (cross-account workload) | **HighSensitivity → redacted** |
| `mysql_data_locks_v1` | `LOCK_DATA` | the locked index-key values (row data) | **HighSensitivity → redacted** |

Non-SQL metadata (schema/table names, engine/version, counters, replication
channel/GTID state, account user/host in `PROCESSLIST`) is collected as native
evidence. Redaction nils the sensitive columns by default (when high-sensitivity
collection is disabled); it is **defense in depth** — the `PROCESS` grant is the
primary control over whether cross-user SQL is reachable at all.

## Rules

| ID | Rule |
|----|------|
| **MYG-R001** | Baseline role = `GRANT SELECT, PROCESS ON *.*`; read-only, covers all 16 collectors. No write/admin/superuser privilege is ever required. |
| **MYG-R002** | `PROCESS` is the only privilege beyond `SELECT`; it broadens `PROCESSLIST` / `INNODB_TRX` from own-session to all sessions. Without it those are own-session only (not denied) and the rest of the catalog is unaffected. |
| **MYG-R003** | Every collector that can surface cross-user SQL or locked row data (`INFO`, `trx_query`, `DIGEST_TEXT`, `LOCK_DATA`) is `HighSensitivity` and redacted by default (defense in depth); the DB grant is the primary boundary. |
| **MYG-R004** | No elevated/optional grant is required; replication/Group-Replication absence is a legitimate empty result, never a failure. |

## Example

```sql
CREATE USER IF NOT EXISTS 'signals_ro'@'%' IDENTIFIED BY '<password>';
GRANT SELECT, PROCESS ON *.* TO 'signals_ro'@'%';
```

Operator privacy note: granting `PROCESS` lets the monitoring role observe every
session's live SQL and active-transaction SQL (redacted by Signals before
persistence). Omitting `PROCESS` limits that to the collector's own session at
the cost of operational visibility. The password is supplied out of band (env /
secret manager), never checked in.

## Traceability

Acceptance: `specifications/mysql-grants.acceptance.md`. MYG-R003's redaction
surface is enforced by the catalog redaction guard
(`TestMySQLCatalogNoUnredactedSensitiveColumn`, MYC-R004/R008); the SELECT+PROCESS
grant model is exercised by the `integration-mysql` live qualification (16/16
status=success). Pairs with `mysql-collectors.md`.
