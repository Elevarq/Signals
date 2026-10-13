# Marketplace EKS Add-On Delivery Option

## Status

ACTIVE

## Type

Integration mapping — contract across the boundary between the re-hosted
Marketplace-ECR image + Helm chart, the AWS Marketplace `ContainerProduct@1.0`
listing, and the Amazon EKS add-on catalog (console / `eksctl` add-on flow).
Tests mandatory.

## Purpose

Add an **Amazon EKS add-on** delivery option to the live Elevarq Signals
listing, so the collector is discoverable and installable directly from the EKS
console and the `eksctl`/`aws eks create-addon` flow — native EKS-console reach.
It reuses the **same** re-hosted Marketplace-ECR image **and** Helm chart the
existing Helm/EKS delivery option ships (`EksAddOnDeliveryOptionDetails` wraps
both `ContainerImages` and `HelmChartUri`). No new build, no new chart, and the
existing Helm and container-image options are untouched.

The value is reach within the EKS audience: buyers who discover software through
the EKS add-on catalog rather than `helm install`. Part of #233 (reach
expansion). Inherits the umbrella's cross-cutting decisions (#233) — do not
restate them here.

## Correction (#297) — the add-on config schema source

The 1.0.3 add-on ingested cleanly but `describe-addon-configuration` still
returned *"No configuration support"*, so the add-on could not be pointed at a
database and could not go Public. Root cause (verified against the AWS
Marketplace container-product requirements): AWS does **not** derive the add-on
configuration schema from the chart's `values.schema.json` — the whole
#285 → #290 → 1.0.2/1.0.3 effort targeted the wrong file. The schema must be a
**dedicated `aws_mp_configuration_schema.json` at the Helm chart top level**
(supported JSON Schema drafts: draft-04/06/07, draft/2019-09; camelCase keys;
`description` on every property; no secret-value fields;
`aws_mp_addon_parameters.json` is an optional sibling for pod-identity IAM).
This spec is corrected accordingly (R-EAO-07..09, FC-EAO-05..07). The earlier
1.0.1/1.0.2/1.0.3 releases remain valid patches; they simply did not expose
add-on config.

## Scope

One `AddDeliveryOptions` change-set against the existing `ContainerProduct@1.0`,
adding exactly one `EksAddOnDeliveryOptionDetails` delivery option (AWS allows
only one add-on option per version), plus the buyer-facing usage instructions
for the add-on install path, plus the add-on configuration schema the chart
must carry.

In scope:

- A committed change-set template
  `docs/marketplace/catalog-api/05-add-eks-addon-delivery.json`.
- The add-on configuration schema `deploy/helm/signals/aws_mp_configuration_schema.json`
  (#297), shipped inside the chart so AWS reads it at ingestion.
- A preflight guard on the immutable `AddOnType` (valid-set check).
- README documentation of the add-on step, its immutability constraints, and the
  separate add-on visibility step.

The repository change here (the schema file + corrected contract) is the part
that lives in source. **Making the live add-on configurable is a human/live
gate** (R-EAO-05/06): cut a new chart version carrying this file, re-host it to
the Marketplace ECR, re-submit the delivery option, verify
`aws eks describe-addon-configuration` now returns the schema, run a live
`create-addon`, then `UpdateDeliveryOptions` to Public — none of which is
automatable from this repo.

## Interfaces

### Inputs (`EksAddOnDeliveryOptionDetails`)

| Field | Type | Constraint / value |
|-------|------|--------------------|
| `AddOnName` | string | **`signals`** (immutable). Buyer sees `<SellerAlias>_signals` in the EKS catalog. Changing it later fails `INCOMPATIBLE_ADDON_NAME`. |
| `AddOnType` | string | **`observability`** (immutable — decided in #236, confirmed with product owner). Changing it later fails `INCOMPATIBLE_ADDON_TYPE`. Valid AWS set: Gitops, monitoring, logging, cert-management, policy-management, cost-management, autoscaling, storage, kubernetes-management, service-mesh, etcd-backup, ingress-service-type, load-balancer, local-registry, networking, Security, backup, ingress-controller, observability. |
| `AddOnVersion` | string | `${VERSION}` — semantic `major.minor.patch` (AWS pattern; the released SemVer satisfies it). |
| `Namespace` | string | **`signals`** (immutable; matches the chart's release namespace). Changing it later fails `INCOMPATIBLE_ADDON_NAMESPACE`. |
| `ContainerImages` | array<string> | The Marketplace-ECR image at `${VERSION}` — the same image the Helm option ships. |
| `HelmChartUri` | string | The Marketplace-ECR chart at `${VERSION}` — the same chart the Helm option ships. |
| `CompatibleKubernetesVersions` | array<string> | `${K8S_VERSIONS}` — the EKS Kubernetes versions actually validated for this release, set at submit time (honest-claim: declare only tested versions). |
| `SupportedArchitectures` | array<string> | `["amd64", "arm64"]` — the image is genuinely multi-arch. |
| `Description` | string | `${ADDON_DELIVERY_DESCRIPTION}`. |
| `UsageInstructions` | string | `${ADDON_USAGE_INSTRUCTIONS}`, ≤ 4000 chars, ASCII. The EKS add-on install path (console / `aws eks create-addon`), plus the same durable-storage note as the chart (the collector's local snapshot store needs a PersistentVolume). |

### Outputs

- A submitted change-set that makes the listing expose the EKS add-on option
  (in addition to Helm and container-image), all for the same version.
- No new listing, no new product, no price change (free product unchanged).

## Rules

- **R-EAO-01**: Additive. MUST NOT modify, reorder, or remove the existing Helm
  or container-image delivery options.
- **R-EAO-02**: Exactly **one** `EksAddOnDeliveryOptionDetails` per version
  (AWS: `TOO_MANY_EKS_ADDON_DELIVERY_OPTIONS` otherwise).
- **R-EAO-03**: `AddOnName`, `AddOnType`, and `Namespace` are **immutable across
  all future versions**. They are pinned in the committed template and MUST NOT
  be changed once the first add-on version is published.
- **R-EAO-04**: `CompatibleKubernetesVersions` MUST list only EKS Kubernetes
  versions validated for the release (honest-claim; do not declare untested
  versions).
- **R-EAO-05**: Its own AWS review round (umbrella decision 3) — not bundled with
  #234 or #235.
- **R-EAO-06**: EKS add-on delivery-option visibility is **not** auto-Public
  (unlike Helm/container options). Making the add-on public is a separate
  `UpdateDeliveryOptions`/visibility step, gated on an explicit go.
- **R-EAO-07** (CORRECTED by #297 — the #285/#290 premise was wrong): The
  add-on's buyer configuration surface is supplied by a **dedicated
  `aws_mp_configuration_schema.json` at the Helm chart top level**, NOT by the
  chart's `values.schema.json`. AWS reads that file to render the add-on
  configuration in the EKS console and to answer
  `aws eks describe-addon-configuration`; a chart without it (or with an empty
  one) yields *"No configuration support"* and the add-on cannot be pointed at a
  database (INV-EAO-02 unreachable). It MUST declare the buyer-configurable
  contract — at minimum `target.host`, `target.user`, `target.authMethod`,
  `target.sslmode`, `target.sslRootCertFile`, and `persistence.storageClass`
  (+ `persistence.enabled/size`, `target.port/dbname/region/secretRef`,
  `api.tokenSecretName`, `collector.*`, `env`, `resources`). Its keys mirror the
  chart's value paths so a `configurationValues` blob overrides the chart. It
  reaches the live add-on only via a new released chart version re-hosted and
  re-submitted (the schema is read at ingestion, per release).
- **R-EAO-08** (schema file requirements, #297): `aws_mp_configuration_schema.json`
  MUST use a JSON Schema draft AWS supports (draft-04/06/07 or draft/2019-09),
  every property MUST carry a `description` (rendered as the console label),
  keys use the chart's value paths, and it MUST NOT accept any **secret value**
  (password, API key, certificate, private key) — identity is by reference only
  (Kubernetes Secret *names*, filesystem *paths*, or the IRSA role ARN set via
  the Amazon EKS add-on API). A sensitive field fails AWS ingestion
  `INVALID_HELM_SENSITIVE_CONFIG`.
- **R-EAO-09** (auto-injected / API-set keys excluded, #297): cluster metadata
  (AWS region, cluster name, VPC id, …) is auto-injected by Amazon EKS and MUST
  be absent from the schema; the IRSA role ARN is set via the add-on API's
  `serviceAccountRoleArn`, so `serviceAccount.annotations` is NOT a config-schema
  field; `image` is add-on-managed and excluded.

## Invariants

- **INV-EAO-01** (one artifact set): the `ContainerImages` and `HelmChartUri` the
  add-on references MUST be byte-identical (same MP-ECR repo, tag, resolved
  digest) to those the live Helm delivery option ships. One image + one chart,
  installed three ways.
- **INV-EAO-02** (behavioral parity): the collector installed via the EKS add-on
  MUST be the **same running collector** as the Helm delivery — same read-only
  enforcement, passwordless onboarding, snapshot output. The add-on is a
  console-native installer of the same chart; behavior is unchanged.
- **INV-EAO-03** (immutable identity stable): across versions, `AddOnName`,
  `AddOnType`, `Namespace` never change.

## Failure Conditions

- **FC-EAO-01**: Unsubstituted `${...}` in the rendered change-set → script exits
  non-zero before `start-change-set` (existing guard).
- **FC-EAO-02**: Non-ASCII/control bytes in the rendered change-set → script's
  non-ASCII guard exits non-zero (existing guard).
- **FC-EAO-03**: `AddOnType` outside the AWS-valid set → preflight guard rejects
  before submit (new guard); AWS would otherwise fail `INVALID_ADDON_TYPE`.
- **FC-EAO-04**: `ContainerImages`/`HelmChartUri` pointing at a
  non-Marketplace-ECR (e.g. `ghcr.io`) artifact → AWS rejects on ingestion;
  preflight asserts MP-ECR.
- **FC-EAO-05** (CORRECTED, #297): the chart lacks `aws_mp_configuration_schema.json`
  (or it omits the required keys in R-EAO-07) → `describe-addon-configuration`
  reports *"No configuration support"* → the add-on cannot be pointed at a
  database and INV-EAO-02 is unreachable. Guard: a chart test asserts
  `aws_mp_configuration_schema.json` exists, uses a supported draft, exposes the
  required keys, describes every property, and declares no secret-value field.
  (The earlier #285 guard on `values.schema.json` was based on the disproven
  premise and is replaced.)
- **FC-EAO-06** (#297): `aws_mp_configuration_schema.json` collects a secret
  value (password / API key / certificate / private key) → AWS ingestion fails
  `INVALID_HELM_SENSITIVE_CONFIG`. Guard: the chart test rejects any such field.
- **FC-EAO-07** (#297): an unsupported JSON Schema draft in the file → AWS fails
  `INCOMPATIBLE_CONFIGURATION_SCHEMA_VERSION`. Guard: the chart test asserts a
  supported `$schema`.

## Constraints

- `UsageInstructions` ≤ 4000 characters.
- Image + chart already re-hosted to the Marketplace ECR at the released version
  (they are — the Helm option uses them). No rebuild.
- The Helm chart must satisfy EKS add-on packaging requirements
  (namespace-scoped install, no unmet cluster-admin assumptions). Validated as
  part of the live add-on smoke.

## Out of scope

- **Container-image delivery** (`EcrDeliveryOptionDetails`) — that is #234
  (shipped).
- **AMI product** — that is #235 (separate `AmiProduct@1.0`).
- **A new product version / new image or chart build.**
- **Automated live add-on smoke in CI** — the live install smoke is a gated
  manual/representative-environment run for this slice; CI wiring is #212.
- **`EnvironmentOverrideParameters`.** Signals does not require EKS system
  parameters (cluster name/region) injected at launch; omitted unless a future
  need appears.

## Traceability

specification (this file) -> acceptance cases
(`marketplace-eks-addon-delivery.acceptance.md`) -> change-set template
`docs/marketplace/catalog-api/05-add-eks-addon-delivery.json` +
`scripts/marketplace-changeset.sh` guards.
