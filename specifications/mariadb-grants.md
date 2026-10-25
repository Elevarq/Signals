# MariaDB Monitoring Grants

## Status

ACTIVE

## Purpose

Define the least-privilege, read-only MariaDB role Signals uses to run the MariaDB
collector catalog (`specifications/mariadb-collectors.md`, 15 collectors), and
characterize the cross-user / sensitive evidence each grant exposes
(Elevarq/Signals #626, audit finding F2). MariaDB's privilege + Performance Schema
behavior differs from MySQL, so it gets its own assessment — MySQL grants are not
assumed equivalent. Source-permission-first: the database grant is the primary
boundary; Signals-side redaction is defense in depth. PostgreSQL behavior is
unchanged.

## Empirical findings (live MariaDB 11.4)

| Grant | Collectors enabled |
|-------|--------------------|
| `GRANT SELECT ON *.*` | identity, global_variables, global_status, engines, schemata, table_statistics, innodb_metrics, innodb_transactions, innodb_locks, innodb_lock_waits, metadata_locks, statement_digests, replication_status, galera_status — **but** `information_schema.PROCESSLIST` / `INNODB_TRX` / `INNODB_LOCKS` show only the monitoring session's own rows |
| `+ GRANT PROCESS ON *.*` | the above with `mariadb_processlist_v1` / `mariadb_innodb_transactions_v1` / `mariadb_innodb_locks_v1` surfacing **all** sessions/transactions/locks |

The full 15-collector catalog runs green under `SELECT, PROCESS` (the
`integration-mariadb` live qualification: all collectors `status=success`). No
elevated grant is needed.

## MariaDB-distinct behavior

- `GLOBAL_VARIABLES` / `GLOBAL_STATUS` and the InnoDB transaction/lock views
  (`INNODB_TRX`, `INNODB_LOCKS`, `INNODB_LOCK_WAITS`) are in `information_schema`
  (MySQL 8 moved these / removed the lock views), all `SELECT`-readable.
- `performance_schema` is **OFF by default** on MariaDB, so
  `mariadb_metadata_locks_v1` and `mariadb_statement_digests_v1` return a
  legitimate empty result until an operator enables it — a capability gate, not a
  grant gate, and not a failure.
- MariaDB has **Galera (wsrep)**, not Group Replication; `mariadb_galera_status_v1`
  reads non-sensitive cluster membership/state counters from
  `information_schema.GLOBAL_STATUS`.

## Decision

- **Baseline monitoring role = `GRANT SELECT, PROCESS ON *.*`** — read-only,
  covers the entire MariaDB catalog. `PROCESS` is the one privilege beyond
  `SELECT`; read-only monitoring, no mutation.
- **No elevated/optional grant** (unlike SingleStore's `CLUSTER`). perf_schema-off
  collectors simply return empty.
- The role has no schema write privilege and never mutates the target.

## Cross-user / sensitive exposure (what each grant makes visible)

`PROCESS` broadens visibility from the collector's own session to **every**
session/account. With `PROCESS`, these collectors can surface other users' SQL and
locked data:

| Collector | Sensitive field | What it exposes | Default handling |
|-----------|-----------------|-----------------|------------------|
| `mariadb_processlist_v1` | `INFO` | the live SQL text of every session | **HighSensitivity → redacted** |
| `mariadb_innodb_transactions_v1` | `trx_query` | the live SQL of every active transaction | **HighSensitivity → redacted** |
| `mariadb_innodb_locks_v1` | `lock_data` | the locked index-key values (row data) | **HighSensitivity → redacted** |
| `mariadb_statement_digests_v1` | `DIGEST_TEXT` | normalized sample SQL per digest (perf_schema-gated) | **HighSensitivity → redacted** |

Non-SQL metadata (schema/table names, engine/version, counters, GTID/replication
state, Galera membership, account user/host) is native evidence. Redaction nils
the sensitive columns by default; it is **defense in depth** — the `PROCESS` grant
is the primary control over whether cross-user SQL is reachable at all.

## Rules

| ID | Rule |
|----|------|
| **MDG-R001** | Baseline role = `GRANT SELECT, PROCESS ON *.*`; read-only, covers all 15 collectors. No write/admin/superuser privilege is ever required. |
| **MDG-R002** | `PROCESS` is the only privilege beyond `SELECT`; it broadens `PROCESSLIST` / `INNODB_TRX` / `INNODB_LOCKS` from own-session to all sessions. Without it those are own-session only (not denied); the rest of the catalog is unaffected. |
| **MDG-R003** | Every collector that can surface cross-user SQL or locked row data (`INFO`, `trx_query`, `lock_data`, `DIGEST_TEXT`) is `HighSensitivity` and redacted by default (defense in depth); the DB grant is the primary boundary. |
| **MDG-R004** | No elevated/optional grant is required; perf_schema-off (`metadata_locks`, `statement_digests`), standalone-replication, and Galera-not-connected states are legitimate empty results, never failures. |

## Example

```sql
CREATE USER IF NOT EXISTS 'signals_ro'@'%' IDENTIFIED BY '<password>';
GRANT SELECT, PROCESS ON *.* TO 'signals_ro'@'%';
```

Operator privacy note: granting `PROCESS` lets the monitoring role observe every
session's live SQL, active-transaction SQL, and locked row-key values (all
redacted by Signals before persistence). Omitting `PROCESS` limits that to the
collector's own session. The password is supplied out of band, never checked in.

## Traceability

Acceptance: `specifications/mariadb-grants.acceptance.md`. MDG-R003's redaction
surface is enforced by the catalog guard
(`TestMariaDBCatalogNoUnredactedSensitiveColumn`, MDC-R004/R008); the
SELECT+PROCESS grant model is exercised by the `integration-mariadb` live
qualification (15/15 status=success). Pairs with `mariadb-collectors.md`.
