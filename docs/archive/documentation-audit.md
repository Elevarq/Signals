# Documentation Consistency Audit

Audit date: 2026-03-14
Scope: all implementation, test, and documentation files in arq-signals

## Findings

### ISSUE 1 — Wraparound query cadences wrong in README (INCORRECT)

**Location:** README.md, "Collected signals" table, lines 233-234

**Documentation says:**
```
| wraparound_db_level_v1  | pg_database     | 15m  |
| wraparound_rel_level_v1 | pg_class        | 15m  |
```

**Implementation says** (catalog_wraparound.go:27, 70):
```go
Cadence: CadenceDaily,  // 24 hours
```

**Impact:** Users will expect wraparound data every 15 minutes but it
only collects daily. Misleading for capacity planning.

**Fix:** Change both entries from `15m` to `24h` in the README table.

---

### ISSUE 2 — Specification has leftover "pending" fragment (INCORRECT)

**Location:** features/arq-signals/specification.md, line 229

**Documentation says:**
```
All 26 requirements are covered by automated tests (16 original + 10 runtime safety).
requirements (R017–R026) are pending implementation.
```

**Reality:** R017-R026 are fully implemented. Line 229 is a leftover
fragment from a partial edit — the coverage table correctly shows 26/26
COVERED, but the dangling sentence contradicts it.

**Fix:** Delete line 229.

---

### ISSUE 3 — PostgreSQL version claim inconsistent (MINOR)

**Location:** README.md line 78 says "PostgreSQL instances (14+)";
FAQ says "PostgreSQL 14 and later"

**Implementation:** catalog_wraparound.go sets `MinPGVersion: 10` for
wraparound queries. The core queries in catalog.go have no
MinPGVersion constraint. The codebase works with PG 10+ technically,
though 14+ is the stated supported baseline.

**Assessment:** This is a deliberate product decision (support only
actively-maintained PG versions), not a documentation error. The code
is permissive; the docs set the support boundary. No change needed,
but could add a clarifying note.

---

### ISSUE 4 — Lock timeout value undocumented (GAP)

**Location:** internal/collector/collector.go:282

**Implementation:**
```go
lockTimeoutMs := 5000 // 5 seconds — conservative default
```

**Documentation:** docs/runtime-safety-model.md says "lock_timeout"
with no specific value. README does not mention lock_timeout at all.

**Fix:** Add "5 seconds (hardcoded)" to the runtime safety model
timeout table.

---

### ISSUE 5 — Commit error handling not in safety model doc (GAP)

**Location:** docs/runtime-safety-model.md

**Implementation:** collector.go:423-425 checks tx.Commit(ctx) error
and returns immediately on failure, blocking downstream persistence.

**Documentation:** The runtime safety model describes read-only
enforcement and timeouts but does not mention commit-error handling.

**Fix:** Add a brief note about transaction commit verification.

---

## Areas verified as correct

The following documentation areas match the implementation:

| Area | Files checked | Status |
|------|---------------|--------|
| Three-layer read-only enforcement | README, safety model, SECURITY, spec | Correct |
| Role safety checks (super/repl/bypass) | README, safety model, FAQ, spec | Correct |
| Unsafe override (ARQ_SIGNALS_ALLOW_UNSAFE_ROLE) | README, safety model, FAQ, adoption guide | Correct |
| Credential handling | README, credential review, SECURITY, spec | Correct |
| /status field exclusions (no secret_type/ref) | credential review, safety model | Correct |
| Export metadata (unsafe_mode, unsafe_reasons) | safety model, spec | Correct |
| API endpoints (/health, /status, /collect/now, /export) | README, adoption guide | Correct |
| CLI commands (version, status, collect now, export) | README, adoption guide, arqctl main.go | Correct |
| Environment variables (22 vars) | README table, config.go applyEnvOverrides | Correct |
| Config defaults | README, adoption guide, examples/signals.yaml, config.go | Correct |
| Snapshot format (arq-snapshot.v1) | README, spec, snapshot/schema.go | Correct |
| NDJSON compression threshold (4096 bytes) | spec, db/ndjson.go | Correct |
| Dedicated connection + SET LOCAL timeouts | safety model, collector.go | Correct |
| Query catalog (9 core + 3 wraparound) | README, spec, catalog.go, catalog_wraparound.go | Correct (except cadences) |
| STDD traceability | traceability.md vs test files | Correct |
| STDD acceptance tests | acceptance-tests.md vs test files | Correct |
| GitHub templates | issue/PR templates | Correct |
| Contributing scope | CONTRIBUTING.md | Correct |
| License | LICENSE, README | Correct |

## Recommended corrections (minimal)

| # | File | Change | Effort |
|---|------|--------|--------|
| 1 | README.md | Fix wraparound_db_level_v1 and wraparound_rel_level_v1 cadence from 15m to 24h | Trivial |
| 2 | features/arq-signals/specification.md | Delete leftover line 229 | Trivial |
| 3 | docs/runtime-safety-model.md | Add lock_timeout=5s to timeout table, add commit verification note | Small |

Total: 3 files, 5 line-level changes.
