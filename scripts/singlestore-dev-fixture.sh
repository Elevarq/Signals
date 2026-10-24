#!/usr/bin/env bash
#
# singlestore-dev-fixture.sh — the reproducible, version-controlled SingleStore
# test environment (Elevarq/Signals#577).
#
# This brings up a REAL SingleStore server for the M4 live qualification: the
# PINNED official SingleStore Dev Image (by digest — never `latest`) plus the
# deterministic seed (examples/singlestore-init.sql: read-only signals_ro user +
# exact grants per specifications/singlestore-grants.md + schema + seed + a small
# workload that exercises the Signals collectors). No interactive /tmp SQL, no
# hand-trimming, no developer-workspace dependence.
#
# It is ENVIRONMENT bring-up ONLY (up/down). Collection + export qualification is
# driven by the NORMAL cmd/signals daemon through scripts/singlestore-live-qual.sh
# — there is no hand-built fixture/conformance JSON here (that was the superseded
# producer-evidence anti-pattern; #583/#584 require the real daemon + exporter).
#
#   bash scripts/singlestore-dev-fixture.sh up     # start pinned image + seed
#   bash scripts/singlestore-dev-fixture.sh down   # tear down
#
# Prerequisites: docker. No cloud, no AWS, no license (the Dev Image ships a
# built-in developer license).

set -euo pipefail

# --- Pinned Dev Image (NEVER float `latest`) ---------------------------------
# singlestoredb-dev, reports @@memsql_version 9.1.1. Pinned by index digest so a
# clean machine pulls the exact same image bytes.
SS_IMAGE_REPO="ghcr.io/singlestore-labs/singlestoredb-dev"
SS_IMAGE_DIGEST="sha256:a1382fb701a4ccd1c15ad3ff26de20a896fbd57792f097f516398c1cac245910"
SS_IMAGE="${SS_IMAGE_REPO}@${SS_IMAGE_DIGEST}"
SS_EXPECTED_VERSION_PREFIX="9."

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INIT_SQL="${REPO_ROOT}/examples/singlestore-init.sql"

# A dedicated container name so the qualification NEVER touches a developer's
# own pre-existing SingleStore container (#583 design constraint).
CONTAINER="${SS_FIXTURE_CONTAINER:-ss-dev-fixture}"
ROOT_PW="dev-root-pw"
HOST_PORT="${SS_FIXTURE_PORT:-3306}"

say() { printf '>> %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

cmd_up() {
  [ -f "${INIT_SQL}" ] || die "seed not found: ${INIT_SQL}"
  say "starting pinned SingleStore Dev Image (${SS_IMAGE_DIGEST})"
  docker rm -f "${CONTAINER}" >/dev/null 2>&1 || true
  docker run -d --name "${CONTAINER}" -e ROOT_PASSWORD="${ROOT_PW}" \
    -p "${HOST_PORT}:3306" "${SS_IMAGE}" >/dev/null
  say "waiting for SingleStore to accept connections"
  local ok=""
  for _ in $(seq 1 60); do
    if docker exec "${CONTAINER}" singlestore -u root -p"${ROOT_PW}" -N -e "SELECT 1" >/dev/null 2>&1; then
      ok=1; break
    fi
    sleep 3
  done
  [ -n "${ok}" ] || { docker logs "${CONTAINER}" 2>&1 | tail -30; die "SingleStore did not become ready"; }
  local ver
  ver="$(docker exec "${CONTAINER}" singlestore -u root -p"${ROOT_PW}" -N -e "SELECT @@memsql_version" 2>/dev/null | grep -vi getopt | tr -d '[:space:]')"
  case "${ver}" in
    ${SS_EXPECTED_VERSION_PREFIX}*) say "SingleStore ready: @@memsql_version=${ver}" ;;
    *) die "unexpected SingleStore version '${ver}' (want ${SS_EXPECTED_VERSION_PREFIX}x from the pinned image)" ;;
  esac
  say "applying deterministic seed (user + grants + schema + seed + workload)"
  docker exec -i "${CONTAINER}" singlestore -u root -p"${ROOT_PW}" < "${INIT_SQL}" 2>&1 \
    | grep -v 'getopt: command not found' || true
  say "seed applied — SingleStore ${ver} ready on 127.0.0.1:${HOST_PORT}"
}

cmd_down() {
  say "tearing down ${CONTAINER}"
  docker rm -f "${CONTAINER}" >/dev/null 2>&1 || true
}

case "${1:-}" in
  up)   cmd_up ;;
  down) cmd_down ;;
  *)    die "usage: $0 {up|down}" ;;
esac
