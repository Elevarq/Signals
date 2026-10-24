#!/usr/bin/env bash
#
# singlestore-live-qual.sh — the M4 live qualification driver
# (Elevarq/Signals#583, #584; specifications/singlestore-live-qualification.md).
#
# Drives the NORMAL cmd/signals daemon + signalsctl against the pinned
# SingleStore Dev Image (scripts/singlestore-dev-fixture.sh) and then runs the
# Go verifier (//go:build singlestore_live) against the real producer artifacts:
#
#   #583 cadence store   — short-poll daemon, multiple cycles + a restart, so
#                          every snapshot is verified status='success' per
#                          collector with reconciled result evidence, ids
#                          unique, restart appends (no overwrite).
#   #584 export store    — a FRESH store, one forced collection, exported
#                          through the production exporter; collected ==
#                          persisted == exported, redaction held, manifest
#                          complete, no foreign PostgreSQL collector.
#
# This is the ONLY source of SingleStore producer evidence. It runs in two modes
# (SLQ-R010), mirroring the MySQL/MariaDB/ClickHouse live-qual drivers:
#   LOCAL (default): brings the pinned Dev Image up via
#                    scripts/singlestore-dev-fixture.sh (docker) and tears it
#                    down on exit — never touches a developer's own SingleStore
#                    container or store (INV-SLQ-02).
#   EXTERNAL (CI):   SINGLESTORE_QUAL_EXTERNAL=1 — the Dev Image is already up and
#                    seeded (the integration-singlestore CI service container), so
#                    this script only builds, drives the real daemon, and verifies.
#
#   bash scripts/singlestore-live-qual.sh all                            # local, docker
#   SINGLESTORE_QUAL_EXTERNAL=1 bash scripts/singlestore-live-qual.sh all # CI, service container
#
# Requires a Go toolchain (+ docker for LOCAL mode). No cloud, no AWS, no license.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FIXTURE="${REPO_ROOT}/scripts/singlestore-dev-fixture.sh"
CONFIG="${REPO_ROOT}/examples/singlestore-daemon.yaml"
EXTERNAL="${SINGLESTORE_QUAL_EXTERNAL:-}"
WORK="$(mktemp -d)"
CADENCE_DB="${WORK}/cadence.db"
EXPORT_DB="${WORK}/export.db"
EXPORT_ZIP="${WORK}/snapshot.zip"
BIN="${WORK}/signals"
CTL="${WORK}/signalsctl"
TOKEN="ss-live-qual-token-aaaaaaaaaaaaaaaaaaaaaa"
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

# run_daemon <db> <poll> <min> <seconds> — start the real daemon against the live
# SingleStore for <seconds>, then stop it cleanly.
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

say "workdir=${WORK}; external=${EXTERNAL:-no}"
[ -z "${EXTERNAL}" ] && bash "${FIXTURE}" up
( cd "${REPO_ROOT}" && go build -o "${BIN}" ./cmd/signals ) || die "build cmd/signals failed"
( cd "${REPO_ROOT}" && go build -o "${CTL}" ./cmd/signalsctl ) || die "build cmd/signalsctl failed"
say "built the real daemon + signalsctl"

# --- #583 cadence: multiple short-poll cycles, then a restart (append) --------
sqlite_count() { sqlite3 "$1" "SELECT COUNT(*) FROM snapshots;" 2>/dev/null || echo 0; }

say "cadence: first short-poll run (poll=2s, min=2s, 16s)"
run_daemon "${CADENCE_DB}" 2s 2s 16
before="$(sqlite_count "${CADENCE_DB}")"
say "snapshots after first run: ${before}"
[ "${before}" -ge 3 ] || die "expected >=3 scheduled snapshots in the first run, got ${before}"
say "cadence: RESTART against the SAME store (resume + no-overwrite)"
run_daemon "${CADENCE_DB}" 2s 2s 8

# --- #584 export: a FRESH store, ONE exact snapshot, production export --------
# A long poll (5m) so only the t=0 baseline collection lands within the window:
# the store holds exactly ONE snapshot, whose exact ULID we export by id (#584
# "one exact snapshot"). Exporting default-scope here would be latest-per-
# collector and could mix runs across cycles for live system views (e.g.
# MV_ACTIVITIES), so the export MUST be pinned to the exact snapshot id.
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
# Wait for the baseline snapshot to land (exactly one), then capture its ULID.
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

# --- Verify producer artifacts with the Go verifier (//go:build singlestore_live)
say "running the Go live verifier against the real producer artifacts"
( cd "${REPO_ROOT}" &&
  SINGLESTORE_QUAL_CADENCE_DB="${CADENCE_DB}" \
  SINGLESTORE_QUAL_CADENCE_BEFORE="${before}" \
  SINGLESTORE_QUAL_CADENCE_MIN="3" \
  SINGLESTORE_QUAL_EXPORT_DB="${EXPORT_DB}" \
  SINGLESTORE_QUAL_EXPORT_ZIP="${EXPORT_ZIP}" \
  SINGLESTORE_QUAL_EXPORT_SNAPSHOT="${EXPORT_ULID}" \
    go test -tags singlestore_live -count=1 -v \
      -run 'TestSingleStoreLive' ./internal/collector/ )

say "LIVE QUALIFICATION OK — real daemon + real SingleStore + production exporter, verified status=success + reconciled evidence"
