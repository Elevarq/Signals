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
#   # For 02-add-helm-delivery.json, export the variables it references first.
#   # The Marketplace ECR is namespaced under elevarq/, and the chart is
#   # re-hosted by RENAMING it to the granted repo's last segment
#   # (elevarq-signals-chart) and pushing to the parent, so it lands at
#   # elevarq/elevarq-signals-chart (NOT .../elevarq-signals-chart/signals).
#   PRODUCT_ID=prod-xxxx VERSION=1.0.0 \
#   IMAGE_URI=<acct>.dkr.ecr.us-east-1.amazonaws.com/elevarq/elevarq-signals:1.0.0 \
#   CHART_URI=<acct>.dkr.ecr.us-east-1.amazonaws.com/elevarq/elevarq-signals-chart:1.0.0 \
#   RELEASE_NOTES="..." DELIVERY_DESCRIPTION="..." USAGE_INSTRUCTIONS="..." \
#     scripts/marketplace-changeset.sh docs/marketplace/catalog-api/02-add-helm-delivery.json
#
#   # For 04-add-container-image-delivery.json (adds the ECS / Fargate /
#   # docker-pull delivery option alongside Helm — same re-hosted image), export:
#   PRODUCT_ID=prod-xxxx VERSION=1.0.0 \
#   IMAGE_URI=<acct>.dkr.ecr.us-east-1.amazonaws.com/elevarq/elevarq-signals:1.0.0 \
#   RELEASE_NOTES="..." CI_DELIVERY_DESCRIPTION="..." CI_USAGE_INSTRUCTIONS="..." \
#     scripts/marketplace-changeset.sh docs/marketplace/catalog-api/04-add-container-image-delivery.json
#   # IMAGE_URI MUST be the same Marketplace-ECR image the Helm option ships
#   # (one artifact, two delivery options). CI_USAGE_INSTRUCTIONS must document
#   # durable snapshot storage (EFS on Fargate; the task filesystem is ephemeral).
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

# Fail fast on non-ASCII (em/en-dashes, curly quotes, ellipsis, ...). AWS
# Marketplace text fields (LongDescription, ReleaseNotes, UsageInstructions,
# ...) reject them with INVALID_INPUT "Remove unsupported characters". This
# commonly sneaks in from copy-pasted prose or the RELEASE_NOTES /
# USAGE_INSTRUCTIONS values. Replace with ASCII (-, ', "). Byte-wise via tr
# (portable across BSD/GNU; [:print:] is locale-dependent and unreliable).
# Keep only TAB(011) NL(012) CR(015) and printable ASCII (040-176); if
# anything survives, the file has non-ASCII or stray control bytes.
if LC_ALL=C tr -d '\11\12\15\40-\176' < "$rendered" | grep -q .; then
  echo "error: non-ASCII/control characters in the rendered change set (AWS rejects these)." >&2
  echo "       Replace em/en-dashes with '-', curly quotes with ' or \", ellipsis with '...'." >&2
  exit 1
fi
jq . "$rendered" >/dev/null || { echo "error: rendered change set is not valid JSON" >&2; exit 1; }

# Fail fast on an unsupported CompatibleServices value. AWS validates this only
# after submission (a typo like "Fargate" or "GKE" burns a change-set); the valid
# set is ECS, EKS, ECS-Anywhere, EKS-Anywhere, Bedrock-AgentCore. Fargate is an
# ECS launch type -> use "ECS". See spec FC-CID-03.
bad_services="$(jq -r '[.. | objects | .CompatibleServices? // empty] | add // [] | .[]' "$rendered" \
  | grep -vxE 'ECS|EKS|ECS-Anywhere|EKS-Anywhere|Bedrock-AgentCore' | sort -u || true)"
if [ -n "$bad_services" ]; then
  echo "error: unsupported CompatibleServices value(s) in the rendered change set:" >&2
  echo "$bad_services" | awk '{print "       " $0}' >&2
  echo "       valid: ECS, EKS, ECS-Anywhere, EKS-Anywhere, Bedrock-AgentCore" >&2
  exit 1
fi

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
