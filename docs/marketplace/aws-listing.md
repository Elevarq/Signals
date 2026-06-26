# AWS Marketplace listing — Elevarq Signals (free container product)

Working package for listing **Elevarq Signals 1.0** on AWS Marketplace as a
**free container product** (Helm chart delivery). Tracks
[Elevarq/Signals#218](https://github.com/Elevarq/Signals/issues/218).

Requirements are summarized in the issue; this file is the content + steps we
paste into the AWS Marketplace Management Portal (AMMP) / drive via the
Catalog API.

> **Status:** draft. Fields marked _(confirm)_ depend on the open questions in
> §6 (multi-arch in one delivery option, exact field limits). The EULA and
> cosign questions are now decided — see §6.

## 0. Pre-publish gate

- [x] Seller account is a **registered seller** — confirmed: the AMMP
      products console (`.../marketplace/management/products/server`) loads and
      reports the account ready. Free publishing needs no tax/bank, and is
      independent of the paid Analyzer review.
- [ ] **v1.0.0** is tagged and published (non-prerelease) — `release.yml`.

## 1. Product metadata

| Field | Value |
|-------|-------|
| Product title | Elevarq Signals |
| Short description | Local-first, read-only PostgreSQL diagnostic collector — no diagnostic-data egress to Elevarq. |
| Long description | Elevarq Signals is an open-source (BSD-3-Clause) diagnostic collector for PostgreSQL. It runs next to your database, collects a structured, read-only diagnostic snapshot on a schedule, and keeps the data local: **no telemetry and no diagnostic-data egress to Elevarq** — the only outbound calls are the optional cloud-auth requests (RDS IAM / Secrets Manager / SSM / TLS), which stay within your own cloud's control plane. It onboards **passwordlessly** against managed Postgres (Amazon RDS / Aurora via `aws_rds_iam`, or a cloud secret store) over `verify-full` TLS, using a least-privilege `pg_monitor` role. Snapshots are exported as a portable archive for downstream analysis. |
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
```

Configure in a values file (`signals-values.yaml`), then install with `-f`:

```yaml
# signals-values.yaml
target:
  host: <rds-endpoint>
  dbname: <db>
  user: signals
  authMethod: aws_rds_iam
  sslmode: verify-full
```

```sh
helm install signals oci://<marketplace-ecr-chart-repo>/signals \
  --version 1.0.0 -f signals-values.yaml
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

- Draft EULA: [`EULA.md`](EULA.md) — a custom EULA referencing the
  BSD-3-Clause license (recommended for a free OSS product over the Standard
  Contract for AWS Marketplace / SCMP). **Needs legal review** before
  submission.

## Related artifacts

- **Buyer install guide:** [`install-from-aws-marketplace.md`](install-from-aws-marketplace.md)
  (the source for the delivery option's usage instructions).
- **Architecture diagram (upload asset):** [`assets/architecture.png`](assets/architecture.png)
  — generated from [`docs/architecture.md`](../architecture.md).
- **Catalog-API automation:** [`catalog-api/`](catalog-api/) + `scripts/marketplace-changeset.sh`.
- **ECR re-host:** `scripts/marketplace-ecr-push.sh`.

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

## 6. Decisions & open questions

**Decided**

- **Cosign signature does not carry over.** The `skopeo` copy moves only the
  image (not the `.sig` tag); AWS re-scans and may re-sign on ingestion.
  Marketplace artifacts are AWS-scanned, not verifiable with our GHCR `cosign
  verify` commands. Accepted for 1.0; re-signing the Marketplace ECR artifacts
  is a possible later enhancement.
- **EULA:** custom EULA referencing BSD-3-Clause (see [`EULA.md`](EULA.md)),
  not SCMP. Pending legal review.

**Open (confirm during submission)**

- **Multi-arch** (linux/amd64 + linux/arm64) as a single manifest-list
  delivery option — supported? (We ship a multi-arch manifest.)
- Exact **listing-content field limits**.
- Does the **90-day paid-equivalent** rule bind a standalone free OSS product?
