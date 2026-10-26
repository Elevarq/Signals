#!/usr/bin/env bash
#
# mysql-live-qual.sh — the MySQL live qualification driver
# (specifications/mysql-live-qualification.md). The MySQL analog of
# scripts/singlestore-live-qual.sh.
#
# Drives the NORMAL cmd/signals daemon + signalsctl against a real MySQL and runs
# the Go verifier (//go:build mysql_live) over the real producer artifacts:
#   cadence store — short-poll daemon, multiple cycles + a restart; every
#                   snapshot verified status='success' per collector, ids unique.
#   export store  — a FRESH store, one exact snapshot exported through the
#                   production exporter; collected==persisted==exported,
#                   redaction held, manifest complete, no foreign PG collector.
#
# Two modes:
#   LOCAL (default): brings MySQL up via scripts/mysql-dev-fixture.sh (docker) and
#                    tears it down on exit.
#   EXTERNAL (CI):   MYSQL_QUAL_EXTERNAL=1 — MySQL is already up + seeded (a CI
#                    service container); this script only builds + drives + verifies.
#
#   bash scripts/mysql-live-qual.sh all                       # local, docker
#   MYSQL_QUAL_EXTERNAL=1 bash scripts/mysql-live-qual.sh all  # CI, service container
#
# Requires a Go toolchain (+ docker for LOCAL mode). No cloud, no AWS.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FIXTURE="${REPO_ROOT}/scripts/mysql-dev-fixture.sh"
CONFIG="${REPO_ROOT}/examples/mysql-daemon.yaml"
EXTERNAL="${MYSQL_QUAL_EXTERNAL:-}"
WORK="$(mktemp -d)"
CADENCE_DB="${WORK}/cadence.db"
EXPORT_DB="${WORK}/export.db"
EXPORT_ZIP="${WORK}/snapshot.zip"
# #647 structural-query qualification: a second export with the opt-in ON.
# The EXPORT_ZIP above (opt-in OFF) doubles as the control.
STRUCTURAL_DB="${WORK}/structural.db"
STRUCTURAL_ZIP="${WORK}/structural.zip"
BIN="${WORK}/signals"
CTL="${WORK}/signalsctl"
TOKEN="mysql-live-qual-token-aaaaaaaaaaaaaaaaaaaa"
ADDR="127.0.0.1:8099"
DAEMON_PID=""

say() { printf '>> %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

# #530 conformance corpus: when SIGNALS_CORPUS_OUT is set, publish the exact
# live-qualified export ZIP (the one that just passed the Go verifier) as the
# producer conformance artifact Analyzer #3890 consumes, with reproducible
# provenance. No behaviour change when unset (CI is unaffected). The artifact is
# NEVER hand-edited — it is the real production-exporter output, verbatim.
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

# --- #647 structural export: a FRESH store, opt-in ON, production export ------
# The adversarial workload (examples/mysql-init.sql) has already populated the
# digest table, so the normalized DIGEST_TEXT is present to retain.
say "structural: fresh store, SIGNALS_COLLECT_NORMALIZED_QUERY_TEXT=1, production export"
SS_RO_PASSWORD='signals_ro_pw' \
SIGNALS_DB_PATH="${STRUCTURAL_DB}" \
SIGNALS_POLL_INTERVAL="5m" \
SIGNALS_MIN_SNAPSHOT_INTERVAL="1s" \
SIGNALS_API_TOKEN="${TOKEN}" \
SIGNALS_LISTEN_ADDR="${ADDR}" \
SIGNALS_ALLOW_INSECURE_PG_TLS='true' \
SIGNALS_COLLECT_NORMALIZED_QUERY_TEXT='1' \
  "${BIN}" --config "${CONFIG}" > "${WORK}/daemon.log" 2>&1 &
DAEMON_PID=$!
wait_health
STRUCTURAL_ULID=""
for _ in $(seq 1 30); do
  n="$(sqlite_count "${STRUCTURAL_DB}")"
  if [ "${n}" -ge 1 ]; then
    STRUCTURAL_ULID="$(sqlite3 "${STRUCTURAL_DB}" "SELECT id FROM snapshots ORDER BY collected_at, id LIMIT 1;")"
    break
  fi
  kill -0 "${DAEMON_PID}" 2>/dev/null || { cat "${WORK}/daemon.log"; die "daemon exited before structural baseline"; }
  sleep 1
done
[ -n "${STRUCTURAL_ULID}" ] || die "no structural snapshot landed"
SIGNALS_API_TOKEN="${TOKEN}" "${CTL}" --api-addr "http://${ADDR}" export --snapshot-id "${STRUCTURAL_ULID}" --output "${STRUCTURAL_ZIP}"
kill -TERM "${DAEMON_PID}" 2>/dev/null || true
wait "${DAEMON_PID}" 2>/dev/null || true
DAEMON_PID=""
[ -s "${STRUCTURAL_ZIP}" ] || die "structural export did not produce a ZIP"

# --- verify producer artifacts with the Go verifier (//go:build mysql_live) --
say "running the Go live verifier against the real producer artifacts"
( cd "${REPO_ROOT}" &&
  MYSQL_QUAL_CADENCE_DB="${CADENCE_DB}" \
  MYSQL_QUAL_CADENCE_BEFORE="${before}" \
  MYSQL_QUAL_CADENCE_MIN="3" \
  MYSQL_QUAL_EXPORT_DB="${EXPORT_DB}" \
  MYSQL_QUAL_EXPORT_ZIP="${EXPORT_ZIP}" \
  MYSQL_QUAL_EXPORT_SNAPSHOT="${EXPORT_ULID}" \
  MYSQL_QUAL_VERSION_PREFIX="${MYSQL_QUAL_VERSION_PREFIX:-8.}" \
    go test -tags mysql_live -count=1 -v -run 'TestMySQLLive' ./internal/collector/ )

# --- #647 structural-query safety qualification (INV-SQT-01, per engine line) -
say "running the structural-query qualification (opt-in ON vs control)"
( cd "${REPO_ROOT}" &&
  STRUCTURAL_QUAL_ENGINE="mysql" \
  STRUCTURAL_QUAL_STRUCTURAL_ZIP="${STRUCTURAL_ZIP}" \
  STRUCTURAL_QUAL_CONTROL_ZIP="${EXPORT_ZIP}" \
    go test -tags mysql_live -count=1 -v -run 'TestLiveStructuralQueryText' ./internal/collector/ )

[ -n "${SIGNALS_CORPUS_OUT:-}" ] && emit_conformance_artifact "mysql" "${EXPORT_ZIP}"

say "LIVE QUALIFICATION OK — real daemon + real MySQL + production exporter, verified status=success + reconciled evidence + structural-query INV-SQT-01"
