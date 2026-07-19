# Marketplace AMI Product (live AmiProduct@1.0 standup)

## Status

DRAFT

## Type

Integration mapping — contract between a **pre-baked Signals golden AMI** (built
by EC2 Image Builder from the committed `signals-collector` component) and a live
AWS Marketplace `AmiProduct@1.0` listing that shares that AMI. Tests mandatory
for the statically-checkable parts; the live AMI bake, submit, and AWS review are
gated operator steps.

## Purpose

Stand up the live `AmiProduct@1.0` Marketplace listing for Signals (#235, part of
#233), un-deferred by the product owner on 2026-07-19 (see #233 decision log).
The listing ships a **pre-baked Amazon Linux 2023 AMI** with the collector
installed by the committed `signals-collector` Image Builder component (docker +
the pinned `SignalsImage` + the `signals.service` unit). Reach: non-container,
EC2-baked deployments — buyers launch the AMI and supply config at launch.

**Correction (2026-07-19):** an earlier groundwork premise (share the Image
Builder *component* via `AddDeliveryOptions` with a `ComponentArn`) does not match
the AWS Marketplace Catalog API. The AMI product delivery option is
`AmiDeliveryOptionDetails` → `AmiSource` (a built `AmiId` + an `AccessRoleArn` AWS
uses to scan it); there is no component ARN in the schema. This spec uses the
supported model: publish a pre-baked AMI. The EC2 Image Builder component remains
the reproducible *build input* for that AMI (INV-AMI-01..03 still hold).

## Scope

In scope:

- Build a golden AMI via EC2 Image Builder: an image recipe (Amazon Linux 2023
  base + the `signals-collector` component at the released version), an
  infrastructure configuration, and a build that yields an `AmiId`.
- An `AccessRoleArn` IAM role AWS Marketplace assumes to scan/copy the AMI
  (the AWS-documented AMI-product access role; least-privilege).
- Catalog-API standup of a **separate** `AmiProduct@1.0`: `CreateProduct`,
  `UpdateInformation` (reuse shared Elevarq assets), offer + `CustomEula` +
  support terms (free — no pricing term), `AddDeliveryOptions` with the
  `AmiSource` (`AmiId`, `AccessRoleArn`), `OperatingSystem`,
  `RecommendedInstanceType`, supported instance types, and regions, then
  `ReleaseProduct` + `ReleaseOffer`.
- Documentation of the sequence + identifiers under `docs/marketplace/`.

Out of scope:

- Any change to the container product (`prod-7tz6zxncwjmw4`) or its options.
- Component-catalog "Image Builder integration" (a separate mechanism; not this
  Catalog-API path).
- Pricing (free product).

## Interfaces

### Inputs

| Field | Value / constraint |
|-------|--------------------|
| Image recipe | Amazon Linux 2023 base + `signals-collector` component at the released SemVer; `SignalsImage` pinned (R-AMI-02). |
| `Type` | `AmiProduct@1.0` (separate product; not a delivery option on the container product). |
| `AmiSource.AmiId` | The built golden AMI id in the seller account. |
| `AmiSource.AccessRoleArn` | IAM role trusted by AWS Marketplace for AMI scanning/copy — the AWS-documented AMI-product access role, least-privilege. |
| `OperatingSystem` | Amazon Linux 2023 (name + version), matching the recipe base. |
| `RecommendedInstanceType` | A small general-purpose type (e.g. `t3.small`); the collector is lightweight. |
| Supported instance types / Regions | The validated set (honest-claim; only what is tested). |
| `UsageInstructions` | ASCII, launch-time config path: how the buyer supplies `/etc/signals/signals.yaml`, the RDS CA, and `SIGNALS_API_TOKEN` via user-data/SSM; how `signals.service` starts on first boot. |
| `UpdateInformation` | Title, ShortDescription (≤1000), LongDescription (≤5000), Highlights (1-3), Categories (`Infrastructure Software`), SearchKeywords (≤15), LogoUrl (shared S3 Elevarq logo). ASCII-only. |
| `LegalTerm` | `CustomEula` → the shared Elevarq EULA PDF in S3. |

### Outputs

- A `Limited` `AmiProduct@1.0` listing shipping the pre-baked AMI, pending the
  explicit-go Public submit.

## Rules

- **R-AMIP-01**: Separate product. MUST NOT modify the container product or bundle
  with #234/#236 (umbrella decision 3 — its own AWS review round).
- **R-AMIP-02**: The AMI is baked from the committed `signals-collector` component
  at a pinned `SignalsImage` (no `latest`) — reproducible and traceable to a
  released image (R-AMI-02).
- **R-AMIP-03**: Credentials-by-reference — the baked AMI contains no secret,
  token, or database config (INV-AMI-01); buyer supplies config at launch.
- **R-AMIP-04**: `AccessRoleArn` is the least-privilege AMI-product access role
  AWS documents (AMI scan/copy on the specific AMI), trust scoped to the AWS
  Marketplace service principal. No broader EC2 permissions.
- **R-AMIP-05**: Change-set copy is ASCII-only and fully `${...}`-substituted
  (same guards as the container change-sets; `${VERSION}==literal` trap and
  version immutability apply).

## Invariants

- **INV-AMIP-01** (traceable bake): the AMI is built solely from the committed
  component + the pinned released image on an AL2023 base — no out-of-band
  manual mutation.
- **INV-AMIP-02** (parity): a collector from the baked AMI (given buyer-supplied
  config at launch) equals the container/Helm collector — same image, read-only
  enforcement, passwordless onboarding (inherits INV-AMI-02).

## Failure Conditions

- **FC-AMIP-01**: The AMI is built from anything other than the committed
  component + pinned image (out-of-band packages, unpinned image) → violates
  INV-AMIP-01 / R-AMIP-02.
- **FC-AMIP-02**: `AccessRoleArn` grants more than the documented AMI-product
  scan/copy access → violates R-AMIP-04.
- **FC-AMIP-03**: Non-ASCII / unsubstituted `${...}` in a rendered change-set →
  script guards exit non-zero before `start-change-set`.
- **FC-AMIP-04**: A secret/token/config baked into the AMI → violates R-AMIP-03 /
  INV-AMI-01.

## Constraints

- The released image the component pins (`SignalsImage`) MUST exist before the
  bake (the release is published) — the AMI pre-pulls it at build time.
- `AmiProduct@1.0` uses the Catalog-API AMI schema (`AmiDeliveryOptionDetails` /
  `AmiSource`); the container-product `ContainerProduct@1.0` schema does not
  apply. All delivery options in one version share the same `AmiSource`, and
  delivery options cannot be added to an existing version.

## Human gates (never automate)

- EULA / entity wording — counsel (reuse the signed shared Elevarq EULA).
- AMI build provenance sign-off.
- Final `UpdateVisibility: Public` submit — explicit go + AWS Seller-Ops review.

## Traceability

specification (this file) -> acceptance cases
(`marketplace-ami-product.acceptance.md`) -> change-set templates
`docs/marketplace/catalog-api/06-create-ami-product.json` +
`07-add-ami-delivery.json`, authored and dry-run validated against the live
Catalog API (`StartChangeSet` with `Intent: VALIDATE`, which does not create the
product) during the standup, then submitted + `scripts/marketplace-changeset.sh`
guards.
