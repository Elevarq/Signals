# Pre-Publication Go/No-Go Report

**Date:** 2026-03-14
**Repository:** Arq Signals
**Version:** v0.1.0

---

## Verification Results

### 1. Dedicated connection for collection

**VERIFIED.** `collectTarget()` in `collector.go`:
- Line 256: `conn, err := pool.Acquire(ctx)` — acquires dedicated connection
- Line 264: `conn.QueryRow(ctx, "SHOW default_transaction_read_only")` — verifies read-only on that connection
- Line 272: `tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})` — transaction on same connection
- Line 292: `tx.Exec(ctx, fmt.Sprintf("SET LOCAL %s = %d", ...))` — timeouts inside that transaction
- Lines 299+: `tx.QueryRow` / `tx.Query` — all queries through same transaction

No pool-level BeginTx. No possibility of timeout/query connection mismatch.

### 2. Session timeouts inside executing transaction

**VERIFIED.** SET LOCAL is used at line 292 inside the transaction opened at line 272. Three parameters applied:
- `statement_timeout` (from configured query timeout, default 10s)
- `lock_timeout` (5000ms hardcoded conservative)
- `idle_in_transaction_session_timeout` (from configured target timeout, default 60s)

SET LOCAL is transaction-scoped by PostgreSQL semantics — automatically reset on COMMIT/ROLLBACK.

### 3. /status does not expose credential metadata

**VERIFIED.** `server.go` line 114 contains only a comment:
```go
// secret_type and secret_ref are intentionally omitted from
// /status to avoid revealing credential source details.
```
The response map at lines 105-113 contains only: id, name, host, port, dbname, user, sslmode, enabled. No secret_type, no secret_ref.

### 4. Unsafe-mode export metadata contains actual bypass reasons

**VERIFIED.** `main.go` lines 97-102 pass `coll.GetBypassedChecks` as a function to the exporter. `collector.go` line 248 calls `c.recordBypassedChecks(safetyResult.HardFailures)` with the actual failure messages. `export.go` line 91 calls `b.unsafeReasonsFunc()` at export time to get current bypassed checks.

Example output:
```json
{"unsafe_mode": true, "unsafe_reasons": ["role \"postgres\" has superuser attribute (rolsuper=true) — collection requires a non-superuser role"]}
```

### 5. STDD artifacts present and aligned

**VERIFIED.** All three files exist at `features/arq-signals/`:
- `specification.md`: 26 requirements (R001-R026), 7 invariants
- `acceptance-tests.md`: 35 test cases (TC-SIG-001 through TC-SIG-035)
- `traceability.md`: 26 rows with Evidence Type column (BEHAVIORAL/STRUCTURAL/INTEGRATION)

Coverage summary in specification.md states 26/26 COVERED, which matches the traceability matrix.

### 6. Test evidence quality

**VERIFIED.** The traceability matrix honestly classifies each requirement's evidence:

| Evidence Type | Requirements | Count |
|---------------|-------------|-------|
| BEHAVIORAL | R001-R006, R012, R014, R015, R018-R020, R023, R025 | 14 |
| BEHAVIORAL + STRUCTURAL | R011, R013, R017, R021, R022, R024, R026 | 7 |
| STRUCTURAL only | R007-R010, R016 | 5 |

The 5 structural-only requirements are boundary enforcement (R007-R009: no analyzer code) and code structure checks (R010: CLI commands, R016: no password fields). These are appropriately structural — there is no behavioral way to prove "code X does not exist."

### 7. Documentation matches implementation

**VERIFIED.**
- README env var table: 22 variables, all match `config.go` `applyEnvOverrides()`
- Adoption guide: Uses structured config (host/port/dbname/user/password_file), correct port 8081, correct CLI commands
- FAQ: Accurate on fail-closed behavior, credential handling, no DSN references
- Runtime safety model: Documents SET LOCAL (transaction-scoped), /status omits secret fields

No `dsn:`, `dsn_env:`, port 8065, or `arqctl --config` patterns remain in any documentation.

### 8. No secrets or proprietary logic

**VERIFIED.**
- `grep -rn` for proprietary markers in Go source: 0 matches
- `TestNoProprietaryContent`: PASS (scans all files for prohibited proprietary markers)
- `TestNoAnalyzerImports`: PASS (no scoring/llm/report/requirements/dashboard imports)
- `TestNoLLMCode`: PASS
- `TestNoScoringCode`: PASS
- No credentials, API keys, or internal URLs in any file

---

## Test Suite

| Metric | Value |
|--------|-------|
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| Total test functions | 94 |
| Passing | 94 |
| Failing | 0 |
| Integration tests (guarded) | 1 (not in default run) |

---

## Publication Blockers

**None identified.**

---

## Known Limitations (acceptable for v0.1.0)

1. Role attribute checks (R018-R020) verify SafetyResult logic behaviorally but the actual `SELECT FROM pg_roles` query requires a live PostgreSQL. An integration test is provided (`//go:build integration`).

2. SET LOCAL timeout execution (R022) is verified structurally (correct code path) but runtime proof requires live PostgreSQL.

3. Boundary tests (R007-R010) are structural by nature — this is the only way to prove absence of code.

4. The collector does not check every theoretical privilege escalation path in PostgreSQL (e.g. security definer functions). This is documented in the runtime safety model as a known limitation.

---

## Decision

### **GO** for public GitHub release.

The repository meets the quality bar for an initial open-source release:
- Fail-closed safety model is correctly implemented
- Session timeouts apply to the executing transaction (critical fix verified)
- No credential exposure in status/export/logs
- STDD artifacts present with honest coverage classification
- Documentation matches actual implementation
- No proprietary logic or secrets present
- 94 tests pass with 0 failures
