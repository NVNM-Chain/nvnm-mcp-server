# Inveniam EVM MCP Server

A Go-based [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) server that exposes the Inveniam EVM chain (NVNM) through a curated set of typed tools, with special emphasis on the chain's built-in anchoring interface.

This is **not** a generic JSON-RPC passthrough. It provides stable, typed, high-value MCP tools with normalized responses designed for both human and LLM consumers.

## Target Chain

| Property | Value |
|---|---|
| **Network** | NVNM Chain -- Inveniam L2 (MANTRA-secured consumer chain) |
| **Chain ID** | `58887` (`0xe607`) |
| **Cosmos chain ID** | `manveniam-1` |
| **Native currency** | mUSD (MANTRA US Dollars) -- used for gas fees |
| **EVM RPC** | `https://evm.inveniam.mantrachain.io` |
| **Cosmos RPC** | `https://rpc.inveniam.mantrachain.io` |
| **Explorer** | `https://explorer.inveniam.mantrachain.io/` |
| **Anchor precompile** | `0x0000000000000000000000000000000000000A00` |

## Status

**Code review complete.** Generic EVM tools, anchor read tools, write support (prepare-sign-submit), and full observability instrumentation are implemented and tested against the live Inveniam L2 testnet. A comprehensive 13-point code review has been applied -- see the implementation plan for details. The precompile ABI is loaded from `abi/anchoring.json`. Read queries (`registries`, `records`) return decoded, normalized results. Write tools construct complete unsigned transactions but never hold private keys -- see [Write Architecture](#write-architecture-phase-3). OpenTelemetry instrumentation provides traces, metrics, and health check endpoints -- see [Observability](#observability).

## Prerequisites

- Go 1.26+
- Access to the Inveniam EVM RPC endpoint
- (Optional) `golangci-lint` for linting
- (Optional) `pre-commit` for git hooks
- (Optional) Docker for containerized deployment

## Quick Start

```bash
# Clone and enter the project
cd NVNM_mcp_server

# Install dev tools and pre-commit hooks
make setup-dev

# Build
make build

# Configure (minimum required)
export INVENIAM_EVM_RPC_URL=https://evm.inveniam.mantrachain.io
export INVENIAM_CHAIN_ID=58887
export ANCHOR_ABI_PATH=abi/anchoring.json

# Run (stdio transport -- for local MCP client integration)
make run

# Run (HTTP transport -- for remote/production deployment)
make run-http
```

### Connect to an MCP client

**Stdio (local):**
```json
{
  "mcpServers": {
    "inveniam-evm": {
      "command": "/path/to/inveniam-mcp-server",
      "args": ["--transport", "stdio"]
    }
  }
}
```

**HTTP (remote):**
```bash
# Server runs on :8080 by default
export MCP_HTTP_ADDR=:8080
./inveniam-mcp-server --transport http
```

## Configuration

All configuration is via environment variables. No config files required.

### Required

| Variable | Description |
|---|---|
| `INVENIAM_EVM_RPC_URL` | Primary EVM JSON-RPC endpoint |
| `INVENIAM_CHAIN_ID` | Expected chain ID (`58887` for NVNM testnet) |

### Optional

| Variable | Default | Description |
|---|---|---|
| `INVENIAM_EVM_ARCHIVE_RPC_URL` | _(none)_ | Archive node RPC for historical queries |
| `ANCHOR_ADDRESS` | `0x0000000000000000000000000000000000000A00` | Anchor precompile address |
| `ANCHOR_ABI_PATH` | _(none)_ | Path to anchor ABI JSON file |
| `REQUEST_TIMEOUT` | `15s` | Timeout for upstream RPC calls |
| `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `ENABLE_WRITE_TOOLS` | `false` | Enable write (prepare) tools |
| `MCP_TRANSPORT` | `stdio` | Transport: `stdio` or `http` |
| `MCP_HTTP_ADDR` | `:8080` | Listen address for HTTP transport |

### Observability

| Variable | Default | Description |
|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | _(none)_ | OTel Collector endpoint (e.g. `localhost:4317`). Enables OTLP trace + metric export |
| `OTEL_SERVICE_NAME` | `inveniam-mcp-server` | Service name in traces and metrics |
| `ENABLE_PROMETHEUS` | `true` | Expose `/metrics` endpoint on metrics port |
| `ENABLE_STDOUT_TELEMETRY` | `false` | Dump spans/metrics to stderr (dev only) |
| `METRICS_ADDR` | `:9090` | Listen address for health + metrics endpoints |

## MCP Tools

### Phase 1: Generic EVM

| Tool | Description |
|---|---|
| `evm_get_chain_id` | Chain ID, latest block number, RPC label |
| `evm_get_block` | Block by number or hash, with optional full transactions |
| `evm_get_transaction` | Transaction details by hash |
| `evm_get_transaction_receipt` | Receipt with status, gas, logs, created contract |
| `evm_get_balance` | Balance at address (wei + ether) |
| `evm_get_code` | Contract bytecode at address |
| `evm_get_logs` | Filtered event logs |
| `evm_call_contract` | Read-only contract call with ABI decoding |

### Phase 2: Anchor Reads

| Tool | Description |
|---|---|
| `anchor_info` | Precompile config status: address, ABI loaded, method count |
| `anchor_get_registry` | Fetch a registry by numeric ID or unique name |
| `anchor_get_registries` | Paginated list of registries, with optional filters |
| `anchor_get_records` | Flexible record query: by version, by checksum, by registry, with pagination |

### Phase 3: Anchor Writes

Write tools follow a **prepare-sign-submit** pattern. The MCP server constructs complete unsigned transactions but **never holds private keys**. Requires `ENABLE_WRITE_TOOLS=true`. See [Write Architecture](#write-architecture-phase-3) below.

| Tool | Description |
|---|---|
| `anchor_prepare_add_registry` | Build unsigned tx to create a new registry |
| `anchor_prepare_add_record` | Build unsigned tx to anchor a document (checksum + URI) |
| `anchor_prepare_grant_role` | Build unsigned tx to grant admin/editor role |
| `evm_send_raw_transaction` | Broadcast a signed transaction, return tx hash |

## Write Architecture (Phase 3)

The MCP server handles all blockchain complexity (ABI encoding, nonce lookup, gas estimation, transaction construction) but signing stays with the caller. This keeps private keys out of the MCP server entirely.

**Flow:**

1. **Prepare** -- Caller invokes `anchor_prepare_add_registry(from, name, description, metadata)`. The MCP server ABI-encodes the call, fetches the nonce for the sender address, estimates gas, and returns a complete unsigned transaction as hex plus metadata (gas estimate, nonce, chain ID).

2. **Sign** -- The caller signs the unsigned transaction bytes with their private key. This is a single ECDSA operation -- no web3 library required.

3. **Submit** -- The caller sends the signed transaction back via `evm_send_raw_transaction(signed_tx_hex)`. The MCP server broadcasts it to the chain and returns the transaction hash. The caller can then monitor the result with `evm_get_transaction_receipt`.

This pattern means:
- Private keys never touch the MCP server
- Signing can happen in an HSM, Vault, or any secure enclave
- The caller needs minimal crypto capability (just ECDSA sign)
- The MCP server handles all the "fiddly" ABI and RPC work

## Observability

The server includes vendor-agnostic observability via OpenTelemetry, with out-of-the-box support for Prometheus/Grafana and CloudWatch/X-Ray.

### Endpoints

The health and metrics server runs on a separate port (default `:9090`), independent of the MCP transport.

| Endpoint | Purpose |
|---|---|
| `GET /healthz` | Liveness probe -- returns `200 OK` if the process is running |
| `GET /readyz` | Readiness probe -- returns `200 OK` if the EVM RPC is reachable and the ABI is loaded |
| `GET /metrics` | Prometheus scrape endpoint (when `ENABLE_PROMETHEUS=true`) |

### What Gets Instrumented

- **Every MCP tool call** gets a trace span and metrics (duration histogram, call counter, error counter, active request gauge)
- **Every upstream EVM RPC call** gets a child trace span with method name and duration
- **Structured logs** include request IDs and can be correlated with OTel traces
- **Sensitive data** (addresses, URLs, tx data) is redacted in all log output

### Deployment Integration

- **Kubernetes:** Liveness/readiness probes on `:9090`. `ServiceMonitor`/`PodMonitor` for Prometheus auto-discovery. OTel Collector as DaemonSet or sidecar.
- **AWS ECS/Fargate:** ALB health check on `/readyz`. `aws-otel-collector` sidecar for CloudWatch/X-Ray. Structured JSON logs via `awslogs` driver.
- **Azure:** Compatible with AKS probes and Azure Monitor via OTel Collector.

## Development

```bash
make build          # Build binary
make run            # Run with stdio transport
make run-http       # Run with HTTP transport
make test           # Run all tests
make test-unit      # Unit tests only (-short)
make test-coverage  # Tests with -race + coverage report
make test-verbose   # Verbose test output
make check-all      # format + vet + lint
make format         # gofmt + goimports
make vet            # go vet
make lint           # golangci-lint
make pre-commit     # Run pre-commit hooks on all files
make install-hooks  # Install pre-commit git hooks
make setup-dev      # Install dev deps + hooks
make ci             # install-dev + check-all + test-coverage
make release-check  # clean + ci + build
make info           # Show project info
make docker-build   # Build Docker image
make docker-run     # Run in Docker
make clean          # Remove build artifacts
```

## Deployment

### Docker

```bash
make docker-build
docker run --rm \
  -e INVENIAM_EVM_RPC_URL=https://evm.inveniam.mantrachain.io \
  -e INVENIAM_CHAIN_ID=58887 \
  -e ANCHOR_ABI_PATH=/app/abi/anchoring.json \
  -e MCP_TRANSPORT=http \
  -p 8080:8080 \
  -p 9090:9090 \
  inveniam-mcp-server
```

### AWS (ECS/Fargate)

The Docker image is designed for deployment on ECS/Fargate with HTTP transport. Configure environment variables through ECS task definitions. The server exposes health check endpoints on `:9090` (`/healthz`, `/readyz`). Add an `aws-otel-collector` sidecar for CloudWatch/X-Ray telemetry.

### MANTRA Validator Nodes

The server can run directly on MANTRA validator nodes, connecting to `localhost` RPC endpoints for minimal latency. Use stdio transport if co-located with an MCP client, or HTTP transport for remote access.

## Project Structure

```
cmd/inveniam-mcp-server/     Entrypoint
internal/
  config/                    Environment-based configuration
  logging/                   slog-based structured logging + redaction
  errors/                    Shared sentinel errors
  evm/                       Generic EVM RPC client layer + tracing wrapper
  anchor/                    Inveniam anchor adapter (with address validation)
  mcp/                       MCP tool registration and handlers
  telemetry/                 OTel providers, MCP middleware, health server, metrics
  version/                   Canonical version constant (single source of truth)
abi/
  anchoring.json             Anchor precompile ABI
docs/
  DESIGN.md                  Architecture and design decisions
  IMPLEMENTATION_PLAN.md     Phased implementation plan
.github/workflows/ci.yml    CI pipeline
.pre-commit-config.yaml     Pre-commit hooks
.golangci.yml               Linter configuration
```

## License

Proprietary. All rights reserved.
