# Stage 1: Build
FROM golang:1.25-alpine AS builder

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
FROM alpine:3.21

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
