#!/usr/bin/env bash
#
# check-spec-status.sh — STDD spec-lifecycle gate, Elevarq/Signals#451.
#
# Every ROOT specification (specifications/*.md, excluding *.acceptance.md)
# must carry an explicit `## Status` section, and every SHIPPED spec must be
# ACTIVE. This closes the gap the Sep-2026 review flagged: only ~6/19 root
# specs carried a Status marker and some shipped specs were still DRAFT (e.g.
# doctor.md). A spec without a lifecycle state — or a shipped spec stuck in
# DRAFT — is a documentation-vs-reality drift an auditor flags.
#
# Scope: ROOT specs only. The per-collector contracts under
# specifications/collectors/ are a separate inventory (guarded by the
# collector-inventory checks) and are intentionally not swept here.
#
# A spec may be legitimately non-ACTIVE (DRAFT / SUPERSEDED / DEPRECATED /
# REJECTED) only by listing its basename in SPEC_STATUS_ALLOW below, with a
# reason — so a deliberate design-only spec is a conscious, reviewed exception,
# never a silent one. The allow-list is empty today: every root spec is ACTIVE.
#
# Dependency-free (bash + awk). Exit 0 = all good, 1 = a violation.

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

# Basenames allowed to be non-ACTIVE, each with a reason. Format: "name reason".
SPEC_STATUS_ALLOW=(
  # (none — every shipped root spec is ACTIVE)
)
allowed() {
  local base="$1"
  for entry in "${SPEC_STATUS_ALLOW[@]}"; do
    [ "${entry%% *}" = "${base}" ] && return 0
  done
  return 1
}

log_step "spec lifecycle: every root spec has an explicit Status, shipped specs are ACTIVE"

fail=0
count=0
for f in specifications/*.md; do
  case "${f}" in
    *.acceptance.md) continue ;;
  esac
  count=$((count + 1))
  base="$(basename "${f}")"

  # The lifecycle marker is the first non-empty line under a heading that is
  # EXACTLY "## Status" (not "## Status values" or similar domain sections).
  status="$(awk '
    /^##[[:space:]]+Status[[:space:]]*$/ { found=1; next }
    found && /^[[:space:]]*$/            { next }
    found                               { gsub(/^[ \t]+|[ \t]+$/, ""); print; exit }
  ' "${f}")"

  if [ -z "${status}" ]; then
    log_fail "${base}: no '## Status' section (add one: ACTIVE for a shipped spec)"
    fail=1
    continue
  fi
  if [ "${status}" != "ACTIVE" ]; then
    if allowed "${base}"; then
      continue
    fi
    log_fail "${base}: Status is '${status}', must be ACTIVE for a shipped spec (or allow-list it with a reason)"
    fail=1
  fi
done

if [ "${fail}" -ne 0 ]; then
  printf "%sspec lifecycle: check failed%s\n" "${C_RED}" "${C_RESET}" >&2
  exit 1
fi
log_ok "spec lifecycle: ${count} root specs, all ACTIVE"
