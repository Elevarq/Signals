#!/usr/bin/env bash
#
# gen-conformance-corpus.sh — generate the #530 producer conformance corpus.
#
# Produces real Signals production-exporter ZIP artifacts for MySQL, MariaDB, and
# ClickHouse, each with reproducible provenance, by running the REAL live
# qualification (local docker) and capturing the EXACT exported ZIP it verified.
# The artifacts are the genuine output of the normal cmd/signals collection ->
# persistence -> production export path; they are NEVER hand-constructed or
# edited. Hand the result to Analyzer #3890 as the engine conformance corpus.
#
#   bash scripts/gen-conformance-corpus.sh [OUTPUT_DIR]
#
# Default OUTPUT_DIR: ./conformance-corpus. Requires docker + a Go toolchain +
# python3 + unzip. No cloud, no AWS. Each engine uses its pinned dev-fixture
# image (MySQL 8.4, MariaDB 11.4, ClickHouse 24.8 — the representative qualified
# versions); override per engine via the live-qual env if a different pinned
# version is wanted.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${1:-${REPO_ROOT}/conformance-corpus}"
mkdir -p "${OUT}"

echo ">> #530 conformance corpus -> ${OUT}"
echo ">> signals commit: $(git -C "${REPO_ROOT}" rev-parse HEAD)"

for engine in mysql mariadb clickhouse; do
  echo "============================================================"
  echo ">> ${engine}: real cmd/signals daemon -> production exporter (live qualification)"
  SIGNALS_CORPUS_OUT="${OUT}" bash "${REPO_ROOT}/scripts/${engine}-live-qual.sh" all
done

echo "============================================================"
echo ">> corpus artifacts + provenance:"
ls -la "${OUT}"
echo ">> done — hand ${OUT}/*.zip + *.provenance.json to Analyzer #3890"
