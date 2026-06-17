# Kubernetes — production deployment profile

Tracking: [#140](https://github.com/Elevarq/Arq-Signals/issues/140).
Related: [`access-control.md`](../security/access-control.md),
[`operational-readiness.md`](../observability/operational-readiness.md),
[`support-matrix.md`](../compatibility/support-matrix.md).

This page documents how to deploy Elevarq Signals on Kubernetes with
the controls a platform team will want to see before approving
the workload. Pairs with the chart at
`deploy/helm/signals/`.

## TL;DR

```sh
helm install signals deploy/helm/signals/ \
  --namespace observability \
  --create-namespace \
  --values production.yaml
```

Where `production.yaml` flips the production-ready settings on:

```yaml
env: prod
target:
  host: postgres.prod.svc.cluster.local
  passwordSecretName: pg-credentials
  passwordSecretKey: password
  sslmode: verify-full
  sslrootcertFile: /etc/ssl/certs/ca-certificates.crt
api:
  tokenSecretName: signals-api
  tokenSecretKey: token
serviceAccount:
  create: true
networkPolicy:
  enabled: true
  ingressPodSelectors:
    - app.kubernetes.io/name: prometheus
  targetCIDRs:
    - 10.42.0.0/24    # the postgres-server subnet
  kubeDNS:
    - 10.96.0.10/32
podDisruptionBudget:
  enabled: true
  maxUnavailable: 0
persistence:
  enabled: true
  size: 10Gi
  storageClass: ssd-retain
resources:
  requests: { cpu: 100m, memory: 128Mi }
  limits:   { cpu: 500m, memory: 256Mi }
```

The bearer-token Secret is provisioned out-of-band:

```sh
kubectl -n observability create secret generic signals-api \
  --from-literal=token="$(openssl rand -base64 32)"
kubectl -n observability create secret generic pg-credentials \
  --from-literal=password="$(operator-pasted-value)"
```

## Production vs evaluation

Two values knobs control the production posture:

| Setting | Eval default | Production |
|---|---|---|
| `env` | `dev` | `prod` (hard-fails on weak API token) |
| `target.sslmode` | `prefer` (warns) | `verify-full` (target identity verified) |
| `api.tokenSecretName` | empty (auto-gen) | set (Secret-backed; survives restart) |
| `serviceAccount.create` | true | true |
| `networkPolicy.enabled` | false | **true** (locks egress + ingress) |
| `podDisruptionBudget.enabled` | false | true |
| `persistence.enabled` | true | true |
| `persistence.size` | 1Gi | sized for retention × snapshot rate |
| `persistence.storageClass` | "" (cluster default) | named storage class with retain policy |
| `resources.requests / limits` | low | sized for collection-batch peak |

`env=prod` is the load-bearing distinction — it triggers the
`config.ValidateStrict` rules that refuse to start with weak
tokens / missing security baselines.

## Least-privilege posture

Per-component summary for the platform team's review:

| Surface | Posture |
|---|---|
| User | `runAsNonRoot: true`, `runAsUser: 10001`, `runAsGroup: 10001` |
| Filesystem | `readOnlyRootFilesystem: true`; only the SQLite snapshot PVC is mounted writable |
| Capabilities | All dropped; `allowPrivilegeEscalation: false` |
| Seccomp | `RuntimeDefault` |
| ServiceAccount | Dedicated per release; `automountServiceAccountToken: false` (Signals never talks to the apiserver) |
| Network | Optional NetworkPolicy denies all ingress + egress except Postgres + DNS + named scrapers |
| Image | Multi-arch (`amd64` / `arm64`) distroless base; no shell, no package manager |
| API auth | Bearer token via Secret; `subtle.ConstantTimeCompare`; strong-token validation |
| Outbound network | None except Postgres + DNS (NetworkPolicy enforces) |
| Volumes | PVC for SQLite store; emptyDir for `/tmp` (read-only root) |

## Deployment topologies

### Single-replica (default)

Signals is a stateful collector that maintains a local SQLite
snapshot store. The store assumes single-writer semantics; the
chart ships `replicas: 1`. This is the supported topology at v1.

Survival expectations:

- **Pod restart**: snapshot store survives via PVC.
- **Node failure**: if the PVC's `accessModes: [ReadWriteOnce]`
  binds to a node-local volume, recovery requires node restoration
  OR a fresh-cluster install. Use a network-backed storage class
  for production.
- **Cluster upgrade**: PodDisruptionBudget with
  `maxUnavailable: 0` blocks voluntary eviction during drains;
  the platform team coordinates upgrades.

### Multi-replica (future v1.1)

Out of scope for v1.0. SQLite-on-network-FS has correctness
considerations (lock semantics across NFS / EFS / longhorn). The
roadmap is a per-target sharded collector pool; tracked in
[v1.1 milestone](https://github.com/Elevarq/Arq-Signals/milestone/2).

## Upgrade behavior

```sh
helm upgrade signals deploy/helm/signals/ \
  --namespace observability \
  --values production.yaml \
  --version 0.8.0
```

What happens:

1. Helm renders the new release; new ConfigMap + Deployment.
2. Deployment rollout: new pod scheduled, readiness probe waits
   for `/health` → 200, old pod terminated.
3. PVC unchanged; the new pod re-mounts the existing SQLite store
   on `/data/arq-signals.db`. Schema migrations run on startup
   per `internal/store::migrate`.
4. Secrets unchanged; the bearer token survives.

The SQLite migration path is forward-only and idempotent —
re-running the same version against an already-migrated store is
a no-op. Downgrading is NOT supported (schema may have evolved).

## Rollback

```sh
helm rollback signals 0 --namespace observability
```

What survives:

- PVC (the new pod re-mounts whatever schema the OLD version
  expects). If the OLD version's binary can't open the NEW
  schema, the readiness probe will fail and the rollback will be
  visible as a pod-crash-loop.

What does NOT survive:

- Configuration changes that were also rolled back (env vars,
  new collector toggles, etc.) revert to the previous values
  file's content.

**Safe-rollback recipe**: before a risky upgrade, snapshot the
PVC (volume-snapshot API or backup tool) so you can restore the
old schema if rollback fails.

## Uninstall

```sh
helm uninstall signals --namespace observability
```

Removed:

- Deployment, ConfigMap, Service, ServiceAccount, NetworkPolicy
  (if enabled), PodDisruptionBudget (if enabled).

**Retained (intentional — operator must explicitly clean up):**

- **PVC**: Helm does not delete PVCs by default; the SQLite
  snapshot store survives uninstall. Delete with:
  ```sh
  kubectl -n observability delete pvc signals-signals-data
  ```
- **Secrets**: `signals-api` (API bearer token) and
  `pg-credentials` (Postgres password) were provisioned out-of-
  band and Helm does not manage them. Delete with:
  ```sh
  kubectl -n observability delete secret signals-api pg-credentials
  ```
- **Namespace**: not deleted by `helm uninstall`. If you want the
  namespace gone, `kubectl delete namespace observability` after
  the helm uninstall + PVC/Secret cleanup.

The retention is deliberate: an operator who runs `helm uninstall`
for a reconfiguration round-trip MUST NOT lose collected
historical snapshots. Production-team review feedback drove this
choice.

## Validation

After install:

```sh
# Pod ready?
kubectl -n observability get pods -l app.kubernetes.io/name=signals

# Run the operator preflight from inside the pod
kubectl -n observability exec deploy/signals-signals -- \
  signalsctl doctor --config /etc/arq-signals/signals.yaml --json

# Confirm the API token Secret is wired (no token in the rendered manifest):
helm template signals deploy/helm/signals --values production.yaml | \
  grep -A 5 ARQ_SIGNALS_API_TOKEN
```

The `doctor --json` output is the suitable evidence artefact for
SOC 2 / ISO 27001 audits — closed schema, no secrets, deterministic.

## Test surface

The chart rendering rules above are pinned by:

| Control | Test file |
|---|---|
| API token Secret wiring | `tests/signals_helm_api_token_test.go` |
| Production-profile manifests render (PVC, SA, NetworkPolicy, PDB) | `tests/signals_helm_production_test.go` (this PR) |

## Threat model

In scope:

- Cluster-internal lateral movement (NetworkPolicy denies).
- Image-level privilege escalation (distroless, dropped caps,
  read-only root, non-root user, seccomp).
- Token leakage (Secret-backed + valueFrom + no manifest echo).
- Voluntary disruption during cluster maintenance (PDB).

Out of scope (separate controls):

- Cluster-level RBAC misconfiguration (operator's responsibility).
- Multi-tenant noisy-neighbour resource pressure (cluster's
  scheduling + LimitRange / ResourceQuota responsibility).
- Volume encryption at rest (storage-class responsibility; not
  application-layer).
