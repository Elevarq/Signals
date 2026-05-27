# Appendix B: Configuration Schema

This appendix defines the configuration model for Arq Signals. Any
conforming implementation must accept this configuration format.

## Configuration sources

Configuration is loaded from (in priority order):

1. **CLI flag**: `--config <path>` (highest priority for file location)
2. **System path**: `/etc/arq/signals.yaml`
3. **Local path**: `./signals.yaml`

The first file found is used. Environment variables override file
values for all supported fields.

## YAML schema

```yaml
# Environment: "dev" (default), "lab", or "prod"
# In prod, TLS enforcement is strict (see R013, TLS validation below).
env: dev

# Collector settings
signals:
  poll_interval: 5m         # Collection cycle interval (duration string)
  retention_days: 30         # Days to retain collected data; 0 or negative disables cleanup
  log_level: info            # debug, info, warn, error
  log_json: false            # Output logs as JSON
  max_concurrent_targets: 4  # Max targets collected in parallel
  target_timeout: 60s        # Per-target collection time budget
  query_timeout: 10s         # Per-query execution timeout
  high_sensitivity_collectors_enabled: false  # Opt-in for collectors
                              # that emit application-authored SQL text
                              # (view/matview/trigger definitions and
                              # function bodies). See "High-sensitivity
                              # collectors" below. Default: false.
  metrics_enabled: false      # Expose the Prometheus /metrics endpoint
                              # on the API listener. Default: false.
                              # The endpoint emits operational metrics
                              # only — see R079 for the full set.
  metrics_path: /metrics      # Path the metrics endpoint is mounted on
                              # when metrics_enabled is true.
                              # Setting this to /health is rejected.
  export_per_collector_files: false  # When true, the export ZIP also
                              # contains a per-collector/<id>.json
                              # directory with the latest-run output
                              # of each collector. Off by default
                              # to keep exports small. See R080.

# PostgreSQL targets (one or more)
targets:
  - name: <string>           # Required. Unique target identifier.
    host: <string>           # Required. PostgreSQL hostname or IP.
    port: <integer>          # Optional. Default: 5432.
    dbname: <string>         # Required. Database name.
    user: <string>           # Required. PostgreSQL username.
    enabled: <boolean>       # Optional. Default: true.
    sslmode: <string>        # Optional. PostgreSQL sslmode value.
    sslrootcert_file: <path> # Optional. Path to CA certificate.

    # Credential source (at most one):
    password_file: <path>    # Read password from file (newline-trimmed)
    password_env: <string>   # Read password from this env var's value
    pgpass_file: <path>      # Read password from pgpass-format file

# Local storage
database:
  path: /data/arq-signals.db # Path to local database file
  wal: true                  # Enable write-ahead logging

# HTTP API
api:
  listen_addr: "127.0.0.1:8081"  # Bind address
  read_timeout: 30s              # HTTP read timeout
  write_timeout: 180s            # HTTP write timeout
```

## Environment variable overrides

All supported environment variables and their corresponding config
fields:

| Variable | Config field | Default | Notes |
|----------|-------------|---------|-------|
| `ARQ_ENV` | `env` | `dev` | |
| `ARQ_ALLOW_INSECURE_PG_TLS` | (env-only) | `false` | Allows weak sslmode in non-prod |
| `ARQ_SIGNALS_ALLOW_UNSAFE_ROLE` | (env-only) | `false` | Allows unsafe role attributes |
| `ARQ_SIGNALS_POLL_INTERVAL` | `signals.poll_interval` | `5m` | |
| `ARQ_SIGNALS_RETENTION_DAYS` | `signals.retention_days` | `30` | |
| `ARQ_SIGNALS_LOG_LEVEL` | `signals.log_level` | `info` | |
| `ARQ_SIGNALS_LOG_JSON` | `signals.log_json` | `false` | |
| `ARQ_SIGNALS_MAX_CONCURRENT_TARGETS` | `signals.max_concurrent_targets` | `4` | |
| `ARQ_SIGNALS_TARGET_TIMEOUT` | `signals.target_timeout` | `60s` | |
| `ARQ_SIGNALS_QUERY_TIMEOUT` | `signals.query_timeout` | `10s` | |
| `ARQ_SIGNALS_HIGH_SENSITIVITY_COLLECTORS_ENABLED` | `signals.high_sensitivity_collectors_enabled` | `false` | Opt-in for definition/body collectors |
| `ARQ_SIGNALS_METRICS_ENABLED` | `signals.metrics_enabled` | `false` | Enable the Prometheus `/metrics` endpoint (R079) |
| `ARQ_SIGNALS_METRICS_PATH` | `signals.metrics_path` | `/metrics` | Path for the metrics endpoint when enabled |
| `ARQ_SIGNALS_EXPORT_PER_COLLECTOR_FILES` | `signals.export_per_collector_files` | `false` | Add `per-collector/<id>.json` files to export ZIPs (R080) |
| `ARQ_SIGNALS_LISTEN_ADDR` | `api.listen_addr` | `127.0.0.1:8081` | |
| `ARQ_SIGNALS_WRITE_TIMEOUT` | `api.write_timeout` | `180s` | |
| `ARQ_SIGNALS_DB_PATH` | `database.path` | `/data/arq-signals.db` | |
| `ARQ_SIGNALS_API_TOKEN` | `api.token` | auto-generated | Bearer token for the local HTTP API. |
| `ARQ_SIGNALS_API_TOKEN_FILE` | `api.token_file` | — | Path to a file containing the bearer token. Beats `ARQ_SIGNALS_API_TOKEN` when both are set. |

Precedence for the resolved API token (low → high, later wins):
`api.token` → `api.token_file` → `ARQ_SIGNALS_API_TOKEN` →
`ARQ_SIGNALS_API_TOKEN_FILE`. `api.token` and `api.token_file`
are mutually exclusive in YAML — setting both is a hard error.
If none of the four are supplied, the daemon generates a 32-byte
random token at startup and logs the SHA-256 fingerprint (not the
value).

## Single-target container mode

For containerized deployments, a single target can be configured
entirely via environment variables. These are appended to any
file-based targets:

| Variable | Default | Required |
|----------|---------|----------|
| `ARQ_SIGNALS_TARGET_HOST` | — | Yes (activates container mode) |
| `ARQ_SIGNALS_TARGET_PORT` | `5432` | No |
| `ARQ_SIGNALS_TARGET_DBNAME` | `postgres` | No |
| `ARQ_SIGNALS_TARGET_USER` | — | Yes |
| `ARQ_SIGNALS_TARGET_NAME` | `default` | No |
| `ARQ_SIGNALS_TARGET_SSLMODE` | — | No |
| `ARQ_SIGNALS_TARGET_PASSWORD_FILE` | — | No |
| `ARQ_SIGNALS_TARGET_PASSWORD_ENV` | — | No |
| `ARQ_SIGNALS_TARGET_PGPASS_FILE` | — | No |

## Credential sources

Each target supports at most one credential source:

| Source | Behavior |
|--------|----------|
| `password_file` | Read file contents, trim trailing newline |
| `password_env` | Read the value of the named environment variable |
| `pgpass_file` | Parse pgpass-format file, match by host:port:dbname:user |
| (none) | Attempt connection without password (peer/trust auth) |

Specifying more than one source for the same target is a validation
error.

Credentials are read fresh on every new connection to support password
rotation without restart.

## High-sensitivity collectors

A subset of collectors emit application-authored SQL text — view
definitions, materialized view definitions, trigger source, and
stored procedure bodies. These can include proprietary business
logic, embedded literals, or commentary the operator may not want
in every snapshot, even when the snapshot stays inside the operator's
own environment.

These collectors are **disabled by default** and require explicit
opt-in via:

- `signals.high_sensitivity_collectors_enabled: true` in the YAML
  config, or
- `ARQ_SIGNALS_HIGH_SENSITIVITY_COLLECTORS_ENABLED=true` env var

Collectors classified as high-sensitivity:

| Collector | Emits |
|---|---|
| `pg_views_definitions_v1` | Full view SQL via `pg_get_viewdef()` |
| `pg_matviews_definitions_v1` | Full materialized-view SQL |
| `pg_triggers_definitions_v1` | Full `CREATE TRIGGER` via `pg_get_triggerdef()` |
| `pg_functions_definitions_v1` | Function/procedure body (`pg_proc.prosrc`) |

When the opt-in flag is `false` (the default), each high-sensitivity
collector appears in `collector_status.json` with `status=skipped`
and `reason=config_disabled`.

This control is for **local operator control over data sensitivity**,
not exfiltration prevention — Arq Signals runs inside the customer's
environment and the snapshot file does not leave the site. The
default-off posture exists because some operators do not want SQL
bodies materialized into the snapshot artifact at all, even for
internal analysis.

## TLS validation

| Environment | Behavior |
|-------------|----------|
| `prod` | Weak sslmode (disable, allow, prefer, require) is rejected. Only verify-ca and verify-full are allowed. `sslrootcert_file` is required. `ARQ_ALLOW_INSECURE_PG_TLS` is not permitted. |
| `dev`, `lab` | Weak sslmode is allowed only if `ARQ_ALLOW_INSECURE_PG_TLS=true` is set. Otherwise the system rejects weak modes with an actionable error message. |

## Validation rules

At startup, the system shall validate the loaded configuration before
starting any collection. Validation produces two outcomes:

### Hard errors (abort startup)

- Unparseable duration strings (`poll_interval`, `target_timeout`,
  `query_timeout`, `read_timeout`, `write_timeout`).
- Missing required target fields: `name`, `host`, `dbname`, `user`.
- Multiple credential sources specified for the same target
  (`password_file`, `password_env`, `pgpass_file` are mutually
  exclusive).
- Duplicate target `name` across the targets list.
- Non-positive `poll_interval`, `target_timeout`, or `query_timeout`.
  (`retention_days` <= 0 is allowed and disables cleanup — see
  warnings below.)
- Empty `database.path`.
- Empty `api.listen_addr`.
- Invalid integer or boolean value in any `ARQ_SIGNALS_*` environment
  variable. Silent parse failures are no longer accepted; a malformed
  override is treated as operator intent that the system cannot honor.
- In `prod` env: weak `sslmode` (`disable`, `allow`, `prefer`,
  `require`) on any enabled target; missing `sslrootcert_file` when
  `sslmode` is `verify-ca` / `verify-full`; `ARQ_ALLOW_INSECURE_PG_TLS`
  set to true.
- `signals.metrics_path` does not start with `/`, equals `/health`,
  or collides with an existing API path (`/status`, `/collect/now`,
  `/export`).

### Warnings (log, continue startup)

- `sslmode=prefer` on a target outside `prod` (recommend `verify-ca`
  or `verify-full`).
- `poll_interval` < 30 seconds (very frequent collection).
- `retention_days` <= 0 (cleanup disabled — snapshots and query runs
  retained indefinitely; the daemon does not delete on its own).
- No targets configured (collector starts but does nothing).

The daemon logs warnings and proceeds. Hard errors abort with a
clear actionable message naming the offending config field or env
variable.
