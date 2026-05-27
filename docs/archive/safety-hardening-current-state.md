# Safety Hardening — Current State Analysis

## Current read-only enforcement

### Layer 1: Static SQL linting (startup)
- `internal/pgqueries/linter.go` — rejects DDL, DML, dangerous functions
  at `Register()` time. Process panics on linter failure.
- **Enforcement**: compile-time (init). No runtime bypass possible.

### Layer 2: Session-level read-only (connection setup)
- `internal/collector/secrets.go:55` — sets
  `default_transaction_read_only=on` in `RuntimeParams` when building
  the connection config.
- **Enforcement**: applied once per connection. Not re-validated per
  collection cycle. No check that it actually took effect.

### Layer 3: Per-query read-only transaction (collection)
- `internal/collector/collector.go:201` — calls `pool.BeginTx(ctx,
  pgx.TxOptions{AccessMode: pgx.ReadOnly})` before running queries.
- **Enforcement**: per-target, per-cycle. Relies on PostgreSQL honoring
  the access mode. No explicit validation afterward.

## What is NOT currently checked

1. **Role attributes**: No check for `rolsuper`, `rolreplication`, or
   `rolbypassrls`. A superuser connection would succeed silently.
2. **Session timeouts**: No `statement_timeout`, `lock_timeout`, or
   `idle_in_transaction_session_timeout` set. A long-running query
   could hold resources.
3. **Write capability verification**: No explicit test that
   `SET default_transaction_read_only = on` actually took effect.
4. **Unsafe override model**: No explicit opt-in for unsafe roles. No
   recording of safety posture in metadata.
5. **Credential exposure**: `redactError()` and `RedactDSN()` exist in
   `secrets.go` but are only used in specific paths. The `/status`
   endpoint exposes target host/port/user but not passwords. Export
   metadata does not contain credentials.

## Where connection/session setup happens

| Step | File:Line | What happens |
|------|-----------|-------------|
| Build config | `secrets.go:19-58` | Host/port/user/ssl/password + runtime params |
| Pool creation | `collector.go:368-407` | pgxpool with MaxConns=2, BeforeConnect password re-resolve |
| Transaction open | `collector.go:201` | BeginTx with ReadOnly access mode |
| Query execution | `collector.go:248-331` | Per-query timeout, queryToMaps() |

## Where credentials are loaded

| Source | File | Redaction |
|--------|------|-----------|
| password_file | `secrets.go:79-86` | Content never logged |
| password_env | `secrets.go:88-93` | Env var name logged, value never |
| pgpass_file | `secrets.go:99-135` | Path logged in error, value never |
| BeforeConnect | `collector.go:390-398` | Error redacted via `redactError()` |

## Status/API endpoint exposure

- `GET /status`: Returns target name, host, port, dbname, user, sslmode,
  secret_type, enabled. Does NOT return passwords or credential values.
- `GET /export`: Contains query results only. No credentials.
- `GET /health`: No target information.

## Config/env handling for unsafe modes

- `ARQ_ALLOW_INSECURE_PG_TLS`: Exists for TLS downgrade in non-prod.
  Not related to role safety.
- No equivalent for unsafe role override. No `ARQ_SIGNALS_ALLOW_UNSAFE_ROLE`
  or similar.

## Summary of gaps

| Gap | Severity | Impact |
|-----|----------|--------|
| No role attribute validation | HIGH | Superuser/replication roles accepted silently |
| No session timeout enforcement | MEDIUM | Runaway queries could hold resources |
| No read-only verification | MEDIUM | Trust but don't verify pattern |
| No unsafe override model | LOW | No way to explicitly acknowledge risk |
| No safety posture in metadata | LOW | Cannot audit collection safety after the fact |
