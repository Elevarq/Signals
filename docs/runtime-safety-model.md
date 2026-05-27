# Runtime Safety Model

## Overview
Arq Signals enforces a fail-closed safety model. Before collecting diagnostic data from a PostgreSQL target, the system validates that the connection meets strict safety requirements. If validation fails, collection is blocked.

## Safety Layers

### Layer 1: Static SQL Linting (startup)
Every SQL collector query is validated at process startup. Queries containing DDL (CREATE, ALTER, DROP), DML (INSERT, UPDATE, DELETE), or dangerous functions (pg_terminate_backend, pg_sleep) cause the process to abort immediately. No collector query can be registered without passing the linter.

### Layer 2: Role Attribute Validation (per-target)
Before each collection cycle, Arq Signals queries pg_roles for the connected role's attributes. The following attributes are hard blockers:
- rolsuper=true -- Superuser roles have unrestricted access. Collection requires a least-privilege role.
- rolreplication=true -- Replication roles can read WAL streams. Not needed for diagnostic collection.
- rolbypassrls=true -- Bypass Row Level Security is unnecessary and reduces isolation.

If any of these attributes are detected, collection is blocked with an actionable error message.

### Layer 3: Session Read-Only Posture (per-target)
Connections set default_transaction_read_only=on as a session parameter. This is verified before collection begins by checking the actual session value. All collection queries execute inside BEGIN ... READ ONLY transactions.

### Layer 4: Transaction-Scoped Timeouts (per-target)
Conservative timeouts are applied via `SET LOCAL` inside the collection transaction, guaranteeing they apply to the exact connection and transaction that executes queries. This avoids any risk of timeout state leaking across pooled connections or concurrent operations.
- statement_timeout -- SET LOCAL to the configured query timeout (default: 10s)
- lock_timeout -- SET LOCAL to 5 seconds (hardcoded conservative value)
- idle_in_transaction_session_timeout -- SET LOCAL to the configured target timeout (default: 60s)

## Hard Failures vs Warnings

| Check | Type | Behavior |
|-------|------|----------|
| rolsuper=true | HARD FAILURE | Collection blocked |
| rolreplication=true | HARD FAILURE | Collection blocked |
| rolbypassrls=true | HARD FAILURE | Collection blocked |
| Session not read-only | HARD FAILURE | Collection blocked |
| pg_write_all_data member | WARNING | Logged, collection proceeds |

## Credential Handling
- Passwords are read from file, env var, or pgpass at connection time
- Never cached in memory beyond a single connection attempt
- Never written to SQLite
- Never included in snapshot exports or API responses
- Error messages containing credential information are redacted

## Unsafe Override
An explicit override is available for lab/dev environments only:
```
ARQ_SIGNALS_ALLOW_UNSAFE_ROLE=true
```
When enabled:
- Hard failures are downgraded to warnings
- Collection proceeds with a prominent log warning
- Export metadata includes unsafe_mode=true
- NOT recommended for production

## Recommended Production Setup
Create a dedicated, least-privilege monitoring role:
```sql
CREATE ROLE arq_monitor WITH LOGIN PASSWORD '...';
GRANT pg_monitor TO arq_monitor;
-- Do NOT grant superuser, replication, or bypassrls
```

## API Information Disclosure

The `/status` endpoint exposes connection metadata (host, port, user, sslmode) but does **not** expose `secret_type` or `secret_ref`. This means the API response does not reveal whether credentials come from a file, environment variable, or pgpass, nor does it reveal the path or variable name used to source them. This minimizes information leakage about the credential management strategy.

## Error Messages
When safety validation fails, Arq Signals provides actionable error messages that include:
- Which check failed (e.g., "role has superuser attribute")
- The attribute value detected
- Remediation guidance (CREATE ROLE + GRANT pg_monitor)
