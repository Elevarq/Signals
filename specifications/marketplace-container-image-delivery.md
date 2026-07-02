# Marketplace Container-Image Delivery Option (ECS / Fargate / `docker pull`)

## Status

ACTIVE

## Type

Integration mapping — contract across the boundary between the re-hosted
Marketplace-ECR image, the AWS Marketplace `ContainerProduct@1.0` listing, and a
buyer's non-Helm container runtime (Amazon ECS, including Fargate). Tests
mandatory.

## Purpose

Add a **second delivery option** to the live Elevarq Signals Marketplace listing
so buyers who do not run Helm/EKS can obtain and run the collector on Amazon ECS
/ AWS Fargate (or any `docker pull`-based runtime). The listing today ships a
single `HelmDeliveryOptionDetails` option (Amazon EKS). This slice adds an
`EcrDeliveryOptionDetails` option that points at the **same** already-re-hosted
Marketplace-ECR image — no new build, no new artifact, no change to the Helm
option.

The value is pure reach: the ECS / Fargate / non-Helm buyer segment can adopt
Signals without a Kubernetes cluster. Part of #233 (reach expansion). Inherits
the umbrella's cross-cutting decisions (#233) — do not restate them here.

## Scope

One `AddDeliveryOptions` change-set against the existing `ContainerProduct@1.0`,
adding exactly one `EcrDeliveryOptionDetails` delivery option alongside the
existing Helm option, plus the buyer-facing usage instructions that reproduce —
outside of Helm — the deployment contract the chart encodes (writable snapshot
volume, non-root, config + secret injection, `us-east-1` ECR login).

In scope:

- A committed change-set template `docs/marketplace/catalog-api/04-add-container-image-delivery.json`.
- Usage instructions for the ECS/Fargate/`docker pull` path.
- The same preflight guards the Helm change-set already passes (ASCII-only, no
  unsubstituted `${...}`, valid JSON) via `scripts/marketplace-changeset.sh`.

Out of scope (see "Out of scope").

## Interfaces

### Inputs (change-set, `EcrDeliveryOptionDetails`)

| Field | Type | Constraint |
|-------|------|------------|
| `DeliveryOptionTitle` | string | Buyer-facing, e.g. `Container image (Amazon ECS / Fargate)`. ASCII. |
| `ContainerImages` | array<string> | Exactly the Marketplace-ECR image URI at the released version, e.g. `<mp-ecr>/elevarq/elevarq-signals:<VERSION>`. Same image as the Helm option. |
| `CompatibleServices` | array<string> | `["ECS"]`. Valid AWS set: `ECS`, `EKS`, `ECS-Anywhere`, `EKS-Anywhere`, `Bedrock-AgentCore`. Fargate is an ECS launch type — covered by `ECS`. |
| `UsageInstructions` | string | ≤ 4000 chars. ASCII-only. Must document: `docker login`/`pull` with `--region us-east-1`, a writable volume mount for snapshots, non-root execution, and config + secret injection. |

Rendered through the same `${VERSION}` / `${IMAGE_URI}` / `${USAGE_INSTRUCTIONS}`
substitution mechanism as `02-add-helm-delivery.json`.

### Outputs

- A submitted change-set that, once AWS ingestion + scan pass, makes the listing
  expose two delivery options (Helm + container image) for the same version.
- No new listing, no new product, no price change (free product unchanged).

## Rules

- **R-CID-01**: The change is **additive**. It MUST NOT modify, reorder, or
  remove the existing `HelmDeliveryOptionDetails` option.
- **R-CID-02**: `CompatibleServices` MUST be `["ECS"]` for this option (EKS is
  already served by the Helm option; do not duplicate).
- **R-CID-03**: `UsageInstructions` MUST reproduce, in prose, the deployment
  guarantees the chart encodes for an operator with no Helm/K8s: (a) writable
  volume for local snapshots, (b) non-root / least-privilege container, (c)
  config-file and secret (env / secret-store) injection, (d) `us-east-1` ECR
  login region. It MUST additionally warn that the collector's local snapshot
  store needs **durable** storage: on AWS Fargate the task filesystem is
  ephemeral, so the buyer MUST attach an Amazon EFS volume (Fargate) or an EBS /
  bind volume (EC2-backed ECS) — without it, snapshots are lost on task restart.
  This is the one place ECS materially differs from the chart's PersistentVolume,
  and calling it out is what makes INV-CID-02 honest in practice.
- **R-CID-04**: Adding this option is its **own AWS review round** (umbrella
  decision 3) — it MUST NOT be bundled with any other lever (#235, #236) in the
  same change-set.

## Invariants

- **INV-CID-01** (one image, two options): the image referenced by
  `EcrDeliveryOptionDetails.ContainerImages` MUST be byte-identical (same
  Marketplace-ECR repository, tag, and resolved digest) to the image referenced
  by the live `HelmDeliveryOptionDetails.ContainerImages`. There is exactly one
  collector artifact; the delivery options differ only in launch method.
- **INV-CID-02** (behavioral parity): the collector produced by the
  container-image delivery, run standalone on ECS/Fargate/`docker run`, MUST be
  the **same running collector** as the Helm delivery — same read-only
  enforcement, same passwordless onboarding, same snapshot output — given an
  equivalent configuration. Deployment mechanics differ; collector behavior does
  not.
- **INV-CID-03** (read-only preserved): nothing in the ECS/Fargate path relaxes
  the collector's read-only guarantees or unsafe-role blocking; those live in the
  image, not the chart.

## Failure Conditions

- **FC-CID-01**: Rendered change-set contains an unsubstituted `${...}` →
  `scripts/marketplace-changeset.sh` exits non-zero before `start-change-set`
  (reuses the existing placeholder guard).
- **FC-CID-02**: Rendered change-set contains non-ASCII/control bytes →
  the script's non-ASCII guard exits non-zero before submit.
- **FC-CID-03**: `CompatibleServices` contains a value outside the AWS-valid set
  → AWS rejects the change-set; preflight SHOULD catch it locally first.
- **FC-CID-04**: `ContainerImages` points at a non-Marketplace-ECR (e.g.
  `ghcr.io`) image → AWS rejects on ingestion; preflight MUST assert the URI is
  the Marketplace-ECR image.

## Constraints

- `UsageInstructions` ≤ 4000 characters (AWS limit).
- Image must already be re-hosted to the Marketplace ECR at the released version
  (it is — the Helm delivery uses it). No rebuild in this slice.
- No `latest` tag (`INVALID_CONTAINER_IMAGE_TAG`); the tag is the released
  SemVer.

## Out of scope

- **EKS add-on delivery** (`EksAddOnDeliveryOptionDetails`) — that is #236.
- **AMI product** — that is #235 (separate `AmiProduct@1.0`).
- **A new product version / new image build.** This slice adds a delivery option
  to the existing version.
- **Automated live ECS smoke in CI.** The live-artifact smoke (TC-CID-05) is a
  gated manual/representative-environment run for this slice; wiring it into CI is
  tracked by #212.
- **Paid pricing / dimensions.** The product stays free.

## Traceability

specification (this file) -> acceptance cases
(`marketplace-container-image-delivery.acceptance.md`) -> change-set template
`docs/marketplace/catalog-api/04-add-container-image-delivery.json` +
`scripts/marketplace-changeset.sh` guards.
