# Remediation — Current State Findings

## CRITICAL: Session timeout connection mismatch

In `collector.go:239-246`, `ApplySessionTimeouts` acquires a connection
from the pool, sets timeouts, then releases it. The subsequent
`pool.BeginTx` at line 249 may acquire a **different** connection from
the pool. The timeouts are therefore not guaranteed to protect the
actual collection queries.

**Fix required**: Acquire a dedicated connection, apply timeouts via
SET LOCAL inside the collection transaction, and run all queries through
that same connection/transaction.

## CRITICAL: STDD artifacts not in repository

The STDD artifacts exist at `/Users/frankheikens/Projects/elevarq/arq/features/arq-signals/`
which is **outside** the Arq Signals repository root. The repo at
`repo-split/arq-signals/` has no `features/` directory. Any CI or
contributor clone would not have these files.

**Fix required**: Copy the STDD artifacts into the repository under
`features/arq-signals/`.

## MAJOR: docs/adoption-guide.md has wrong config schema

The adoption guide references config fields that do not exist:
- `dsn:` — the actual config uses `host`, `port`, `dbname`, `user`, `password_file`
- `dsn_env:` — the actual config uses `password_env`
- `workers:` / `target_timeout:` / `query_timeout:` under `collection:` — the actual
  config uses these under `signals:`
- Port 8065 — the actual default is 8081
- Config filename `arq-signals.yaml` — the actual lookup is `signals.yaml`
- `arqctl collect --config` — the CLI uses `arqctl collect now` with
  API token, not `--config`

## MAJOR: /status exposes secret_type and secret_ref

`api/server.go:112` emits `secret_type` for each target. While this is
not the actual password, revealing "FILE" or "ENV" and the ref path
could aid an attacker in locating credentials.

The `secret_ref` field is not currently emitted (it's stored in the DB
as `SecretRef` but the handler uses `t.SecretType`). However,
`secret_type` should also be removed from the default output.

## MAJOR: Unsafe mode metadata records generic reason

`export.go:88-90` records `unsafe_reasons` but `cmd/arq-signals/main.go`
sets this to `["ARQ_SIGNALS_ALLOW_UNSAFE_ROLE=true"]` — a generic
string, not the actual bypassed role checks. The collector knows the
specific bypassed checks (from `safetyResult.HardFailures`) but does
not propagate them to the exporter.

## MAJOR: AST tests provide false confidence

Many safety tests (e.g. `TestCollectorCallsValidateRoleSafety`) just
scan source code for function call strings. These prove the call exists
in source but not that it executes on the correct code path or that the
behavior is correct. Should be supplemented with behavioral tests.

## MINOR: Missing env vars in README

The README env var table omits:
- `ARQ_SIGNALS_ALLOW_UNSAFE_ROLE`
- `ARQ_SIGNALS_LOG_JSON`
- `ARQ_SIGNALS_MAX_CONCURRENT_TARGETS`
- `ARQ_SIGNALS_TARGET_TIMEOUT`
- `ARQ_SIGNALS_QUERY_TIMEOUT`
- `ARQ_SIGNALS_TARGET_NAME`
- `ARQ_SIGNALS_TARGET_PGPASS_FILE`
