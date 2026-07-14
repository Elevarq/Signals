#!/usr/bin/env bash
#
# check-docs.sh — lightweight documentation-drift guard.
#
# Catches the two failure modes that let docs/adoption-guide.md drift
# from reality between releases (Elevarq/Signals#261):
#
#   1. A hard-coded image version. The guide must reference the image
#      as ghcr.io/elevarq/signals:<version> (a placeholder), never a
#      pinned SemVer that silently goes stale every release.
#   2. A broken relative link. Every relative Markdown link in the
#      guarded files must resolve to a file that exists in the tree,
#      so a moved/renamed doc is caught here instead of by a reader.
#
# Dependency-free (bash + grep + sed) so it runs anywhere CI or the
# pre-push hook runs, with no toolchain to install.
#
# Usage:
#   scripts/check-docs.sh          # check the guarded doc set
#
# Exit codes:
#   0 — all checks passed
#   1 — at least one check failed

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

if [ -t 1 ]; then
  C_RED=$'\033[0;31m'; C_GREEN=$'\033[0;32m'; C_YELLOW=$'\033[0;33m'; C_RESET=$'\033[0m'
else
  C_RED=""; C_GREEN=""; C_YELLOW=""; C_RESET=""
fi
log_step() { printf "%s==> %s%s\n" "${C_YELLOW}" "$1" "${C_RESET}"; }
log_ok()   { printf "%s   ✓ %s%s\n" "${C_GREEN}" "$1" "${C_RESET}"; }
log_fail() { printf "%s   ✗ %s%s\n" "${C_RED}"   "$1" "${C_RESET}" >&2; }

# The docs whose examples and links are load-bearing for adopters.
# Add files here as their content becomes release-sensitive.
GUARDED_DOCS=(
  "docs/adoption-guide.md"
)

fail=0

# --- Check 1: no hard-coded image SemVer -----------------------------------
# The guide must use the :<version> placeholder. A pinned tag such as
# ghcr.io/elevarq/signals:1.0.0 is the exact stale-version drift #261
# removed; reject any digit after the image's colon.
log_step "docs: no hard-coded image version"
for doc in "${GUARDED_DOCS[@]}"; do
  [ -f "${doc}" ] || continue
  if hits="$(grep -nE 'ghcr\.io/elevarq/signals:[0-9]' "${doc}")"; then
    log_fail "${doc}: hard-coded image version — use ghcr.io/elevarq/signals:<version>"
    printf "%s\n" "${hits}" >&2
    fail=1
  fi
done
[ "${fail}" -eq 0 ] && log_ok "no hard-coded image version"

# --- Check 2: relative Markdown links resolve ------------------------------
# Extract [text](target) links, keep only relative ones (skip http(s):,
# mailto:, and pure #anchors), strip any #fragment, and assert the file
# exists relative to the linking doc's directory.
log_step "docs: relative links resolve"
link_fail=0
for doc in "${GUARDED_DOCS[@]}"; do
  [ -f "${doc}" ] || continue
  doc_dir="$(dirname "${doc}")"
  while IFS= read -r target; do
    [ -z "${target}" ] && continue
    case "${target}" in
      http://*|https://*|mailto:*|"#"*) continue ;;
    esac
    path="${target%%#*}"                       # drop #fragment
    [ -z "${path}" ] && continue               # was a pure anchor
    if [ ! -e "${doc_dir}/${path}" ]; then
      log_fail "${doc}: broken relative link -> ${target}"
      link_fail=1
    fi
  done < <(grep -oE '\]\([^)]+\)' "${doc}" | sed -E 's/^\]\(//; s/\)$//')
done
if [ "${link_fail}" -eq 0 ]; then
  log_ok "relative links resolve"
else
  fail=1
fi

if [ "${fail}" -ne 0 ]; then
  printf "%sdocs: check failed%s\n" "${C_RED}" "${C_RESET}" >&2
  exit 1
fi
printf "%sdocs: all checks passed%s\n" "${C_GREEN}" "${C_RESET}"
