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

RUN apk add --no-cache tini ca-certificates \
    && adduser -D -u 10001 signals

COPY --from=builder /out/signals /usr/local/bin/signals
COPY --from=builder /out/signalsctl /usr/local/bin/signalsctl

RUN mkdir -p /data && chown signals:signals /data
VOLUME /data

USER signals
EXPOSE 8081

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -qO /dev/null http://localhost:8081/health || exit 1

ENTRYPOINT ["tini", "--"]
CMD ["signals"]
