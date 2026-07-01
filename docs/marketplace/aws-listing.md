# AWS Marketplace listing — Elevarq Signals (free container product)

Working package for listing **Elevarq Signals** on AWS Marketplace as a
**free container product** (Helm chart delivery). Tracks
[Elevarq/Signals#218](https://github.com/Elevarq/Signals/issues/218).

This file is the content + steps we drive via the AWS Marketplace Catalog API
(`StartChangeSet`); no AMMP web UI is required. The verified publish sequence
and per-call gotchas live in [`catalog-api/README.md`](catalog-api/README.md).

> **Status: EULA signed off; pre-submission items remain.** Signals is GA at
> **v1.0.0** (release gate met) and the **EULA wording is finalized and signed
> off** (product/compliance, 2026-06-30 — see
> [`EULA-review-notes.md`](EULA-review-notes.md)). Before public submission:
> (1) add the **website Marketplace-install path**, and (2) operationally
> confirm the AWS seller-registration entity matches "Scantr LLC d/b/a
> Elevarq". Then run the Catalog-API sequence (§5). The
> `SignatureVerificationKey` question is resolved (§6 — it is the inert
> metering key, not an image-signing requirement).

## 0. Pre-publish gate

- [x] Seller account is a registered seller (free publishing needs no
      tax/bank; independent of the paid Analyzer review).
- [ ] **v1.0.0** tagged and published (non-prerelease) — `release.yml`. Image +
      signed multi-arch OCI Helm chart on ghcr, GitHub Release live.
- [ ] Marketplace ECR repos created (`AddRepositories`): `elevarq-signals`
      (image) and `elevarq-signals-chart` (chart).
- [ ] 1.0.0 image + chart re-hosted into the Marketplace ECR (see
      [`catalog-api/README.md`](catalog-api/README.md); chart repoints its
      default image to the Marketplace ECR repo, no `ghcr.io`).
- [ ] Product logo hosted in S3 — `LogoUrl` requires an S3 URL (§1).
- [ ] EULA rendered to PDF and hosted in S3 for the offer's `CustomEula` term
      (§4) — gated on legal sign-off.

## 1. Product metadata

These are the `UpdateInformation` fields — captured in
[`catalog-api/03-update-information.json`](catalog-api/03-update-information.json).

| Field | Value |
|-------|-------|
| Product title | Elevarq Signals |
| Short description | Local-first, read-only PostgreSQL diagnostic collector. Passwordless onboarding to Amazon RDS and Aurora over verify-full TLS, with no diagnostic-data egress to Elevarq. |
| Long description | Elevarq Signals is an open-source (BSD-3-Clause) diagnostic collector for PostgreSQL that runs entirely inside your own environment. It connects to a managed database, collects a structured, read-only diagnostic snapshot on a schedule, and keeps every byte local: no telemetry and no diagnostic-data egress to Elevarq. Read-only is enforced by three independent layers, and unsafe roles (superuser, replication) are blocked before collection begins. Onboarding is passwordless against Amazon RDS and Aurora using RDS IAM authentication (or a cloud secret store via AWS Secrets Manager / AWS Systems Manager Parameter Store) over verify-full TLS, with a least-privilege `pg_monitor` role. The collector ships as a signed, multi-architecture (linux/amd64 and linux/arm64) container with an SBOM, and a production Helm chart for Amazon EKS provides liveness/readiness probes, least-privilege security contexts, and a persistent volume for local snapshots. |
| Highlights (≤3) | (1) Read-only by design: three independent enforcement layers, unsafe roles blocked before collection. (2) Passwordless onboarding to Amazon RDS / Aurora via RDS IAM (or a cloud secret store) over verify-full TLS with a least-privilege `pg_monitor` role. (3) Local-first: no telemetry and no diagnostic-data egress to Elevarq; signed multi-arch container + Helm chart for Amazon EKS. |
| Category | Infrastructure Software (validated; AWS has **no "Database" category**) |
| Logo | `https://elevarq-marketplace-public.s3.amazonaws.com/logos/elevarq-512.png` — must be an **S3 URL** (a non-S3 host fails `INVALID_MEDIA`). Reuses the **Elevarq company logo** for the whole portfolio (512×512 transparent PNG; spec: square 1:1, 120–640px, transparent PNG, <5MB). |
| Search keywords (≤15, ≤250 combined chars) | PostgreSQL, Postgres, diagnostics, observability, monitoring, database, Amazon RDS, Aurora, pg_monitor, Kubernetes, Helm, EKS, read-only, snapshot |
| Vendor | Elevarq (DBA of Scantr LLC) |
| Pricing | Free (no pricing term — `UpdatePricingTerms PricingModel:Free` is rejected) |
| Source / homepage | https://github.com/Elevarq/Signals |
| License | BSD-3-Clause |

## 2. Delivery option — Helm chart (primary)

- **Method:** Helm chart, installed via the Helm CLI (Amazon EKS, or
  self-managed EKS Anywhere / EC2 / on-prem).
- **Chart + image:** re-hosted into the AWS-Marketplace-owned ECR repos. The
  published chart's default image points at the Marketplace ECR image repo —
  **not** `ghcr.io` (AWS rejects external chart images:
  `INVALID_HELM_CHART_IMAGES`).
- Catalog-API change-set: [`catalog-api/02-add-helm-delivery.json`](catalog-api/02-add-helm-delivery.json)
  (`ReleaseName`/`Namespace` = `signals`). Runs only **after** the product is
  Limited.

### Buyer usage instructions (rendered on the listing)

```sh
# Authenticate Helm to the AWS Marketplace registry (buyer side)
aws ecr get-login-password --region us-east-1 \
  | helm registry login --username AWS --password-stdin <marketplace-ecr-registry>
```

Configure a values file, then install with `-f`:

```yaml
# signals-values.yaml
target:
  host: <rds-endpoint>
  dbname: <db>
  user: signals
  authMethod: aws_rds_iam      # passwordless RDS IAM
  sslmode: verify-full
```

```sh
helm install signals \
  oci://<marketplace-ecr-registry>/<seller-ns>/elevarq-signals-chart \
  --version 1.0.0 -n signals --create-namespace -f signals-values.yaml
```

Note the chart URI ends at the **granted repo** (`elevarq-signals-chart`), not
a `…/signals` sub-path — the chart is renamed to land there. Resource names are
unaffected: the chart keys off the Helm release name (`signals`), not the chart
name, so no `nameOverride` is needed (see the chart-path gotcha in
[`catalog-api/README.md`](catalog-api/README.md)). Full passwordless onboarding
(IAM role / DB grant / CA bundle) is documented in
[`install-from-aws-marketplace.md`](install-from-aws-marketplace.md), the Helm
chart README, and [`docs/database-connections.md`](../database-connections.md).

### Reach: additional delivery options (assessed)

- **Container image (Amazon ECS / Fargate / `docker pull`)** — _optional, low
  effort._ A second delivery option pointing at the **same** Marketplace ECR
  image we already re-hosted; reaches ECS/non-Helm buyers with no new artifact.
  Defer until after the first publish.
- **CloudFormation / QuickLaunch (deploy-to-EKS)** — _optional, higher effort._
  Defer.
- **EKS-Anywhere** compatibility — requires a license-secret
  `OverrideParameters` entry; out of scope for the free EKS listing.

## 3. Support & maintenance (free-product eligibility)

- **Support process:** community support via GitHub Issues at
  https://github.com/Elevarq/Signals; security reports via `SECURITY.md`
  (`security@elevarq.com`). No commercial-support upsell on the listing.
- **Update cadence:** versioned releases via CI (`release.yml`) with
  cosign-signed, SBOM-attached, Trivy-scanned multi-arch images and a signed
  OCI Helm chart; security patches cut as needed.

## 4. License terms

- Customer-facing EULA (upload-ready): [`EULA.md`](EULA.md) — a clean custom
  EULA referencing the BSD-3-Clause license. Submitted to the free offer as a
  `CustomEula` legal term, referenced as a **PDF in S3** (the offer takes no
  pricing term). This artifact contains contract text only.
- Decision rationale + counsel gates: [`EULA-review-notes.md`](EULA-review-notes.md)
  — custom-EULA-vs-SCMP decision, the Scantr LLC dba Elevarq entity gate, and
  the seller-entity / EULA-party / `LICENSE`-copyright alignment. **Legal
  sign-off required** before submission.

## Related artifacts

- **Buyer install guide:** [`install-from-aws-marketplace.md`](install-from-aws-marketplace.md).
- **Architecture diagram (upload asset):** [`assets/architecture-listing.png`](assets/architecture-listing.png).
- **Catalog-API automation:** [`catalog-api/`](catalog-api/) +
  `scripts/marketplace-changeset.sh`.
- **ECR re-host:** `scripts/marketplace-ecr-push.sh`.

## 5. Submit checklist (ordered — verified sequence)

1. [ ] Confirm registered-seller status (§0).
2. [ ] Publish `v1.0.0`.
3. [ ] **`CreateProduct` + `AddRepositories`**
       ([`catalog-api/01-create-product-and-repos.json`](catalog-api/01-create-product-and-repos.json))
       — Draft product + ECR repos. Capture the product Identifier.
4. [ ] Re-host the 1.0.0 image + Helm chart into the Marketplace ECR repos
       (`docker buildx imagetools create` + chart repackage; **not** skopeo).
5. [ ] Host the product logo in S3 (`LogoUrl` requires S3 — non-S3 fails
       `INVALID_MEDIA`).
6. [ ] **`UpdateInformation`** ([`catalog-api/03-update-information.json`](catalog-api/03-update-information.json))
       — title, descriptions, highlights, category, keywords, logo. Works on
       the Draft.
7. [ ] **Free `Offer@1.0`** — `CreateOffer` + offer `UpdateInformation` +
       `UpdateLegalTerms` (`CustomEula` S3 PDF) + `UpdateSupportTerms`. **No
       pricing term.** **Gated on EULA legal sign-off (§4).**
8. [ ] **`ReleaseProduct` + `ReleaseOffer` (combined)** — Draft → **Limited**.
9. [ ] **`AddDeliveryOptions`** ([`catalog-api/02-add-helm-delivery.json`](catalog-api/02-add-helm-delivery.json))
       — add the Helm version (now allowed, product is Limited). Triggers AWS's
       async image + chart scan (the long pole).
10. [ ] **`UpdateVisibility: Public`** — own change-set; AWS Seller-Ops manual
        review; needs a published public seller profile
        (`MISSING_SELLER_PROFILE_INFORMATION` otherwise). Gated on the EULA +
        explicit go.

> The whole sequence is Catalog-API-driven — no AMMP web UI required. See
> [`catalog-api/README.md`](catalog-api/README.md) for the verified per-step
> detail and error catalog.

## 6. Decisions & open questions

**Open (must clear before submit)**

- **EULA / entity wording** — counsel review (Scantr LLC dba Elevarq); see
  [`EULA-review-notes.md`](EULA-review-notes.md). This is the remaining gate
  before submit (the v1.0.0 GA release is cut).

**Decided**

- **First publish is fully Catalog-API-driven** (not portal-only). Verified on
  the live pgAgroal launch — see [`catalog-api/README.md`](catalog-api/README.md).
- **Free offer takes no pricing term.** `UpdatePricingTerms PricingModel:Free`
  is rejected; the offer's terms are legal (`CustomEula`) + support only.
- **`SignatureVerificationKey` is NOT an image-signing gate.** The active RSA
  public key on the product is the **metering** signature-verification key for
  the `RegisterUsage` flow: AWS auto-generates the keypair at product creation
  and holds the private half; the seller never signs anything with it. Image
  signing is not required to `AddDeliveryOptions` or publish, and free ECS/EKS
  products are exempt from `RegisterUsage`. The real ingestion gate is AWS's
  vulnerability/secret **scan** (`SCAN_ERROR`), which a Trivy-clean image
  passes. No action needed on the key.
- **Re-host uses `docker buildx imagetools create`** (multi-arch,
  registry-to-registry) — skopeo is **not** required or installed.
- **Cosign GHCR signatures do not carry into the Marketplace ECR.** The
  multi-arch copy moves only the image; AWS re-scans (and may re-sign) on
  ingestion. Re-signing the Marketplace ECR artifacts is a possible later
  enhancement.
- **Chart path.** The chart is published at the granted ECR repo
  `<seller-ns>/elevarq-signals-chart` (chart renamed to match the repo's last
  path segment, pushed to the parent namespace). Deployed resource names are
  unaffected because the Signals chart keys off the Helm release name, not the
  chart name — no `nameOverride` needed. Pushing the unrenamed chart to the
  repo creates a non-existent hierarchical sub-repo → `403`.
