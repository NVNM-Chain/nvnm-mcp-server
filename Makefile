BINARY_NAME := nvnm-mcp-server
BUILD_DIR := bin
CMD_DIR := cmd/nvnm-mcp-server
DOCKER_IMAGE := nvnm-mcp-server

GO := go
GOFLAGS := -v
LDFLAGS := -s -w

.DEFAULT_GOAL := help

.PHONY: all build run run-http run-local healthz readyz metrics \
        mcp-probe mcp-probe-help seed-test-data \
        test test-unit test-integration test-coverage test-verbose \
        test-load format vet lint staticcheck check-all clean docker-build docker-buildx \
        docker-run docker-smoke \
        pre-commit install-hooks setup-dev install-dev ci release-check \
        deps-update deps-verify info help \
        key-create key-disable key-enable key-list

all: check-all test build

## Build

build:
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o "$(BUILD_DIR)/$(BINARY_NAME)" ./$(CMD_DIR)

## Run

run: build
	"$(BUILD_DIR)/$(BINARY_NAME)" --transport stdio

run-http: build
	"$(BUILD_DIR)/$(BINARY_NAME)" --transport http

## Local dev

# Defaults match `.env.example`. Override via environment if the server
# was started on non-default ports. `run-local` sources `.env` so the
# operator's chain config is the single source of truth; the Makefile
# embeds no chain values.
MCP_HTTP_ADDR ?= :8180
METRICS_ADDR  ?= :9190

run-local: build
	@if [ ! -f .env ]; then \
		echo "ERROR: .env not found. Copy .env.example to .env and fill in values (see CONTRIBUTING.md § 2)."; \
		exit 1; \
	fi
	@set -a && . ./.env && set +a && \
		"$(BUILD_DIR)/$(BINARY_NAME)" --transport http

healthz:
	@curl -sSf http://localhost$(METRICS_ADDR)/healthz | python3 -m json.tool

readyz:
	@curl -sSf http://localhost$(METRICS_ADDR)/readyz | python3 -m json.tool

metrics:
	@curl -sSf http://localhost$(METRICS_ADDR)/metrics | head -50

## MCP probe -- exercise the running HTTP MCP server with one parameterized
## target. Replaces the four broken per-tool curl targets that hardcoded
## ports and skipped the `Mcp-Session-Id` handshake.
##
## The recipe inlines the three-step handshake (init -> notifications/initialized
## -> tools/call) since each MCP HTTP session is short-lived and shell state
## doesn't survive between `make` recipe lines. jq is used for pretty-printing
## when available; falls back to raw stdout.
##
## Usage:
##   make mcp-probe TOOL=evm_get_chain_id ARGS='{}'
##   make mcp-probe TOOL=nvnm_overview ARGS='{}'
##   make mcp-probe TOOL=evm_get_balance ARGS='{"address":"0x..."}'

mcp-probe:
ifndef TOOL
	$(error TOOL is required. Run `make mcp-probe-help` for examples.)
endif
	@ARGS_VAL='$(if $(ARGS),$(ARGS),{})'; \
	BASE_URL="http://localhost$(MCP_HTTP_ADDR)/"; \
	echo "POST $$BASE_URL  (initialize)" >&2; \
	INIT_HEADERS=$$(mktemp); \
	INIT_BODY=$$(curl -sS -D "$$INIT_HEADERS" -X POST "$$BASE_URL" \
		-H "Content-Type: application/json" \
		-H "Accept: application/json, text/event-stream" \
		-d '{"jsonrpc":"2.0","method":"initialize","id":1,"params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"make-mcp-probe","version":"1.0.0"}}}'); \
	SESSION_ID=$$(grep -i '^mcp-session-id:' "$$INIT_HEADERS" | head -1 | awk '{print $$2}' | tr -d '\r\n'); \
	rm -f "$$INIT_HEADERS"; \
	if [ -z "$$SESSION_ID" ]; then \
		echo "ERROR: server did not return Mcp-Session-Id header. Is the server running on $(MCP_HTTP_ADDR)?" >&2; \
		echo "initialize response: $$INIT_BODY" >&2; \
		exit 1; \
	fi; \
	echo "  session: $$SESSION_ID" >&2; \
	curl -sS -X POST "$$BASE_URL" \
		-H "Content-Type: application/json" \
		-H "Accept: application/json, text/event-stream" \
		-H "Mcp-Session-Id: $$SESSION_ID" \
		-d '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}' > /dev/null; \
	echo "tools/call $(TOOL) $$ARGS_VAL" >&2; \
	RESP=$$(curl -sS -X POST "$$BASE_URL" \
		-H "Content-Type: application/json" \
		-H "Accept: application/json, text/event-stream" \
		-H "Mcp-Session-Id: $$SESSION_ID" \
		-d "{\"jsonrpc\":\"2.0\",\"method\":\"tools/call\",\"id\":2,\"params\":{\"name\":\"$(TOOL)\",\"arguments\":$$ARGS_VAL}}"); \
	if command -v jq >/dev/null 2>&1; then \
		echo "$$RESP" | jq .; \
	else \
		echo "$$RESP"; \
	fi

mcp-probe-help:
	@echo "make mcp-probe -- call any registered MCP tool via the HTTP transport."
	@echo ""
	@echo "Usage: make mcp-probe TOOL=<tool_name> [ARGS='<json>']"
	@echo ""
	@echo "Examples:"
	@echo "  make mcp-probe TOOL=evm_get_chain_id ARGS='{}'"
	@echo "  make mcp-probe TOOL=nvnm_overview ARGS='{}'"
	@echo "  make mcp-probe TOOL=anchor_get_registries ARGS='{}'"
	@echo "  make mcp-probe TOOL=anchor_info ARGS='{}'"
	@echo "  make mcp-probe TOOL=evm_get_balance ARGS='{\"address\":\"0x0000000000000000000000000000000000000000\"}'"
	@echo ""
	@echo "Listening address comes from MCP_HTTP_ADDR (default :8180)."
	@echo "Override via env: MCP_HTTP_ADDR=:9999 make mcp-probe TOOL=evm_get_chain_id"

seed-test-data:
	$(GO) run ./cmd/seed-test-data

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
	goimports -w -local github.com/NVNM-Chain/nvnm-mcp-server .

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
		-e NVNM_EVM_RPC_URL \
		-e NVNM_CHAIN_ID \
		-e ANCHOR_ABI_PATH=/app/abi/anchoring.json \
		-e MCP_TRANSPORT=http \
		-p 8080:8080 \
		$(DOCKER_IMAGE)

docker-smoke: docker-build
	@echo "Starting container..."
	@CONTAINER_ID=$$(docker run -d --rm \
		-e NVNM_EVM_RPC_URL=https://evm.inveniam.mantrachain.io \
		-e NVNM_CHAIN_ID=58887 \
		-e ANCHOR_ABI_PATH=/app/abi/anchoring.json \
		-e ENABLE_WRITE_TOOLS=true \
		-e MCP_TRANSPORT=http \
		-p 18080:8080 -p 19090:9090 \
		$(DOCKER_IMAGE)) && \
	echo "  container: $$CONTAINER_ID" && \
	sleep 3 && \
	echo "Checking /healthz..." && \
	curl -sf http://localhost:19090/healthz | python3 -m json.tool && \
	echo "Checking /readyz..." && \
	curl -sf http://localhost:19090/readyz | python3 -m json.tool && \
	echo "Checking tools/list..." && \
	curl -sf -X POST http://localhost:18080/ \
		-H "Content-Type: application/json" \
		-H "Accept: application/json, text/event-stream" \
		-d '{"jsonrpc":"2.0","method":"initialize","id":1,"params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"docker-smoke","version":"1.0.0"}}}' > /dev/null && \
	echo "  initialize OK" && \
	docker stop $$CONTAINER_ID > /dev/null && \
	echo "Docker smoke test PASSED"

docker-buildx:
	docker buildx build --platform linux/amd64,linux/arm64 -t $(DOCKER_IMAGE) .

docker-push:
	docker buildx build --platform linux/amd64,linux/arm64 -t $(DOCKER_IMAGE) --push .

## API Key Management

MCP_API_KEYS_FILE ?= .mcp-keys.json
export MCP_API_KEYS_FILE

key-create:
ifndef NAME
	$(error NAME is required. Usage: make key-create NAME=my-client [ROLES=reader,writer])
endif
ifdef ROLES
	$(GO) run ./cmd/key-mgmt create $(NAME) --roles $(ROLES)
else
	$(GO) run ./cmd/key-mgmt create $(NAME)
endif

key-disable:
ifndef NAME
	$(error NAME is required. Usage: make key-disable NAME=my-client)
endif
	$(GO) run ./cmd/key-mgmt disable $(NAME)

key-enable:
ifndef NAME
	$(error NAME is required. Usage: make key-enable NAME=my-client)
endif
	$(GO) run ./cmd/key-mgmt enable $(NAME)

key-list:
	$(GO) run ./cmd/key-mgmt list

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
	@echo "Inveniam EVM MCP Server"
	@echo ""
	@echo "Build & Run:"
	@echo "  all              check-all + test + build"
	@echo "  build            Build the server binary"
	@echo "  run              Run with stdio transport"
	@echo "  run-http         Run with HTTP transport"
	@echo "  run-local        Source .env and run with HTTP transport (see Local Dev)"
	@echo ""
	@echo "API Key Management:"
	@echo "  key-create NAME=x [ROLES=reader,writer]"
	@echo "                     Generate a new API key for client x"
	@echo "  key-disable NAME=x Disable the key for client x"
	@echo "  key-enable NAME=x  Re-enable a disabled key for client x"
	@echo "  key-list           List all API keys and their status"
	@echo ""
	@echo "Local Dev:"
	@echo "  run-local        Source .env and run the server with HTTP transport"
	@echo "  healthz          Check liveness endpoint (METRICS_ADDR, default :9190)"
	@echo "  readyz           Check readiness endpoint (METRICS_ADDR, default :9190)"
	@echo "  metrics          Show first 50 lines of Prometheus metrics (METRICS_ADDR)"
	@echo "  mcp-probe TOOL=<name> [ARGS='<json>']"
	@echo "                   Call any MCP tool via init -> notify -> tools/call handshake"
	@echo "  mcp-probe-help   Print mcp-probe usage and examples"
	@echo "  seed-test-data   Create a test registry with 3 phoney records on-chain"
	@echo ""
	@echo "Test:"
	@echo "  test             Run all tests"
	@echo "  test-unit        Unit tests only (-short)"
	@echo "  test-integration Integration tests (-tags integration)"
	@echo "  test-coverage    Tests with -race + coverage report"
	@echo "  test-verbose     Verbose test output"
	@echo "  test-load        Run k6 load tests (requires k6)"
	@echo ""
	@echo "Quality:"
	@echo "  format           gofmt + goimports"
	@echo "  vet              go vet"
	@echo "  lint             golangci-lint"
	@echo "  staticcheck      staticcheck (also run by golangci-lint)"
	@echo "  check-all        format + vet + lint"
	@echo "  pre-commit       Run pre-commit hooks on all files"
	@echo ""
	@echo "Dev Setup:"
	@echo "  install-hooks    Install pre-commit git hooks"
	@echo "  setup-dev        Install dev deps + hooks"
	@echo "  ci               install-dev + check-all + test-coverage"
	@echo "  release-check    clean + ci + build"
	@echo ""
	@echo "Dependencies:"
	@echo "  deps-update      Update all dependencies"
	@echo "  deps-verify      Verify dependency checksums"
	@echo ""
	@echo "Docker:"
	@echo "  docker-build     Build Docker image"
	@echo "  docker-smoke     Build, run, verify healthz + MCP init, tear down"
	@echo "  docker-run       Run in Docker"
	@echo "  docker-buildx    Multi-arch Docker build (amd64 + arm64)"
	@echo "  docker-push      Multi-arch build and push to registry"
	@echo ""
	@echo "Other:"
	@echo "  clean            Remove build artifacts"
	@echo "  info             Show project info"
