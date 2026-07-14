#!/usr/bin/env bash
#
# check-imagebuilder-component.sh — static guard for the AMI / EC2 Image
# Builder groundwork (Elevarq/Signals#266, part of #233, refs #235).
#
# Enforces the statically-checkable acceptance cases of
# specifications/marketplace-ami-image-builder.md (ACTIVE) so the
# demand-gated AMI groundwork stays submission-ready and cannot regress
# (the #240 slug defect is exactly the class this catches). It runs NO
# AWS API call and NO Marketplace change-set.
#
#   TC-AMI-01  component is a valid AWSTOE doc (name/description/
#              schemaVersion: 1.0/phases with build+validate+test)
#   TC-AMI-02  no baked secrets/config — build phase leaves /etc/signals empty
#   TC-AMI-03  SignalsImage default is a pinned :<x.y.z> tag at the ghcr slug
#   TC-AMI-04  no runnable AmiProduct@1.0 change-set template is committed
#
# TC-AMI-05 (live baked-AMI smoke) is deferred and intentionally not here.
#
# Dependency-light: grep/sed for every assertion, with a best-effort real
# YAML parse (TC-AMI-01) only when python3 + PyYAML are present — skipped,
# not failed, when they are not (CI runners are not guaranteed to have it).
#
# Usage:   scripts/check-imagebuilder-component.sh
# Exit:    0 all pass · 1 a check failed

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

COMPONENT="deploy/aws/imagebuilder/signals-collector-component.yaml"
CATALOG_DIR="docs/marketplace/catalog-api"
fail=0

if [ ! -f "${COMPONENT}" ]; then
  log_fail "component not found: ${COMPONENT}"
  exit 1
fi

# --- TC-AMI-01: valid AWSTOE document ---------------------------------------
log_step "TC-AMI-01: component is a valid AWSTOE document"
tc01=0
for key in '^name:' '^description:' '^schemaVersion:' '^phases:'; do
  grep -qE "${key}" "${COMPONENT}" || { log_fail "missing top-level key: ${key#^}"; tc01=1; }
done
grep -qE '^schemaVersion:[[:space:]]*("?1\.0"?)[[:space:]]*$' "${COMPONENT}" \
  || { log_fail "schemaVersion is not 1.0"; tc01=1; }
for phase in build validate test; do
  grep -qE "^[[:space:]]*-[[:space:]]*name:[[:space:]]*${phase}([[:space:]]|$)" "${COMPONENT}" \
    || { log_fail "missing '${phase}' phase"; tc01=1; }
done
# Best-effort strict YAML parse when the toolchain is available; a parse
# error is a hard fail, an absent parser is a skip (not a fail).
if command -v python3 >/dev/null 2>&1 && python3 -c 'import yaml' >/dev/null 2>&1; then
  if ! python3 - "${COMPONENT}" <<'PY'
import sys, yaml
doc = yaml.safe_load(open(sys.argv[1]))
assert isinstance(doc, dict), "top level is not a mapping"
assert doc.get("schemaVersion") in (1.0, "1.0"), "schemaVersion != 1.0"
phases = {p.get("name") for p in doc.get("phases", []) if isinstance(p, dict)}
assert {"build", "validate", "test"} <= phases, f"phases missing: {phases}"
PY
  then
    log_fail "python YAML parse failed"; tc01=1
  fi
else
  printf "   (python3+yaml absent — strict parse skipped, grep checks stand)\n"
fi
[ "${tc01}" -eq 0 ] && log_ok "valid AWSTOE document" || fail=1

# --- TC-AMI-02: no baked secrets / config -----------------------------------
# R-AMI-01 / INV-AMI-01: the build phase must leave /etc/signals empty — no
# signals.yaml, signals.env, token, or password written at bake time. Config
# and the token are launch-time inputs. Scan only the build phase.
log_step "TC-AMI-02: no baked secrets or config in the build phase"
tc02=0
# Extract the build phase: from its `- name: build` line up to the next
# phase (`- name: validate`). Anchored on end-of-line (portable BRE; avoids
# the GNU-only `\b`, which BSD/macOS sed silently ignores).
build_block="$(sed -n '/^[[:space:]]*-[[:space:]]*name:[[:space:]]*build[[:space:]]*$/,/^[[:space:]]*-[[:space:]]*name:[[:space:]]*validate[[:space:]]*$/p' "${COMPONENT}")"
if [ -z "${build_block}" ]; then
  log_fail "could not isolate the build phase — component structure unexpected"
  tc02=1
fi
# A bake-time WRITE of a file under /etc/signals is a config/secret leak: a
# CreateFile `path:` under it, or a shell redirection / tee into it. Merely
# REFERENCING those paths inside the systemd unit (ExecStart `--config …`,
# `EnvironmentFile=…`) is the launch-time contract and is allowed, as is
# `install -d /etc/signals` (the empty dir).
if printf '%s\n' "${build_block}" \
    | grep -nE 'path:[[:space:]]*"?/etc/signals/|>[[:space:]]*/etc/signals/|tee[[:space:]]+[^|]*/etc/signals/' ; then
  log_fail "build phase writes a file under /etc/signals (must stay empty at bake)"
  tc02=1
fi
# A hardcoded credential literal (a value on the RHS) is a leak. The passthrough
# form `-e SIGNALS_API_TOKEN` (no '=') and `EnvironmentFile=<path>` are allowed,
# as are comment lines (documentation like `# SIGNALS_API_TOKEN=...` bakes no
# functional secret — a real leak lands in a command or file content).
if printf '%s\n' "${build_block}" | grep -vE '^[[:space:]]*#' \
    | grep -nEi '(password|secret|api[_-]?token|bearer)[[:space:]]*[:=][[:space:]]*[^[:space:]"'"'"']' ; then
  log_fail "build phase contains a hardcoded credential literal"
  tc02=1
fi
[ "${tc02}" -eq 0 ] && log_ok "no baked secrets or config" || fail=1

# --- TC-AMI-03: SignalsImage default is pinned at the ghcr slug --------------
# R-AMI-02: reproducible, traceable image. Must be ghcr.io/elevarq/signals
# at a :<major>.<minor>.<patch> tag — not latest, not untagged, not the
# MP-ECR slug (elevarq-signals — the #240 regression).
log_step "TC-AMI-03: SignalsImage default is a pinned ghcr tag"
tc03=0
default_line="$(grep -E '^[[:space:]]*default:[[:space:]]*"?ghcr\.io/' "${COMPONENT}" | head -1)"
if [ -z "${default_line}" ]; then
  log_fail "SignalsImage default not found or not a ghcr.io reference"
  tc03=1
elif ! printf '%s\n' "${default_line}" | grep -qE 'ghcr\.io/elevarq/signals:[0-9]+\.[0-9]+\.[0-9]+"?[[:space:]]*$'; then
  log_fail "SignalsImage default is not a pinned ghcr.io/elevarq/signals:<x.y.z> tag:"
  printf '     %s\n' "$(printf '%s' "${default_line}" | sed 's/^[[:space:]]*//')" >&2
  tc03=1
fi
# Belt-and-braces: no wrong-slug or floating tag anywhere in the component.
if grep -qE 'ghcr\.io/elevarq/elevarq-signals' "${COMPONENT}"; then
  log_fail "component references the MP-ECR slug elevarq-signals (see #240)"; tc03=1
fi
if grep -qE 'ghcr\.io/elevarq/signals:latest' "${COMPONENT}"; then
  log_fail "component references a :latest tag (must be pinned)"; tc03=1
fi
[ "${tc03}" -eq 0 ] && log_ok "SignalsImage default is pinned at the ghcr slug" || fail=1

# --- TC-AMI-04: no runnable AmiProduct change-set committed ------------------
# R-AMI-04: the groundwork must not stand up the live AMI product. No committed
# catalog-api *change-set template* (a .json) may create/target an AmiProduct.
# Prose scaffolding in the README is explicitly allowed by the acceptance case,
# so scan only the runnable JSON templates.
log_step "TC-AMI-04: no runnable AmiProduct@1.0 change-set template committed"
tc04=0
if [ -d "${CATALOG_DIR}" ]; then
  if hits="$(grep -rlE 'AmiProduct' "${CATALOG_DIR}" --include='*.json' 2>/dev/null)"; then
    log_fail "AmiProduct change-set template(s) committed — groundwork must stay demand-gated:"
    printf '%s\n' "${hits}" | sed 's/^/     /' >&2
    tc04=1
  fi
fi
[ "${tc04}" -eq 0 ] && log_ok "no runnable AmiProduct change-set committed" || fail=1

if [ "${fail}" -ne 0 ]; then
  printf "%simagebuilder-component: check failed%s\n" "${C_RED}" "${C_RESET}" >&2
  exit 1
fi
printf "%simagebuilder-component: all checks passed%s\n" "${C_GREEN}" "${C_RESET}"
