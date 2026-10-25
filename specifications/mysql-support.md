# MySQL Support

## Status

ACTIVE

## Purpose

Operator-facing support page for MySQL as a Signals target (Elevarq/Signals #547):
engine identification, version policy, configuration, least-privilege grants,
native workload/storage evidence, and known limitations. It is an INDEX that
cites the authoritative specs — it does not restate (and cannot drift from) the
cross-engine version matrix or the grant model, which are owned elsewhere
(VSP-R005). It agrees with those specs by construction.

## Engine identification

A MySQL target is identified by a **positive signal**, never inferred from port or
wire protocol: `internal/engineid` resolves `VERSION()` / `@@version_comment` /
`@@version_compile_os`. MySQL, MariaDB (distinct), and SingleStore all speak the
MySQL wire protocol but resolve to distinct engine identities
(`singlestore-engine-identity.md`). The snapshot records `engine=mysql` and the
real server version as `engine_version`.

## Version policy

Owned by `specifications/version-support-policy.md`. Principle: Signals qualifies
the relevant MySQL **LTS** releases and may additionally qualify selected widely
deployed **legacy** releases; **Innovation** releases are best-effort unless
explicitly qualified. For MySQL:

- **Qualified (live CI):** MySQL **8.0** (8.0.46, legacy), **8.4 LTS**, and
  **9.7 LTS** (9.7.4) — each a pinned digest in the `integration-mysql` matrix,
  all 16 collectors `status=success`.
- **Best-effort:** the Innovation track (calendar-versioned since 26.7, e.g.
  26.x) and non-pinned LTS patch releases. 9.7 qualification does not imply 26.x.
- **Unsupported:** < 8.0 (the activity/lock/digest/replication collectors require
  `performance_schema` views introduced in 8.0).
- **Unknown versions** are collected best-effort with the real `engine_version`
  recorded; never claimed qualified without CI evidence.

## Configuration

- Connection defaults are engine-aware (`engine-connection-defaults.md`): MySQL
  uses port **3306**; an explicit port/database always wins; `dbname` is optional
  for MySQL (server-wide monitoring). Env (`SIGNALS_TARGET_ENGINE=mysql`) and YAML
  produce equivalent resolved targets.
- TLS: PostgreSQL-style `sslmode` (verify-ca / verify-full verify against the
  configured CA; fail-closed) via the shared MySQL-wire path
  (`singlestore-connection-hardening.md`).

## Least-privilege grants

Owned by `specifications/mysql-grants.md`: baseline `GRANT SELECT, PROCESS ON *.*`
(read-only, no write/admin). `PROCESS` broadens `PROCESSLIST`/`INNODB_TRX` to all
sessions (the cross-user exposure). Cross-user SQL / locked data
(`INFO`/`trx_query`/`DIGEST_TEXT`/`LOCK_DATA`) is HighSensitivity + redacted by
default (defense in depth). No elevated grant required.

## Native workload / storage evidence

The 16-collector catalog (`mysql-collectors.md`) covers identity, config/status,
InnoDB metrics, schema/table sizing, processlist, replication + Group Replication
state, InnoDB transactions / data-locks / lock-waits / metadata-locks, and the
Performance Schema statement-digest workload (`events_statements_summary_by_digest`).
The workload identity is the MySQL `DIGEST` (`workload-measurement-model.md`), never
equated with a PostgreSQL `queryid` or another engine's identity.

## Known limitations

- `performance_schema` statement-digest collection is empty when Performance
  Schema instrumentation is off — a legitimate empty result, not a failure.
- `data_locks` / `data_lock_waits` require MySQL 8.0+ (hence the <8.0 unsupported
  line).
- Replication / Group Replication collectors return empty on a standalone server.
- Signals is read-only; it never requires a write/admin grant and never mutates
  the target.

## Rules

| ID | Rule |
|----|------|
| **MYS-R001** | This support page carries no independent support claim: the version matrix, grants, defaults, collectors, and identity are owned by the cited specs and this page agrees with them (VSP-R005). |
| **MYS-R002** | MySQL support claims distinguish qualified (live CI: 8.0 + 8.4 + 9.7) from best-effort and unsupported, per `version-support-policy.md`; one image never qualifies the whole 8.x line. |

## Traceability

Acceptance: `specifications/mysql-support.acceptance.md`. Cites
`version-support-policy.md` (VSP), `mysql-grants.md` (MYG),
`mysql-collectors.md` (MYC), `engine-connection-defaults.md` (ECONN),
`workload-measurement-model.md` (WMM), `singlestore-engine-identity.md`.
