#!/usr/bin/env bash
#
# Regenerate THIRD-PARTY-NOTICES from the modules actually linked into the
# distributed binaries (cmd/signals + cmd/signalsctl). It reproduces each
# dependency's own license text, so the redistributed artifact carries proper
# third-party attribution.
#
# This intentionally uses only the Go toolchain (no external tooling): it lists
# the real import-graph modules with `go list -deps` and copies each module's
# LICENSE/COPYING/NOTICE file from the module cache. Test-only and unused
# indirect modules are excluded because their code is not distributed.
#
# Usage:  bash scripts/gen-third-party-notices.sh
# CI:     run it and `git diff --exit-code THIRD-PARTY-NOTICES` to catch drift.
set -euo pipefail

cd "$(dirname "$0")/.."

OUT="THIRD-PARTY-NOTICES"
PKGS=(./cmd/signals ./cmd/signalsctl)
SELF="github.com/elevarq/signals"

# Distinct (path, version, cache-dir) for every module in the linked import
# graph, excluding the standard library (nil .Module) and our own module.
# Fields are space-separated; module paths/versions/cache-dirs contain no spaces.
mods="$(go list -deps -f '{{with .Module}}{{.Path}} {{.Version}} {{.Dir}}{{end}}' "${PKGS[@]}" \
  | sort -u | grep -v "^${SELF} " | grep -v '^$')"

{
  echo "# Third-Party Notices"
  echo
  echo "Elevarq Signals (BSD-3-Clause) is distributed with the third-party Go"
  echo "modules listed below, each under its own license, reproduced in full."
  echo
  echo "Regenerate with \`bash scripts/gen-third-party-notices.sh\`. Do not edit by hand."
  echo
} > "$OUT"

printf '%s\n' "$mods" | while read -r path version dir; do
  [ -z "${path:-}" ] && continue
  lic="$(find "$dir" -maxdepth 1 -type f \
        \( -iname 'LICENSE*' -o -iname 'LICENCE*' -o -iname 'COPYING*' -o -iname 'NOTICE*' \) \
        2>/dev/null | sort | head -1)"
  {
    echo "================================================================================"
    echo "## ${path} ${version}"
    echo
    if [ -n "$lic" ]; then
      cat "$lic"
    else
      echo "(No license file bundled in the module distribution; see https://${path})"
    fi
    echo
  } >> "$OUT"
done

count="$(printf '%s\n' "$mods" | grep -c . || true)"
echo "Wrote $OUT covering ${count} third-party modules."
