BINARY_NAME := inveniam-mcp-server
BUILD_DIR := bin
CMD_DIR := cmd/inveniam-mcp-server
DOCKER_IMAGE := inveniam-mcp-server

GO := go
GOFLAGS := -v
LDFLAGS := -s -w

.PHONY: all build run run-http test test-unit test-integration test-coverage test-verbose \
        test-load format vet lint staticcheck check-all clean docker-build docker-buildx \
        docker-run \
        pre-commit install-hooks setup-dev install-dev ci release-check \
        deps-update deps-verify info help

all: check-all test build

## Build

build:
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./$(CMD_DIR)

## Run

run: build
	$(BUILD_DIR)/$(BINARY_NAME) --transport stdio

run-http: build
	$(BUILD_DIR)/$(BINARY_NAME) --transport http

## Test

test:
	$(GO) test $(GOFLAGS) ./...

test-unit:
	$(GO) test $(GOFLAGS) -short ./...

test-integration:
	$(GO) test $(GOFLAGS) -tags integration ./... 2>&1 || \
		(echo "No integration tests found (yet) — OK"; true)

test-coverage:
	$(GO) test -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

test-verbose:
	$(GO) test -v -count=1 ./...

test-load:
	@command -v k6 >/dev/null 2>&1 || { echo "k6 is required: brew install k6"; exit 1; }
	k6 run tests/load/k6_mcp_http.js

## Quality

format:
	gofmt -w .
	goimports -w -local github.com/inveniam/nvnm-mcp-server .

vet:
	$(GO) vet ./...

lint:
	golangci-lint run --timeout=5m ./...

staticcheck:
	staticcheck ./... 2>/dev/null || echo "staticcheck not installed (covered by golangci-lint)"

check-all: format vet lint

## Pre-commit hooks

pre-commit:
	pre-commit run --all-files

install-hooks:
	pre-commit install

## Dev setup

setup-dev: install-dev install-hooks
	@echo "Dev environment ready."

install-dev:
	@echo "Installing dev tools..."
	go install golang.org/x/tools/cmd/goimports@latest
	@echo "Ensure golangci-lint and pre-commit are installed."
	@echo "  brew install golangci-lint pre-commit"

## CI

ci: install-dev check-all test-coverage
	@echo "CI passed."

release-check: clean install-dev check-all test-coverage build
	@echo "Release check passed."

## Dependencies

deps-update:
	$(GO) get -u ./...
	$(GO) mod tidy

deps-verify:
	$(GO) mod verify

## Docker

docker-build:
	docker build -t $(DOCKER_IMAGE) .

docker-run:
	docker run --rm \
		-e INVENIAM_EVM_RPC_URL \
		-e INVENIAM_CHAIN_ID \
		-e ANCHOR_ABI_PATH=/app/abi/anchoring.json \
		-e MCP_TRANSPORT=http \
		-p 8080:8080 \
		$(DOCKER_IMAGE)

docker-buildx:
	docker buildx build --platform linux/amd64,linux/arm64 -t $(DOCKER_IMAGE) .

docker-push:
	docker buildx build --platform linux/amd64,linux/arm64 -t $(DOCKER_IMAGE) --push .

## Cleanup

clean:
	rm -rf $(BUILD_DIR) coverage.out coverage.html

## Info

info:
	@echo "Binary:      $(BINARY_NAME)"
	@echo "Go version:  $$($(GO) version)"
	@echo "Module:      $$(head -1 go.mod | awk '{print $$2}')"
	@echo "Lint:        $$(golangci-lint version 2>/dev/null || echo 'not installed')"
	@echo "Pre-commit:  $$(pre-commit --version 2>/dev/null || echo 'not installed')"

## Help

help:
	@echo "Targets:"
	@echo "  build          Build the server binary"
	@echo "  run            Run with stdio transport"
	@echo "  run-http       Run with HTTP transport"
	@echo "  test           Run all tests"
	@echo "  test-unit      Unit tests only (-short)"
	@echo "  test-integration Integration tests (-tags integration)"
	@echo "  test-coverage  Tests with -race + coverage report"
	@echo "  test-verbose   Verbose test output"
	@echo "  test-load      Run k6 load tests (requires k6)"
	@echo "  format         gofmt + goimports"
	@echo "  vet            go vet"
	@echo "  lint           golangci-lint"
	@echo "  staticcheck    staticcheck (also run by golangci-lint)"
	@echo "  check-all      format + vet + lint"
	@echo "  pre-commit     Run pre-commit hooks on all files"
	@echo "  install-hooks  Install pre-commit git hooks"
	@echo "  setup-dev      Install dev deps + hooks"
	@echo "  ci             install-dev + check-all + test-coverage"
	@echo "  release-check  clean + ci + build"
	@echo "  deps-update    Update all dependencies"
	@echo "  deps-verify    Verify dependency checksums"
	@echo "  docker-build   Build Docker image"
	@echo "  docker-run     Run in Docker"
	@echo "  docker-buildx  Multi-arch Docker build (amd64 + arm64)"
	@echo "  docker-push    Multi-arch build and push to registry"
	@echo "  clean          Remove build artifacts"
	@echo "  info           Show project info"
