#!/usr/bin/env bash
#
# clickhouse-cluster-fixture.sh — brings up the pinned REPLICATED ClickHouse
# cluster (#566): one shard, two replicas (ch1, ch2) coordinated by a single
# ClickHouse Keeper, via examples/clickhouse-cluster/docker-compose.yaml. The
# replicated analog of scripts/clickhouse-dev-fixture.sh (single node).
#
# The Signals daemon collects from ch1; system.clusters / system.replicas on ch1
# carry the whole cluster's topology + replication state, which is exactly the
# cluster-scope evidence the ClickHouse collectors (clickhouse_clusters_v1 /
# clickhouse_replicas_v1) must surface.
#
#   bash scripts/clickhouse-cluster-fixture.sh up     # start + seed, wait ready
#   bash scripts/clickhouse-cluster-fixture.sh down    # stop + remove volumes
#   bash scripts/clickhouse-cluster-fixture.sh verify  # assert cluster-scope evidence
#
# Image pinned by digest in the compose file (upgrades require re-qualification,
# specifications/clickhouse-live-qualification.md). Requires docker. No cloud.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIR="${REPO_ROOT}/examples/clickhouse-cluster"
COMPOSE="${DIR}/docker-compose.yaml"
INIT_SQL="${DIR}/init-cluster.sql"
PREFIX="${CH_CLUSTER_PREFIX:-signals-ch-cluster}"
EXPECTED_VERSION_PREFIX="24."

export CH_CLUSTER_PREFIX="${PREFIX}"

say() { printf '>> %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
dc()  { docker compose -f "${COMPOSE}" "$@"; }
ch1() { docker exec -i "${PREFIX}-ch1" clickhouse-client "$@"; }

cmd_up() {
  [ -f "${INIT_SQL}" ] || die "seed not found: ${INIT_SQL}"
  if docker ps -aq -f "name=^${PREFIX}-ch1$" | grep -q .; then
    die "cluster ${PREFIX} already exists — run '$0 down' first (refusing to remove containers this script did not create)"
  fi
  say "starting pinned ClickHouse cluster (keeper + ch1 + ch2)"
  dc up -d >/dev/null
  say "waiting for ch1 to accept connections"
  local ok=""
  for _ in $(seq 1 60); do
    if ch1 --query "SELECT 1" >/dev/null 2>&1; then ok=1; break; fi
    sleep 2
  done
  [ -n "${ok}" ] || { dc logs 2>&1 | tail -40; die "ClickHouse ch1 did not become ready"; }
  local ver
  ver="$(ch1 --query "SELECT version()" 2>/dev/null | tr -d '[:space:]')"
  case "${ver}" in
    "${EXPECTED_VERSION_PREFIX}"*) say "ClickHouse ready: version()=${ver}" ;;
    *) die "unexpected ClickHouse version '${ver}' (want ${EXPECTED_VERSION_PREFIX}x from the pinned image)" ;;
  esac
  say "waiting for the cluster definition to resolve (keeper up)"
  ok=""
  for _ in $(seq 1 30); do
    if [ "$(ch1 --query "SELECT count() FROM system.clusters WHERE cluster='signals_cluster'" 2>/dev/null | tr -d '[:space:]')" = "2" ]; then ok=1; break; fi
    sleep 2
  done
  [ -n "${ok}" ] || die "signals_cluster did not resolve to 2 replicas (keeper/cluster config problem)"
  say "applying deterministic seed (read-only user + ReplicatedMergeTree ON CLUSTER + flush logs)"
  ch1 --multiquery < "${INIT_SQL}" || die "seed failed — setup SQL did not apply cleanly (see above)"
  say "cluster ready + seeded on 127.0.0.1:${CH_CLUSTER_PORT:-9000} (ch1)"
}

cmd_verify() {
  local replicas tbls
  replicas="$(ch1 --query "SELECT count() FROM system.clusters WHERE cluster='signals_cluster'" 2>/dev/null | tr -d '[:space:]')"
  [ "${replicas}" = "2" ] || die "system.clusters: expected 2 replicas for signals_cluster, got '${replicas}'"
  tbls="$(ch1 --query "SELECT count() FROM system.replicas WHERE database='signals_demo' AND table='repl'" 2>/dev/null | tr -d '[:space:]')"
  [ "${tbls}" = "1" ] || die "system.replicas: expected the ReplicatedMergeTree signals_demo.repl, got '${tbls}' rows"
  say "VERIFIED cluster-scope evidence: system.clusters=2 replicas, system.replicas carries signals_demo.repl"
}

cmd_down() {
  say "tearing down cluster ${PREFIX}"
  dc down -v >/dev/null 2>&1 || true
  say "cluster removed"
}

case "${1:-}" in
  up)     cmd_up ;;
  verify) cmd_verify ;;
  down)   cmd_down ;;
  *) die "usage: $0 {up|verify|down}" ;;
esac
