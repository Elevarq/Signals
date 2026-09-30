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
#   # For 05-add-eks-addon-delivery.json (adds the EKS add-on delivery option —
#   # same re-hosted image AND chart), export. K8S_VERSIONS is a JSON array of
#   # the EKS Kubernetes versions ACTUALLY validated for this release (declare
#   # only tested versions):
#   PRODUCT_ID=prod-xxxx VERSION=1.0.0 \
#   IMAGE_URI=<acct>.dkr.ecr.us-east-1.amazonaws.com/elevarq/elevarq-signals:1.0.0 \
#   CHART_URI=<acct>.dkr.ecr.us-east-1.amazonaws.com/elevarq/elevarq-signals-chart:1.0.0 \
#   K8S_VERSIONS='["1.30","1.31","1.32"]' \
#   RELEASE_NOTES="..." ADDON_DELIVERY_DESCRIPTION="..." ADDON_USAGE_INSTRUCTIONS="..." \
#     scripts/marketplace-changeset.sh docs/marketplace/catalog-api/05-add-eks-addon-delivery.json
#   # AddOnName/AddOnType/Namespace in the template are IMMUTABLE across versions
#   # (signals / observability / signals) — do not change once published.
#
#   # For 06-restrict-delivery.json (RETIRE a superseded delivery option once a
#   # newer version is Public), export the product id and the delivery option id
#   # to restrict. RestrictDeliveryOptions accepts ONLY options in the Public
#   # state (a Limited/Restricted one fails INVALID_DELIVERY_OPTIONS_STATUS), and
#   # must run AFTER the superseding version is Public so the listing is never
#   # left without a public option. Find the id with describe-entity:
#   #   aws marketplace-catalog describe-entity --catalog AWSMarketplace \
#   #     --entity-id $PRODUCT_ID --query 'Details' --output text | \
#   #     jq '.Versions[].DeliveryOptions[] | {Id,Type,Visibility}'
#   PRODUCT_ID=prod-xxxx DELIVERY_OPTION_ID=<uuid> \
#     scripts/marketplace-changeset.sh docs/marketplace/catalog-api/06-restrict-delivery.json
#   # For an AMI product, add ENTITY_TYPE=AmiProduct@1.0 (defaults to
#   # ContainerProduct@1.0). NOTE: RestrictDeliveryOptions does NOT support
#   # Intent: VALIDATE on AmiProduct@1.0, so an AMI restrict cannot be dry-run —
#   # it runs APPLY directly (a FAILED change-set is a no-op, so this is safe).
#   PRODUCT_ID=prod-xxxx ENTITY_TYPE=AmiProduct@1.0 DELIVERY_OPTION_ID=<uuid> \
#     scripts/marketplace-changeset.sh docs/marketplace/catalog-api/06-restrict-delivery.json
#
# Env: AWS_PROFILE (default elevarq), AWS_REGION (default us-east-1),
#      INTENT (default APPLY; set VALIDATE to dry-run — AWS validates the
#      change set without creating/modifying any entity, per the AMI-product
#      spec's authoring step). VALIDATE still returns a ChangeSetId that reaches
#      a terminal SUCCEEDED/FAILED status, so the same guards + polling apply.
# Requires: aws, jq, envsubst (gettext).

set -euo pipefail

TEMPLATE="${1:?usage: marketplace-changeset.sh <change-set-template.json>}"
[ -f "$TEMPLATE" ] || { echo "error: no such template: $TEMPLATE" >&2; exit 1; }

export AWS_PROFILE="${AWS_PROFILE:-elevarq}"
export AWS_REGION="${AWS_REGION:-us-east-1}"
# Entity type for templates that parameterize it (06-restrict-delivery.json).
# Defaults to the container product so existing container callers are unchanged;
# set ENTITY_TYPE=AmiProduct@1.0 to restrict an AMI-product delivery option.
export ENTITY_TYPE="${ENTITY_TYPE:-ContainerProduct@1.0}"
INTENT="${INTENT:-APPLY}"
case "$INTENT" in
  APPLY | VALIDATE) ;;
  *) echo "error: INTENT must be APPLY or VALIDATE (got: $INTENT)" >&2; exit 1 ;;
esac

for bin in aws jq envsubst; do
  command -v "$bin" >/dev/null 2>&1 || { echo "error: missing tool: $bin" >&2; exit 1; }
done

# Fail closed on any ${VAR} the template references that is UNSET or EMPTY in
# the environment, BEFORE envsubst runs (Elevarq/Signals#470). This is the
# real fix for the "VersionTitle literally ${VERSION}" incident: envsubst
# silently replaces an UNSET variable with an empty string, so the
# post-render '${' grep below can never catch a missing VERSION — the title
# would have shipped empty (or, with a restricted format list, as the literal
# placeholder). Marketplace VersionTitles are IMMUTABLE, so a wrong/empty
# title is unrecoverable. Requiring every referenced var to be non-empty up
# front makes an unset VERSION (or any other input) abort loudly instead.
# SC2016: single quotes are deliberate — grep the LITERAL ${...} in the
# template, not a shell expansion.
missing=""
# shellcheck disable=SC2016
while IFS= read -r var; do
  [ -n "$var" ] || continue
  # Indirect expansion: is the referenced variable set and non-empty?
  if [ -z "${!var:-}" ]; then
    missing="${missing}${missing:+ }${var}"
  fi
done < <(grep -oE '\$\{[A-Za-z_][A-Za-z0-9_]*\}' "$TEMPLATE" | sed -E 's/^\$\{//; s/\}$//' | sort -u)
if [ -n "$missing" ]; then
  echo "error: required template variable(s) unset or empty for $TEMPLATE:" >&2
  for var in $missing; do echo "       \${$var}" >&2; done
  echo "       set every referenced variable before submitting — envsubst would" >&2
  echo "       otherwise blank them, and Marketplace VersionTitles are immutable." >&2
  exit 1
fi

rendered="$(mktemp)"
trap 'rm -f "$rendered"' EXIT
envsubst < "$TEMPLATE" > "$rendered"

# Belt-and-braces: fail fast on any LITERAL ${...} that survived envsubst
# (e.g. if the template was submitted with a restricted substitution list).
# SC2016: the single quotes are deliberate — we match the LITERAL ${ that
# envsubst would have replaced, not a shell expansion.
# shellcheck disable=SC2016
if grep -q '\${' "$rendered"; then
  echo "error: unsubstituted variables remain in the rendered change set:" >&2
  # shellcheck disable=SC2016
  grep -oE '\$\{[A-Za-z_][A-Za-z0-9_]*\}' "$rendered" | sort -u >&2
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

# Fail fast on an unsupported EKS add-on AddOnType. AWS validates this only after
# submission (INVALID_ADDON_TYPE), and AddOnType is immutable across versions
# once published, so a typo is costly. See spec FC-EAO-03.
addon_valid='Gitops|monitoring|logging|cert-management|policy-management|cost-management|autoscaling|storage|kubernetes-management|service-mesh|etcd-backup|ingress-service-type|load-balancer|local-registry|networking|Security|backup|ingress-controller|observability'
bad_addon_type="$(jq -r '[.. | objects | .AddOnType? // empty] | .[]' "$rendered" \
  | grep -vxE "$addon_valid" | sort -u || true)"
if [ -n "$bad_addon_type" ]; then
  echo "error: unsupported EKS AddOnType value(s) in the rendered change set:" >&2
  echo "$bad_addon_type" | awk '{print "       " $0}' >&2
  echo "       valid: $(echo "$addon_valid" | tr '|' ' ')" >&2
  exit 1
fi

echo "==> Submitting change set from $TEMPLATE (intent: $INTENT)"
CHANGE_SET_ID="$(aws marketplace-catalog start-change-set \
  --intent "$INTENT" \
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
