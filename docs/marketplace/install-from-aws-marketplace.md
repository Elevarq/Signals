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
aws ecr get-login-password --region <region> \
  | helm registry login --username AWS --password-stdin <marketplace-ecr-registry>
```

`<marketplace-ecr-registry>` is the `*.dkr.ecr.<region>.amazonaws.com` host
shown on the listing's launch page.

## 3. Install the chart

Onto an existing Amazon EKS cluster (or a self-managed cluster):

```sh
helm install signals oci://<marketplace-ecr-chart-repo>/signals \
  --version 1.0.0 \
  --namespace signals --create-namespace \
  --set target.host=<rds-endpoint> \
  --set target.dbname=<db> \
  --set target.user=signals \
  --set target.authMethod=aws_rds_iam \
  --set target.sslmode=verify-full \
  --set target.sslRootCertFile=/etc/ssl/db/rds-ca.pem
```

The chart is the **same chart** as
[`deploy/helm/signals`](../../deploy/helm/signals/) — only the registry
differs. Its values are documented in
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
least-privilege `pg_monitor` role, and produces a local snapshot — **no data
leaves your account** (Signals has no egress to Elevarq).

## Notes

- **Supported delivery:** Helm on Amazon EKS. Self-managed clusters
  (EKS Anywhere / EC2 / on-prem) use the same chart from the OSS registry —
  see the [open-source install docs](../install/kubernetes-production.md).
- **Architecture:** [`docs/architecture.md`](../architecture.md).
- **Support:** [`SUPPORT.md`](../../SUPPORT.md) · security:
  [`SECURITY.md`](../../SECURITY.md).
