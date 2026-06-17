# Container Deployment

## Build the container

```bash
docker build -t signals .
```

With version metadata:

```bash
docker build \
  --build-arg VERSION=0.2.0 \
  --build-arg COMMIT=$(git rev-parse --short HEAD) \
  --build-arg DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  -t signals:0.2.0 .
```

## Run the collector

### Minimal example

```bash
docker run -d --name signals \
  -e SIGNALS_TARGET_HOST=db.example.com \
  -e SIGNALS_TARGET_USER=signals \
  -e SIGNALS_TARGET_DBNAME=postgres \
  -e SIGNALS_TARGET_PASSWORD_ENV=PG_PASSWORD \
  -e PG_PASSWORD=your_password \
  -e SIGNALS_ALLOW_INSECURE_PG_TLS=true \
  -e SIGNALS_ENV=dev \
  -v signals-data:/data \
  -p 8081:8081 \
  signals
```

### With a config file

```bash
docker run -d --name signals \
  -v /etc/signals/signals.yaml:/etc/signals/signals.yaml:ro \
  -v signals-data:/data \
  -p 127.0.0.1:8081:8081 \
  signals --config /etc/signals/signals.yaml
```

### Production TLS

```bash
docker run -d --name signals \
  -e SIGNALS_TARGET_HOST=db.prod.internal \
  -e SIGNALS_TARGET_USER=signals \
  -e SIGNALS_TARGET_DBNAME=postgres \
  -e SIGNALS_TARGET_SSLMODE=verify-full \
  -e SIGNALS_TARGET_PASSWORD_FILE=/run/secrets/pg_password \
  -e SIGNALS_ENV=prod \
  -v signals-data:/data \
  -v /run/secrets:/run/secrets:ro \
  -p 127.0.0.1:8081:8081 \
  signals
```

## Environment variables

All `SIGNALS_*` environment variables are supported. See the
README for the complete list.

Key variables:

| Variable | Description |
|----------|-------------|
| `SIGNALS_TARGET_HOST` | PostgreSQL hostname |
| `SIGNALS_TARGET_PORT` | PostgreSQL port (default: 5432) |
| `SIGNALS_TARGET_DBNAME` | Database name (default: postgres) |
| `SIGNALS_TARGET_USER` | PostgreSQL user |
| `SIGNALS_TARGET_PASSWORD_FILE` | Path to password file |
| `SIGNALS_TARGET_PASSWORD_ENV` | Env var containing password |
| `SIGNALS_POLL_INTERVAL` | Collection interval (default: 5m) |
| `SIGNALS_API_TOKEN` | API bearer token (auto-generated if unset) |
| `SIGNALS_ENV` | Environment: dev, lab, prod |

## Container details

- **Base image:** Alpine 3.21
- **User:** non-root (UID 10001)
- **Init:** tini (PID 1 reaping)
- **Volumes:** `/data` for SQLite storage
- **Port:** 8081 (HTTP API)
- **Binaries:** `signals` (daemon), `signalsctl` (CLI)

## Triggering collection and export

```bash
# From outside the container
curl -X POST http://localhost:8081/collect/now \
  -H "Authorization: Bearer $SIGNALS_API_TOKEN"

curl -o snapshot.zip http://localhost:8081/export \
  -H "Authorization: Bearer $SIGNALS_API_TOKEN"

# From inside the container
docker exec signals signalsctl collect now
docker exec signals signalsctl export --output /data/snapshot.zip
```
