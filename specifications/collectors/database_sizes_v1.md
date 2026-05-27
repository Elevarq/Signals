# database_sizes_v1 — Collector Specification

## Purpose

Per-database sizes, wraparound context (XID and MXID freeze age),
connection limits, encoding, locale, default tablespace, and owner.
Extended from its original size+connection+xid scope to supersede a
proposed `pg_database_v1` collector — the retired `pg_database_v1`
spec is folded in here.

## Catalog source

- `pg_database` joined with `pg_tablespace`
- `pg_database_size(datname)`, `age(datfrozenxid)`, `mxid_age(datminmxid)`
- `pg_encoding_to_char(encoding)` for human-readable encoding

## Output columns

| Column | Type | Description |
|---|---|---|
| datid | oid | Database OID |
| database_name | text | `datname` — retained alias for backward compatibility |
| datdba_oid | oid | Owner role OID |
| encoding_name | text | Client encoding (`UTF8`, `LATIN1`, ...) |
| datcollate | text | Default collation |
| datctype | text | Default character type |
| datallowconn | boolean | Connections allowed |
| tablespace_name | text | Default tablespace (NULL if no join match — unusual) |
| connection_limit | int | `datconnlimit` (-1 = unlimited) |
| xid_age | bigint | `age(datfrozenxid)` |
| dat_minmxid_age | bigint | `mxid_age(datminmxid)` |
| size_bytes | bigint | `pg_database_size(datname)` |

## Scope filter

`WHERE datistemplate = false` — templates (`template0`, `template1`)
excluded. This is unchanged from the original collector; changing it
would affect downstream consumers that expect non-template-only rows.

## Invariants

- Deterministic ordering: `ORDER BY pg_database_size(datname) DESC`
  — largest first for triage.
- Stable output column order.
- Read-only, passes linter.
- Column aliases `database_name`, `connection_limit`, `xid_age` are
  retained verbatim from the original collector for backward
  compatibility; all additional columns use the canonical names from
  the retired `pg_database_v1` spec.

## Failure Conditions

- FC-01: `pg_database_size()` cannot stat a database the role
  cannot connect to (rare on hyperscalers) → NULL for that row's
  `size_bytes`; row still emitted.
- FC-02: Permission denied on `pg_database` (unusual — world-readable
  by default) → standard collector error path.

## Configuration

- Category: server
- Cadence: 1h (Cadence1h)
- Retention: RetentionMedium
- Min PG version: 10 (mxid_age is available since PG 9.3)
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Low.

## Relationship to retired `pg_database_v1`

`pg_database_v1` was drafted to provide fuller per-database catalog
metadata than the original `database_sizes_v1`. Per the project
decision (2026-04-24), `database_sizes_v1` is extended in place
rather than adding a second collector; `pg_database_v1` is retired
and its spec file removed.

## Analyzer requirements unblocked

- `xid-wraparound-risk` — per-database freeze age.
- `io-cost-calibration` — database-size weighting.
- Capacity reporting.
