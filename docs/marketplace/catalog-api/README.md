# AWS Marketplace Catalog API change-sets (Signals container product)

Change-sets to drive the AWS Marketplace container listing for **Elevarq
Signals** via the **Catalog API** (`StartChangeSet`) — no AMMP web UI required.
Tracks [Elevarq/Signals#218](https://github.com/Elevarq/Signals/issues/218);
schemas are from the official
[container-products Catalog API reference](https://docs.aws.amazon.com/marketplace-catalog/latest/api-reference/container-products.html).

> **EULA signed off; pre-submission items remain.** Signals is GA at
> **v1.0.0** and the EULA is finalized + signed off. Before running these
> change-sets, land the website Marketplace-install path and confirm the
> seller-registration entity (see [`../aws-listing.md`](../aws-listing.md)
> status banner). The product does not exist yet; the identifiers below are
> populated once `CreateProduct` runs.

Run a change-set with
[`scripts/marketplace-changeset.sh`](../../../scripts/marketplace-changeset.sh)
(renders `${VARS}`, submits, polls `DescribeChangeSet`).

## The first publish IS fully Catalog-API-driven (verified)

The first publish is **not** portal-only. Verified on the live Elevarq
pgAgroal launch by running the calls against current AWS docs, the whole
sequence is API-driven:

| # | Step | Change type | State transition |
|---|------|-------------|------------------|
| 1 | Create product + ECR repos | `CreateProduct` + `AddRepositories` (`01-create-product-and-repos.json`, chained via `$CreateProduct.Entity.Identifier`) | → Draft |
| 2 | Push image + Helm chart into the ECR repos | CLI re-host — see "Re-host" below | — |
| 3 | Product info (title, descriptions, highlights, category, keywords, logo) | `UpdateInformation` (`03-update-information.json`) — **works on a Draft** | Draft |
| 4 | Free offer: create + terms | `CreateOffer`, offer `UpdateInformation`, `UpdateLegalTerms` (`CustomEula`), `UpdateSupportTerms` — **no pricing term** (see below) | Draft |
| 5 | Release product **and** offer (combined) | `ReleaseProduct` + `ReleaseOffer` in one change-set | Draft → **Limited** |
| 6 | Add the Helm delivery version | `AddDeliveryOptions` (`02-add-helm-delivery.json`) — **only once Limited** | Limited |
| 7 | Make public | `UpdateVisibility` `TargetVisibility: Public` (own change-set; AWS Seller-Ops manual review) | Limited → Public |

### Reach expansion — additional delivery options (post-launch, #233)

Once the listing is live, extra delivery options can be added — each reuses the
**same** re-hosted Marketplace-ECR image and is its **own** AWS review round
(never bundle). See `specifications/marketplace-container-image-delivery.md`.

| Lever | Change type | Details type | Notes |
|-------|-------------|--------------|-------|
| Container image (ECS / Fargate / `docker pull`), #234 | `AddDeliveryOptions` (`04-add-container-image-delivery.json`) | `EcrDeliveryOptionDetails`, `CompatibleServices: ["ECS"]` | Additive; does not touch the Helm option. `ContainerImages` MUST equal the Helm option's image (one artifact, two options). Buyer runs it with no Helm/K8s, so `CI_USAGE_INSTRUCTIONS` must document durable snapshot storage — the Fargate task filesystem is ephemeral, attach EFS (or an EBS/bind volume on EC2-backed ECS). |
| EKS add-on, #236 | `AddDeliveryOptions` (`05-add-eks-addon-delivery.json`) | `EksAddOnDeliveryOptionDetails` | Additive; reuses the same image **and** chart as the Helm option. `AddOnName: signals`, `AddOnType: observability`, `Namespace: signals` are **immutable across all future versions** — do not change once published (`INCOMPATIBLE_ADDON_*`). Only **one** add-on option per version. `K8S_VERSIONS` must list only tested EKS versions. Unlike Helm/container options, the add-on option is **not** auto-Public — publishing it is a separate visibility step. See `specifications/marketplace-eks-addon-delivery.md`. |
| AMI / EC2 Image Builder, #235 | **separate `AmiProduct@1.0` product** (not a delivery option on this container product) | — | **Groundwork only**, demand-gated. The reusable EC2 Image Builder component lives in `deploy/aws/imagebuilder/` (`specifications/marketplace-ami-image-builder.md`); the live AMI product + its onboarding/review are deferred until real EC2-baked-deployment demand. No runnable AMI change-set is committed. |

Notes on the ordering, all verified on the pgAgroal launch:

1. **`UpdateInformation` requires all core fields in one call** and works on a
   Draft: `ProductTitle`, `ShortDescription`, `LongDescription`, `LogoUrl`,
   `Highlights` (max 3), `AdditionalResources`, `SupportDescription`,
   `Categories` (1-3), `SearchKeywords` (max 15, ≤250 combined chars). See
   `03-update-information.json`.
2. **A free offer takes no pricing term.** `CreateOffer` + offer
   `UpdateInformation` + `UpdateLegalTerms` + `UpdateSupportTerms` are the
   terms. `UpdatePricingTerms` with `PricingModel: Free` is **rejected** —
   omit it entirely.
3. **`AddDeliveryOptions` needs the product Limited first.** On a Draft it
   fails `INCOMPATIBLE_PRODUCT_STATUS` ("Use a Public or Limited or Restricted
   product"). Release the product (step 5) before adding the version. Adding
   the version triggers AWS's async image + chart **scan** — the long pole.
4. **`UpdateVisibility: Public` is its own change-set** and runs an AWS Seller
   Operations manual review. It needs a published **public seller profile**, or
   it fails `MISSING_SELLER_PROFILE_INFORMATION`. Do not run without the EULA
   signed and an explicit go.

### LegalTerm / CustomEula (free offer)

The free offer's legal term references the EULA as a **PDF in an accessible S3
bucket** (not inline text):

```json
{ "Type": "LegalTerm", "Documents": [
  { "Type": "CustomEula",
    "Url": "https://elevarq-marketplace-public.s3.amazonaws.com/eula/elevarq-signals-eula-v1.pdf" } ] }
```

Render [`../EULA.md`](../EULA.md) to PDF and host it at that S3 URL when 1.0
ships. Rationale + counsel gates: [`../EULA-review-notes.md`](../EULA-review-notes.md).

### LogoUrl must be an S3 URL

`UpdateInformation`'s `LogoUrl` regex accepts any https URL, but a deeper
`INVALID_MEDIA` check rejects non-S3 hosts (*"Provide a new URL for media
stored in S3."*). The logo therefore lives in S3, reusing the **Elevarq
company logo** already hosted for the portfolio (repackaged-software rule: the
product logo is the company logo — one asset for every Elevarq listing):

```
https://elevarq-marketplace-public.s3.amazonaws.com/logos/elevarq-512.png
```

Spec: square 1:1, 120–640px, transparent PNG, <5MB (the hosted asset is a
512×512 transparent PNG).

## Re-host the image + chart (verified commands)

Push the released 1.0.0 image and Helm chart into the Marketplace ECR repos.
**skopeo is not required or installed** — `docker buildx imagetools create`
copies the full multi-arch manifest registry-to-registry:

```sh
MP=<acct>.dkr.ecr.us-east-1.amazonaws.com   # Marketplace ECR registry host
NS=<seller-ns>                              # AWS prepends a seller namespace

aws ecr get-login-password --region us-east-1 \
  | docker login --username AWS --password-stdin "$MP"
aws ecr get-login-password --region us-east-1 \
  | helm registry login --username AWS --password-stdin "$MP"

# Image: multi-arch copy ghcr -> Marketplace ECR (no skopeo)
docker buildx imagetools create \
  -t "$MP/$NS/elevarq-signals:1.0.0" ghcr.io/elevarq/signals:1.0.0

# Chart: pull, repoint default image to the MP ECR repo, rename so it lands at
# the granted repo path, repackage, push (see "Chart path gotcha" below).
```

[`scripts/marketplace-ecr-push.sh`](../../../scripts/marketplace-ecr-push.sh)
automates the chart repackage/rename and asserts no `ghcr.io` image remains.

To find the created ECR repo URIs after step 1:

```sh
aws ecr describe-repositories \
  --query 'repositories[?contains(repositoryName, `elevarq-signals`)].repositoryUri'
```

The full paths are `<acct>.dkr.ecr.<region>.amazonaws.com/<seller-ns>/elevarq-signals`
(image) and `…/<seller-ns>/elevarq-signals-chart` (chart).

## Apply a change-set

```sh
aws marketplace-catalog start-change-set \
  --catalog AWSMarketplace \
  --cli-input-json file://docs/marketplace/catalog-api/03-update-information.json
# poll WITHOUT piping the full body to jq — embedded UsageInstructions newlines
# break jq. Use --query instead:
aws marketplace-catalog describe-change-set \
  --catalog AWSMarketplace --change-set-id <id> \
  --query '{Status:Status,Errors:ChangeSet[].ErrorDetailList}'
```

`ListEntities` for offers uses the bare entity type **`Offer`** (no `@version`);
every other call uses **`Offer@1.0`** / `ContainerProduct@1.0`.

## Constraints baked into the change-sets (from the API error catalog)

- `RepositoryType` must be `"ECR"` (only allowed value).
- Helm `HelmChartUri` tag must be **SemVer 2** — `1.0.0` qualifies.
- No `latest` image tag (`INVALID_CONTAINER_IMAGE_TAG`).
- All Helm chart images must live in repos created via `AddRepositories`
  (`INVALID_HELM_CHART_IMAGES`). The published chart's default
  `image.repository` is repointed from `ghcr.io/elevarq/signals` to the
  Marketplace ECR image repo before the chart is pushed; `helm template`
  asserts no `ghcr.io` image remains. AWS rejects charts that still reference
  ghcr images.
- Repository names are **flat** (`elevarq-signals`, `elevarq-signals-chart`) —
  the API rejects names that aren't in the `nginx-web-app` format.
- `SCAN_ERROR` blocks the version if image scanning finds vulnerabilities —
  the Trivy-clean 1.0.0 image must pass; patch before submitting.
- `CompatibleServices: ["EKS"]` only. Adding `EKS-Anywhere` requires a
  license-secret `OverrideParameters` entry — out of scope for the free EKS
  listing.

## Chart path gotcha (ECR repos are not hierarchical)

`helm push chart.tgz oci://$MP/$NS/elevarq-signals-chart` appends the chart
name and tries to create `$NS/elevarq-signals-chart/signals` — a **separate**
ECR repository that does not exist and the seller cannot auto-create
cross-account → `403 Forbidden`. Fix: land the chart **exactly** at the granted
repo by renaming the chart so its name is the repo's last path segment
(`Chart.yaml name: elevarq-signals-chart`) and pushing to the **parent**
namespace (`oci://$MP/$NS`). Renaming the chart artifact does **not** change the
deployed resource names: the Signals chart derives every name from the Helm
**release name** (`{{ .Release.Name }}-signals`) and fixed
`app.kubernetes.io/name: signals` labels, not from `.Chart.Name` — so a buyer
running `helm install signals …` still gets `signals`-named resources, and no
`nameOverride` is needed (unlike pgAgroal, whose chart keyed off `.Chart.Name`).
Final chart URI: `$MP/$NS/elevarq-signals-chart:1.0.0`.

## SignatureVerificationKey — not an image-signing gate

The product entity carries an active RSA public key (`PublicKeyVersion 1`)
after creation. This is the **metering** signature-verification key for the
`RegisterUsage` flow (AWS generates the keypair, holds the private half, stamps
the public half into the product). It is **not** an image-signing requirement:
there is no signing field or signing-related error in the container-product
API, image signing is not required to `AddDeliveryOptions` or publish, and free
ECS/EKS products are exempt from `RegisterUsage`. No action needed on the key.
