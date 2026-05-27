# wraparound_db_level_v1 — Collector Specification

## Purpose

Per-database wraparound-relevant signals: transaction-ID freeze
age, multixact freeze age, and the two autovacuum freeze-threshold
GUCs for headroom computation. Core evidence for
`xid-wraparound-risk` at the database level.

## Catalog source

- `pg_database` filtered to `datallowconn`
- `current_setting('autovacuum_freeze_max_age')`
- `current_setting('autovacuum_multixact_freeze_max_age')`
- `age(datfrozenxid)`, `mxid_age(datminmxid)`

## Output columns

| Column | Type | Description |
|---|---|---|
| datname | text | Database name |
| db_xid_age | bigint | `age(datfrozenxid)` |
| freeze_max_age | bigint | `autovacuum_freeze_max_age` GUC |
| db_mxid_age | bigint | `mxid_age(datminmxid)` |
| multixact_freeze_max_age | bigint | `autovacuum_multixact_freeze_max_age` GUC |

## Scope filter

`WHERE datallowconn` — excludes `template0` and any disconnected
databases (they cannot trigger autovacuum and do not need freeze
monitoring).

## Invariants

- Deterministic ordering: `ORDER BY age(datfrozenxid) DESC`.
- Stable output column order.
- Read-only, passes linter.
- `current_setting()` is safe under restricted roles.

## Failure Conditions

- FC-01: Permission denied on `pg_database` (unusual) → standard
  collector error path.

## Configuration

- Category: wraparound
- Cadence: 24h (CadenceDaily) — wraparound is a slow-moving concern
- Retention: RetentionMedium
- Min PG version: 10 (for mxid_age)
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Low.

## Analyzer requirements unblocked

- `xid-wraparound-risk` — per-database freeze headroom.
