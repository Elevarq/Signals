# Helm Example

A starter Helm chart is provided at
[`deploy/helm/arq-signals/`](../../deploy/helm/arq-signals/).

## Install

Each release publishes the chart as an OCI artifact to GHCR, so you
can install by reference without a repo checkout (the chart version
matches the release version):

```bash
helm install arq-signals oci://ghcr.io/elevarq/charts/arq-signals \
  --version 0.10.0-beta.1 \
  --set target.host=db.example.com \
  --set target.user=arq_signals \
  --set target.dbname=postgres \
  --set target.passwordSecretName=arq-pg-password
```

The published chart is cosign-signed (keyless, GitHub OIDC) — the same
trust root as the container image. Verify before install:

```bash
cosign verify ghcr.io/elevarq/charts/arq-signals:0.10.0-beta.1 \
  --certificate-identity-regexp='github.com/Elevarq/Arq-Signals/.github/workflows/release.yml@' \
  --certificate-oidc-issuer='https://token.actions.githubusercontent.com'
```

Or install straight from a working-tree checkout:

```bash
helm install arq-signals deploy/helm/arq-signals/ \
  --set target.host=db.example.com \
  --set target.user=arq_signals \
  --set target.dbname=postgres \
  --set target.passwordSecretName=arq-pg-password
```

## Minimal required values

| Value | Description |
|-------|-------------|
| `target.host` | PostgreSQL hostname |
| `target.user` | PostgreSQL monitoring role |
| `target.dbname` | Database to monitor |
| `target.passwordSecretName` | K8s Secret containing the DB password |

## Custom values file

Create a `my-values.yaml`:

```yaml
target:
  host: db.prod.internal
  user: arq_signals
  dbname: myapp
  sslmode: verify-full
  passwordSecretName: arq-db-credentials

collector:
  pollInterval: 5m
  retentionDays: 14

env: prod
```

Install with:

```bash
helm install arq-signals deploy/helm/arq-signals/ \
  -f my-values.yaml
```

## What the chart provides

- **Deployment** with health/readiness probes on `/health`
- **Service** (ClusterIP) on port 8081
- **ConfigMap** with generated `signals.yaml`, mounted into the pod at `/etc/arq/signals.yaml` and consumed by the daemon at startup
- **PVC** for persistent SQLite storage
- Non-root security context (UID 10001)

## Current status

This is a **starter scaffold** suitable for evaluation and simple
deployments. For production Kubernetes use, you may want to add:
- Ingress or NetworkPolicy
- Pod disruption budget
- Custom resource limits
- Monitoring/alerting integration

## Configuration sources

The chart renders two sources of configuration:

1. **`signals.yaml`** in the ConfigMap, mounted at
   `/etc/arq/signals.yaml`. Carries `env`, collector cadences,
   database path, and the API listen address.
2. **Environment variables** on the container — the same
   `.Values.collector.*` / `.Values.env` / `.Values.api.port`
   values plus the single-target overrides
   (`ARQ_SIGNALS_TARGET_*`) and any secret-backed values
   (`PG_PASSWORD`, optional `ARQ_SIGNALS_API_TOKEN`).

When both sources set the same field, **environment variables
win** (this is the documented config-loader precedence). Both
sources are rendered from the same `values.yaml` so they cannot
diverge in a normal `helm install`.

## DB credentials

Create a Kubernetes Secret:

```bash
kubectl create secret generic arq-db-credentials \
  --from-literal=password='your-pg-password'
```

The chart injects this as the `PG_PASSWORD` environment variable via
`ARQ_SIGNALS_TARGET_PASSWORD_ENV`.

## API bearer token

The daemon's HTTP API (pause / resume / reload / status / export /
metrics) is bearer-token authenticated. Two installation modes:

**Default — auto-generated.** When no token is supplied, the binary
generates a 32-byte random token at startup and logs its SHA-256
fingerprint (never the value). Suitable for evaluation only — the
token changes on every restart, so any caller that pinned the
previous value gets 401.

**Production — managed Secret.** Create a Kubernetes Secret holding
the token and reference it from values:

```bash
# 32 bytes is the minimum the binary will accept in env=prod.
TOKEN=$(openssl rand -base64 32)
kubectl create secret generic arq-api-token \
  --from-literal=token="${TOKEN}"
```

```yaml
# my-values.yaml
api:
  tokenSecretName: arq-api-token
  # tokenSecretKey: token   # default; override only if your Secret
  #                         # uses a different key name.
```

The chart projects the value as `ARQ_SIGNALS_API_TOKEN` via
`secretKeyRef` — the token never lands in a ConfigMap or in any
rendered manifest beyond the Secret reference name. The daemon's
weak-token validator runs at startup; weak tokens are warnings in
`env=dev`/`lab` and hard errors in `env=prod`.
