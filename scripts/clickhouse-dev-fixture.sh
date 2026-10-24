#!/usr/bin/env bash
#
# clickhouse-dev-fixture.sh — the reproducible, version-controlled ClickHouse
# test environment for the ClickHouse live qualification (CH-3). Fail-closed from
# the start (#588).
#
# Brings up a REAL ClickHouse server: the PINNED official image (by digest) seeded
# by examples/clickhouse-init.sql (read-only signals_ro with GRANT SELECT ON
# system.* + a workload + SYSTEM FLUSH LOGS so system.query_log exists).
# Environment bring-up ONLY (up/down); collection + export qualification is driven
# by the NORMAL cmd/signals daemon via scripts/clickhouse-live-qual.sh.
#
#   bash scripts/clickhouse-dev-fixture.sh up     # start pinned image + seed
#   bash scripts/clickhouse-dev-fixture.sh down   # tear down
#
# Prerequisites: docker. No cloud, no AWS.

set -euo pipefail

# --- Pinned image (NEVER float a tag) ----------------------------------------
# clickhouse/clickhouse-server:24.8; pinned by digest.
CH_IMAGE="clickhouse/clickhouse-server@sha256:1ffa82edee000a42c09313bd9f1293d94c570aee74babc1b3ca9983a35fa597b"
EXPECTED_VERSION_PREFIX="24."

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INIT_SQL="${REPO_ROOT}/examples/clickhouse-init.sql"

# Dedicated container name so a developer's own ClickHouse container is never touched.
CONTAINER="${CH_FIXTURE_CONTAINER:-clickhouse-dev-fixture}"
HOST_PORT="${CH_FIXTURE_PORT:-9000}"

say() { printf '>> %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

ch() { docker exec -i "${CONTAINER}" clickhouse-client "$@"; }

cmd_up() {
  [ -f "${INIT_SQL}" ] || die "seed not found: ${INIT_SQL}"
  # #588: never silently remove a container this script did not create.
  if [ -n "$(docker ps -aq -f "name=^${CONTAINER}$" 2>/dev/null)" ]; then
    die "container ${CONTAINER} already exists — run '$0 down' first (refusing to remove a container this script did not create)"
  fi
  say "starting pinned ClickHouse image"
  docker run -d --name "${CONTAINER}" --ulimit nofile=262144:262144 \
    -p "${HOST_PORT}:9000" "${CH_IMAGE}" >/dev/null
  say "waiting for ClickHouse to accept connections"
  local ok=""
  for _ in $(seq 1 60); do
    if docker exec "${CONTAINER}" clickhouse-client --query "SELECT 1" >/dev/null 2>&1; then
      ok=1; break
    fi
    sleep 2
  done
  [ -n "${ok}" ] || { docker logs "${CONTAINER}" 2>&1 | tail -30; die "ClickHouse did not become ready"; }
  local ver
  ver="$(docker exec "${CONTAINER}" clickhouse-client --query "SELECT version()" 2>/dev/null | tr -d '[:space:]')"
  case "${ver}" in
    ${EXPECTED_VERSION_PREFIX}*) say "ClickHouse ready: version()=${ver}" ;;
    *) die "unexpected ClickHouse version '${ver}' (want ${EXPECTED_VERSION_PREFIX}x from the pinned image)" ;;
  esac
  say "applying deterministic seed (user + grants + workload + flush logs)"
  # #588: fail closed — the client propagates a non-zero exit on any SQL error.
  if ! ch --multiquery < "${INIT_SQL}"; then
    die "seed failed — required setup SQL did not apply cleanly (see errors above)"
  fi
  say "seed applied — ClickHouse ${ver} ready on 127.0.0.1:${HOST_PORT}"
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
