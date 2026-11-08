#!/usr/bin/env bash
#
# install-pinned-tool.sh — fetch a release tarball, verify its content
# identity against a pinned SHA-256, and only then extract a member to an
# executable path. Fail closed on any mismatch, missing verification
# material, or missing binary — never extract or execute unverified bytes.
#
# This is the supply-chain hardening for the CI Helm/Kubernetes gate
# (Elevarq/Signals#733, audit A8): the kube-linter and conftest installers
# previously piped versioned HTTPS tarballs straight into `tar -x` in
# /usr/local/bin with no content check, so mutable or replaced upstream
# bytes would be executed despite a fixed version string.
#
# Usage:
#   install-pinned-tool.sh <url> <sha256> <member> <dest-dir>
#
#   url       release tarball URL (https:// in CI; file:// accepted for tests)
#   sha256    expected lowercase hex SHA-256 of the downloaded tarball
#   member    the single archive member to extract (e.g. kube-linter)
#   dest-dir  directory to extract the member into (e.g. /usr/local/bin)
#
# Exit status: 0 on verified extraction; non-zero (fail closed) on download
# failure, digest mismatch, missing member, or bad arguments. On any failure
# the destination is left without a freshly-extracted member.
#
set -euo pipefail

die() {
  echo "install-pinned-tool: $*" >&2
  exit 1
}

[ "$#" -eq 4 ] || die "usage: install-pinned-tool.sh <url> <sha256> <member> <dest-dir>"

url="$1"
expected="$2"
member="$3"
dest="$4"

# Normalise/validate the expected digest up front: a malformed or empty pin
# is a configuration error and must fail closed, not pass silently.
expected="$(printf '%s' "$expected" | tr '[:upper:]' '[:lower:]')"
printf '%s' "$expected" | grep -qE '^[0-9a-f]{64}$' \
  || die "expected SHA-256 must be 64 hex chars: '${2}'"

[ -d "$dest" ] || die "destination directory does not exist: $dest"

# Pick a sha256 tool (Linux CI: sha256sum; macOS dev: shasum -a 256).
if command -v sha256sum >/dev/null 2>&1; then
  sha256_of() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
  sha256_of() { shasum -a 256 "$1" | awk '{print $1}'; }
else
  die "no sha256 tool available (need sha256sum or shasum)"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
tarball="$tmp/download.tar.gz"

# Stage the download to a temp file first — never stream straight into tar.
# Deliberately preserve transient-network retry behaviour (--retry) while
# failing on HTTP errors (-f) so a 404/5xx cannot be mistaken for content.
curl -sSfL --retry 3 --retry-delay 2 -o "$tarball" "$url" \
  || die "download failed (fail closed, nothing extracted): $url"

[ -s "$tarball" ] || die "downloaded tarball is empty: $url"

actual="$(sha256_of "$tarball")"
if [ "$actual" != "$expected" ]; then
  die "SHA-256 mismatch for $url
  expected: $expected
  actual:   $actual
  refusing to extract or execute unverified bytes (fail closed)"
fi

# Verified — extract exactly the requested member into the destination.
tar -xz -C "$dest" -f "$tarball" "$member" \
  || die "verified tarball did not contain member '$member'"

[ -e "$dest/$member" ] || die "member '$member' not present after extraction"

echo "install-pinned-tool: verified $member (sha256 $expected) -> $dest/$member"
