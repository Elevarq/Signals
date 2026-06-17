# Elevarq Signals — Examples

Deployment templates and reference configurations.

## Which one do I want?

| Pattern | File | Use when |
|---|---|---|
| Dev quickstart | [`docker-compose.yml`](docker-compose.yml) | Trying arq-signals on a self-contained local PostgreSQL. Bootstraps PG 16 + monitoring role + sample data. |
| Production | [`docker-compose.prod.yml`](docker-compose.prod.yml) | Connecting to your real PostgreSQL with a Docker secret for the password. No embedded DB. |
| Kubernetes | [`helm/`](helm/) | Helm-based deployment on a K8s cluster. |

---

## Dev quickstart

Self-contained. PG password is in the compose file, database lives in a
volume. Good for "does it work on my machine" validation.

    docker compose -f examples/docker-compose.yml up -d

    # Trigger a collection:
    curl -X POST http://localhost:8081/collect/now \
      -H "Authorization: Bearer dev-local-only-replace-in-prod-32chars"

    # Export:
    curl -o snapshot.zip http://localhost:8081/export \
      -H "Authorization: Bearer dev-local-only-replace-in-prod-32chars"

Teardown and wipe:

    docker compose -f examples/docker-compose.yml down -v

---

## Production

Connects to an **existing** PostgreSQL. Password is delivered via a
Docker secret, read fresh on every connection, never logged or embedded.

Three-step setup:

1. **Store credentials outside version control.**

        mkdir -p secrets
        echo -n 'YOUR_MONITOR_PASSWORD' > secrets/pg_password
        openssl rand -hex 32 > secrets/api_token
        chmod 600 secrets/pg_password secrets/api_token
        echo 'secrets/' >> .gitignore

2. **Copy and edit the YAML config** to point at your database.

        cp examples/signals.yaml examples/signals.prod.yaml
        # Edit target block: host, port, dbname, user, sslmode.
        # Leave: password_file: /run/secrets/pg_password

3. **Start the container.**

        docker compose -f examples/docker-compose.prod.yml up -d

Both the PG password and the API token are delivered via Docker secrets;
neither appears in the environment, the compose file, or container logs.

The compose file binds the API to `127.0.0.1` — do not expose it
publicly. Use SSH tunneling or a reverse proxy with authentication for
remote access.

### Rotating credentials

The PG password is re-read on every new PostgreSQL connection, so
rotation does not require restart:

    echo -n 'NEW_MONITOR_PASSWORD' > secrets/pg_password

(Existing pooled connections keep the old password until they close;
to force an immediate refresh, `docker compose restart signals`.)

The API token is read at startup, so rotation requires a restart:

    openssl rand -hex 32 > secrets/api_token
    docker compose -f examples/docker-compose.prod.yml restart signals

---

## Credential sources

`signals.yaml` supports three password sources per target. Pick whichever
matches your deployment:

| Source | YAML field | Good for |
|---|---|---|
| File | `password_file: /path/to/file` | Docker secrets, Kubernetes secret mounts, bind-mounted files. Trailing newline is stripped. |
| Env var | `password_env: VAR_NAME` | Dev/test, CI, managed-secret injection that only exposes env. |
| pgpass | `pgpass_file: /path/to/.pgpass` | Multi-target deployments that share a standard `.pgpass`. Supports `*` wildcards. |

All three are re-read on every new PostgreSQL connection. Credential
rotation therefore does not require a restart.

## Safety defaults

- Collection runs in a read-only transaction
  (`default_transaction_read_only=on`).
- The monitoring role must be a member of `pg_monitor` unless
  `SIGNALS_ALLOW_UNSAFE_ROLE=true` is explicitly set (dev only).
- Superuser roles are blocked by default.
- SQL queries pass a static linter that rejects any non-SELECT statement,
  embedded semicolons, and a denylist of side-effect functions
  (`pg_sleep`, `pg_terminate_backend`, `dblink`, etc.).
- Passwords never appear in error messages — a redaction layer ensures
  credential-handling failures log as "credential resolution failed
  (details redacted)".

See [`local-safe-role/`](local-safe-role/) for the recommended
monitoring-role setup on your own PostgreSQL.

## Environment-variable reference

For single-target deployments without a YAML file, see
[`docker/README.md`](docker/README.md).

## Kubernetes

See [`helm/`](helm/) for the Helm chart and sample values, including
how to point `password_file` at a Kubernetes Secret mount.
