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
- `/etc/signals/signals.env` — `SIGNALS_API_TOKEN=...` (root-only), read by the
  unit's `EnvironmentFile`.

On first boot, once `/etc/signals` is populated, `signals.service` starts the
collector. This keeps the AMI credential-free (credentials-by-reference), the
same posture as the container and Helm deliveries.

### Parameters

| Parameter | Default | Notes |
|-----------|---------|-------|
| `SignalsImage` | `ghcr.io/elevarq/elevarq-signals:1.0.0` | Pinned version (no `latest`). Bump per release. |

### Baking locally / in a pipeline

The component is consumed by an EC2 Image Builder **image recipe** on an Amazon
Linux 2023 base image. Register it and reference it from a recipe:

```bash
aws imagebuilder create-component \
  --name signals-collector \
  --semantic-version 1.0.0 \
  --platform Linux \
  --data file://signals-collector-component.yaml
# -> ComponentArn, referenced by an image recipe + pipeline that produces the AMI.
```

## Live AMI product (deferred — demand-gated, #235)

Publishing the baked AMI as a Marketplace **`AmiProduct@1.0`** is a *separate
product* with its own onboarding and review, pursued only on **real demand for
non-container, EC2-baked deployment** (#235's gate). It is NOT set up here and no
runnable change-set is committed for it. When demand justifies it, the Catalog
API path is:

1. `CreateProduct` with `Type: AmiProduct@1.0` (separate from the container
   product `prod-...`).
2. `UpdateInformation` — title, descriptions, logo (S3), categories, keywords
   (reuse the shared Elevarq assets).
3. Offer + legal terms (`CustomEula`) + support terms; free offer takes no
   pricing term.
4. `AddDeliveryOptions` for the AMI, providing the Image Builder integration:
   the component `ComponentArn` and an `AccessRoleArn` AWS Marketplace assumes to
   read it, so buyers find the component in EC2 Image Builder.
5. `ReleaseProduct` + `ReleaseOffer`, then `UpdateVisibility: Public` (Seller-Ops
   review).

Human gates (never automate): the EULA/entity wording (counsel), the AMI build
provenance, and the final public submit (explicit go).
