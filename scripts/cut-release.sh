#!/usr/bin/env bash
#
# cut-release.sh — the canonical, gated way to cut an Elevarq Signals
# release tag (Elevarq/Signals#480).
#
# Signals releases are tag-driven: pushing `vX.Y.Z` fires release.yml
# (build -> scan -> SBOM -> cosign -> chart publish -> GitHub release).
# Hand-cranking that tag (`git tag` + `git push`) let avoidable defects
# reach main or only surface mid-build — the v1.5.1 cut hit three in a row:
#   - Chart.yaml version vs values.yaml image.tag drift (shipped to main);
#   - release.yml's CHANGELOG `## [X.Y.Z]` gate (only caught by the build);
#   - annotated-tag message required (`fatal: no tag message`).
#
# This script runs every pre-tag gate locally and only then creates and
# pushes the annotated tag, so a release can't ship broken.
#
# Usage:
#   scripts/cut-release.sh vX.Y.Z [--dry-run] [--yes]
#
#   --dry-run  run all gates, print the plan, do NOT tag or push
#   --yes      skip the interactive confirmation (for automation)
#
# Exit codes:
#   0 — all gates passed (and, unless --dry-run, the tag was pushed)
#   1 — a gate failed, or input was invalid
#
# Gates (in order):
#   1. tag format `^v[0-9]+\.[0-9]+\.[0-9]+$`; VERSION = tag without `v`.
#   2. clean worktree + HEAD == origin/main (fetch first).
#   3. version lockstep: Chart.yaml `version` == `appVersion`
#      == values.yaml `image.tag` == VERSION.
#   4. CHANGELOG has a `## [VERSION]` section (mirrors release.yml).
#   5. tag does not already exist (local + remote).

set -euo pipefail

if [ -t 1 ]; then
  C_RED=$'\033[0;31m'; C_GREEN=$'\033[0;32m'; C_YELLOW=$'\033[0;33m'; C_RESET=$'\033[0m'
else
  C_RED=""; C_GREEN=""; C_YELLOW=""; C_RESET=""
fi
log_step() { printf "%s==> %s%s\n" "${C_YELLOW}" "$1" "${C_RESET}"; }
log_ok()   { printf "%s   ✓ %s%s\n" "${C_GREEN}" "$1" "${C_RESET}"; }
log_fail() { printf "%s   ✗ %s%s\n" "${C_RED}"   "$1" "${C_RESET}" >&2; }
die()      { log_fail "$1"; exit 1; }

TAG=""
DRY_RUN=0
ASSUME_YES=0
for arg in "$@"; do
  case "${arg}" in
    --dry-run) DRY_RUN=1 ;;
    --yes|-y)  ASSUME_YES=1 ;;
    -*)        die "unknown flag: ${arg} (usage: cut-release.sh vX.Y.Z [--dry-run] [--yes])" ;;
    *)
      [ -z "${TAG}" ] || die "unexpected extra argument: ${arg}"
      TAG="${arg}"
      ;;
  esac
done

[ -n "${TAG}" ] || die "usage: cut-release.sh vX.Y.Z [--dry-run] [--yes]"

# --- Gate 1: tag format ----------------------------------------------------
log_step "gate 1: tag format"
if ! printf '%s' "${TAG}" | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
  die "tag ${TAG} is not a release tag — expected ^v[0-9]+\\.[0-9]+\\.[0-9]+\$ (no -rc/-beta)"
fi
VERSION="${TAG#v}"
log_ok "tag ${TAG} (VERSION=${VERSION})"

# Resolve repo root from the current git worktree so the script works from
# any subdirectory (and is testable in a throwaway repo).
REPO_ROOT="$(git rev-parse --show-toplevel)" || die "not inside a git repository"
cd "${REPO_ROOT}"

CHART="deploy/helm/signals/Chart.yaml"
VALUES="deploy/helm/signals/values.yaml"
CHANGELOG="CHANGELOG.md"

# --- Gate 2: clean worktree + HEAD == origin/main --------------------------
log_step "gate 2: clean worktree + HEAD == origin/main"
git fetch --quiet origin || die "git fetch origin failed"
if [ -n "$(git status --porcelain)" ]; then
  die "worktree is not clean — commit, stash, or discard changes before tagging"
fi
HEAD_SHA="$(git rev-parse HEAD)"
MAIN_SHA="$(git rev-parse origin/main)"
if [ "${HEAD_SHA}" != "${MAIN_SHA}" ]; then
  die "HEAD (${HEAD_SHA:0:9}) != origin/main (${MAIN_SHA:0:9}) — tag only the merged release commit on main"
fi
log_ok "clean worktree, HEAD == origin/main (${HEAD_SHA:0:9})"

# --- Gate 3: version lockstep ----------------------------------------------
log_step "gate 3: version lockstep (Chart.yaml version/appVersion == values image.tag == ${VERSION})"
[ -f "${CHART}" ]  || die "missing ${CHART}"
[ -f "${VALUES}" ] || die "missing ${VALUES}"

chart_version="$(grep -E '^version:'    "${CHART}" | head -1 | sed -E 's/^version:[[:space:]]*"?([^"[:space:]]+)"?.*/\1/')"
app_version="$(grep -E '^appVersion:'    "${CHART}" | head -1 | sed -E 's/^appVersion:[[:space:]]*"?([^"[:space:]]+)"?.*/\1/')"
image_tag="$(grep -E '^[[:space:]]+tag:' "${VALUES}" | head -1 | sed -E 's/^[[:space:]]+tag:[[:space:]]*"?([^"[:space:]]+)"?.*/\1/')"

lockstep_ok=1
[ "${chart_version}" = "${VERSION}" ] || { log_fail "Chart.yaml version=${chart_version} != ${VERSION}"; lockstep_ok=0; }
[ "${app_version}"   = "${VERSION}" ] || { log_fail "Chart.yaml appVersion=${app_version} != ${VERSION}"; lockstep_ok=0; }
[ "${image_tag}"     = "${VERSION}" ] || { log_fail "values.yaml image.tag=${image_tag} != ${VERSION}"; lockstep_ok=0; }
[ "${lockstep_ok}" -eq 1 ] || die "version lockstep failed — bump Chart.yaml version/appVersion and values.yaml image.tag to ${VERSION} together"
log_ok "version lockstep: ${VERSION}"

# --- Gate 4: CHANGELOG entry (mirrors release.yml) -------------------------
log_step "gate 4: CHANGELOG has a [${VERSION}] section"
[ -f "${CHANGELOG}" ] || die "missing ${CHANGELOG}"
if ! grep -qE "^## \[${VERSION}\]" "${CHANGELOG}"; then
  die "${CHANGELOG} has no '## [${VERSION}]' section — release.yml's CHANGELOG gate would STOP the build"
fi
log_ok "CHANGELOG entry found for ${VERSION}"

# --- Gate 5: tag does not already exist ------------------------------------
log_step "gate 5: tag ${TAG} is new (local + remote)"
if git rev-parse -q --verify "refs/tags/${TAG}" >/dev/null 2>&1; then
  die "tag ${TAG} already exists locally"
fi
if [ -n "$(git ls-remote --tags origin "refs/tags/${TAG}")" ]; then
  die "tag ${TAG} already exists on origin"
fi
log_ok "tag ${TAG} is new"

printf "\n%sAll pre-tag gates passed for %s.%s\n" "${C_GREEN}" "${TAG}" "${C_RESET}"

if [ "${DRY_RUN}" -eq 1 ]; then
  printf "%s--dry-run: would create annotated tag %s at %s and push it to origin.%s\n" \
    "${C_YELLOW}" "${TAG}" "${HEAD_SHA:0:9}" "${C_RESET}"
  exit 0
fi

if [ "${ASSUME_YES}" -ne 1 ]; then
  printf "Create and push annotated tag %s at %s? [y/N] " "${TAG}" "${HEAD_SHA:0:9}"
  read -r reply
  case "${reply}" in
    y|Y|yes|YES) ;;
    *) die "aborted by user — no tag created" ;;
  esac
fi

log_step "tagging"
git tag -a "${TAG}" -m "Release ${TAG}"
git push origin "${TAG}"
log_ok "pushed ${TAG} — release.yml is now running"
