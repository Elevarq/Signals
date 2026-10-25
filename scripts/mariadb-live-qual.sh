#!/usr/bin/env bash
#
# mariadb-live-qual.sh — the MariaDB live qualification driver
# (specifications/mariadb-live-qualification.md). The MariaDB analog of
# scripts/singlestore-live-qual.sh.
#
# Drives the NORMAL cmd/signals daemon + signalsctl against a real MariaDB and runs
# the Go verifier (//go:build mariadb_live) over the real producer artifacts:
#   cadence store — short-poll daemon, multiple cycles + a restart; every
#                   snapshot verified status='success' per collector, ids unique.
#   export store  — a FRESH store, one exact snapshot exported through the
#                   production exporter; collected==persisted==exported,
#                   redaction held, manifest complete, no foreign PG collector.
#
# Two modes:
#   LOCAL (default): brings MariaDB up via scripts/mariadb-dev-fixture.sh (docker) and
#                    tears it down on exit.
#   EXTERNAL (CI):   MARIADB_QUAL_EXTERNAL=1 — MariaDB is already up + seeded (a CI
#                    service container); this script only builds + drives + verifies.
#
#   bash scripts/mariadb-live-qual.sh all                       # local, docker
#   MARIADB_QUAL_EXTERNAL=1 bash scripts/mariadb-live-qual.sh all  # CI, service container
#
# Requires a Go toolchain (+ docker for LOCAL mode). No cloud, no AWS.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FIXTURE="${REPO_ROOT}/scripts/mariadb-dev-fixture.sh"
CONFIG="${REPO_ROOT}/examples/mariadb-daemon.yaml"
EXTERNAL="${MARIADB_QUAL_EXTERNAL:-}"
WORK="$(mktemp -d)"
CADENCE_DB="${WORK}/cadence.db"
EXPORT_DB="${WORK}/export.db"
EXPORT_ZIP="${WORK}/snapshot.zip"
BIN="${WORK}/signals"
CTL="${WORK}/signalsctl"
TOKEN="mariadb-live-qual-token-aaaaaaaaaaaaaaaaaaaa"
ADDR="127.0.0.1:8099"
DAEMON_PID=""

say() { printf '>> %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

cleanup() {
  [ -n "${DAEMON_PID}" ] && kill -TERM "${DAEMON_PID}" >/dev/null 2>&1 || true
  [ -z "${EXTERNAL}" ] && bash "${FIXTURE}" down >/dev/null 2>&1 || true
  rm -rf "${WORK}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

run_daemon() {
  local dbpath="$1" poll="$2" min="$3" seconds="$4"
  SS_RO_PASSWORD='signals_ro_pw' \
  SIGNALS_DB_PATH="${dbpath}" \
  SIGNALS_POLL_INTERVAL="${poll}" \
  SIGNALS_MIN_SNAPSHOT_INTERVAL="${min}" \
  SIGNALS_API_TOKEN="${TOKEN}" \
  SIGNALS_LISTEN_ADDR="${ADDR}" \
  SIGNALS_ALLOW_INSECURE_PG_TLS='true' \
    "${BIN}" --config "${CONFIG}" > "${WORK}/daemon.log" 2>&1 &
  DAEMON_PID=$!
  local waited=0
  while [ "${waited}" -lt "${seconds}" ]; do
    kill -0 "${DAEMON_PID}" 2>/dev/null || { cat "${WORK}/daemon.log"; die "daemon exited early"; }
    sleep 2; waited=$((waited + 2))
  done
  kill -TERM "${DAEMON_PID}" 2>/dev/null || true
  wait "${DAEMON_PID}" 2>/dev/null || true
  DAEMON_PID=""
}

wait_health() {
  local waited=0
  while [ "${waited}" -lt 60 ]; do
    if curl -fsS -H "Authorization: Bearer ${TOKEN}" "http://${ADDR}/health" >/dev/null 2>&1; then return 0; fi
    kill -0 "${DAEMON_PID}" 2>/dev/null || { cat "${WORK}/daemon.log"; die "daemon exited before healthy"; }
    sleep 1; waited=$((waited + 1))
  done
  die "daemon did not become healthy"
}

sqlite_count() { sqlite3 "$1" "SELECT COUNT(*) FROM snapshots;" 2>/dev/null || echo 0; }

say "workdir=${WORK}; external=${EXTERNAL:-no}"
[ -z "${EXTERNAL}" ] && bash "${FIXTURE}" up
( cd "${REPO_ROOT}" && go build -o "${BIN}" ./cmd/signals ) || die "build cmd/signals failed"
( cd "${REPO_ROOT}" && go build -o "${CTL}" ./cmd/signalsctl ) || die "build cmd/signalsctl failed"
say "built the real daemon + signalsctl"

# --- cadence: multiple short-poll cycles, then a restart (append) ------------
say "cadence: first short-poll run (poll=2s, min=2s, 16s)"
run_daemon "${CADENCE_DB}" 2s 2s 16
before="$(sqlite_count "${CADENCE_DB}")"
say "snapshots after first run: ${before}"
[ "${before}" -ge 3 ] || die "expected >=3 scheduled snapshots in the first run, got ${before}"
say "cadence: RESTART against the SAME store"
run_daemon "${CADENCE_DB}" 2s 2s 8

# --- export: a FRESH store, one exact snapshot, production export ------------
say "export: fresh store, one baseline collection, production export by exact snapshot id"
SS_RO_PASSWORD='signals_ro_pw' \
SIGNALS_DB_PATH="${EXPORT_DB}" \
SIGNALS_POLL_INTERVAL="5m" \
SIGNALS_MIN_SNAPSHOT_INTERVAL="1s" \
SIGNALS_API_TOKEN="${TOKEN}" \
SIGNALS_LISTEN_ADDR="${ADDR}" \
SIGNALS_ALLOW_INSECURE_PG_TLS='true' \
  "${BIN}" --config "${CONFIG}" > "${WORK}/daemon.log" 2>&1 &
DAEMON_PID=$!
wait_health
EXPORT_ULID=""
for _ in $(seq 1 30); do
  n="$(sqlite_count "${EXPORT_DB}")"
  if [ "${n}" -ge 1 ]; then
    EXPORT_ULID="$(sqlite3 "${EXPORT_DB}" "SELECT id FROM snapshots ORDER BY collected_at, id LIMIT 1;")"
    break
  fi
  kill -0 "${DAEMON_PID}" 2>/dev/null || { cat "${WORK}/daemon.log"; die "daemon exited before baseline"; }
  sleep 1
done
[ -n "${EXPORT_ULID}" ] || die "no baseline snapshot landed in the export store"
say "exact snapshot id: ${EXPORT_ULID}"
SIGNALS_API_TOKEN="${TOKEN}" "${CTL}" --api-addr "http://${ADDR}" export --snapshot-id "${EXPORT_ULID}" --output "${EXPORT_ZIP}"
kill -TERM "${DAEMON_PID}" 2>/dev/null || true
wait "${DAEMON_PID}" 2>/dev/null || true
DAEMON_PID=""
[ -s "${EXPORT_ZIP}" ] || die "export did not produce a ZIP"

# --- verify producer artifacts with the Go verifier (//go:build mariadb_live) --
say "running the Go live verifier against the real producer artifacts"
( cd "${REPO_ROOT}" &&
  MARIADB_QUAL_CADENCE_DB="${CADENCE_DB}" \
  MARIADB_QUAL_CADENCE_BEFORE="${before}" \
  MARIADB_QUAL_CADENCE_MIN="3" \
  MARIADB_QUAL_EXPORT_DB="${EXPORT_DB}" \
  MARIADB_QUAL_EXPORT_ZIP="${EXPORT_ZIP}" \
  MARIADB_QUAL_EXPORT_SNAPSHOT="${EXPORT_ULID}" \
  MARIADB_QUAL_VERSION_PREFIX="${MARIADB_QUAL_VERSION_PREFIX:-11.}" \
    go test -tags mariadb_live -count=1 -v -run 'TestMariaDBLive' ./internal/collector/ )

say "LIVE QUALIFICATION OK — real daemon + real MariaDB + production exporter, verified status=success + reconciled evidence"
