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
    -ldflags "-X github.com/elevarq/arq-signals/internal/safety.Version=${VERSION} \
              -X github.com/elevarq/arq-signals/internal/safety.Commit=${COMMIT} \
              -X github.com/elevarq/arq-signals/internal/safety.BuildDate=${DATE}" \
    -o /out/arq-signals ./cmd/arq-signals

RUN CGO_ENABLED=0 go build \
    -ldflags "-X github.com/elevarq/arq-signals/internal/safety.Version=${VERSION} \
              -X github.com/elevarq/arq-signals/internal/safety.Commit=${COMMIT} \
              -X github.com/elevarq/arq-signals/internal/safety.BuildDate=${DATE}" \
    -o /out/arqctl ./cmd/arqctl

# Stage 2: Runtime
FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d

RUN apk add --no-cache tini ca-certificates \
    && adduser -D -u 10001 arq

COPY --from=builder /out/arq-signals /usr/local/bin/arq-signals
COPY --from=builder /out/arqctl /usr/local/bin/arqctl

RUN mkdir -p /data && chown arq:arq /data
VOLUME /data

USER arq
EXPOSE 8081

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -qO /dev/null http://localhost:8081/health || exit 1

ENTRYPOINT ["tini", "--"]
CMD ["arq-signals"]
