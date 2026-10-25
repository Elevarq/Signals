# MariaDB Support

## Status

ACTIVE

## Purpose

Operator-facing support page for MariaDB as a Signals target (Elevarq/Signals #638):
engine identification, version policy, configuration, least-privilege grants,
native workload/storage evidence, and known limitations. It is an INDEX that
cites the authoritative specs — it does not restate (and cannot drift from) the
cross-engine version matrix or the grant model, which are owned elsewhere
(VSP-R005). It agrees with those specs by construction.

## Engine identification

A MariaDB target is identified by a **positive signal**, never inferred from port
or wire protocol: `internal/engineid` resolves `VERSION()` / `@@version_comment`
(MariaDB servers carry the `MariaDB` marker). MySQL, MariaDB, and SingleStore all
speak the MySQL wire protocol but resolve to **distinct** engine identities
(`singlestore-engine-identity.md`); MariaDB is never treated as MySQL. The
snapshot records `engine=mariadb` and the real server version as `engine_version`.

## Version policy

Owned by `specifications/version-support-policy.md`. Principle: Signals qualifies
the current maintained MariaDB **LTS** lines, each a separate pinned live leg;
MariaDB qualification is **independent of MySQL** and one LTS line never implies
another. For MariaDB:

- **Qualified (live CI):** MariaDB **10.11 LTS** (10.11.19), **11.4 LTS** (11.4.13),
  **11.8 LTS** (11.8.9), and **12.3 LTS** (12.3.3) — each a pinned digest in the
  `integration-mariadb` matrix, all 15 collectors `status=success`. Verified live
  across all four with no version-specific SQL needed.
- **Best-effort:** other maintained 10.x / 11.x / 12.x releases and non-pinned LTS
  patch releases. The MariaDB-native catalog SQL is stable across these, but they
  are not pinned in CI.
- **Unsupported:** 10.6 and earlier (10.6 reached upstream EOL 2026-07-06); any
  line lacking the `information_schema.INNODB_*` views the catalog reads.
- **Unknown versions** are collected best-effort with the real `engine_version`
  recorded; never claimed qualified without CI evidence.

## Configuration

- Connection defaults are engine-aware (`engine-connection-defaults.md`): MariaDB
  uses port **3306**; an explicit port/database always wins; `dbname` is optional
  for MariaDB (server-wide monitoring). Env (`SIGNALS_TARGET_ENGINE=mariadb`) and
  YAML produce equivalent resolved targets.
- TLS: PostgreSQL-style `sslmode` (verify-ca / verify-full verify against the
  configured CA; fail-closed) via the shared MySQL-wire path
  (`singlestore-connection-hardening.md`).

## Least-privilege grants

Owned by `specifications/mariadb-grants.md`: baseline `GRANT SELECT, PROCESS ON *.*`
(read-only, no write/admin). `PROCESS` broadens `PROCESSLIST` / `INNODB_TRX` to all
sessions (the cross-user exposure). Cross-user SQL / locked data
(`INFO` / `trx_query` / `DIGEST_TEXT` / `lock_data`) is HighSensitivity + redacted
by default (defense in depth). No elevated grant required; verified sufficient on
all four qualified LTS lines.

## Native workload / storage evidence

The 15-collector catalog (`mariadb-collectors.md`) is **MariaDB-native**, not a copy
of MySQL's: identity, config/status via `information_schema.GLOBAL_VARIABLES` /
`GLOBAL_STATUS` (MySQL 8 moved these to `performance_schema`), InnoDB metrics
(`ENABLED=1`, not MySQL's `STATUS='enabled'`), schema/table sizing, processlist,
InnoDB transactions, **`information_schema.INNODB_LOCKS` / `INNODB_LOCK_WAITS`**
(which MySQL 8.0 removed and MariaDB retains), metadata locks + statement digests
(Performance Schema, off by default → legitimate empty), GTID-based replication
state, and Galera (wsrep) cluster status. The workload identity is the MariaDB
`DIGEST` (`workload-measurement-model.md`), never equated with a PostgreSQL
`queryid` or another engine's identity.

## Known limitations

- `performance_schema` is **off by default** on MariaDB, so `metadata_locks` and
  the statement-digest workload collection return a legitimate empty result until
  an operator enables it — not a failure.
- MariaDB has **Galera (wsrep)**, not MySQL Group Replication, and exposes replica
  state via **GTID server variables**, not a `SHOW SLAVE STATUS` table / the MySQL
  `performance_schema.replication_*` views; these collectors return the standalone
  state on a non-clustered/standalone server.
- Signals is read-only; it never requires a write/admin grant and never mutates
  the target.

## Rules

| ID | Rule |
|----|------|
| **MDS-R001** | This support page carries no independent support claim: the version matrix, grants, defaults, collectors, and identity are owned by the cited specs and this page agrees with them (VSP-R005). |
| **MDS-R002** | MariaDB support claims distinguish qualified (live CI: 10.11 + 11.4 + 11.8 + 12.3 LTS) from best-effort and unsupported, per `version-support-policy.md`; one image never qualifies another LTS line, and MySQL qualification never implies MariaDB (VSP-R006). |

## Traceability

Acceptance: `specifications/mariadb-support.acceptance.md`. Cites
`version-support-policy.md` (VSP), `mariadb-grants.md` (MDG),
`mariadb-collectors.md` (MDC), `engine-connection-defaults.md` (ECONN),
`workload-measurement-model.md` (WMM), `singlestore-engine-identity.md`.
