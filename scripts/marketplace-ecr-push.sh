#!/usr/bin/env bash
#
# marketplace-ecr-push.sh — copy a released Elevarq Signals image + Helm chart
# from ghcr.io into the AWS-Marketplace-owned Amazon ECR repositories.
#
# AWS Marketplace container products MUST serve images and charts from ECR
# repos it owns (RepositoryType=ECR, created via the AddRepositories change in
# AMMP / the Catalog API). ghcr.io cannot be referenced. This script re-pushes
# a published version into those repos so a delivery-option version can point
# at them. Tracks Elevarq/Signals#218; see docs/marketplace/aws-listing.md.
#
# Nothing here is run by CI — it is an operator step, after the ECR repos exist.
#
# Required env:
#   VERSION         release version, e.g. 1.0.0
#   MP_REGISTRY     Marketplace ECR registry host
#                   (e.g. 123456789012.dkr.ecr.us-east-1.amazonaws.com)
#   MP_IMAGE_REPO   Marketplace ECR image repo path (from AddRepositories),
#                   e.g. <seller-ns>/elevarq-signals
#   MP_CHART_REPO   Marketplace ECR chart repo path,
#                   e.g. <seller-ns>/elevarq-signals-chart
# Optional env:
#   SOURCE_IMAGE    default ghcr.io/elevarq/signals:${VERSION}
#   SOURCE_CHART    default oci://ghcr.io/elevarq/charts/signals (version ${VERSION})
#   AWS_REGION      default us-east-1
#   AWS_PROFILE     default elevarq
#
# Requires: aws, docker (with buildx), helm. Auth is via
# `aws ecr get-login-password` (no long-lived creds). skopeo is NOT required —
# `docker buildx imagetools create` copies the multi-arch manifest
# registry-to-registry.

set -euo pipefail

die() { echo "error: $*" >&2; exit 1; }

case "${1:-}" in
  -h | --help)
    sed -n '2,32p' "$0" | sed 's/^# \{0,1\}//'
    exit 0
    ;;
esac

: "${VERSION:?set VERSION (e.g. 1.0.0)}"
: "${MP_REGISTRY:?set MP_REGISTRY (Marketplace ECR registry host)}"
: "${MP_IMAGE_REPO:?set MP_IMAGE_REPO (Marketplace ECR image repo path)}"
: "${MP_CHART_REPO:?set MP_CHART_REPO (Marketplace ECR chart repo path)}"

AWS_REGION="${AWS_REGION:-us-east-1}"
AWS_PROFILE="${AWS_PROFILE:-elevarq}"
SOURCE_IMAGE="${SOURCE_IMAGE:-ghcr.io/elevarq/signals:${VERSION}}"
SOURCE_CHART="${SOURCE_CHART:-oci://ghcr.io/elevarq/charts/signals}"
export AWS_REGION AWS_PROFILE

for bin in aws docker helm; do
  command -v "$bin" >/dev/null 2>&1 || die "missing required tool: $bin"
done
docker buildx version >/dev/null 2>&1 || die "docker buildx is required"

# The ECR login token is scoped to the REGISTRY's region, which is fixed by the
# MP_REGISTRY host (<acct>.dkr.ecr.<region>.amazonaws.com) — not by the operator's
# AWS_REGION. Deriving it here keeps an exported AWS_REGION=<other> from minting a
# token for the wrong region and authenticating the push to the wrong registry.
# Falls back to us-east-1 if the host cannot be parsed.
MP_REGION="$(printf '%s' "$MP_REGISTRY" | sed -n 's/.*\.dkr\.ecr\.\([a-z0-9-]\{1,\}\)\.amazonaws\.com.*/\1/p')"
MP_REGION="${MP_REGION:-us-east-1}"

echo "==> Authenticating to Marketplace ECR (${MP_REGISTRY}, region ${MP_REGION})"
PW="$(aws ecr get-login-password --region "$MP_REGION")"
printf '%s' "$PW" | docker login --username AWS --password-stdin "$MP_REGISTRY"
printf '%s' "$PW" | helm registry login --username AWS --password-stdin "$MP_REGISTRY"

DEST_IMAGE="${MP_REGISTRY}/${MP_IMAGE_REPO}:${VERSION}"

echo "==> Copying image (multi-arch manifest preserved)"
echo "    ${SOURCE_IMAGE}  ->  ${DEST_IMAGE}"
# `imagetools create` copies the full manifest list (linux/amd64 + linux/arm64)
# registry-to-registry, as-is. Note: cosign signatures live under separate .sig
# tags and are NOT copied; AWS Marketplace scans (and may re-sign) the image on
# ingestion regardless.
docker buildx imagetools create -t "${DEST_IMAGE}" "${SOURCE_IMAGE}"

echo "==> Repackaging + pushing Helm chart"
# AWS rejects Helm charts whose images live outside the Marketplace ECR repos
# (INVALID_HELM_CHART_IMAGES). The published chart must default its image to the
# Marketplace ECR image we just pushed — NOT ghcr.io. Pull the released chart,
# repoint image.repository, prove via `helm template` that no ghcr.io image
# remains, then rename + repackage + push.
workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT
helm pull "${SOURCE_CHART}" --version "${VERSION}" --untar --untardir "$workdir"
chartdir="$workdir/signals"
[ -d "$chartdir" ] || die "chart not found after pull at $chartdir"

# Repoint the default image from ghcr.io to the Marketplace ECR repository.
# (The chart's default tag is already ${VERSION}; buyers can still override.)
sed -i.bak "s#ghcr.io/elevarq/signals#${DEST_IMAGE%:*}#g" "$chartdir/values.yaml"
rm -f "$chartdir/values.yaml.bak"

# Chart-path 403 gotcha: ECR repos are NOT hierarchical. `helm push` appends the
# chart name, so pushing the "signals" chart to oci://$MP/$MP_CHART_REPO tries to
# create $MP_CHART_REPO/signals — a separate, non-existent repo the seller cannot
# auto-create cross-account (403). Fix: rename the chart so its name is the
# repo's last path segment and push to the PARENT namespace; the chart then lands
# exactly at the granted repo. Deployed resource names are unaffected — the
# Signals chart keys off the Helm release name ({{ .Release.Name }}-signals),
# not .Chart.Name, so no nameOverride is needed.
CHART_ARTIFACT_NAME="$(basename "$MP_CHART_REPO")"
CHART_PARENT="$(dirname "$MP_CHART_REPO")"
if [ "$CHART_PARENT" = "." ]; then
  CHART_PUSH_TARGET="oci://${MP_REGISTRY}"
else
  CHART_PUSH_TARGET="oci://${MP_REGISTRY}/${CHART_PARENT}"
fi
sed -i.bak "s#^name:.*#name: ${CHART_ARTIFACT_NAME}#" "$chartdir/Chart.yaml"
rm -f "$chartdir/Chart.yaml.bak"

echo "==> Validating repackaged chart (helm lint + no external images)"
helm lint "$chartdir"
printf 'target:\n  host: example.invalid\n' > "$workdir/lint-values.yaml"
RENDERED="$(helm template signals "$chartdir" -f "$workdir/lint-values.yaml")"
printf '%s\n' "$RENDERED" | grep -E '^[[:space:]]*image:' || true
if printf '%s\n' "$RENDERED" | grep -q 'ghcr.io'; then
  die "repackaged chart still renders a ghcr.io image — AWS rejects external chart images (INVALID_HELM_CHART_IMAGES)"
fi
printf '%s\n' "$RENDERED" | grep -q "${DEST_IMAGE%:*}" \
  || die "repackaged chart does not reference the Marketplace ECR image ${DEST_IMAGE%:*}"

helm package "$chartdir" --destination "$workdir"
chart_tgz="$(ls "$workdir/${CHART_ARTIFACT_NAME}"-*.tgz)"
helm push "$chart_tgz" "$CHART_PUSH_TARGET"

echo
echo "==> Done. Digests for the Marketplace AddVersion / delivery option:"
echo "    image: ${DEST_IMAGE}"
echo "    image digest: $(docker buildx imagetools inspect "${DEST_IMAGE}" 2>/dev/null | awk '/^Digest:/{print $2; exit}' || echo '(inspect manually)')"
echo "    chart: oci://${MP_REGISTRY}/${MP_CHART_REPO}:${VERSION}"
echo
echo "Next: reference these in the container product's Helm delivery option,"
echo "submit, then approve the limited listing URL (docs/marketplace/aws-listing.md)."
