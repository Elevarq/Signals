#!/usr/bin/env bash
#
# reconcile-collector.sh — idempotent bootstrap/reboot reconcile for the GCP
# Signals collector (#732 / audit A7b).
#
# The GCP startup script runs on every boot and could be re-run. The previous
# logic unconditionally minted a NEW operator token and `docker run --name
# signals` — so a re-run with the container already present failed on the name
# conflict after the token file had already been replaced, orphaning the
# operator's access to the still-running daemon and breaking reliable
# reboot/recovery.
#
# This script is idempotent:
#   * Operator token: reuse the already-minted token if present; mint only on
#     first boot. A re-run never rotates the token out from under the running
#     daemon.
#   * Container: reconcile by name — if a `signals` container already exists,
#     ensure it is running (reboot is normally handled by --restart=always, but
#     a script re-run must not fail on the name clash); only `docker run` when
#     absent. Durable /data and the bind-mounted config are untouched.
#
# Inputs (environment):
#   IMAGE_URI        required — the collector image reference.
#   TOKEN_FILE       operator token path (default /root/signals-api-token).
#   SIGNALS_EXTRA_ENV optional — extra `-e K=V` docker args (e.g. cloud creds).
#
# Exit non-zero on any failure so a partial bootstrap is retried by the next run.
set -euo pipefail

: "${IMAGE_URI:?IMAGE_URI is required}"
TOKEN_FILE="${TOKEN_FILE:-/root/signals-api-token}"

umask 077

# Reuse an existing operator token; mint only on first boot. A non-empty token
# file is authoritative — the running container already carries this token in
# its environment from the first run, so reuse keeps them synchronized.
if [ -s "${TOKEN_FILE}" ]; then
  SIGNALS_API_TOKEN="$(cat "${TOKEN_FILE}")"
else
  SIGNALS_API_TOKEN="$(openssl rand -hex 32)"
  printf '%s\n' "${SIGNALS_API_TOKEN}" > "${TOKEN_FILE}"
fi
export SIGNALS_API_TOKEN

# Reconcile the container by name — never an unconditional `docker run`.
if docker inspect signals >/dev/null 2>&1; then
  # Already created (reboot or script re-run): ensure it is running. Idempotent;
  # a no-op when it is already up. Durable data and token are preserved.
  docker start signals >/dev/null 2>&1 || true
else
  # First boot (or after a deliberate removal): create it with the (possibly
  # reused) token so the operator-retrievable token matches the daemon's.
  # shellcheck disable=SC2086
  docker run -d --name signals --restart=always \
    -e SIGNALS_API_TOKEN="${SIGNALS_API_TOKEN}" \
    ${SIGNALS_EXTRA_ENV:-} \
    -v /etc/signals:/etc/signals:ro \
    -v /data:/data \
    -p 127.0.0.1:8081:8081 \
    "${IMAGE_URI}" signals --config /etc/signals/signals.yaml
fi
