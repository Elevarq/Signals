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
   AWS prepends a seller namespace, so the full paths are
   `<acct>.dkr.ecr.<region>.amazonaws.com/<seller-ns>/elevarq-signals` (image)
   and `…/<seller-ns>/elevarq-signals-chart` (chart).

3. **Push the 1.0.0 image + chart** into those repos. The script also
   **repackages the chart** so its default image points at the Marketplace ECR
   repo (not ghcr) — required, see Constraints below:
   ```sh
   MP_REGISTRY=<acct>.dkr.ecr.us-east-1.amazonaws.com \
   MP_IMAGE_REPO=<seller-ns>/elevarq-signals \
   MP_CHART_REPO=<seller-ns>/elevarq-signals-chart \
   VERSION=1.0.0 scripts/marketplace-ecr-push.sh
   ```

4. **Add the Helm delivery version:**
   ```sh
   PRODUCT_ID=<product-id> VERSION=1.0.0 \
   IMAGE_URI=<acct>.dkr.ecr.us-east-1.amazonaws.com/<seller-ns>/elevarq-signals:1.0.0 \
   CHART_URI=<acct>.dkr.ecr.us-east-1.amazonaws.com/<seller-ns>/elevarq-signals-chart/signals:1.0.0 \
   RELEASE_NOTES="Elevarq Signals 1.0.0 — first stable release." \
   DELIVERY_DESCRIPTION="Helm install on Amazon EKS. Local-first, read-only PostgreSQL diagnostic collector; passwordless onboarding; no diagnostic-data egress to Elevarq." \
   USAGE_INSTRUCTIONS="1) aws ecr get-login-password --region <region> | helm registry login --username AWS --password-stdin <registry>. 2) Create signals-values.yaml with: target.host, target.dbname, target.user=signals, target.authMethod=aws_rds_iam, target.sslmode=verify-full. 3) helm install signals oci://<chart-repo>/signals --version 1.0.0 -n signals --create-namespace -f signals-values.yaml. Full onboarding: docs/marketplace/install-from-aws-marketplace.md" \
     scripts/marketplace-changeset.sh docs/marketplace/catalog-api/02-add-helm-delivery.json
   ```

5. **Product info + publish** in AMMP: fill description/categories/support/EULA,
   submit, approve the limited listing URL, request Limited → Public.

## Constraints baked into the templates (from the API error catalog)

- `RepositoryType` must be `"ECR"` (only allowed value).
- Helm `HelmChartUri` tag must be **SemVer 2** (our chart tag `1.0.0` qualifies).
- Don't use the `latest` image tag (`INVALID_CONTAINER_IMAGE_TAG`).
- All Helm chart images must be in repos created via `AddRepositories`
  (`INVALID_HELM_CHART_IMAGES`) — i.e. re-host everything (no ghcr). The chart's
  **default `image.repository` must point at the Marketplace ECR repo**, not
  ghcr — `marketplace-ecr-push.sh` repackages the chart and `helm template`-
  asserts no ghcr image remains before pushing.
- Repository names are **flat** (`elevarq-signals`, `elevarq-signals-chart`) —
  the API rejects names that aren't in the `nginx-web-app` format.
- **Cosign decision:** the GHCR cosign signature does **not** carry over — the
  `skopeo` copy moves only the image, not the `.sig` tag, and AWS re-scans (and
  may re-sign) on ingestion. Marketplace artifacts are AWS-scanned, not
  verifiable with our GHCR `cosign verify` commands. We accept this for 1.0
  (re-signing the Marketplace ECR artifacts is a possible later enhancement).
- `SCAN_ERROR` blocks the version if image scanning finds vulnerabilities —
  our Trivy-clean base helps; patch before submitting.
- We list `CompatibleServices: ["EKS"]` only. **Adding `EKS-Anywhere` requires
  a license-secret `OverrideParameters` entry** (`DefaultValue:
  "${AWSMP_LICENSE_SECRET}"`, `NO_LICENSE_SECRET_KEYS`) — out of scope for the
  free EKS listing; add later if we want EKS-Anywhere.

> All values in `02-add-helm-delivery.json` are `${PLACEHOLDERS}`; nothing is
> submitted until you export them and run the script (which fails closed on any
> unsubstituted variable).
