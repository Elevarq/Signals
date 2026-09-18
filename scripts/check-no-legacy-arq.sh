#!/usr/bin/env bash
# Copyright (c) 2026 Scantr LLC. All rights reserved.
# Elevarq is a trade name of Scantr LLC.
#
# De-arq guard (#2568 / Signals#416): a BURN-TO-ZERO gate that forbids ANY
# legacy "arq" naming (the pre-rebrand product name) anywhere in the tracked
# tree except the immutable/history surfaces allow-listed below.
#
# The product is Elevarq; legacy standalone "arq" must not appear in runtime
# strings (container names, /var/lib/arq, arq-license.json, image refs, helm
# chart dirs), code identifiers, or live docs. The rename debt has been burned
# to zero (Signals#416); this guard keeps it there — the check FAILS on the
# first legacy-arq hit, it is not a "no new beyond a baseline" ratchet.
#
# Detection: case-insensitive "arq" at a WORD BOUNDARY (\barq), excluding the
# tooling's own vocabulary "de-arq" / "legacy-arq" (and thus "no-legacy-arq").
# The word boundary is the whole trick — it cannot fall inside "elev|arq" (the
# "a" is preceded by the word char "v"), so `\barq` matches standalone `arq` /
# `arq-*` / `/…/arq` but NEVER matches `elevarq`, and never matches `pgagroal`.
#
# ENGINE — honest by construction. macOS `git` is frequently built WITHOUT
# PCRE, and `git grep -P` then silently matches nothing and reports GREEN — the
# exact bug that let this rename stay "done" for months. So the primary engine
# is Python 3 (the authoritative engine used to produce the census). We fall
# back to `git grep -P` ONLY after probing that it really supports PCRE, and if
# NEITHER is available we hard-error (exit 2) rather than pass silently.
#
# Modes:
#   check  (default) — exit 1 if any legacy-arq remains; exit 0 only at zero.
#   list             — print the current per-file inventory (path:count).
#
# Scope decision (#2568): runtime + code/docs, NOT GitHub repo names. History
# and immutable files are excluded below. Note the Elevarq repos have already
# been renamed (Arq -> Analyzer, Arq-Workbench -> Workbench, Arq-Signals ->
# Signals), so references to those old names are stale and were updated to the
# current names during the burn-down, not left as "repo names".

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

SELF="scripts/check-no-legacy-arq.sh"

# The canonical pattern, shared by both engines.
PATTERN='(?i)(?<!de-)(?<!legacy-)\barq'

# Excluded surfaces (allow-list): history + immutable + the guard's own file.
# Kept in sync between the Python engine and the git-grep fallback.
EXCLUDES_GREP=(
  ':!CHANGELOG.md'
  ':!**/CHANGELOG.md'
  ':!LICENSE'
  ':!NOTICE'
  ":!$SELF"
)

# Python engine: authoritative. Reads tracked files, skips binaries, applies
# the same allow-list, prints "path:count" for every file with >=1 match.
scan_python() {
  python3 - "$SELF" <<'PY'
import os, re, subprocess, sys
self_path = sys.argv[1]
pattern = re.compile(r'(?i)(?<!de-)(?<!legacy-)\barq')
excluded_basenames = {'CHANGELOG.md', 'LICENSE', 'NOTICE'}
excluded_paths = {self_path}
def is_binary(p):
    try:
        with open(p, 'rb') as f:
            return b'\x00' in f.read(8192)
    except OSError:
        return True
files = subprocess.check_output(['git', 'ls-files'], text=True).splitlines()
out = []
for f in files:
    if f in excluded_paths:
        continue
    if os.path.basename(f) in excluded_basenames:
        continue
    if not os.path.isfile(f) or is_binary(f):
        continue
    try:
        with open(f, encoding='utf-8', errors='replace') as fh:
            n = sum(len(pattern.findall(line)) for line in fh)
    except OSError:
        continue
    if n:
        out.append(f'{f}:{n}')
out.sort()
print('\n'.join(out))
PY
}

# git-grep fallback: only used when Python is absent AND git grep -P really
# supports PCRE (probed below). Counts matching lines per file.
scan_gitgrep() {
  git grep -I -c -P "$PATTERN" -- "${EXCLUDES_GREP[@]}" 2>/dev/null \
    | LC_ALL=C sort || true
}

# Probe whether `git grep -P` actually supports PCRE on this machine. A git
# built without PCRE errors out on a lookbehind pattern; one with PCRE matches
# the literal "arq". Probe against a temp file via --no-index.
gitgrep_has_pcre() {
  local tmp rc=0
  tmp="$(mktemp)"
  printf 'arq\n' > "$tmp"
  git grep --no-index -P -q "$PATTERN" -- "$tmp" >/dev/null 2>&1 || rc=$?
  rm -f "$tmp"
  return $rc
}

scan() {
  if command -v python3 >/dev/null 2>&1; then
    scan_python
  elif gitgrep_has_pcre; then
    scan_gitgrep
  else
    echo "de-arq guard: no usable regex engine (python3 absent and git grep" >&2
    echo "lacks PCRE). Cannot verify legacy-arq honestly — refusing to pass." >&2
    exit 2
  fi
}

case "${1:-check}" in
  list)
    scan
    ;;
  check)
    hits="$(scan)"
    if [ -n "$hits" ]; then
      files=$(printf '%s\n' "$hits" | grep -c ':' || true)
      lines=$(printf '%s\n' "$hits" | awk -F: '{s+=$NF} END{print s+0}')
      echo "de-arq guard FAILED: legacy 'arq' still present"
      echo "  ${files} file(s), ${lines} occurrence(s):"
      printf '%s\n' "$hits" | sed 's/^/    /'
      echo ""
      echo "Rename every occurrence to 'elevarq' (or the current canonical"
      echo "name for a renamed repo/module/contract). This gate burns to zero."
      exit 1
    fi
    echo "de-arq guard OK: zero legacy 'arq' in the tracked tree."
    ;;
  *)
    echo "usage: $SELF [check|list]" >&2
    exit 2
    ;;
esac
