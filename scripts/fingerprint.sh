#!/usr/bin/env bash
#
# fingerprint.sh — STDD Gate 3, Elevarq/Signals#451.
#
# Computes a single deterministic SHA-256 over the KNOWLEDGE LAYER — the
# specifications, acceptance cases, traceability matrix, and tests — and
# verifies it against the committed `.fingerprint`. This is the
# cryptographic form of STDD's atomic-commit rule: a change to a spec or a
# test that does not update the fingerprint (and vice versa) fails CI, so the
# knowledge layer can never drift from what was reviewed.
#
# Covered: specifications/**, features/signals/**, tests/**.
#
# Usage:
#   scripts/fingerprint.sh            # verify (default); exit 1 on mismatch
#   scripts/fingerprint.sh --check    # same as default
#   scripts/fingerprint.sh --write    # recompute and update .fingerprint
#
# Regenerate after any intentional spec/test change:
#   scripts/fingerprint.sh --write && git add .fingerprint
#
# Dependency-light: bash + find + sort + a sha256 tool (sha256sum or
# `shasum -a 256`). Exit 0 = match (or written), 1 = mismatch / missing.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

FINGERPRINT_FILE=".fingerprint"
LAYER_DIRS=("specifications" "features/signals" "tests")

if [ -t 1 ]; then
  C_RED=$'\033[0;31m'; C_GREEN=$'\033[0;32m'; C_YELLOW=$'\033[0;33m'; C_RESET=$'\033[0m'
else
  C_RED=""; C_GREEN=""; C_YELLOW=""; C_RESET=""
fi
log_step() { printf "%s==> %s%s\n" "${C_YELLOW}" "$1" "${C_RESET}"; }
log_ok()   { printf "%s   ✓ %s%s\n" "${C_GREEN}" "$1" "${C_RESET}"; }
log_fail() { printf "%s   ✗ %s%s\n" "${C_RED}"   "$1" "${C_RESET}" >&2; }

# sha256_stdin reads stdin and prints only the hex digest, on both Linux CI
# (sha256sum) and macOS dev (shasum -a 256).
if command -v sha256sum >/dev/null 2>&1; then
  sha256_stdin() { sha256sum | awk '{print $1}'; }
  sha256_file()  { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
  sha256_stdin() { shasum -a 256 | awk '{print $1}'; }
  sha256_file()  { shasum -a 256 "$1" | awk '{print $1}'; }
else
  log_fail "no sha256 tool found (need sha256sum or shasum)"
  exit 1
fi

# compute emits one deterministic digest over "<file-digest>  <path>" lines,
# sorted in C locale so the result is stable across machines.
compute() {
  local f
  while IFS= read -r f; do
    printf '%s  %s\n' "$(sha256_file "${f}")" "${f}"
  done < <(find "${LAYER_DIRS[@]}" -type f | LC_ALL=C sort) | sha256_stdin
}

MODE="${1:---check}"
case "${MODE}" in
  --write)
    digest="$(compute)"
    printf '%s\n' "${digest}" > "${FINGERPRINT_FILE}"
    log_ok "fingerprint: wrote ${FINGERPRINT_FILE} (${digest})"
    ;;
  --check|"")
    log_step "fingerprint: verify knowledge layer (${LAYER_DIRS[*]})"
    if [ ! -f "${FINGERPRINT_FILE}" ]; then
      log_fail "missing ${FINGERPRINT_FILE} — run: scripts/fingerprint.sh --write"
      exit 1
    fi
    want="$(tr -d '[:space:]' < "${FINGERPRINT_FILE}")"
    got="$(compute)"
    if [ "${want}" != "${got}" ]; then
      log_fail "fingerprint mismatch: knowledge layer changed without updating ${FINGERPRINT_FILE}"
      printf "   committed: %s\n   computed:  %s\n" "${want}" "${got}" >&2
      printf "   Regenerate: scripts/fingerprint.sh --write && git add %s\n" "${FINGERPRINT_FILE}" >&2
      exit 1
    fi
    log_ok "fingerprint: knowledge layer matches ${FINGERPRINT_FILE}"
    ;;
  *)
    log_fail "unknown mode ${MODE} (use --check or --write)"
    exit 1
    ;;
esac
