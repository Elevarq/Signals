# replication_slots_risk_v1 — Collector Specification

## Purpose

Replication-slot state with derived WAL retention. An inactive slot
is the single most common cause of runaway WAL retention and
wraparound risk; this collector surfaces each slot with the bytes
of WAL it is retaining beyond the current flush point.

## Catalog source

- `pg_replication_slots`
- `pg_current_wal_lsn()`, `pg_wal_lsn_diff()` — derived retention

## Output columns

| Column | Type | Description |
|---|---|---|
| slot_name | text | Slot name |
| slot_type | text | `physical` or `logical` |
| active | boolean | True if currently consumed |
| database | text | Database name (logical slots only) |
| plugin | text | Output plugin (logical slots only) |
| retained_wal_bytes | bigint | `pg_current_wal_lsn() - restart_lsn` in bytes |
| unconfirmed_wal_bytes | bigint | `pg_current_wal_lsn() - confirmed_flush_lsn` |

Emitted sort: `retained_wal_bytes DESC NULLS LAST`.

## Scope filter

All slots. Empty rowset on standalone instances.

## Invariants

- Deterministic ordering by retention.
- Stable output column order.
- Read-only, passes linter.
- Derivations computed server-side; analyzer does not recompute.

## Failure Conditions

- FC-01: No slots configured → empty result. Not an error.
- FC-02: Permission denied → standard collector error path
  (`pg_replication_slots` is readable by `pg_monitor` since PG 10).

## Configuration

- Category: replication
- Cadence: 5m (Cadence5m)
- Retention: RetentionShort
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot (state); `retained_wal_bytes` behaves as a
  rate proxy across samples
- Enabled by default: yes

## Sensitivity

Low. Slot names are schema-like metadata.

## Analyzer requirements unblocked

- `replication-slot-retention` — primary evidence.
- `wal-retention-risk` — combined with `pg_stat_wal_v1`.

## Relationship to other collectors

`pg_stat_replication_v1` — the analogous catalog-mirror ID — is NOT
registered; this behavior-named collector is the authoritative
source for slot risk. For active-replica lag, see
`replication_status_v1`.
