#!/usr/bin/env bash
#
# singlestore-dev-fixture.sh — the reproducible, version-controlled SingleStore
# Signals test environment (Elevarq/Signals#577).
#
# Everything needed to reproduce the SingleStore collector fixture lives in the
# repository: the PINNED official SingleStore Dev Image (by digest — never
# `latest`), the deterministic seed (examples/singlestore-init.sql: read-only
# signals_ro user + exact grants per specifications/singlestore-grants.md +
# schema + seed + a small workload that exercises the Signals collectors), the
# NORMAL Signals collector invocation, deterministic redaction, and cleanup.
# No interactive /tmp SQL, no hand-trimming, no developer-workspace dependence.
#
# Workflow:  up -> capture -> verify -> down   (or `all`)
#
#   bash scripts/singlestore-dev-fixture.sh all        # full cycle, auto-teardown
#   bash scripts/singlestore-dev-fixture.sh up         # start + seed only
#   bash scripts/singlestore-dev-fixture.sh capture    # regenerate the fixture
#   bash scripts/singlestore-dev-fixture.sh verify      # unit replay + fidelity
#   bash scripts/singlestore-dev-fixture.sh down        # tear down
#
# The workload exercises the Signals collectors ONLY; it is NOT shaped to
# trigger any Analyzer rule/finding (Analyzer is downstream, out of scope).
#
# Prerequisites: docker + a working Go toolchain. No cloud, no AWS, no license
# (the Dev Image ships a built-in developer license).

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
FIXTURE="${REPO_ROOT}/internal/singlestore/testdata/singlestore_collect_fixture.json"
PROVENANCE="${REPO_ROOT}/internal/singlestore/testdata/singlestore_collect_fixture.provenance.json"

CONTAINER="ss-dev-fixture"
ROOT_PW="dev-root-pw"
RO_USER="signals_ro"
RO_PW="signals_ro_pw"   # matches examples/singlestore-init.sql
HOST_PORT="3306"
DSN="${RO_USER}:${RO_PW}@tcp(127.0.0.1:${HOST_PORT})/?tls=false&interpolateParams=false"

say() { printf '>> %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

# Run SQL inside the container as root (the client's getopt warning is benign).
ss_root() { docker exec -i "${CONTAINER}" singlestore -u root -p"${ROOT_PW}" "$@" 2>&1 | grep -v 'getopt: command not found' || true; }

cmd_up() {
  [ -f "${INIT_SQL}" ] || die "seed not found: ${INIT_SQL}"
  say "pulling + starting pinned SingleStore Dev Image (${SS_IMAGE_DIGEST})"
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
  ss_root < "${INIT_SQL}" >/dev/null
  say "seed applied"
}

cmd_capture() {
  say "regenerating fixture from the NORMAL Signals collector path (redacted)"
  ( cd "${REPO_ROOT}" && SINGLESTORE_TEST_DSN="${DSN}" SS_WRITE_FIXTURE="${FIXTURE}" \
      go test -tags singlestore_integration -run TestCollect_Live -count=1 ./internal/singlestore/ )
  [ -s "${FIXTURE}" ] || die "fixture was not written: ${FIXTURE}"

  # Provenance receipt: trace the fixture to the SingleStore version, the pinned
  # image digest, the Signals commit, and the capture procedure.
  local ver commit
  ver="$(docker exec "${CONTAINER}" singlestore -u ${RO_USER} -p"${RO_PW}" -N -e "SELECT @@memsql_version" 2>/dev/null | grep -vi getopt | tr -d '[:space:]')"
  commit="$(cd "${REPO_ROOT}" && git rev-parse HEAD 2>/dev/null || echo unknown)"
  cat > "${PROVENANCE}" <<JSON
{
  "fixture": "internal/singlestore/testdata/singlestore_collect_fixture.json",
  "singlestore_version": "${ver}",
  "image_ref": "${SS_IMAGE}",
  "image_digest": "${SS_IMAGE_DIGEST}",
  "signals_commit": "${commit}",
  "capture_procedure": "bash scripts/singlestore-dev-fixture.sh all",
  "redacted": true,
  "note": "Regenerate with the capture procedure; non-deterministic rows (timestamps, latencies, connection/process state) may differ, collector set + columns + identity version are stable."
}
JSON
  say "fixture + provenance written (SingleStore ${ver})"
}

cmd_verify() {
  say "unit replay + collector tests (no live DB needed)"
  ( cd "${REPO_ROOT}" && go test -count=1 ./internal/singlestore/... )
  say "fidelity: fixture identity version matches the live capture, redaction holds"
  ( cd "${REPO_ROOT}" && python3 - "${FIXTURE}" "${PROVENANCE}" <<'PY'
import json, sys
fx, pv = sys.argv[1], sys.argv[2]
results = json.load(open(fx))
prov = json.load(open(pv))
ident = next((r for r in results if r["CollectorID"] == "singlestore_identity_v1"), None)
assert ident and len(ident["Rows"]) == 1, "identity collector must have exactly one row"
ver = ident["Rows"][0].get("memsql_version")
assert ver == prov["singlestore_version"], f"fixture identity {ver} != provenance {prov['singlestore_version']}"
assert len(results) >= 8, f"fixture must carry the full catalog, got {len(results)}"
# redaction: no raw SQL text persisted
for r in results:
    for row in r["Rows"]:
        assert "QUERY_TEXT" not in row or row["QUERY_TEXT"] in (None, ""), "QUERY_TEXT leaked"
        assert "INFO" not in row or row["INFO"] in (None, ""), "processlist INFO leaked"
print(f"verify OK: {len(results)} collectors, identity={ver}, redaction held")
PY
  )
}

cmd_down() {
  say "tearing down ${CONTAINER}"
  docker rm -f "${CONTAINER}" >/dev/null 2>&1 || true
}

case "${1:-all}" in
  up)      cmd_up ;;
  capture) cmd_capture ;;
  verify)  cmd_verify ;;
  down)    cmd_down ;;
  all)
    trap cmd_down EXIT
    cmd_up
    cmd_capture
    cmd_verify
    say "ALL OK — fixture regenerated + verified from the pinned Dev Image"
    ;;
  *) die "usage: $0 {up|capture|verify|down|all}" ;;
esac
