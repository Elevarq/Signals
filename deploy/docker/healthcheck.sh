#!/bin/sh
# healthcheck.sh — Docker HEALTHCHECK probe that honors the configured API
# port (Elevarq/Signals#474).
#
# The API listener is configurable (api.listen_addr / SIGNALS_LISTEN_ADDR /
# chart api.port). A HEALTHCHECK that hardcodes :8081 probes the wrong port
# whenever an operator changes it, so Docker/ECS report the container
# `unhealthy` even though the collector is working — and an orchestrator may
# kill or stop routing to it. This script derives the probe target from
# SIGNALS_LISTEN_ADDR at run time, defaulting to 8081 when it is unset.
#
# SIGNALS_LISTEN_ADDR is a host:port (e.g. "0.0.0.0:9090", "127.0.0.1:8081").
# We probe localhost on the configured PORT — inside the container the daemon
# is always reachable on loopback regardless of the bind host (0.0.0.0 or a
# specific address), and probing the port is what the fix requires.
set -eu

addr="${SIGNALS_LISTEN_ADDR:-}"
# Strip everything up to and including the last ':' to get the port. Handles
# "0.0.0.0:9090" and bare-host forms; an empty/absent addr falls back to 8081.
case "$addr" in
  *:*) port="${addr##*:}" ;;
  *)   port="" ;;
esac
: "${port:=8081}"

exec wget -qO /dev/null "http://localhost:${port}/health"
