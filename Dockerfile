# Stage 1: Build
FROM golang:1.26.4-alpine@sha256:f1ddd9fe14fffc091dd98cb4bfa999f32c5fc77d2f2305ea9f0e2595c5437c14 AS builder

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
FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d

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
