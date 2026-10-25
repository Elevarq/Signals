# Non-PostgreSQL Version Support Policy

## Status

ACTIVE

## Purpose

Define, per engine, which versions Signals **qualifies**, treats as
**best-effort**, or considers **unsupported**, and how **unknown** versions are
handled (Elevarq/Signals #625, audit finding F9). One passing CI image does not
establish compatibility across a vendor's whole version history, and the four
non-PostgreSQL engines have genuinely different release/support models — this
policy states each honestly rather than pretending they are uniform. PostgreSQL's
established support matrix is unchanged and referenced, not expanded.

## Support tiers

| Tier | Meaning |
|------|---------|
| **Qualified** | Run live in CI against an exact pinned image with every collector `status=success` through the real daemon → exporter. Linked to the image + result below. |
| **Best-effort** | Expected to work (driver + collector SQL are compatible) but not pinned in CI. Collected normally; the snapshot's `engine_version` records the real version; **not** claimed as qualified. |
| **Unsupported** | Known-incompatible (missing driver support or catalog views the collectors require). |
| **Unknown** | Any version not matched above: collected **best-effort** (never rejected at runtime), `engine_version` recorded truthfully; a view a collector needs that is absent degrades to a legitimate empty/skipped result (never silently wrong). |

## Per-engine matrix (each vendor on its own terms)

### PostgreSQL (community major-version model)
- **Qualified:** 14, 15, 16, 17, 18 — the `integration-pg` CI matrix (live service
  containers `postgres:14`…`18`), the established 1.x support matrix. Unchanged by
  this policy.

### MySQL (LTS + Innovation model; driver: go-sql-driver/mysql)
- **Qualified:** **8.0** (`mysql@sha256:7dcddc01…`, 8.0.46) **and 8.4 LTS**
  (`mysql@sha256:76f03588…`, 8.4.x) — both live in the `integration-mysql` CI
  matrix, all 16 collectors `status=success`. 8.0 is evaluated and qualified
  explicitly; a green 8.4 image alone never established 8.0.
- **Best-effort:** current forward/Innovation releases (8.x > 8.4) — the catalog
  SQL is version-stable, but not pinned in CI.
- **Unsupported:** < 8.0 — the activity/lock collectors rely on
  `performance_schema.data_locks` / `data_lock_waits` and the digest/replication
  views introduced in 8.0; earlier lines lack them.

### MariaDB (rolling + LTS model; distinct from MySQL — never inferred from it)
- **Qualified:** 11.4 (`mariadb@sha256:250e1609…`, 11.4.x) — live in
  `integration-mariadb`, all 15 collectors `status=success`.
- **Best-effort:** other maintained 10.x / 11.x lines. MariaDB's `information_schema`
  (InnoDB views, GLOBAL_*), `performance_schema`-off-by-default behavior, and
  Galera differ from MySQL, so MySQL qualification says **nothing** about MariaDB.
- **Unsupported:** lines without the `information_schema.INNODB_*` views the
  catalog reads.

### SingleStore (SingleStoreDB versioning; MySQL-wire)
- **Qualified:** 9.1.x (pinned Dev Image `@sha256:a1382fb7…`, `@@memsql_version`
  9.1.1) — live in `integration-singlestore`, all 15 collectors `status=success`.
- **Best-effort:** other maintained 8.x / 9.x releases (the `MV_*` catalog is
  stable across them, but not pinned in CI).

### ClickHouse (calendar-versioned LTS/stable; native protocol via clickhouse-go)
- **Qualified:** 24.8 (`clickhouse/clickhouse-server@sha256:1ffa82ed…`, 24.8.x) —
  live in `integration-clickhouse`, all 14 collectors `status=success`.
- **Best-effort:** other maintained stable/LTS releases (the `system.*` surface is
  stable across them, but not pinned in CI).

## Rules

- **VSP-R001** | "Qualified" means live-CI-qualified against an exact pinned image
  with every collector `status=success`; the matrix links each qualified range to
  its CI image + result. No version is claimed qualified without that evidence
  (INV-VSP-01).
- **VSP-R002** | MySQL **8.0 and 8.4 are both qualified** as separate pinned live
  legs of `integration-mysql`; neither image qualifies the rest of the 8.x line.
- **VSP-R003** | Each engine's policy is independent: MariaDB is never inferred
  from MySQL; SingleStore and ClickHouse are stated from their own qualified
  release + upstream status, not stale assumptions.
- **VSP-R004** | Unknown / best-effort / older-than-qualified versions are
  collected **best-effort** — never rejected at runtime (no blanket version gate)
  — with `engine_version` recorded truthfully; a collector whose view is absent on
  that version degrades to a legitimate empty/skipped result, never silently
  wrong. They are never reported as fully qualified without CI evidence.
- **VSP-R005** | The engine support docs, configuration validation, and the CI
  matrix MUST agree; this spec is the single source of truth they cite.

## Invariants

- **INV-VSP-01** | No qualification claim exists without a pinned live-CI image and
  a green result.
- **INV-VSP-02** | Signals never silently downgrades evidence: a version-specific
  missing view is an explicit empty/skipped collector, never a fabricated or
  partial success reported as complete.

## Analyzer dependency

None for defining the Signals-side policy. Changing any **report-level**
unsupported-version status contract (how the consumer treats a best-effort/
unsupported version) requires Analyzer coordination and is out of scope here.

## Traceability

Acceptance: `specifications/version-support-policy.acceptance.md`. VSP-R002 is
CI-gated by `TestMySQLVersionMatrixQualifies80And84` (parses `ci.yml`). The
per-engine qualified rows are exercised by the `integration-*` live
qualifications. The MySQL support page (`mysql-support.md`, #547) and the
`integration-mysql` matrix cite this spec (VSP-R005).
