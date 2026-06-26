#!/usr/bin/env bash
#
# marketplace-changeset.sh — render an AWS Marketplace Catalog API change-set
# template (envsubst), submit it with `aws marketplace-catalog start-change-set`,
# and poll `describe-change-set` until it reaches a terminal state.
#
# Operator step (not CI). Drives the API-automatable parts of the container
# listing — create product + ECR repos, then add the Helm delivery version.
# See docs/marketplace/catalog-api/README.md. Tracks Elevarq/Signals#218.
#
# Usage:
#   scripts/marketplace-changeset.sh docs/marketplace/catalog-api/01-create-product-and-repos.json
#
#   # For 02-add-helm-delivery.json, export the variables it references first:
#   PRODUCT_ID=prod-xxxx VERSION=1.0.0 \
#   IMAGE_URI=<acct>.dkr.ecr.us-east-1.amazonaws.com/<ns>/elevarq-signals:1.0.0 \
#   CHART_URI=<acct>.dkr.ecr.us-east-1.amazonaws.com/<ns>/elevarq-signals-chart/signals:1.0.0 \
#   RELEASE_NOTES="..." DELIVERY_DESCRIPTION="..." USAGE_INSTRUCTIONS="..." \
#     scripts/marketplace-changeset.sh docs/marketplace/catalog-api/02-add-helm-delivery.json
#
# Env: AWS_PROFILE (default elevarq), AWS_REGION (default us-east-1).
# Requires: aws, jq, envsubst (gettext).

set -euo pipefail

TEMPLATE="${1:?usage: marketplace-changeset.sh <change-set-template.json>}"
[ -f "$TEMPLATE" ] || { echo "error: no such template: $TEMPLATE" >&2; exit 1; }

export AWS_PROFILE="${AWS_PROFILE:-elevarq}"
export AWS_REGION="${AWS_REGION:-us-east-1}"

for bin in aws jq envsubst; do
  command -v "$bin" >/dev/null 2>&1 || { echo "error: missing tool: $bin" >&2; exit 1; }
done

rendered="$(mktemp)"
trap 'rm -f "$rendered"' EXIT
envsubst < "$TEMPLATE" > "$rendered"

# Fail fast on an unsubstituted ${VAR} or invalid JSON before we submit.
# SC2016: the single quotes are deliberate — we match the LITERAL ${ that
# envsubst would have replaced, not a shell expansion.
# shellcheck disable=SC2016
if grep -q '\${' "$rendered"; then
  echo "error: unsubstituted variables remain in the rendered change set:" >&2
  # shellcheck disable=SC2016
  grep -o '\${[A-Z_]*}' "$rendered" | sort -u >&2
  exit 1
fi
jq . "$rendered" >/dev/null || { echo "error: rendered change set is not valid JSON" >&2; exit 1; }

echo "==> Submitting change set from $TEMPLATE"
CHANGE_SET_ID="$(aws marketplace-catalog start-change-set \
  --cli-input-json "file://$rendered" \
  --query ChangeSetId --output text)"
echo "    ChangeSetId: $CHANGE_SET_ID"

echo "==> Polling (scanning images can take minutes to hours)…"
while :; do
  STATUS="$(aws marketplace-catalog describe-change-set \
    --catalog AWSMarketplace --change-set-id "$CHANGE_SET_ID" \
    --query Status --output text)"
  echo "    status: $STATUS"
  case "$STATUS" in
    SUCCEEDED) break ;;
    FAILED | CANCELLED)
      echo "==> Change set $STATUS — errors:" >&2
      aws marketplace-catalog describe-change-set \
        --catalog AWSMarketplace --change-set-id "$CHANGE_SET_ID" \
        --query 'ChangeSet[].{Change:ChangeType,Errors:ErrorDetailList}' --output json >&2
      exit 1
      ;;
  esac
  sleep 30
done

echo "==> SUCCEEDED. Resulting entity / details:"
aws marketplace-catalog describe-change-set \
  --catalog AWSMarketplace --change-set-id "$CHANGE_SET_ID" \
  --query 'ChangeSet[].{Change:ChangeType,Entity:Entity.Identifier}' --output table
echo
echo "Tip: for the product/delivery-option IDs, run:"
echo "  aws marketplace-catalog describe-entity --catalog AWSMarketplace --entity-id <PRODUCT_ID>"
