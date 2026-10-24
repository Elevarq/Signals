# MariaDB Monitoring Grants

## Status

ACTIVE

## Purpose

Define the least-privilege, read-only MariaDB role Signals uses to run the
MariaDB collector catalog (`specifications/mariadb-collectors.md`). Mirrors the
MySQL grants decision.

## Decision

- **Baseline monitoring role = `GRANT SELECT, PROCESS ON *.*`** — read-only,
  covers the entire MariaDB catalog. `PROCESS` is required so
  `mariadb_processlist_v1` surfaces all sessions, not just the monitor's own.
  `SELECT` covers the `information_schema.GLOBAL_VARIABLES` / `GLOBAL_STATUS` /
  `ENGINES` / `SCHEMATA` / `TABLES` / `PROCESSLIST` views the catalog reads.
- **No elevated/optional grant** is needed. Collectors that would require more
  than the baseline on MariaDB (InnoDB metrics, perf_schema replication, which
  are unavailable under the default config / baseline role) are **omitted** from
  the MariaDB catalog rather than shipped as failures.
- The user has no schema write privileges and never mutates the target (Signals
  safety baseline).

## Example

```sql
CREATE USER IF NOT EXISTS 'signals_ro'@'%' IDENTIFIED BY '<password>';
GRANT SELECT, PROCESS ON *.* TO 'signals_ro'@'%';
```

The password is supplied out of band (env / secret manager), never checked in.

## Traceability

Referenced by `specifications/mariadb-collectors.md`. No separate matrix rules —
a configuration-decision spec; the behavior is exercised by the MariaDB live
qualification (follow-up PR) and the deterministic catalog tests.
