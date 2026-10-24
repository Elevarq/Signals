# MySQL Monitoring Grants

## Status

ACTIVE

## Purpose

Define the least-privilege, read-only MySQL role Signals uses to run the MySQL
collector catalog (`specifications/mysql-collectors.md`). Mirrors the SingleStore
grants decision: a baseline monitoring role that covers the catalog, with any
heavier privilege called out explicitly.

## Empirical findings (live MySQL 8.4 Community Server)

| Grant | Collectors enabled |
|-------|--------------------|
| `GRANT SELECT ON *.*` | identity, global_variables, global_status, innodb_metrics, table_statistics, engines, schemata, replication_status (perf_schema is SELECT-readable) |
| `+ GRANT PROCESS ON *.*` | the above **+ full `information_schema.PROCESSLIST`** — without `PROCESS`, `PROCESSLIST` shows only the monitoring session's own rows |

`REPLICATION CLIENT` is **not** required: `mysql_replication_status_v1` reads
`performance_schema.replication_connection_status`, which a `SELECT`-granted role
can read (it returns zero rows on a standalone server — a legitimate empty
result, not a failure).

## Decision

- **Baseline monitoring role = `GRANT SELECT, PROCESS ON *.*`** — read-only,
  covers the entire MySQL catalog. `PROCESS` is required so
  `mysql_processlist_v1` surfaces all sessions (operational visibility), not just
  the collector's own.
- **No elevated/optional grant** is needed for the MySQL catalog (unlike
  SingleStore's `CLUSTER` for `MV_CLUSTER_STATUS`). A standalone server simply
  returns an empty replication result.
- The user is created with no schema write privileges and is never used to
  mutate the target (Signals safety baseline).

## Example

```sql
CREATE USER IF NOT EXISTS 'signals_ro'@'%' IDENTIFIED BY '<password>';
GRANT SELECT, PROCESS ON *.* TO 'signals_ro'@'%';
```

The password is supplied out of band (env / secret manager), never checked in.

## Traceability

Referenced by `specifications/mysql-collectors.md`. No separate matrix rules —
this is a configuration-decision spec; the behavior it governs is exercised by
the MySQL live qualification (follow-up PR) and the deterministic catalog tests
(`internal/singlestore` MySQL catalog tests).
