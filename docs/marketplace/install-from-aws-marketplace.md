# Install Elevarq Signals from AWS Marketplace

Buyer-facing install guide for the **AWS Marketplace** listing of Elevarq
Signals. The image and Helm chart are served from the
**AWS-Marketplace-managed Amazon ECR** registry (not ghcr.io); everything
else — the passwordless onboarding, roles, and TLS — is identical to the
open-source deployment docs, which this page links to.

> Placeholders below (`<…>`) are filled in by the published listing's launch
> page; they appear automatically in the AWS Marketplace usage instructions
> for the version you subscribed to.

## 1. Subscribe

In **AWS Marketplace → Elevarq Signals → View purchase options**, accept the
terms and subscribe (Signals is **free** — no charges). This grants your
account pull access to the Marketplace ECR repositories for the product.

## 2. Authenticate Helm to the Marketplace registry

```sh
aws ecr get-login-password --region us-east-1 \
  | helm registry login --username AWS --password-stdin <marketplace-ecr-registry>
```

`<marketplace-ecr-registry>` is the `*.dkr.ecr.us-east-1.amazonaws.com` host
shown on the listing's launch page.

## 3. Install the chart

Onto an existing Amazon EKS cluster (or a self-managed cluster). Put your
configuration in a values file (`signals-values.yaml`) rather than `--set`
flags:

```yaml
# signals-values.yaml
target:
  host: <rds-endpoint>
  dbname: <db>
  user: signals
  authMethod: aws_rds_iam      # passwordless RDS IAM
  sslmode: verify-full
  sslRootCertFile: /etc/ssl/db/rds-ca.pem
```

```sh
helm install signals \
  oci://<marketplace-ecr-registry>/<seller-ns>/elevarq-signals-chart \
  --version 1.0.0 \
  --namespace signals --create-namespace \
  -f signals-values.yaml
```

The chart URI ends at the **granted Marketplace ECR repo**
(`elevarq-signals-chart`), not a `…/signals` sub-path — the chart artifact is
renamed to land there. Your Kubernetes resource names are unaffected: they
derive from the Helm **release name** (`signals` above), not the chart name. It
is otherwise the **same chart** as
[`deploy/helm/signals`](../../deploy/helm/signals/); only the registry and the
chart artifact name differ. Its values are documented in
[`deploy/helm/signals/README.md`](../../deploy/helm/signals/README.md).

## 4. Complete the passwordless onboarding

The Marketplace install is just the chart; the one-time identity + database
setup is the same as the open-source path:

- **Auth methods** (`aws_rds_iam`, `secret_store` via Secrets Manager / SSM
  Parameter Store, or password): [`docs/database-connections.md`](../database-connections.md).
- **The database role grant** (`CREATE ROLE signals` / `GRANT rds_iam` /
  `GRANT pg_monitor`) and rationale: [`docs/postgres-role.md`](../postgres-role.md).
- **IRSA / Pod Identity** wiring for the collector's AWS identity and the
  `verify-full` CA bundle: the Helm README (`#114` snippets) and
  [`docs/install/kubernetes-production.md`](../install/kubernetes-production.md).

## 5. Verify

```sh
kubectl -n signals exec deploy/signals -- signalsctl status
kubectl -n signals exec deploy/signals -- signalsctl export --output /data/snapshot.zip
```

A healthy install connects **passwordless** over `verify-full` TLS with a
least-privilege `pg_monitor` role, and produces a local snapshot. Signals sends
**no telemetry and no diagnostic data to Elevarq**; the only outbound calls are
the cloud-auth/TLS requests you configure, made to your own cloud's services.

## 6. Operational details (AWS Marketplace)

- **Secrets / sensitive info.** With `aws_rds_iam` there is **no password** —
  the collector mints a short-lived RDS IAM token from its IRSA / Pod Identity
  role. For the `secret_store` or `password` methods, the credential is a
  Kubernetes `Secret` you provide (referenced by `api.tokenSecretName` for the
  control-plane token and `target.passwordSecretName` for a DB password); it is
  injected as an environment variable and never written to a ConfigMap. The
  control-plane API requires a bearer token.
- **Data location & at-rest encryption.** Collected diagnostics are written to
  a local SQLite database on the pod's `/data` volume (the chart's PVC,
  `persistence.size` default 1Gi). Encryption at rest is **customer-managed**:
  back the PVC with an encrypted `StorageClass` (e.g. EBS `gp3` with a KMS
  key). No data leaves the cluster except the snapshot you explicitly export.
- **Health checks.** The container exposes `GET /health` (unauthenticated) on
  `:8081`; the chart wires liveness/readiness probes to it.
- **AWS infrastructure cost (customer responsibility).** Signals is free, but
  you pay AWS for the resources it runs on: the EKS cluster + worker node
  capacity, the EBS volume backing the PVC, RDS/Aurora, and — if you use the
  `secret_store` method — AWS Secrets Manager / SSM Parameter Store and KMS.
- **Service quotas.** No special quotas beyond normal cluster compute and
  storage capacity; the collector is a single, low-footprint pod
  (default requests 50m CPU / 64Mi).

## Notes

- **Supported delivery:** Helm on Amazon EKS. Self-managed clusters
  (EKS Anywhere / EC2 / on-prem) use the same chart from the OSS registry —
  see the [open-source install docs](../install/kubernetes-production.md).
- **Architecture:** [`docs/architecture.md`](../architecture.md).
- **Support:** [`SUPPORT.md`](../../SUPPORT.md) · security:
  [`SECURITY.md`](../../SECURITY.md).
