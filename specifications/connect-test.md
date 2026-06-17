# `signalsctl connect test` — Connection Diagnostic

## Status

DRAFT

## Purpose

A focused read-only CLI tool that tests a single PostgreSQL connection
and returns a **classified, actionable** failure category. Closes the
gap between "the daemon failed to connect" (buried in journald) and
"is this connection going to work?" (the question operators actually
ask).

Sibling to `signalsctl doctor` (R095) but different shape: doctor runs a
battery of checks against *every* target in config; connect test
exercises *one* target — or an ad-hoc DSN — and only the connection
path. Shares the underlying check helpers; differs in audience and
output.

## Scope

`signalsctl connect test [<target-name>] [--dsn <dsn-fields>] [--verbose]`

Modes:

- **No args** — test every enabled target in config; one result per target.
- **`<target-name>`** — test that one target from config.
- **`--dsn`** — test an ad-hoc connection without it being in config.

For each attempt the tool classifies the outcome into one of:

| Category | Trigger |
|----------|---------|
| `ok` | All checks passed. |
| `dns` | Hostname resolution failed. |
| `tcp` | TCP-layer failure: connection refused, timeout, host unreachable. |
| `tls` | TLS handshake failure (certificate, protocol, hostname mismatch). |
| `auth` | PG authentication rejected. PG SQLSTATE 28P01 / 28000. |
| `startup` | Connected but session-init failed: database doesn't exist (3D000), PG version below `MinPGVersion` floor. |
| `role` | Connected and authenticated but role validation finds an unsafe attribute (superuser / replication / bypassrls — R013, R018–R020). |
| `password_resolve` | Configured secret source can't be read (env var not set, file unreadable, pgpass parse error). |
| `config` | Input parse failure: malformed `--dsn`, missing required field. |

## Inputs

| Flag / arg | Type | Default | Description |
|------------|------|---------|-------------|
| `<target-name>` | positional | empty (= all enabled) | Config target to test. |
| `--config` | path | `$SIGNALS_CONFIG` or `/etc/signals/signals.yaml` | Config file location. Ignored when `--dsn` is supplied. |
| `--dsn` | string (repeatable `key=value`) | empty | Ad-hoc DSN as space-separated `host=X port=N dbname=D user=U sslmode=S [password_env=ENV \| password_file=PATH \| pgpass_file=PATH]`. Mutually exclusive with `<target-name>`. |
| `--json` | bool | `false` | Emit a single JSON object instead of human-readable text. |
| `--verbose` | bool | `false` | Per-phase timing (dns → tcp → tls → auth → role) and the underlying error chain. |
| `--connect-timeout` | duration | `3s` | TCP/auth phase timeout. |

## Outputs

### Text mode (default)

One line per target.

```
OK   prod-db                 connected to prod.example.com:5432/app as signals_ro (PG 16.2) in 47ms
FAIL staging-db   tcp        dial 10.0.0.7:5432: connect: connection refused
FAIL pii-archive  auth       SQLSTATE 28P01: password authentication failed for user "signals_ro"
FAIL dev-broken   password_resolve   password_env "DEV_DB_PW" is not set
```

`--verbose` adds:

- Per-phase elapsed time.
- The full redacted error chain via `errors.Unwrap`.

### JSON mode (`--json`)

```json
{
  "schema_version": "1",
  "generated_at": "<RFC3339>",
  "attempts": [
    {
      "target": "prod-db",
      "category": "ok",
      "detail": "...",
      "host": "prod.example.com",
      "port": 5432,
      "dbname": "app",
      "username": "signals_ro",
      "pg_version": "16.2",
      "phases": {
        "dns_ms": 1,
        "tcp_ms": 12,
        "tls_ms": 18,
        "auth_ms": 16,
        "role_ms": 0
      }
    }
  ],
  "summary": {
    "ok": <integer>,
    "fail": <integer>
  }
}
```

`phases` is populated only when `--verbose` is set.

### Exit codes

| Code | Meaning |
|------|---------|
| `0` | Every attempt returned `ok`. |
| `1` | At least one attempt returned a non-`ok` category. |
| `2` | Usage error (mutually exclusive flags, bad `--dsn` syntax, unknown target name). |

## Invariants

- **INV-CONN-01**: No writes to PostgreSQL. Every connection opens a
  read-only transaction even when the only query is `SELECT 1`.
- **INV-CONN-02**: Credentials never appear in any output channel
  (text, JSON, stderr, audit). Errors are passed through
  `collector.RedactError` and DSNs through `collector.RedactDSN`.
- **INV-CONN-03**: Classification is deterministic — the same
  underlying error always maps to the same category.
- **INV-CONN-04**: When tested against multiple targets, results
  appear in **config-declared target order** regardless of which
  goroutine finishes first (parallelism is allowed; output order is
  pinned).

## Failure Conditions

- **FC-CONN-01**: `<target-name>` and `--dsn` both supplied →
  exit 2 with a usage error.
- **FC-CONN-02**: `--dsn` missing a required field (host, port,
  user, dbname) → exit 2 with a usage error naming the missing
  field.
- **FC-CONN-03**: Unknown `<target-name>` → exit 2 with a list of
  configured target names.
- **FC-CONN-04**: Config file unreadable when no `--dsn` supplied →
  exit 2 with the path and underlying error.

## Configuration

Consumes the same config file as the daemon when no `--dsn` is
supplied. Does not introduce a new config surface.

## Sensitivity

Low. Connection tester reads config and probes a server; it does not
read user data, query results, or table content. Credentials are
resolved in memory and never logged.

## Out of scope

- Performance benchmarking (connection latency tests beyond a single
  attempt). Operators with that need run their own load tools.
- DSN URL form (`postgres://...`). Field-by-field DSN only, matching
  the daemon's config schema.
- Multi-attempt retry loops. One attempt per target; operator
  re-runs the command if they want a retry.

## Analyzer requirements unblocked

None. This is an operator-only tool — no analyzer-side coupling.

## Coordination with R095

Connect test and doctor share infrastructure:

| Component | Owner | Used by |
|-----------|-------|---------|
| `buildSafeDSN` (DSN field assembly) | `internal/conntest` (after extraction) | doctor C4, conntest |
| `collector.ResolvePassword` | `internal/collector/secrets.go` (existing) | doctor C4, conntest |
| `collector.ValidateRoleSafety` | `internal/collector/rolecheck.go` (existing) | doctor C4, conntest |
| `collector.RedactError` / `RedactDSN` | `internal/collector/secrets.go` (existing) | doctor C4, conntest |

When this issue ships, `buildSafeDSN` should be moved from
`internal/doctor` to either `internal/collector` or a new
`internal/pgconn` package so both consumers share one definition.
That refactor is part of this slice, not a follow-up.
