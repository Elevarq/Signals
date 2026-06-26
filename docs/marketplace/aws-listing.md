# AWS Marketplace listing — Elevarq Signals (free container product)

Working package for listing **Elevarq Signals 1.0** on AWS Marketplace as a
**free container product** (Helm chart delivery). Tracks
[Elevarq/Signals#218](https://github.com/Elevarq/Signals/issues/218).

Requirements are summarized in the issue; this file is the content + steps we
paste into the AWS Marketplace Management Portal (AMMP) / drive via the
Catalog API.

> **Status:** draft. Fields marked _(confirm)_ depend on open questions in
> #218 (cosign-signature survival, multi-arch in one delivery option, exact
> field limits, EULA vs Standard Contract).

## 0. Pre-publish gate

- [ ] Seller account is a **registered seller** (not "registration pending").
      Free publishing needs an account in good standing + accepted T&Cs + a
      valid email — **no tax/bank**. Independent of the paid Analyzer review,
      provided registration itself is complete.
- [ ] **v1.0.0** is tagged and published (non-prerelease) — `release.yml`.

## 1. Product metadata

| Field | Value |
|-------|-------|
| Product title | Elevarq Signals |
| Short description | Local-first, read-only PostgreSQL diagnostic signal collector — no data egress. |
| Long description | Elevarq Signals is an open-source (BSD-3-Clause) diagnostic collector for PostgreSQL. It runs next to your database, collects a structured, read-only diagnostic snapshot on a schedule, and keeps everything local — there is no data egress. It onboards **passwordlessly** against managed Postgres (Amazon RDS / Aurora via `aws_rds_iam`, or a cloud secret store) over `verify-full` TLS, using a least-privilege `pg_monitor` role. Snapshots are exported as a portable archive for downstream analysis. |
| Categories _(confirm)_ | Database / Monitoring & Observability |
| Vendor | Elevarq (DBA of Scantr LLC) |
| Pricing | Free |
| Source / homepage | https://github.com/Elevarq/Signals |
| License | BSD-3-Clause |

## 2. Delivery option — Helm chart (primary)

- **Method:** Helm chart, installed via the Helm CLI (Amazon EKS, or
  self-managed EKS Anywhere / EC2 / on-prem).
- **Chart + images:** copied into the AWS-Marketplace-owned ECR repos (see
  `scripts/marketplace-ecr-push.sh`). The published listing references the
  Marketplace ECR repo, **not** `ghcr.io`.
- _(Optional second delivery option)_ a plain **Container image** option for
  ECS / manual `docker pull` from the Marketplace registry.

### Buyer usage instructions (rendered on the listing)

```sh
# Authenticate Helm to the AWS Marketplace registry (buyer side)
aws ecr get-login-password --region <region> \
  | helm registry login --username AWS --password-stdin <marketplace-ecr-registry>

# Install (minimal — set your target DB + a verify-full CA bundle)
helm install signals oci://<marketplace-ecr-chart-repo>/signals \
  --version 1.0.0 \
  --set target.host=<rds-endpoint> \
  --set target.dbname=<db> \
  --set target.user=signals \
  --set target.authMethod=aws_rds_iam \
  --set target.sslmode=verify-full
```

Full passwordless onboarding (IAM role / DB grant / CA bundle) is documented
in [`deploy/helm/signals/README.md`](../../deploy/helm/signals/README.md) and
[`docs/database-connections.md`](../database-connections.md).

## 3. Support & maintenance (free-product eligibility)

- **Support process:** GitHub Issues at https://github.com/Elevarq/Signals +
  `SUPPORT.md`; security reports via `SECURITY.md`.
- **Update cadence:** versioned releases via CI (`release.yml`) with
  cosign-signed, SBOM-attached, Trivy-scanned multi-arch images; security
  patches cut as needed. _(state the committed cadence on the listing.)_

## 4. License terms

- Decision _(confirm)_: offer under a **custom EULA** that points to the
  BSD-3-Clause license, **or** adopt the **Standard Contract for AWS
  Marketplace (SCMP)**. For a free OSS product the custom-EULA-referencing-the
  OSS-license path is usually simplest — confirm acceptable for a free
  listing.

## 5. Submit checklist (ordered)

1. [ ] Confirm registered-seller status (§0).
2. [ ] Publish `v1.0.0`.
3. [ ] Create Marketplace ECR repos (`AddRepositories` in AMMP / Catalog API).
4. [ ] `scripts/marketplace-ecr-push.sh` — copy the 1.0.0 image + Helm chart
       into the Marketplace ECR repos; let AWS scan them.
5. [ ] Create the container product + Helm delivery option; paste §1–§4.
6. [ ] Submit → AWS review.
7. [ ] Preview + approve the **limited listing URL**.
8. [ ] Request **Limited → Public** (Update visibility) → live.

## 6. Open questions (resolve during submission)

- Do **cosign signatures survive** the copy into Marketplace ECR, or does AWS
  re-sign/scan? (Affects whether buyers can verify our signature.)
- **Multi-arch** (linux/amd64 + linux/arm64) as a single manifest-list
  delivery option — supported?
- Exact **listing-content field limits** + **EULA vs SCMP** for a free listing.
- Does the **90-day paid-equivalent** rule bind a standalone free OSS product?
