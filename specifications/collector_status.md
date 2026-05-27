# Collector Execution Status — Specification

## Purpose

Record the execution outcome of every registered collector for each
snapshot cycle. This metadata enables the Arq Analyzer to assess
evidence completeness and communicate reduced reliability when
collector data is missing or incomplete.

## File

`collector_status.json` — written alongside `metadata.json` in the
Arq Signals output ZIP.

## Schema

```json
{
  "schema_version": "1",
  "collected_at": "RFC3339 timestamp",
  "collectors": [ ...CollectorStatus entries... ]
}
```

## CollectorStatus entry

| Field | Type | Description |
|---|---|---|
| id | string | Collector ID (e.g., "pg_constraints_v1") |
| attempted | bool | true if the query was executed |
| status | string | success, partial, skipped, failed |
| reason | string | Reason category (empty on success) |
| detail | string | Human-readable explanation |
| row_count | int | Rows returned (0 for skipped/failed) |
| duration_ms | int | Execution wall-clock time (0 for skipped) |
| collected_at | string | RFC3339 timestamp of execution (empty for skipped) |

## Status values

| Status | Meaning |
|---|---|
| success | Query ran and returned results (or legitimate empty) |
| partial | Query ran with known limitations |
| skipped | Query was not attempted |
| failed | Query was attempted but produced an error |

## Reason categories

| Reason | Status | When used |
|---|---|---|
| (empty) | success | No explanation needed |
| version_unsupported | skipped | PG version below MinPGVersion |
| extension_missing | skipped | Required extension not installed |
| config_disabled | skipped | Collector disabled in configuration |
| execution_error | failed | SQL query failed at runtime |
| permission_denied | failed | Insufficient privileges (SQLSTATE 42501) |
| timeout | failed | Query exceeded timeout |
| savepoint_rollback | failed | Query failed within savepoint |

## Invariants

- Every registered collector appears exactly once in the array
- Collectors are ordered by ID for determinism
- Skipped collectors have attempted=false, duration_ms=0, collected_at=""
- The file is written even when all collectors succeed
- Empty collector list serializes as []
