# wraparound_blockers_v1 — Collector Specification

## Purpose

Identify client-side contributors to XID freeze-horizon retention:
long-running non-idle transactions that hold back the global
`xmin`, with backend_xmin age. Complements
`replication_slots_risk_v1` (slot-side) and `pg_prepared_xacts_v1`
(2PC-side) for the three main `xmin` retention categories.

## Catalog source

- `pg_stat_activity`

## Output columns

| Column | Type | Description |
|---|---|---|
| blocker_type | text | Constant `'long_tx'` — discriminator for future rows from other sources |
| identifier | text | Backend PID as text |
| usename | text | Connected user |
| application_name | text | Application label |
| xmin_age | bigint | `age(backend_xmin)` |
| state | text | Backend state |
| query_snippet | text | `LEFT(query, 200)` |
| xact_age_seconds | double precision | `EXTRACT(EPOCH FROM (now() - xact_start))` |

## Scope filter

- `xact_start IS NOT NULL`
- `pid != pg_backend_pid()` (exclude collector's own session)
- `state != 'idle'` — idle sessions without an xact are filtered
  elsewhere

LIMIT 20 ordered by `xact_start ASC` (oldest first).

## Invariants

- Deterministic ordering: oldest transaction first.
- Stable output column order.
- `blocker_type` is always `'long_tx'` in this version — the field
  is schema-stable for future discriminator additions.
- Read-only, passes linter.

## Failure Conditions

- FC-01: No long-running transactions → empty result.
- FC-02: Role lacks `pg_monitor` → may only see own sessions. If
  so, the analyzer ranker filters the own-session row as
  uninteresting.

## Configuration

- Category: wraparound
- Cadence: 5m (Cadence5m) — this is the fast-moving dimension of
  wraparound risk; relation-level scans at daily cadence
- Retention: RetentionShort
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Medium. Query snippet may embed literals; deployment-boundary
assumption applies.

## Analyzer requirements unblocked

- `xid-wraparound-risk` — the client-side contribution to retained
  `xmin`.
- Cross-referenced with `replication_slots_risk_v1` (slot-side)
  and `pg_prepared_xacts_v1` (2PC-side) for full coverage.
