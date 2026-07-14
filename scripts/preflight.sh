#!/usr/bin/env bash
#
# Preflight checks — runs the same Go gates CI runs, locally.
#
# This script is the single source of truth for the four Go gates
# enforced by .github/workflows/ci.yml. CI calls each subcommand
# below; the .githooks/pre-push hook calls `all` before every push.
#
# Spec: tracking issue Elevarq/Signals#141. Sibling of
# Elevarq/Arq-Workbench#255 — same pattern shared across repos.
#
# Usage:
#   scripts/preflight.sh             # default: run `all`
#   scripts/preflight.sh all         # gofmt + vet + build + test + security, fail-fast
#   scripts/preflight.sh gofmt       # gofmt -l . (fail if any file listed)
#   scripts/preflight.sh vet         # go vet ./...
#   scripts/preflight.sh build       # go build ./...
#   scripts/preflight.sh test        # go test -count=1 -race ./...
#   scripts/preflight.sh docs        # check-docs.sh (link/version drift guard)
#   scripts/preflight.sh imagebuilder # check-imagebuilder-component.sh (AMI groundwork)
#   scripts/preflight.sh secrets     # gitleaks protect --staged + current-commit detect
#   scripts/preflight.sh vuln        # govulncheck ./...
#   scripts/preflight.sh semgrep     # semgrep --config p/golang --config p/security-audit
#   scripts/preflight.sh osv         # osv-scanner --recursive .
#   scripts/preflight.sh kube-lint   # kube-linter + conftest against rendered chart
#   scripts/preflight.sh lint        # golangci-lint run ./...
#   scripts/preflight.sh security    # secrets + vuln + semgrep + osv + kube-lint + lint
#
# Exit codes:
#   0   — all requested checks passed
#   1   — at least one check failed
#   2   — invalid subcommand
#   127 — required tool missing (with install hint)

set -euo pipefail

# Resolve repo root from the script's own location so the script
# works regardless of the caller's CWD (`./scripts/preflight.sh`,
# `bash scripts/preflight.sh`, called from a git hook, etc.).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

# ANSI colours — disabled when stdout is not a TTY (CI logs etc.).
if [ -t 1 ]; then
  C_RED=$'\033[0;31m'
  C_GREEN=$'\033[0;32m'
  C_YELLOW=$'\033[0;33m'
  C_RESET=$'\033[0m'
else
  C_RED=""
  C_GREEN=""
  C_YELLOW=""
  C_RESET=""
fi

log_step() { printf "%s==> %s%s\n" "${C_YELLOW}" "$1" "${C_RESET}"; }
log_ok()   { printf "%s   ✓ %s%s\n" "${C_GREEN}" "$1" "${C_RESET}"; }
log_fail() { printf "%s   ✗ %s%s\n" "${C_RED}"   "$1" "${C_RESET}" >&2; }

# require_cmd returns 127 with an actionable install hint when a
# required tool is missing. Used by the #160 security gates so a
# missing tool fails loudly rather than silently passing.
require_cmd() {
  local cmd="$1"
  local install_hint="$2"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    log_fail "missing required tool: $cmd"
    printf "Install locally with: %s\n" "$install_hint" >&2
    return 127
  fi
}

run_gofmt() {
  log_step "gofmt"
  local unformatted
  unformatted="$(gofmt -l .)"
  if [ -n "$unformatted" ]; then
    log_fail "gofmt: unformatted files"
    echo "$unformatted" >&2
    echo "Run 'gofmt -w .' to fix." >&2
    return 1
  fi
  log_ok "gofmt: all files formatted"
}

run_vet() {
  log_step "go vet"
  if ! go vet ./...; then
    log_fail "go vet failed"
    return 1
  fi
  log_ok "go vet: clean"
}

run_build() {
  log_step "go build"
  if ! go build ./...; then
    log_fail "go build failed"
    return 1
  fi
  log_ok "go build: clean"
}

run_test() {
  log_step "go test -race"
  if ! go test -count=1 -race ./...; then
    log_fail "go test failed"
    return 1
  fi
  log_ok "go test: passing"
}

# #261: documentation-drift guard. Dependency-free, so it sits with the
# fast Go gates rather than the security bundle. Asserts docs/adoption
# -guide.md carries no hard-coded image version and no broken relative
# links.
run_docs() {
  log_step "check-docs.sh"
  if ! bash "${SCRIPT_DIR}/check-docs.sh"; then
    log_fail "docs check failed"
    return 1
  fi
  log_ok "docs: clean"
}

# #266: static guard for the demand-gated AMI / EC2 Image Builder groundwork
# (specifications/marketplace-ami-image-builder.md, TC-AMI-01..04). Dependency
# -light and runs no AWS call, so it sits with the fast gates.
run_imagebuilder() {
  log_step "check-imagebuilder-component.sh"
  if ! bash "${SCRIPT_DIR}/check-imagebuilder-component.sh"; then
    log_fail "imagebuilder-component check failed"
    return 1
  fi
  log_ok "imagebuilder-component: clean"
}

# #160: secrets / vuln / lint are local-fast security gates. CI keeps
# the full-history gitleaks run + SBOM + Grype + Scorecard in
# scheduled/release workflows so push-time stays cheap.
run_secrets() {
  require_cmd gitleaks "brew install gitleaks" || return $?

  log_step "gitleaks protect --staged"
  if ! gitleaks protect \
      --source . \
      --config .gitleaks.toml \
      --redact \
      --no-banner \
      --staged; then
    log_fail "secrets staged"
    return 1
  fi

  log_step "gitleaks detect (current commit only)"
  if ! gitleaks detect \
      --source . \
      --config .gitleaks.toml \
      --redact \
      --no-banner \
      --log-opts="-1"; then
    log_fail "secrets current commit"
    printf "For full-history evidence run: gitleaks detect --source . --config .gitleaks.toml --redact --no-banner\n" >&2
    return 1
  fi

  log_ok "secrets"
}

run_vuln() {
  require_cmd govulncheck "go install golang.org/x/vuln/cmd/govulncheck@latest" || return $?

  log_step "govulncheck ./..."
  if ! govulncheck ./...; then
    log_fail "vuln"
    return 1
  fi
  log_ok "vuln"
}

run_lint() {
  require_cmd golangci-lint "brew install golangci-lint" || return $?

  log_step "golangci-lint run ./..."
  if ! golangci-lint run ./...; then
    log_fail "lint"
    return 1
  fi
  log_ok "lint"
}

# #163: Semgrep (SAST) + OSV-Scanner (dependency CVE feed). Both
# complement the existing gates — Semgrep catches code-level
# patterns no other gate enforces, OSV-Scanner catches stdlib +
# transitive-dep advisories govulncheck's reachability filter
# elides. False-positive suppression for Semgrep lives in code via
# `// nosemgrep: <rule-id>` with a justifying comment; OSV-Scanner
# suppressions, if ever needed, go in `osv-scanner.toml` at the
# repo root.
run_semgrep() {
  require_cmd semgrep "brew install semgrep" || return $?

  log_step "semgrep (p/golang + p/security-audit)"
  if ! semgrep --config p/golang --config p/security-audit --error --quiet; then
    log_fail "semgrep"
    return 1
  fi
  log_ok "semgrep"
}

run_osv() {
  require_cmd osv-scanner "brew install osv-scanner" || return $?

  log_step "osv-scanner --recursive ."
  if ! osv-scanner --recursive .; then
    log_fail "osv"
    return 1
  fi
  log_ok "osv"
}

# #164: KubeLinter + Conftest lint the Helm chart and its
# rendered output. kube-linter covers the CIS-aligned check set;
# conftest evaluates project-specific Rego policies under policy/
# (defence-in-depth: automountServiceAccountToken=false, no
# hostNetwork/hostPID/hostIPC, runAsNonRoot, etc.).
#
# We render the chart once and pipe the manifest stream into both
# linters so they evaluate the same artifact CI would deploy.
run_kube_lint() {
  require_cmd helm        "brew install helm"        || return $?
  require_cmd kube-linter "brew install kube-linter" || return $?
  require_cmd conftest    "brew install conftest"    || return $?

  log_step "helm template + kube-linter + conftest"
  local rendered
  rendered="$(mktemp)"
  # shellcheck disable=SC2064
  trap "rm -f '${rendered}'" RETURN
  if ! helm template deploy/helm/signals > "${rendered}"; then
    log_fail "kube-lint: helm template"
    return 1
  fi
  if ! kube-linter lint "${rendered}"; then
    log_fail "kube-lint: kube-linter"
    return 1
  fi
  # Tempfile has no extension, so tell conftest the parser
  # explicitly — otherwise it can't infer "yaml" from the name.
  if ! conftest test --policy policy/ --parser yaml --all-namespaces "${rendered}"; then
    log_fail "kube-lint: conftest"
    return 1
  fi
  log_ok "kube-lint"
}

run_security() {
  run_secrets
  run_vuln
  run_semgrep
  run_osv
  run_kube_lint
  run_lint
}

run_all() {
  # Fail-fast: stop at the first failure. The Go gates are ordered
  # fastest-first so an unformatted-file gate fails in ~1s instead
  # of after a 30s test run. The security bundle runs last so a
  # missing tool (gitleaks/govulncheck/golangci-lint) doesn't
  # block the Go feedback loop on machines that don't have the
  # security toolchain installed yet.
  run_gofmt
  run_vet
  run_build
  run_test
  run_docs
  run_imagebuilder
  run_security
  echo ""
  printf "%spreflight: all checks passed%s\n" "${C_GREEN}" "${C_RESET}"
}

usage() {
  sed -n 's/^# \{0,1\}//; 1,/^$/p' "$0" | sed -n '/^Usage:/,/^Exit codes:$/p'
}

case "${1:-all}" in
  gofmt)    run_gofmt ;;
  vet)      run_vet ;;
  build)    run_build ;;
  test)     run_test ;;
  docs)     run_docs ;;
  imagebuilder) run_imagebuilder ;;
  secrets)  run_secrets ;;
  vuln)     run_vuln ;;
  semgrep)  run_semgrep ;;
  osv)      run_osv ;;
  kube-lint) run_kube_lint ;;
  lint)     run_lint ;;
  security) run_security ;;
  all)      run_all ;;
  -h|--help|help)
    usage
    exit 0
    ;;
  *)
    printf "preflight: unknown subcommand %q\n\n" "$1" >&2
    usage >&2
    exit 2
    ;;
esac
