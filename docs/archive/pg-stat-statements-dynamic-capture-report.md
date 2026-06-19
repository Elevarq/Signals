# pg_stat_statements Dynamic Capture — Implementation Report

Date: 2026-03-14

## Summary

Changed the `pg_stat_statements_v1` collector from a fixed column list
to `SELECT *` for cross-version compatibility. Added savepoint-based
query isolation so a single query failure no longer aborts the entire
collection transaction.

## Files changed

| File | Change |
|------|--------|
| `internal/pgqueries/catalog.go` | Changed pg_stat_statements_v1 SQL from fixed 22-column SELECT to raw `SELECT *` |
| `internal/collector/collector.go` | Added SAVEPOINT/ROLLBACK TO SAVEPOINT around each query |
| `internal/db/ndjson.go` | Added nil-to-empty-slice guard for zero-row results |
| `features/signals/specification.md` | Added R037-R039 (dynamic capture, failure isolation, safety preservation) |
| `features/signals/acceptance-tests.md` | Added TC-SIG-044 through TC-SIG-046 |
| `features/signals/traceability.md` | Added 3 rows for R037-R039, all COVERED |
| `tests/signals_dynamic_capture_test.go` | 7 new tests |
| `README.md` | Updated pg_stat_statements table entry |
| `docs/faq.md` | Added pg_stat_statements version compatibility FAQ |
| `docs/pg-stat-statements-dynamic-capture-plan.md` | Discovery document |

## STDD changes

- **R037**: Dynamic column capture for version-sensitive views
- **R038**: Query failure isolation via savepoints
- **R039**: Dynamic capture preserves safety model
- **TC-SIG-044**: Dynamic column capture behavior
- **TC-SIG-045**: Query failure isolation
- **TC-SIG-046**: Safety model preservation

## Tests added

| Test | Type | What it proves |
|------|------|----------------|
| TestPgStatStatementsUsesDynamicCapture | BEHAVIORAL | SQL uses SELECT * |
| TestPgStatStatementsPassesLinter | BEHAVIORAL | Dynamic query passes safety linter |
| TestPgStatStatementsRequiresExtension | BEHAVIORAL | Extension gating preserved |
| TestDynamicColumnsPreservedInNDJSON | BEHAVIORAL | NDJSON preserves arbitrary column names including future columns |
| TestZeroRowDynamicCapture | BEHAVIORAL | Empty results produce valid non-nil payload |
| TestSavepointIsolationInCollectorSource | STRUCTURAL | Savepoints present in collector code |
| TestDynamicCaptureQueryIsReadOnly | BEHAVIORAL | No write keywords in dynamic query |

## Version tolerance

The collector is now more version-tolerant:
- PG 14-16: Captures the pre-17 column layout
- PG 17+: Captures the renamed columns (shared_blk_read_time, etc.)
- Future versions: Captures any new columns automatically

## Remaining limitations

- Signals applies no `ORDER BY` or `LIMIT` to
  `pg_stat_statements_v1`. Analyzer owns ranking and top-N policy.

- Only pg_stat_statements uses dynamic capture. Other collectors use
  stable column lists that have not changed across PG 14-18.

## Test results

| Check | Result |
|-------|--------|
| `go vet` | OK |
| `go build` | OK |
| Total tests | 118 |
| Passing | 118 |
| Failing | 0 |

## Smoke test verification

Tested against PostgreSQL 18.1 (localhost:54318) with the `frs`
database and `arq_signals` role:
- Before fix: `pg_stat_statements_v1` failed with "column
  blk_read_time does not exist", cascading to abort all queries
- After fix: `pg_stat_statements_v1` succeeds with all PG 18 columns
  captured dynamically; all other queries succeed independently
