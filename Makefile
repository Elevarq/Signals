.PHONY: build test lint vet clean boundary docker-build

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  = -X github.com/elevarq/arq-signals/internal/safety.Version=$(VERSION) \
           -X github.com/elevarq/arq-signals/internal/safety.Commit=$(COMMIT) \
           -X github.com/elevarq/arq-signals/internal/safety.BuildDate=$(DATE)

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/arq-signals ./cmd/arq-signals
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/arqctl ./cmd/arqctl

test:
	go test -race -count=1 ./...

lint: vet
	@echo "Lint passed (go vet)"

vet:
	go vet ./...

boundary:
	go test -run 'TestNoAnalyzerImports|TestNoLLMCode|TestNoScoringCode|TestNoProprietaryContent|TestLicenseFileExists' -v ./tests/

clean:
	rm -rf bin/

docker-build:
	docker build -t arq-signals:$(VERSION) -f Dockerfile .
