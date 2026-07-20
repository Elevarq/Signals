# EC2 Image Builder — Signals collector (groundwork)

Groundwork for offering Elevarq Signals as an AWS Marketplace AMI / server
product (#235, part of #233). This directory ships the reusable **EC2 Image
Builder component** that bakes the collector into a golden AMI. Standing up the
live `AmiProduct@1.0` listing is **deferred and demand-gated** — see "Live AMI
product" below.

Spec: `specifications/marketplace-ami-image-builder.md`.

## The component

`signals-collector-component.yaml` is an AWSTOE (`schemaVersion: 1.0`) component
with `build` / `validate` / `test` phases. It mirrors the
`deploy/aws/terraform` run path: install docker, pre-pull the pinned Signals
image, and install a `signals.service` systemd unit that docker-runs the image
with `--restart=always`, mounting `/etc/signals` (read-only) and a `signals-data`
volume.

**Nothing secret is baked.** The component creates an empty `/etc/signals` and
enables (does not start) the unit. Config and credentials are supplied by the
buyer **at launch**:

- `/etc/signals/signals.yaml` — collector config + target (user-data / SSM).
- `/etc/signals/rds-ca.pem` — RDS CA bundle if using verify-full TLS.
- `/etc/signals/signals.env` — `SIGNALS_API_TOKEN=...` (root-only) plus **any
  other `SIGNALS_*`** the collector reads (e.g. `SIGNALS_LOG_LEVEL`, or the
  dev-only `SIGNALS_ALLOW_INSECURE_PG_TLS`). The unit forwards the **whole file**
  to the container via `docker run --env-file /etc/signals/signals.env` (#292),
  so every buyer-supplied variable reaches the collector — not only the token.
  `EnvironmentFile=/etc/signals/signals.env` is also kept so systemd fails
  cleanly if the file is absent. Both pass values **by reference**: neither the
  file's contents nor the token is ever printed to the journal.

On first boot, once `/etc/signals` is populated, `signals.service` starts the
collector. This keeps the AMI credential-free (credentials-by-reference), the
same posture as the container and Helm deliveries.

### Parameters

| Parameter | Default | Notes |
|-----------|---------|-------|
| `SignalsImage` | `ghcr.io/elevarq/signals:1.0.2` | Pinned version (no `latest`). Bump per release. |

### Baking locally / in a pipeline

The component is consumed by an EC2 Image Builder **image recipe** on an Amazon
Linux 2023 base image. Register it and reference it from a recipe:

```bash
aws imagebuilder create-component \
  --name signals-collector \
  --semantic-version 1.0.2 \
  --platform Linux \
  --data file://signals-collector-component.yaml
# -> ComponentArn, referenced by an image recipe + pipeline that produces the AMI.
```

## Live AMI product (#235 — un-deferred 2026-07-19)

Publishing Signals as a Marketplace **`AmiProduct@1.0`** is a *separate product*
with its own onboarding and review (#235; un-deferred per the #233 decision log).
Governed by `specifications/marketplace-ami-product.md`.

**The AMI product ships a *pre-baked AMI*, not the component.** The AWS
Marketplace Catalog API AMI delivery option is `AmiDeliveryOptionDetails` →
`AmiSource` (a built `AmiId` + an `AccessRoleArn` AWS uses to scan/copy it); there
is **no** `ComponentArn` field. This component is the reproducible **build input**
for that AMI, not the thing sold. The Catalog-API path is:

1. Bake a golden AMI: an EC2 Image Builder image recipe (Amazon Linux 2023 base +
   this `signals-collector` component at the released version) → `AmiId`.
2. `CreateProduct` with `Type: AmiProduct@1.0` (separate from the container
   product `prod-...`).
3. `UpdateInformation` — title, descriptions, logo (S3), categories, keywords
   (reuse the shared Elevarq assets).
4. Offer + legal terms (`CustomEula`) + support terms; free offer takes no
   pricing term.
5. `AddDeliveryOptions` with `AmiDeliveryOptionDetails`: the `AmiSource`
   (`AmiId` + the AMI-product `AccessRoleArn`), `OperatingSystem`,
   `RecommendedInstanceType`, supported instance types, and regions.
6. `ReleaseProduct` + `ReleaseOffer`, then `UpdateVisibility: Public` (Seller-Ops
   review).

Human gates (never automate): the EULA/entity wording (counsel), the AMI build
provenance, and the final public submit (explicit go).
