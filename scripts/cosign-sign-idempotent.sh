#!/usr/bin/env bash
#
# cosign-sign-idempotent.sh — keyless cosign sign that tolerates the
# Rekor transparency-log 409 ("an equivalent entry already exists").
#
# Elevarq/Signals#675. The tag-driven release workflow signs three refs
# (GHCR image, Docker Hub image, Helm chart) with `cosign sign --yes`.
# On a re-run — or when a digest is unchanged across a re-cut — Rekor
# rejects the duplicate log entry with HTTP 409 `createLogEntryConflict`
# ("an equivalent entry already exists in the transparency log"). That
# 409 means the ref is *already signed*: it is the desired end state,
# not a failure. A bare `cosign sign` exits non-zero and fails the whole
# release run on that re-run.
#
# This wrapper runs `cosign sign --yes <ref>` and treats ONLY a
# Rekor-409 / "already exists" failure as success (idempotent re-sign);
# any other non-zero exit is re-emitted verbatim and propagated as a
# real failure. No other error is swallowed.
#
# Usage: scripts/cosign-sign-idempotent.sh <ref>
#
set -euo pipefail

if [[ $# -ne 1 || -z "${1:-}" ]]; then
  echo "usage: ${0##*/} <ref>" >&2
  exit 2
fi

ref="$1"

# Capture combined stdout+stderr so we can inspect it on failure. Do not
# let `set -e` abort on cosign's non-zero exit — we decide below.
output=""
status=0
output="$(cosign sign --yes "${ref}" 2>&1)" || status=$?

if [[ ${status} -eq 0 ]]; then
  # Success — pass cosign's own output through unchanged.
  printf '%s\n' "${output}"
  exit 0
fi

# Non-zero exit. Tolerate ONLY the Rekor-409 / already-exists signature;
# -i (case-insensitive) because the exact casing varies across cosign
# versions.
if printf '%s' "${output}" | grep -qiE '409|createLogEntryConflict|already exists in the transparency log'; then
  printf '%s\n' "${output}"
  echo "cosign-sign-idempotent: ${ref} already signed (Rekor 409) — treating as success"
  exit 0
fi

# Any other failure is real — re-emit and propagate.
printf '%s\n' "${output}" >&2
exit 1
