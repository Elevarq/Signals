# Runtime Safety Implementation Report

Generated: 2026-03-14

## Summary

Implemented fail-closed runtime safety enforcement for PostgreSQL
role validation and session posture in Arq Signals. Collection is now
blocked by default when the connected role has unsafe attributes.

## Test Results

| Metric | Value |
|--------|-------|
| Total test functions | 87 |
| Passing | 87 |
| Failing | 0 |
| New safety tests | 29 |
| Existing tests (unchanged) | 58 |

## Traceability

| Requirement | Status | Test coverage |
|-------------|--------|---------------|
| R017 Session read-only guard | COVERED | SafetyResult logic + BuildConnConfig param + AST call verification |
| R018 Refuse superuser | COVERED | Hard failure blocks, error contains rolsuper=true |
| R019 Refuse replication | COVERED | Hard failure blocks, error contains rolreplication=true |
| R020 Refuse bypassrls | COVERED | Hard failure blocks, error contains rolbypassrls=true |
| R021 Read-only transaction | COVERED | Param verification + AST call verification |
| R022 Session timeouts | COVERED | Default values, lockTimeoutMs=5000, AST call verification |
| R023 Hard vs soft distinction | COVERED | Warnings do not block, hard failures do |
| R024 No secrets exposed | COVERED | RedactDSN, redactError source verification |
| R025 Actionable errors | COVERED | Error contains remediation guidance |
| R026 Unsafe override | COVERED | Option, default, config, env var, export metadata |

**26/26 requirements COVERED. 0 UNCOVERED.**

## Files Changed

### New files
- `internal/collector/rolecheck.go` — SafetyResult, ValidateRoleSafety,
  ValidateSessionReadOnly, ApplySessionTimeouts
- `tests/signals_safety_test.go` — 29 tests for safety requirements
- `docs/safety-hardening-current-state.md` — discovery analysis
- `docs/runtime-safety-model.md` — operator-facing safety documentation
- `docs/credential-handling-review.md` — credential audit
- `docs/runtime-safety-implementation-report.md` — this file

### Modified files
- `internal/collector/collector.go` — added allowUnsafeRole field,
  WithAllowUnsafeRole option, safety validation block in collectTarget
- `internal/config/config.go` — added AllowUnsafeRole field and env override
- `internal/export/export.go` — added unsafeMode/unsafeReasons to metadata
- `cmd/arq-signals/main.go` — wired AllowUnsafeRole to collector and exporter
- `README.md` — added role safety validation section
- `docs/faq.md` — added superuser FAQ entry
- `docs/adoption-guide.md` — added role safety subsection

### STDD artifacts updated
- `features/arq-signals/specification.md` — added R017-R026, INV-05-07
- `features/arq-signals/acceptance-tests.md` — added TC-SIG-025 through TC-SIG-035
- `features/arq-signals/traceability.md` — added 10 rows, all COVERED

## Justified Limitations

1. **Role attribute checks require live PostgreSQL**: ValidateRoleSafety
   queries pg_roles at runtime. Unit tests verify the SafetyResult logic
   and error formatting. The actual pg_roles query is verified via AST
   scanning (confirming the call exists in the code path). Full
   integration testing requires a live PostgreSQL instance.

2. **pg_write_all_data check is best-effort**: The hygiene warning for
   pg_write_all_data membership silently succeeds if the role does not
   exist (pre-PG14 compatibility). This is intentional — hygiene
   warnings should not block collection.

3. **Session timeouts are defense-in-depth**: ApplySessionTimeouts
   failure is logged as a warning, not a hard failure. Per-query
   timeouts via Go context cancellation remain the primary timeout
   mechanism.

4. **Privilege escalation paths not exhaustively checked**: The role
   check covers rolsuper, rolreplication, and rolbypassrls attributes.
   It does not enumerate all possible privilege escalation paths through
   function ownership, security definer functions, or extension-granted
   privileges. This is documented as a known limitation and follows the
   principle of checking what can be practically and reliably validated.

## Example Failure Messages

### Superuser blocked
```
collection blocked for target prod-primary: safety check failed for connected role:
  BLOCKED: role "postgres" has superuser attribute (rolsuper=true) — collection requires a non-superuser role

Remediation: create a dedicated monitoring role:
  CREATE ROLE arq_monitor WITH LOGIN PASSWORD '...';
  GRANT pg_monitor TO arq_monitor;
```

### Multiple attributes blocked
```
collection blocked for target staging: safety check failed for connected role:
  BLOCKED: role "admin" has superuser attribute (rolsuper=true) — collection requires a non-superuser role
  BLOCKED: role "admin" has replication attribute (rolreplication=true) — collection requires a role without replication privileges

Remediation: create a dedicated monitoring role:
  CREATE ROLE arq_monitor WITH LOGIN PASSWORD '...';
  GRANT pg_monitor TO arq_monitor;
```

### Unsafe override active
```
WARN UNSAFE MODE: bypassing safety checks — not recommended for production
  target=staging bypassed_checks=["role \"admin\" has superuser attribute ..."]
```

## Publication Readiness

This change **strengthens** publication readiness:
- Fail-closed default aligns with security-first OSS expectations
- Clear error messages reduce support burden
- Unsafe override is explicit and auditable
- No proprietary logic introduced (OSS boundary intact)
- All 87 tests pass, 26/26 requirements covered
