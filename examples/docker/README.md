# Docker Example

## Build the image

```bash
docker build -t arq-signals .
```

With version metadata:

```bash
docker build \
  --build-arg VERSION=0.2.0 \
  --build-arg COMMIT=$(git rev-parse --short HEAD) \
  -t arq-signals:0.2.0 .
```

## Run with environment variables

```bash
docker run -d --name arq-signals \
  -e ARQ_SIGNALS_TARGET_HOST=db.example.com \
  -e ARQ_SIGNALS_TARGET_USER=arq_signals \
  -e ARQ_SIGNALS_TARGET_DBNAME=postgres \
  -e ARQ_SIGNALS_TARGET_PASSWORD_ENV=PG_PASSWORD \
  -e PG_PASSWORD=your_password \
  -e ARQ_ALLOW_INSECURE_PG_TLS=true \
  -e ARQ_ENV=dev \
  -e ARQ_SIGNALS_API_TOKEN=dev-local-only-replace-in-prod-32chars \
  -v arq-data:/data \
  -p 8081:8081 \
  arq-signals
```

## Run with a config file

```bash
docker run -d --name arq-signals \
  -v /path/to/signals.yaml:/etc/arq/signals.yaml:ro \
  -v arq-data:/data \
  -p 127.0.0.1:8081:8081 \
  arq-signals --config /etc/arq/signals.yaml
```

## Collect and export

```bash
# Trigger collection
curl -X POST http://localhost:8081/collect/now \
  -H "Authorization: Bearer dev-local-only-replace-in-prod-32chars"

# Download snapshot
curl -o snapshot.zip http://localhost:8081/export \
  -H "Authorization: Bearer dev-local-only-replace-in-prod-32chars"

# Inspect
unzip -l snapshot.zip
```

## Docker Compose (with PostgreSQL)

A ready-to-use Docker Compose file is available at
[`examples/docker-compose.yml`](../docker-compose.yml). It starts
Elevarq Signals alongside PostgreSQL 16 with a pre-configured monitoring
role:

```bash
docker compose -f examples/docker-compose.yml up -d
```

## Container details

- **Base:** Alpine 3.21
- **User:** non-root (UID 10001)
- **Init:** tini
- **Volume:** `/data` for SQLite
- **Port:** 8081

## Safe role vs superuser

By default, Elevarq Signals blocks superuser roles. For local Docker
testing with `postgres`, set `ARQ_SIGNALS_ALLOW_UNSAFE_ROLE=true`.
For production, use a dedicated `arq_signals` role with `pg_monitor`.

See also: [docs/container.md](../../docs/container.md)
