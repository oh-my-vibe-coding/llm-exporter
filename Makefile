BINARY=llm-exporter
MODULE=github.com/taosun/llm-exporter
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS=-s -w \
	-X '$(MODULE)/internal/version.Version=$(VERSION)' \
	-X '$(MODULE)/internal/version.GitCommit=$(GIT_COMMIT)' \
	-X '$(MODULE)/internal/version.BuildTime=$(BUILD_TIME)'

.PHONY: build run clean test docker

build:
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/llm-exporter

run: build
	./$(BINARY) --config config.example.yaml

test:
	go test ./...

clean:
	rm -f $(BINARY)

docker:
	docker build -t llm-exporter .

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down
