#!/usr/bin/env bash
#
# singlestore-daemon-cadence.sh — prove the NORMAL Signals daemon repeatedly
# collects SingleStore on schedule and persists independent snapshots
# (Elevarq/Signals#555). This drives the real `cmd/signals` daemon (not a direct
# Collect() call) against the reproducible #577 environment.
#
# Scheduling semantics (verified from internal/collector/collector.go):
#   - interval mode (poll_interval, no cron): an immediate BASELINE collection at
#     t=0 (forced; bypasses R091), then one cycle per poll_interval (:784/:789);
#   - R091 min_snapshot_interval (default 60s) SKIPS a non-forced cycle within
#     the window, so poll_interval >= min_snapshot_interval;
#   - each cycle inserts a NEW ULID snapshot (no overwrite);
#   - a failed/overlapping cycle is skipped, never kills the Run loop.
#
#   bash scripts/singlestore-daemon-cadence.sh short   # fast automated cadence (default)
#   bash scripts/singlestore-daemon-cadence.sh real    # 5-minute interval, ~1 hour
#
# Self-contained: reuses scripts/singlestore-dev-fixture.sh for the pinned Dev
# Image + seed; builds the real daemon; enumerates persisted snapshots from the
# SQLite store; tears everything down.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODE="${1:-short}"
WORKDIR="$(mktemp -d)"
DB="${WORKDIR}/signals.db"
BIN="${WORKDIR}/signals"
CONFIG="${REPO_ROOT}/examples/singlestore-daemon.yaml"
DAEMON_PID=""

say() { printf '>> %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

cleanup() {
  [ -n "${DAEMON_PID}" ] && kill "${DAEMON_PID}" >/dev/null 2>&1 || true
  bash "${REPO_ROOT}/scripts/singlestore-dev-fixture.sh" down >/dev/null 2>&1 || true
  rm -rf "${WORKDIR}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Enumerate persisted snapshots: collection #, timestamp, engine, version,
# collector count (distinct query ids), id.
enumerate() {
  sqlite3 -noheader -separator '|' "${DB}" \
    "SELECT s.collected_at, s.engine, s.pg_version, s.id,
            (SELECT COUNT(DISTINCT q.query_id) FROM query_runs q WHERE q.snapshot_id = s.id)
     FROM snapshots s ORDER BY s.collected_at, s.id;"
}

run_daemon() {
  local poll="$1" min="$2" seconds="$3"
  say "starting the REAL Signals daemon (poll=${poll}, min_snapshot_interval=${min}) for ${seconds}s"
  SS_RO_PASSWORD='signals_ro_pw' \
  SIGNALS_DB_PATH="${DB}" \
  SIGNALS_POLL_INTERVAL="${poll}" \
  SIGNALS_MIN_SNAPSHOT_INTERVAL="${min}" \
  SIGNALS_API_TOKEN='ss555-daemon-cadence-token-aaaaaaaaaaaa' \
  SIGNALS_LISTEN_ADDR='127.0.0.1:8099' \
  SIGNALS_ALLOW_INSECURE_PG_TLS='true' \
    "${BIN}" --config "${CONFIG}" > "${WORKDIR}/daemon.log" 2>&1 &
  DAEMON_PID=$!
  local waited=0
  while [ "${waited}" -lt "${seconds}" ]; do
    kill -0 "${DAEMON_PID}" 2>/dev/null || { cat "${WORKDIR}/daemon.log"; die "daemon exited early (did not survive scheduling)"; }
    sleep 2; waited=$((waited + 2))
  done
  say "requesting clean shutdown (SIGTERM)"
  kill -TERM "${DAEMON_PID}" 2>/dev/null || true
  wait "${DAEMON_PID}" 2>/dev/null || true
  DAEMON_PID=""
  grep -q "collector stopping" "${WORKDIR}/daemon.log" && say "clean shutdown confirmed (collector stopping)" || say "WARN: no 'collector stopping' line"
}

say "mode=${MODE}; workdir=${WORKDIR}"
bash "${REPO_ROOT}/scripts/singlestore-dev-fixture.sh" up
( cd "${REPO_ROOT}" && go build -o "${BIN}" ./cmd/signals ) || die "build cmd/signals failed"

case "${MODE}" in
  short)
    # Short cadence: both poll and min at 2s so ticker cycles are not R091-skipped.
    run_daemon 2s 2s 16
    before="$(enumerate | wc -l | tr -d ' ')"
    say "snapshots after first run: ${before}"
    [ "${before}" -ge 3 ] || { enumerate; die "expected >=3 scheduled snapshots, got ${before}" ; }

    say "RESTART: run the daemon again against the SAME store (resume + no-overwrite)"
    run_daemon 2s 2s 8
    after="$(enumerate | wc -l | tr -d ' ')"
    say "snapshots after restart: ${after}"
    [ "${after}" -gt "${before}" ] || die "restart did not append new snapshots (resume/no-overwrite failed): ${before} -> ${after}"

    say "=== persisted snapshot sequence ==="
    enumerate | nl -ba -w2 -s'  ' | while IFS='|' read -r line; do echo "   ${line}"; done
    # Assertions: engine + version + 10 collectors on EVERY snapshot; distinct ids+timestamps.
    ( cd "${REPO_ROOT}" && python3 - "${DB}" <<'PY'
import sqlite3, sys
db = sqlite3.connect(sys.argv[1])
rows = db.execute("SELECT s.id, s.collected_at, s.engine, s.pg_version, "
                  "(SELECT COUNT(DISTINCT q.query_id) FROM query_runs q WHERE q.snapshot_id=s.id) "
                  "FROM snapshots s ORDER BY s.collected_at, s.id").fetchall()
assert len(rows) >= 4, f"expected multiple snapshots, got {len(rows)}"
ids = [r[0] for r in rows]; ts = [r[1] for r in rows]
assert len(set(ids)) == len(ids), "snapshot ids not unique (overwrite!)"
for sid, cat, eng, ver, ncol in rows:
    assert eng == "singlestore", f"snapshot {sid} engine={eng}"
    assert ver == "9.1.1", f"snapshot {sid} version={ver}"
    assert ncol == 10, f"snapshot {sid} participated collectors={ncol}, want 10"
print(f"ASSERT OK: {len(rows)} snapshots, all engine=singlestore version=9.1.1 collectors=10, ids unique, distinct timestamps={len(set(ts))}")
PY
    )
    say "SHORT CADENCE OK — daemon scheduled repeated SingleStore collections, each independent, no overwrite, resumed after restart"
    ;;

  real)
    # Real cadence qualification. EXPECTED COUNT RECORDED BEFORE RUNNING:
    #   interval mode => baseline at t=0 + one cycle per 5m.
    #   runtime 58m => 1 baseline + floor(58/5)=11 ticks = 12 snapshots
    #   (sequence t=0,5,10,...,55 — matching 12:00,12:05,...,12:55).
    EXPECTED=12
    say "EXPECTED snapshot count (recorded BEFORE run): ${EXPECTED} (baseline t=0 + 11 five-minute ticks over 58m)"
    run_daemon 5m 60s 3480   # 58 minutes
    say "=== persisted snapshot sequence ==="
    enumerate | nl -ba -w2 -s'  ' | while IFS='|' read -r line; do echo "   ${line}"; done
    actual="$(enumerate | wc -l | tr -d ' ')"
    say "ACTUAL snapshot count: ${actual} (expected ${EXPECTED}; a boundary +60m tick may yield ${EXPECTED}+1)"
    ( cd "${REPO_ROOT}" && python3 - "${DB}" "${EXPECTED}" <<'PY'
import sqlite3, sys
db = sqlite3.connect(sys.argv[1]); exp = int(sys.argv[2])
rows = db.execute("SELECT s.id, s.collected_at, s.engine, s.pg_version, "
                  "(SELECT COUNT(DISTINCT q.query_id) FROM query_runs q WHERE q.snapshot_id=s.id) "
                  "FROM snapshots s ORDER BY s.collected_at, s.id").fetchall()
ids=[r[0] for r in rows]
assert len(set(ids))==len(ids), "snapshot ids not unique (overwrite!)"
for sid,cat,eng,ver,ncol in rows:
    assert eng=="singlestore" and ver=="9.1.1" and ncol==10, f"snapshot {sid}: engine={eng} ver={ver} collectors={ncol}"
assert len(rows) in (exp, exp+1), f"expected {exp} (or {exp}+1 boundary), got {len(rows)}"
print(f"ASSERT OK: {len(rows)} independent snapshots, all engine=singlestore version=9.1.1 collectors=10, ids unique")
PY
    )
    say "REAL CADENCE OK"
    ;;
  *) die "usage: $0 {short|real}" ;;
esac
