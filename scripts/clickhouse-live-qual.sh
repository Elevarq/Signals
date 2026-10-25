#!/usr/bin/env bash
#
# clickhouse-live-qual.sh — the ClickHouse live qualification driver
# (specifications/clickhouse-live-qualification.md). The ClickHouse analog of
# scripts/singlestore-live-qual.sh.
#
# Drives the NORMAL cmd/signals daemon + signalsctl against a real ClickHouse and runs
# the Go verifier (//go:build clickhouse_live) over the real producer artifacts:
#   cadence store — short-poll daemon, multiple cycles + a restart; every
#                   snapshot verified status='success' per collector, ids unique.
#   export store  — a FRESH store, one exact snapshot exported through the
#                   production exporter; collected==persisted==exported,
#                   redaction held, manifest complete, no foreign PG collector.
#
# Two modes:
#   LOCAL (default): brings ClickHouse up via scripts/clickhouse-dev-fixture.sh (docker) and
#                    tears it down on exit.
#   EXTERNAL (CI):   CH_QUAL_EXTERNAL=1 — ClickHouse is already up + seeded (a CI
#                    service container); this script only builds + drives + verifies.
#
#   bash scripts/clickhouse-live-qual.sh all                       # local, docker
#   CH_QUAL_EXTERNAL=1 bash scripts/clickhouse-live-qual.sh all  # CI, service container
#
# Requires a Go toolchain (+ docker for LOCAL mode). No cloud, no AWS.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FIXTURE="${REPO_ROOT}/scripts/clickhouse-dev-fixture.sh"
CONFIG="${REPO_ROOT}/examples/clickhouse-daemon.yaml"
EXTERNAL="${CH_QUAL_EXTERNAL:-}"
WORK="$(mktemp -d)"
CADENCE_DB="${WORK}/cadence.db"
EXPORT_DB="${WORK}/export.db"
EXPORT_ZIP="${WORK}/snapshot.zip"
BIN="${WORK}/signals"
CTL="${WORK}/signalsctl"
TOKEN="clickhouse-live-qual-token-aaaaaaaaaaaaaaaaaaaa"
ADDR="127.0.0.1:8099"
DAEMON_PID=""

say() { printf '>> %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

# #530 conformance corpus: when SIGNALS_CORPUS_OUT is set, publish the exact
# live-qualified export ZIP (the one that just passed the Go verifier) as the
# producer conformance artifact Analyzer #3890 consumes, with reproducible
# provenance. No behaviour change when unset. Never hand-edited — verbatim
# production-exporter output.
emit_conformance_artifact() {
  local engine="$1" zip="$2" out="${SIGNALS_CORPUS_OUT}"
  mkdir -p "${out}"
  local dest="${out}/${engine}-signals.zip"
  cp "${zip}" "${dest}"
  local sha="" commit="" ever=""
  if command -v sha256sum >/dev/null 2>&1; then sha="$(sha256sum "${dest}" | awk '{print $1}')";
  else sha="$(shasum -a 256 "${dest}" | awk '{print $1}')"; fi
  commit="$(git -C "${REPO_ROOT}" rev-parse HEAD 2>/dev/null || echo unknown)"
  ever="$(unzip -p "${dest}" metadata.json 2>/dev/null | python3 -c 'import sys,json; print(json.load(sys.stdin).get("engine_version",""))' 2>/dev/null || true)"
  cat > "${out}/${engine}-signals.provenance.json" <<EOF
{
  "engine": "${engine}",
  "engine_version": "${ever}",
  "signals_commit": "${commit}",
  "artifact": "${engine}-signals.zip",
  "artifact_sha256": "${sha}",
  "generation_mechanism": "scripts/${engine}-live-qual.sh — real cmd/signals daemon -> production exporter (signalsctl export), captured after the live Go verifier passed; never hand-edited",
  "fixture": "examples/${engine}-init.sql + examples/${engine}-daemon.yaml",
  "source_image": "${SIGNALS_CORPUS_IMAGE:-pinned ${engine} dev fixture (see scripts/${engine}-dev-fixture.sh)}"
}
EOF
  say "CONFORMANCE ARTIFACT emitted: ${dest} (engine_version=${ever}, sha256=${sha})"
}

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

# --- verify producer artifacts with the Go verifier (//go:build clickhouse_live) --
say "running the Go live verifier against the real producer artifacts"
( cd "${REPO_ROOT}" &&
  CH_QUAL_CADENCE_DB="${CADENCE_DB}" \
  CH_QUAL_CADENCE_BEFORE="${before}" \
  CH_QUAL_CADENCE_MIN="3" \
  CH_QUAL_EXPORT_DB="${EXPORT_DB}" \
  CH_QUAL_EXPORT_ZIP="${EXPORT_ZIP}" \
  CH_QUAL_EXPORT_SNAPSHOT="${EXPORT_ULID}" \
    go test -tags clickhouse_live -count=1 -v -run 'TestClickHouseLive' ./internal/collector/ )

[ -n "${SIGNALS_CORPUS_OUT:-}" ] && emit_conformance_artifact "clickhouse" "${EXPORT_ZIP}"

say "LIVE QUALIFICATION OK — real daemon + real ClickHouse + production exporter, verified status=success + reconciled evidence"
