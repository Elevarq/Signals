#!/usr/bin/env bash
#
# mysql-dev-fixture.sh — the reproducible, version-controlled MySQL test
# environment for the MySQL live qualification.
#
# Brings up a REAL MySQL server: the PINNED official MySQL Dev Image (by digest,
# never a float tag) seeded by examples/mysql-init.sql (read-only signals_ro with
# SELECT,PROCESS per specifications/mysql-grants.md + deterministic schema +
# workload). Environment bring-up ONLY (up/down); collection + export
# qualification is driven by the NORMAL cmd/signals daemon via
# scripts/mysql-live-qual.sh.
#
#   bash scripts/mysql-dev-fixture.sh up     # start pinned image + seed
#   bash scripts/mysql-dev-fixture.sh down   # tear down
#
# Prerequisites: docker. No cloud, no AWS.

set -euo pipefail

# --- Pinned Dev Image (NEVER float a tag) ------------------------------------
# mysql:8.4; pinned by digest so a clean machine pulls the exact same bytes.
MYSQL_IMAGE="mysql@sha256:76f0358884cb92d9a086d24e5671c38048d3fef40f2588703d73eb55392d6b69"
EXPECTED_VERSION_PREFIX="8."

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INIT_SQL="${REPO_ROOT}/examples/mysql-init.sql"

# Dedicated container name so a developer's own MySQL container is never touched.
CONTAINER="${MYSQL_FIXTURE_CONTAINER:-mysql-dev-fixture}"
ROOT_PW="dev-root-pw"
HOST_PORT="${MYSQL_FIXTURE_PORT:-3306}"

say() { printf '>> %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

cmd_up() {
  [ -f "${INIT_SQL}" ] || die "seed not found: ${INIT_SQL}"
  # #588: never silently remove a container this script did not create. If the
  # dedicated name is taken, refuse and tell the operator to run `down` — we only
  # ever delete it from cmd_down (explicit teardown).
  if [ -n "$(docker ps -aq -f "name=^${CONTAINER}$" 2>/dev/null)" ]; then
    die "container ${CONTAINER} already exists — run '$0 down' first (refusing to remove a container this script did not create)"
  fi
  say "starting pinned MySQL Dev Image"
  docker run -d --name "${CONTAINER}" -e MYSQL_ROOT_PASSWORD="${ROOT_PW}" \
    -p "${HOST_PORT}:3306" "${MYSQL_IMAGE}" >/dev/null
  say "waiting for MySQL to accept connections"
  local ok=""
  for _ in $(seq 1 60); do
    if docker exec "${CONTAINER}" mysqladmin ping -uroot -p"${ROOT_PW}" --silent >/dev/null 2>&1; then
      ok=1; break
    fi
    sleep 2
  done
  [ -n "${ok}" ] || { docker logs "${CONTAINER}" 2>&1 | tail -30; die "MySQL did not become ready"; }
  local ver
  ver="$(docker exec "${CONTAINER}" mysql -uroot -p"${ROOT_PW}" -N -B -e "SELECT VERSION()" 2>/dev/null | tr -d '[:space:]')"
  case "${ver}" in
    ${EXPECTED_VERSION_PREFIX}*) say "MySQL ready: VERSION()=${ver}" ;;
    *) die "unexpected MySQL version '${ver}' (want ${EXPECTED_VERSION_PREFIX}x from the pinned image)" ;;
  esac
  say "applying deterministic seed (user + grants + schema + seed + workload)"
  # #588: fail closed. The benign "Using a password on the command line" warning
  # is filtered from stderr via process substitution so the mysql client's exit
  # status still propagates — a failed grant/seed stops cmd_up (no false
  # "seed applied"), it is no longer masked by a pipe + `|| true`.
  if ! docker exec -i "${CONTAINER}" mysql -uroot -p"${ROOT_PW}" < "${INIT_SQL}" \
      2> >(grep -v 'Using a password on the command line' >&2); then
    die "seed failed — required setup SQL did not apply cleanly (see errors above)"
  fi
  say "seed applied — MySQL ${ver} ready on 127.0.0.1:${HOST_PORT}"
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
