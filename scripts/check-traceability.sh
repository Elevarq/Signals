#!/usr/bin/env bash
#
# check-traceability.sh — STDD Gate 1 (matrix-internal), Elevarq/Signals#451.
#
# The traceability matrix (features/signals/traceability.md) is the
# specification -> test map. This gate enforces the matrix's INTERNAL
# consistency so a spec rule can never claim coverage it does not have:
#
#   1. Every data row has a non-empty Rule ID.
#   2. Rule IDs are unique across the whole matrix (no silent ID reuse).
#   3. A row that claims coverage (Coverage Status = COVERED and Evidence
#      Type != DOC) MUST list at least one Test ID — an orphan rule-ID with
#      no test fails the build. Rows explicitly marked PLANNED, or carrying
#      DOC evidence, may legitimately have an empty Test ID cell.
#
# Scope note (#451): this is the matrix-internal direction (rule -> test).
# Full SOURCE-LEVEL bidirectional enforcement (every matrix Test ID resolves
# to a real test, and every test maps back to a spec ID via a comment
# convention) is tracked as follow-up #519 — it needs a one-time reconciliation
# of pre-existing rows and a test->ID convention rollout.
#
# Dependency-free (bash + awk) so it runs anywhere CI or the pre-push hook
# runs. Exit 0 = consistent, 1 = at least one violation.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
MATRIX="${REPO_ROOT}/features/signals/traceability.md"

if [ -t 1 ]; then
  C_RED=$'\033[0;31m'; C_GREEN=$'\033[0;32m'; C_YELLOW=$'\033[0;33m'; C_RESET=$'\033[0m'
else
  C_RED=""; C_GREEN=""; C_YELLOW=""; C_RESET=""
fi
log_step() { printf "%s==> %s%s\n" "${C_YELLOW}" "$1" "${C_RESET}"; }
log_ok()   { printf "%s   ✓ %s%s\n" "${C_GREEN}" "$1" "${C_RESET}"; }
log_fail() { printf "%s   ✗ %s%s\n" "${C_RED}"   "$1" "${C_RESET}" >&2; }

log_step "traceability: matrix-internal consistency (${MATRIX#"${REPO_ROOT}/"})"

if [ ! -f "${MATRIX}" ]; then
  log_fail "traceability matrix not found: ${MATRIX}"
  exit 1
fi

# awk does all parsing. A data row is a table line whose first cell is NOT the
# "Rule ID" header and NOT a `---`/`:--` separator. Fields are 1=Rule ID,
# 3=Test ID(s), 4=Coverage Status, 5=Evidence Type (cell $1 is empty because
# the row starts with '|').
report="$(
  awk -F'|' '
    function trim(s) { gsub(/^[ \t]+|[ \t]+$/, "", s); return s }
    # only consider markdown table rows with at least 6 pipe-delimited cells
    /^[[:space:]]*\|/ && NF >= 7 {
      rule = trim($2)
      tests = trim($4)
      cover = trim($5)
      evid  = trim($6)
      # skip header + separator rows
      if (rule == "Rule ID") next
      if (rule ~ /^:?-+:?$/) next
      if (rule == "") {
        # a row with no rule id at all is malformed
        printf "MALFORMED\t(line %d) empty Rule ID\n", NR
        next
      }
      # duplicate rule id
      if (rule in seen) {
        printf "DUPLICATE\t%s (first at line %d, again at line %d)\n", rule, seen[rule], NR
      } else {
        seen[rule] = NR
      }
      # orphan: claims COVERED with non-DOC evidence but lists no test
      notest = (tests == "" || tests == "-" || tests == "—")
      if (notest && cover == "COVERED" && evid != "DOC") {
        printf "ORPHAN\t%s (line %d): COVERED/%s but no Test ID\n", rule, NR, evid
      }
    }
    END { }
  ' "${MATRIX}"
)"

if [ -n "${report}" ]; then
  printf "%s\n" "${report}" | while IFS=$'\t' read -r kind detail; do
    log_fail "${kind}: ${detail}"
  done
  printf "%straceability: matrix is inconsistent%s\n" "${C_RED}" "${C_RESET}" >&2
  exit 1
fi

# Count the rows we validated, for a useful green line.
rows="$(awk -F'|' '
  function trim(s){gsub(/^[ \t]+|[ \t]+$/,"",s);return s}
  /^[[:space:]]*\|/ && NF >= 7 {
    r=trim($2); if (r=="Rule ID" || r ~ /^:?-+:?$/ || r=="") next; c++
  }
  END{print c+0}
' "${MATRIX}")"

log_ok "traceability: ${rows} rule rows, all with a test or an explicit PLANNED/DOC marker; IDs unique"
