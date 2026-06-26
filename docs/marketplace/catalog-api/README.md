# AWS Marketplace Catalog API change-sets (Signals container product)

Draft change-sets to drive most of the AWS Marketplace container listing for
Signals via the **Catalog API** (`StartChangeSet`) instead of click-ops.
Tracks [Elevarq/Signals#218](https://github.com/Elevarq/Signals/issues/218);
schemas are from the official
[container-products Catalog API reference](https://docs.aws.amazon.com/marketplace-catalog/latest/api-reference/container-products.html)
and the `CreateProduct` change type (`ContainerProduct@1.0`).

Run them with [`scripts/marketplace-changeset.sh`](../../../scripts/marketplace-changeset.sh)
(renders `${VARS}`, submits, polls `DescribeChangeSet`).

## What is API-driven vs portal-only

| Step | How |
|------|-----|
| Create product (Draft) + create ECR repos | **API** — `01-create-product-and-repos.json` (`CreateProduct` + `AddRepositories`, chained via `$CreateProduct.Entity.Identifier`) |
| Push image + Helm chart into the ECR repos | **CLI** — `scripts/marketplace-ecr-push.sh` |
| Add the 1.0.0 Helm delivery version | **API** — `02-add-helm-delivery.json` (`AddDeliveryOptions` / `HelmDeliveryOptionDetails`) |
| Product info (descriptions, categories, support, EULA) | **Portal** (AMMP) — or `UpdateInformation` (fields not fully captured here). Simplest in the portal for a first listing. |
| Publish **Limited → Public** | **Portal** (AMMP). The API's `UpdateDeliveryOptionsVisibility` is **EKS-add-on only**; Helm/Container delivery options publish to Public via the portal submit + the new-product "limited listing URL" approval. |

## Flow

1. **Create product + repos:**
   ```sh
   scripts/marketplace-changeset.sh docs/marketplace/catalog-api/01-create-product-and-repos.json
   ```
   Capture the new **product Identifier** (printed; also `aws marketplace-catalog
   list-entities --catalog AWSMarketplace --entity-type ContainerProduct`).

2. **Find the created ECR repo URIs:**
   ```sh
   aws ecr describe-repositories \
     --query 'repositories[?contains(repositoryName, `elevarq`)].repositoryUri'
   ```
   AWS prepends a seller namespace, so the full path is
   `<acct>.dkr.ecr.<region>.amazonaws.com/<seller-ns>/elevarq/signals` etc.

3. **Push the 1.0.0 image + chart** into those repos:
   ```sh
   MP_REGISTRY=<acct>.dkr.ecr.us-east-1.amazonaws.com \
   MP_IMAGE_REPO=<seller-ns>/elevarq/signals \
   MP_CHART_REPO=<seller-ns>/elevarq/charts \
   VERSION=1.0.0 scripts/marketplace-ecr-push.sh
   ```

4. **Add the Helm delivery version:**
   ```sh
   PRODUCT_ID=<product-id> VERSION=1.0.0 \
   IMAGE_URI=<acct>.dkr.ecr.us-east-1.amazonaws.com/<seller-ns>/elevarq/signals:1.0.0 \
   CHART_URI=<acct>.dkr.ecr.us-east-1.amazonaws.com/<seller-ns>/elevarq/charts/signals:1.0.0 \
   RELEASE_NOTES="Elevarq Signals 1.0.0 — first stable release." \
   DELIVERY_DESCRIPTION="Helm install on Amazon EKS. Local-first, read-only PostgreSQL diagnostic collector; passwordless onboarding; no data egress." \
   USAGE_INSTRUCTIONS="aws ecr get-login-password | helm registry login --username AWS --password-stdin <registry>; helm install signals oci://<chart-repo> --version 1.0.0 --set target.host=<rds> --set target.authMethod=aws_rds_iam --set target.sslmode=verify-full" \
     scripts/marketplace-changeset.sh docs/marketplace/catalog-api/02-add-helm-delivery.json
   ```

5. **Product info + publish** in AMMP: fill description/categories/support/EULA,
   submit, approve the limited listing URL, request Limited → Public.

## Constraints baked into the templates (from the API error catalog)

- `RepositoryType` must be `"ECR"` (only allowed value).
- Helm `HelmChartUri` tag must be **SemVer 2** (our chart tag `1.0.0` qualifies).
- Don't use the `latest` image tag (`INVALID_CONTAINER_IMAGE_TAG`).
- All Helm chart images must be in repos created via `AddRepositories`
  (`INVALID_HELM_CHART_IMAGES`) — i.e. re-host everything (no ghcr).
- `SCAN_ERROR` blocks the version if image scanning finds vulnerabilities —
  our Trivy-clean base helps; patch before submitting.
- We list `CompatibleServices: ["EKS"]` only. **Adding `EKS-Anywhere` requires
  a license-secret `OverrideParameters` entry** (`DefaultValue:
  "${AWSMP_LICENSE_SECRET}"`, `NO_LICENSE_SECRET_KEYS`) — out of scope for the
  free EKS listing; add later if we want EKS-Anywhere.

> All values in `02-add-helm-delivery.json` are `${PLACEHOLDERS}`; nothing is
> submitted until you export them and run the script (which fails closed on any
> unsubstituted variable).
