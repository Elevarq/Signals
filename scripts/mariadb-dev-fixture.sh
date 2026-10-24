#!/usr/bin/env bash
#
# mariadb-dev-fixture.sh — the reproducible, version-controlled MariaDB test
# environment for the MariaDB live qualification. Mirrors mariadb-dev-fixture.sh,
# written fail-closed from the start (#588).
#
# Brings up a REAL MariaDB server: the PINNED official MariaDB image (by digest,
# never a float tag) seeded by examples/mariadb-init.sql (read-only signals_ro
# with SELECT,PROCESS per specifications/mariadb-grants.md + deterministic
# schema + workload). Environment bring-up ONLY (up/down); collection + export
# qualification is driven by the NORMAL cmd/signals daemon via
# scripts/mariadb-live-qual.sh.
#
#   bash scripts/mariadb-dev-fixture.sh up     # start pinned image + seed
#   bash scripts/mariadb-dev-fixture.sh down   # tear down
#
# Prerequisites: docker. No cloud, no AWS.

set -euo pipefail

# --- Pinned Dev Image (NEVER float a tag) ------------------------------------
# mariadb:11.4; pinned by digest so a clean machine pulls the exact same bytes.
MARIADB_IMAGE="mariadb@sha256:250e1609e93c0e6aecf64bfbab2d84d5e89d2481540fe77447ff6c855c3b1873"
EXPECTED_VERSION_PREFIX="11."

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INIT_SQL="${REPO_ROOT}/examples/mariadb-init.sql"

# Dedicated container name so a developer's own MariaDB container is never touched.
CONTAINER="${MARIADB_FIXTURE_CONTAINER:-mariadb-dev-fixture}"
ROOT_PW="dev-root-pw"
HOST_PORT="${MARIADB_FIXTURE_PORT:-3306}"

say() { printf '>> %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

cmd_up() {
  [ -f "${INIT_SQL}" ] || die "seed not found: ${INIT_SQL}"
  # #588: never silently remove a container this script did not create.
  if [ -n "$(docker ps -aq -f "name=^${CONTAINER}$" 2>/dev/null)" ]; then
    die "container ${CONTAINER} already exists — run '$0 down' first (refusing to remove a container this script did not create)"
  fi
  say "starting pinned MariaDB Dev Image"
  docker run -d --name "${CONTAINER}" -e MARIADB_ROOT_PASSWORD="${ROOT_PW}" \
    -p "${HOST_PORT}:3306" "${MARIADB_IMAGE}" >/dev/null
  say "waiting for MariaDB to accept connections"
  local ok=""
  for _ in $(seq 1 60); do
    if docker exec "${CONTAINER}" mariadb-admin ping -uroot -p"${ROOT_PW}" --silent >/dev/null 2>&1; then
      ok=1; break
    fi
    sleep 2
  done
  [ -n "${ok}" ] || { docker logs "${CONTAINER}" 2>&1 | tail -30; die "MariaDB did not become ready"; }
  local ver
  ver="$(docker exec "${CONTAINER}" mariadb -uroot -p"${ROOT_PW}" -N -B -e "SELECT VERSION()" 2>/dev/null | tr -d '[:space:]')"
  case "${ver}" in
    ${EXPECTED_VERSION_PREFIX}*) say "MariaDB ready: VERSION()=${ver}" ;;
    *) die "unexpected MariaDB version '${ver}' (want ${EXPECTED_VERSION_PREFIX}x from the pinned image)" ;;
  esac
  say "applying deterministic seed (user + grants + schema + seed + workload)"
  # #588: fail closed — filter the benign password warning from stderr via
  # process substitution so the client's exit status still propagates.
  if ! docker exec -i "${CONTAINER}" mariadb -uroot -p"${ROOT_PW}" < "${INIT_SQL}" \
      2> >(grep -v 'Using a password on the command line' >&2); then
    die "seed failed — required setup SQL did not apply cleanly (see errors above)"
  fi
  say "seed applied — MariaDB ${ver} ready on 127.0.0.1:${HOST_PORT}"
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
