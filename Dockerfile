# Stage 1: Build
FROM golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS builder

RUN apk add --no-cache git

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

RUN CGO_ENABLED=0 go build \
    -ldflags "-X github.com/elevarq/signals/internal/safety.Version=${VERSION} \
              -X github.com/elevarq/signals/internal/safety.Commit=${COMMIT} \
              -X github.com/elevarq/signals/internal/safety.BuildDate=${DATE}" \
    -o /out/signals ./cmd/signals

RUN CGO_ENABLED=0 go build \
    -ldflags "-X github.com/elevarq/signals/internal/safety.Version=${VERSION} \
              -X github.com/elevarq/signals/internal/safety.Commit=${COMMIT} \
              -X github.com/elevarq/signals/internal/safety.BuildDate=${DATE}" \
    -o /out/signalsctl ./cmd/signalsctl

# Stage 2: Runtime
FROM alpine:3.21@sha256:ce64758a109eb420d874a118f87920e625e12d3634e03b4a5573fd9f6e5d3507

# Static OCI image labels so a locally built image is self-describing. The
# release workflow's metadata-action re-applies these (plus dynamic
# revision/created/version) at push time with identical values — keep them in
# sync with .github/workflows/release.yml.
LABEL org.opencontainers.image.title="signals" \
      org.opencontainers.image.description="Open-source PostgreSQL diagnostic signal collector — local-first, no data egress." \
      org.opencontainers.image.licenses="BSD-3-Clause" \
      org.opencontainers.image.vendor="Elevarq" \
      org.opencontainers.image.url="https://github.com/Elevarq/signals" \
      org.opencontainers.image.source="https://github.com/Elevarq/signals" \
      org.opencontainers.image.documentation="https://github.com/Elevarq/signals/blob/main/README.md"

# apk upgrade pulls the latest patched base packages (e.g. openssl/libcrypto3/
# libssl3) from the 3.21 repo, so the release Trivy gate isn't blocked by a
# base-image HIGH that Alpine has already fixed but the pinned digest predates
# (Signals#480-adjacent; v1.5.1 was blocked by CVE-2026-75804/-84782, openssl
# fixed in 3.3.7-r2 while the pinned base carried 3.3.7-r1).
RUN apk upgrade --no-cache \
    && apk add --no-cache tini ca-certificates \
    && adduser -D -u 10001 signals

COPY --from=builder /out/signals /usr/local/bin/signals
COPY --from=builder /out/signalsctl /usr/local/bin/signalsctl

# Healthcheck probe that honors the configured API port (Elevarq/Signals#474).
# A hardcoded :8081 probe reports a working collector `unhealthy` whenever the
# operator changes api.port; healthcheck.sh derives the port from
# SIGNALS_LISTEN_ADDR at run time (default 8081).
COPY --chmod=0755 deploy/docker/healthcheck.sh /usr/local/bin/healthcheck.sh

RUN mkdir -p /data && chown signals:signals /data
VOLUME /data

USER signals
# EXPOSE documents the default listener; the actual port follows
# SIGNALS_LISTEN_ADDR / chart api.port at run time (see healthcheck.sh).
EXPOSE 8081

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD ["/usr/local/bin/healthcheck.sh"]

ENTRYPOINT ["tini", "--"]
CMD ["signals"]
