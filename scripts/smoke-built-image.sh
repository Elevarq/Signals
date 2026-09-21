#!/usr/bin/env bash
#
# Post-build smoke against the BUILT container image (Release Protocol
# Gate A step 4: "smoke the built artifact, not only source").
#
# Umbrella: Elevarq/elevarq-website#529 (green CI != working deploy).
# Child:    Elevarq/Signals#421 (Control 3 — post-deploy smoke vs the
#           live built artifact). Spec: specifications/built-image-smoke.md.
#
# What this proves that source-level `go test` cannot: the actual image
# — its entrypoint, baked binaries, non-root user, /data volume perms and
# runtime config wiring — can connect to a real PostgreSQL, run a real
# collection cycle end to end, and produce a non-empty, well-formed
# snapshot ZIP. A broken Dockerfile/entrypoint/permission that every
# green source test misses fails HERE, before publish.
#
# Self-contained: no cloud, no AWS. Spins an ephemeral PostgreSQL with the
# repo's representative seed (examples/init.sql), runs the image against it,
# and asserts a snapshot is produced. Everything is torn down on exit.
#
# Usage:
#   SIGNALS_SMOKE_IMAGE=signals:smoke bash scripts/smoke-built-image.sh
#   bash scripts/smoke-built-image.sh signals:smoke
#
# The caller (CI/release) is responsible for building/loading the image
# and passing its reference; this script never builds the image itself, so
# it smokes exactly the artifact under test.

set -euo pipefail

IMAGE="${1:-${SIGNALS_SMOKE_IMAGE:-}}"
if [ -z "${IMAGE}" ]; then
  echo "smoke: no image reference given (set SIGNALS_SMOKE_IMAGE or pass as \$1)" >&2
  exit 2
fi

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
INIT_SQL="${REPO_ROOT}/examples/init.sql"
if [ ! -f "${INIT_SQL}" ]; then
  echo "smoke: representative seed not found at ${INIT_SQL}" >&2
  exit 2
fi

# Unique names so parallel/repeated runs never collide.
SUFFIX="$$"
NET="signals-smoke-net-${SUFFIX}"
PG="signals-smoke-pg-${SUFFIX}"
APP="signals-smoke-app-${SUFFIX}"
WORKDIR="$(mktemp -d)"

# Dev-only placeholder token: 39 chars, passes the #135 strength validator
# under SIGNALS_ENV=dev; obviously not a real secret.
TOKEN="dev-local-only-replace-in-prod-32chars"
PG_PASSWORD="monitor_pass"   # matches the `signals` role in examples/init.sql

cleanup() {
  local ec=$?
  echo "----- signals container logs (tail) -----"
  docker logs "${APP}" 2>&1 | tail -40 || true
  docker rm -f "${APP}" "${PG}" >/dev/null 2>&1 || true
  docker network rm "${NET}" >/dev/null 2>&1 || true
  rm -rf "${WORKDIR}" || true
  exit "${ec}"
}
trap cleanup EXIT

echo "smoke: image under test = ${IMAGE}"
docker network create "${NET}" >/dev/null

# --- Ephemeral PostgreSQL with the representative seed --------------------
# pg_stat_statements is preloaded so init.sql's CREATE EXTENSION works and
# the pgss collectors have a view to read (mirrors examples/docker-compose.yml).
echo "smoke: starting PostgreSQL"
docker run -d --name "${PG}" --network "${NET}" \
  -e POSTGRES_PASSWORD=postgres_pass \
  -e POSTGRES_DB=postgres \
  -v "${INIT_SQL}:/docker-entrypoint-initdb.d/init.sql:ro" \
  postgres:16-alpine \
  -c shared_preload_libraries=pg_stat_statements >/dev/null

echo "smoke: waiting for PostgreSQL to accept connections"
for i in $(seq 1 60); do
  if docker exec "${PG}" pg_isready -U postgres >/dev/null 2>&1; then
    break
  fi
  [ "${i}" -eq 60 ] && { echo "smoke: PostgreSQL never became ready" >&2; exit 1; }
  sleep 2
done
# init.sql runs asynchronously on first boot; wait until the monitoring role
# it creates actually exists before pointing the collector at it.
for i in $(seq 1 30); do
  if docker exec "${PG}" psql -U postgres -tAc \
       "SELECT 1 FROM pg_roles WHERE rolname='signals'" 2>/dev/null | grep -q 1; then
    break
  fi
  [ "${i}" -eq 30 ] && { echo "smoke: seed role 'signals' never appeared" >&2; exit 1; }
  sleep 2
done

# --- The built image, pointed at that PostgreSQL -------------------------
echo "smoke: starting Signals from the built image"
docker run -d --name "${APP}" --network "${NET}" \
  -e SIGNALS_ENV=dev \
  -e SIGNALS_TARGET_HOST="${PG}" \
  -e SIGNALS_TARGET_PORT=5432 \
  -e SIGNALS_TARGET_USER=signals \
  -e SIGNALS_TARGET_DBNAME=postgres \
  -e SIGNALS_TARGET_PASSWORD_ENV=PG_PASSWORD \
  -e PG_PASSWORD="${PG_PASSWORD}" \
  -e SIGNALS_API_TOKEN="${TOKEN}" \
  -e SIGNALS_LISTEN_ADDR=0.0.0.0:8081 \
  -e SIGNALS_ALLOW_INSECURE_PG_TLS=true \
  -e SIGNALS_POLL_INTERVAL=1m \
  "${IMAGE}" >/dev/null

echo "smoke: waiting for the collector API /health"
for i in $(seq 1 60); do
  if docker exec "${APP}" wget -qO /dev/null http://127.0.0.1:8081/health 2>/dev/null; then
    break
  fi
  if [ "$(docker inspect -f '{{.State.Running}}' "${APP}" 2>/dev/null)" != "true" ]; then
    echo "smoke: container exited before becoming healthy" >&2; exit 1
  fi
  [ "${i}" -eq 60 ] && { echo "smoke: /health never became ready" >&2; exit 1; }
  sleep 2
done

# --- Exercise the real collect -> export path ----------------------------
echo "smoke: forcing a collection cycle"
docker exec -e SIGNALS_API_TOKEN="${TOKEN}" "${APP}" signalsctl collect now --force

echo "smoke: collector status"
docker exec -e SIGNALS_API_TOKEN="${TOKEN}" "${APP}" signalsctl status

echo "smoke: exporting a snapshot"
docker exec -e SIGNALS_API_TOKEN="${TOKEN}" "${APP}" \
  signalsctl export --output /data/snapshot.zip

# --- Assert a non-empty, well-formed snapshot ----------------------------
docker exec "${APP}" sh -c 'test -s /data/snapshot.zip' \
  || { echo "smoke: snapshot.zip is missing or empty" >&2; exit 1; }

docker cp "${APP}:/data/snapshot.zip" "${WORKDIR}/snapshot.zip" >/dev/null
if command -v unzip >/dev/null 2>&1; then
  LISTING="$(unzip -l "${WORKDIR}/snapshot.zip")"
  echo "${LISTING}"
  echo "${LISTING}" | grep -q 'metadata.json' \
    || { echo "smoke: snapshot missing metadata.json" >&2; exit 1; }
  # R006/R125: every emitted ZIP carries a collector_status.json payload; its
  # presence is what distinguishes a real run from a false-clean export.
  echo "${LISTING}" | grep -q 'collector_status.json' \
    || { echo "smoke: snapshot missing collector_status.json" >&2; exit 1; }
else
  echo "smoke: unzip unavailable; asserted non-empty snapshot only"
fi

echo "smoke: PASSED — built image collected against real PostgreSQL and produced a snapshot"
