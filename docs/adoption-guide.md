# Adoption Guide

This guide covers installing, configuring, and operating Elevarq Signals in development and production environments.

## Getting Started

You can go from zero to your first snapshot in under five minutes.

### 1. Install

**From source:**

```bash
git clone https://github.com/elevarq/signals.git
cd signals
make build
```

This produces `bin/signals` (daemon) and `bin/signalsctl` (CLI).

**Docker:**

```bash
docker pull ghcr.io/elevarq/signals:1.0.0
```

### 2. Configure

Create a minimal configuration file:

```yaml
# signals.yaml
env: dev
targets:
  - name: my-database
    host: localhost
    port: 5432
    dbname: postgres
    user: signals
    password_file: /path/to/pg_password
    sslmode: prefer
    enabled: true
```

The config file is called `signals.yaml`. Elevarq Signals searches for it at `/etc/signals/signals.yaml` and `./signals.yaml` by default, or you can pass `--config <path>`.

### 3. Start the daemon

```bash
./bin/signals --config signals.yaml
```

The daemon begins collecting on the configured `poll_interval` (default 5m).

### 4. Trigger a one-shot collection

```bash
signalsctl collect now
```

`signalsctl` talks to the running Elevarq Signals daemon over its HTTP API. Set `SIGNALS_API_TOKEN` to the token shown at daemon startup (or configure a fixed token via the same env var).

### 5. Export

Export the collected data as a snapshot:

```bash
signalsctl export --output snapshot.zip
```

The output is a self-contained ZIP archive in `signals-snapshot.v1` format.

### 6. Check status

```bash
signalsctl status
signalsctl version
```

---

## Production Deployment

### Docker

Run Elevarq Signals as a long-lived container:

```bash
docker run -d \
  --name signals \
  -v /etc/signals/signals.yaml:/etc/signals/signals.yaml:ro \
  -v signals-data:/data \
  -p 127.0.0.1:8081:8081 \
  ghcr.io/elevarq/signals:1.0.0
```

The container runs as a non-root user (UID 10001) on Alpine 3.21. The API listens on port 8081. Bind it to loopback unless you need external access.

### Single-target Docker setup via environment variables

For simple deployments with a single PostgreSQL target, you can configure everything through environment variables instead of a config file:

```bash
docker run -d \
  --name signals \
  -e SIGNALS_TARGET_HOST=db.example.com \
  -e SIGNALS_TARGET_PORT=5432 \
  -e SIGNALS_TARGET_DBNAME=postgres \
  -e SIGNALS_TARGET_USER=signals \
  -e SIGNALS_TARGET_PASSWORD_FILE=/run/secrets/pg_password \
  -e SIGNALS_TARGET_SSLMODE=verify-full \
  -e SIGNALS_ENV=prod \
  -v /run/secrets/pg_password:/run/secrets/pg_password:ro \
  -v signals-data:/data \
  -p 127.0.0.1:8081:8081 \
  ghcr.io/elevarq/signals:1.0.0
```

The following target-level env vars are supported:

| Variable | Description | Default |
|----------|-------------|---------|
| `SIGNALS_TARGET_HOST` | PostgreSQL host (required to activate env-based target) | -- |
| `SIGNALS_TARGET_PORT` | PostgreSQL port | 5432 |
| `SIGNALS_TARGET_DBNAME` | Database name | postgres |
| `SIGNALS_TARGET_USER` | Username | -- |
| `SIGNALS_TARGET_NAME` | Target name | default |
| `SIGNALS_TARGET_PASSWORD_FILE` | Path to password file | -- |
| `SIGNALS_TARGET_PASSWORD_ENV` | Env var containing the password | -- |
| `SIGNALS_TARGET_PGPASS_FILE` | Path to pgpass file | -- |
| `SIGNALS_TARGET_SSLMODE` | TLS mode | -- |

### TLS

Elevarq Signals connects to PostgreSQL over TLS when the target's `sslmode` field is set to `require` or stricter. For production, use `verify-ca` or `verify-full` and provide `sslrootcert_file` pointing to the CA certificate:

```yaml
targets:
  - name: prod-primary
    host: db.example.com
    port: 5432
    dbname: postgres
    user: signals
    password_file: /run/secrets/pg_password
    sslmode: verify-full
    sslrootcert_file: /etc/ssl/certs/pg-ca.crt
    enabled: true
```

In production (`env: prod`), weak TLS modes (`disable`, `allow`, `prefer`, `require`) are rejected. In non-production environments, set `SIGNALS_ALLOW_INSECURE_PG_TLS=true` to allow weak TLS for local development.

For the HTTP API, place Elevarq Signals behind a TLS-terminating reverse proxy (nginx, Caddy, or a cloud load balancer).

---

## Credential Management

Elevarq Signals supports three credential sources. Choose whichever fits your environment. Only one may be specified per target.

| Method | Config field | Description |
|--------|-------------|-------------|
| Password file | `password_file: /path/to/file` | Reads the password from a file. Compatible with Docker secrets and Kubernetes secret volumes. |
| Environment variable | `password_env: PG_PASSWORD` | Reads the password from the named environment variable. The value of that variable is the password. |
| pgpass file | `pgpass_file: /path/to/.pgpass` | Reads credentials from a pgpass-format file. |

Example using `password_file`:

```yaml
targets:
  - name: prod-primary
    host: db.example.com
    port: 5432
    dbname: postgres
    user: signals
    password_file: /run/secrets/pg_password
    sslmode: verify-full
    sslrootcert_file: /etc/ssl/certs/pg-ca.crt
    enabled: true
```

Example using `password_env`:

```yaml
targets:
  - name: prod-primary
    host: db.example.com
    port: 5432
    dbname: postgres
    user: signals
    password_env: PG_PASSWORD_PROD
    sslmode: verify-full
    sslrootcert_file: /etc/ssl/certs/pg-ca.crt
    enabled: true
```

Credentials are read fresh on each connection attempt. They are never cached in memory beyond a single connection, never written to SQLite, and never included in snapshot exports.

### Monitoring Role Setup

Create a dedicated read-only role for Elevarq Signals. The following works across RDS, Cloud SQL, Aurora, and self-managed PostgreSQL:

```sql
CREATE ROLE signals WITH LOGIN PASSWORD 'your-secure-password';

-- Grant read access to statistics views
GRANT pg_monitor TO signals;

-- Optional: enable query-level statistics
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
```

On **Amazon RDS / Aurora**, `pg_monitor` is available on all supported versions (14+).

On **Google Cloud SQL**, grant the `cloudsqlsuperuser` role or assign `pg_monitor` directly.

### Role Safety

Elevarq Signals enforces strict role safety by default. If your monitoring role has superuser, replication, or bypassrls attributes, collection is blocked with an actionable error message. Use the recommended monitoring role setup:

```sql
CREATE ROLE signals WITH LOGIN PASSWORD '...';
GRANT pg_monitor TO signals;
-- Do NOT grant superuser, replication, or bypassrls
```

For managed databases (RDS, Cloud SQL, Aurora), the equivalent role grants are documented in each provider's documentation for pg_monitor.

An explicit override (`SIGNALS_ALLOW_UNSAFE_ROLE=true`) exists for lab/dev environments only and is not recommended for production.

---

## Multi-Target Setup

Elevarq Signals supports concurrent collection across multiple targets:

```yaml
# signals.yaml
env: prod
signals:
  poll_interval: 5m
  retention_days: 30
  max_concurrent_targets: 4
  target_timeout: 60s
  query_timeout: 10s
targets:
  - name: prod-primary
    host: primary.db.internal
    port: 5432
    dbname: app
    user: signals
    password_file: /run/secrets/pg_password_primary
    sslmode: verify-full
    sslrootcert_file: /etc/ssl/certs/pg-ca.crt
    enabled: true

  - name: prod-replica
    host: replica.db.internal
    port: 5432
    dbname: app
    user: signals
    password_file: /run/secrets/pg_password_replica
    sslmode: verify-full
    sslrootcert_file: /etc/ssl/certs/pg-ca.crt
    enabled: true

  - name: staging
    host: staging.db.internal
    port: 5432
    dbname: app
    user: signals
    password_env: PG_PASSWORD_STAGING
    sslmode: require
    enabled: true
database:
  path: /data/signals.db
  wal: true
api:
  listen_addr: "127.0.0.1:8081"
```

Each target is collected independently. A failure on one target does not block collection from others. The `max_concurrent_targets` setting (default 4) controls how many targets are collected in parallel.

---

## Full Configuration Reference

Here is the complete `signals.yaml` schema with all available fields and their defaults:

```yaml
# signals.yaml
env: dev  # dev, lab, prod
signals:
  poll_interval: 5m
  retention_days: 30
  log_level: info           # debug, info, warn, error
  log_json: false
  max_concurrent_targets: 4
  target_timeout: 60s
  query_timeout: 10s
targets:
  - name: my-database
    host: localhost
    port: 5432
    dbname: postgres
    user: signals
    password_file: /path/to/password    # or password_env or pgpass_file
    sslmode: prefer
    sslrootcert_file: /path/to/ca.crt   # required for verify-ca/verify-full
    enabled: true
database:
  path: /data/signals.db
  wal: true
api:
  listen_addr: "127.0.0.1:8081"
  read_timeout: 30s
  write_timeout: 180s
```

---

## Integrating with Existing Workflows

### Scheduled Export via Cron

Run Elevarq Signals as a daemon and export snapshots on a schedule:

```bash
# Export a snapshot every hour
0 * * * * /usr/local/bin/signalsctl export --output /var/snapshots/signals-$(date +\%Y\%m\%d-\%H\%M).zip
```

### Feeding Snapshots to Custom Scripts

The `signals-snapshot.v1` format is a ZIP archive containing NDJSON files. Parse it with standard tools:

```bash
# List contents
unzip -l snapshot.zip

# Extract and process with jq
unzip -p snapshot.zip "*.ndjson" | jq '.query_name'
```

### Archival

Snapshots are self-contained and immutable. Store them in any object store (S3, GCS, MinIO) or local filesystem for historical analysis.

```bash
# Upload to S3
aws s3 cp snapshot.zip s3://my-bucket/signals/$(date +%Y/%m/%d)/
```

---

## Upgrading

### Snapshot Format Versioning

Snapshot archives include a format version identifier (`signals-snapshot.v1`). When the format changes, the version number increments. Older snapshots remain readable by newer versions of Elevarq Signals.

### Binary Upgrades

Replace the binary or container image and restart. Elevarq Signals uses SQLite with WAL mode for local storage; the schema is migrated automatically on startup. No manual migration steps are required.

### Backward Compatibility

- Snapshot format versions follow semantic versioning. Minor versions add fields; major versions may change structure.
- Configuration file format is stable within a major version.
- SQL collectors may be added or updated between releases; existing collector output remains structurally compatible.

---

## Troubleshooting

### Connection Refused

**Symptom:** `dial tcp: connect: connection refused`

**Causes:**
- PostgreSQL is not running or not listening on the specified host/port.
- A firewall or security group is blocking the connection.
- The target `host` is set to `localhost` but PostgreSQL is bound to a different interface.

**Fix:** Verify connectivity with `psql` using the same host, port, user, and dbname. Check `listen_addresses` in `postgresql.conf` and any network-level firewall rules.

### Permission Denied

**Symptom:** `permission denied for relation pg_stat_activity`

**Causes:**
- The monitoring role does not have `pg_monitor` membership.
- On RDS/Aurora, the role was not granted the required managed policy role.

**Fix:** Grant the `pg_monitor` role: `GRANT pg_monitor TO signals;`

### Role Safety Blocked

**Symptom:** `collection blocked: role has superuser attribute`

**Causes:**
- The configured user has superuser, replication, or bypassrls privileges.

**Fix:** Create a dedicated monitoring role without those privileges:
```sql
CREATE ROLE signals WITH LOGIN PASSWORD '...';
GRANT pg_monitor TO signals;
```

For lab/dev environments only, set `SIGNALS_ALLOW_UNSAFE_ROLE=true` to override.

### TLS Rejected

**Symptom:** `sslmode=prefer is not allowed in prod`

**Causes:**
- Production mode (`env: prod`) requires `verify-ca` or `verify-full` with a CA certificate.

**Fix:** Set `sslmode: verify-full` and provide `sslrootcert_file` in your target config. For non-production environments, set `SIGNALS_ALLOW_INSECURE_PG_TLS=true` to allow weaker TLS modes.

### Extension Not Found

**Symptom:** Collector skipped with `extension pg_stat_statements not available`

**Causes:**
- `pg_stat_statements` is not installed or not listed in `shared_preload_libraries`.

**Fix:** This is informational, not an error. Elevarq Signals automatically skips collectors that depend on unavailable extensions. To enable the extension, add `pg_stat_statements` to `shared_preload_libraries` in `postgresql.conf` and run `CREATE EXTENSION pg_stat_statements;` in each target database.

### SQLite Locked

**Symptom:** `database is locked` errors during collection

**Causes:**
- Another process holds a write lock on the SQLite database file.
- The filesystem does not support WAL mode (e.g., some network filesystems).

**Fix:** Ensure only one Elevarq Signals instance writes to a given SQLite database file. Use a local filesystem that supports `fcntl` locking.

### High Memory Usage During Export

**Symptom:** Memory spikes when exporting large snapshots.

**Fix:** Export more frequently to reduce the volume of data per snapshot. Elevarq Signals streams results to disk during collection, but export assembles the ZIP archive in memory.
