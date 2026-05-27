# pg_prepared_xacts_v1 — Collector Specification

## Purpose

Enumerate prepared (two-phase-commit) transactions. Orphaned
prepared transactions are an insidious vacuum blocker and XID
retention source — they hold `xmin` indefinitely and do not surface
in normal session monitoring.

## Catalog source

- `pg_prepared_xacts`

## Output columns

| Column | Type | Description |
|---|---|---|
| transaction | xid | Transaction ID |
| gid | text | Global identifier supplied at PREPARE |
| prepared | timestamptz | When the transaction was prepared |
| owner | text | Role that prepared the transaction |
| database | text | Database name |
| age_seconds | bigint | `EXTRACT(EPOCH FROM now() - prepared)` |
| age_xids | bigint | `age(transaction)` |

## Scope filter

All rows emitted.

## Invariants

- Deterministic ordering: `ORDER BY prepared ASC, gid ASC`.
- Stable output column order.
- Read-only, passes linter.

## Failure Conditions

- FC-01: `max_prepared_transactions = 0` → view exists but will
  never return rows. Not an error.
- FC-02: Permission denied (rare — `pg_prepared_xacts` is generally
  readable) → standard collector error path.

## Configuration

- Category: runtime
- Cadence: 1h (Cadence1h)
- Retention: RetentionMedium
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Low. `gid` is application-supplied and may reveal application names
or transaction metadata; by project assumption the snapshot stays
on site.

## Analyzer requirements unblocked

- `autovacuum-lag` — orphaned 2PC as vacuum blocker.
- `xid-wraparound-risk` — 2PC contribution to retained xmin.
- `replication-slot-retention` — cross-reference with slot `xmin`.
