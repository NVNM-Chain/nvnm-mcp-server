# Testing

This document describes the testing strategy, framework, and latest results for the Inveniam EVM MCP Server.

## Overview

The project uses a layered testing approach: unit tests with mocks for fast feedback, golden tests for response shape stability, integration tests against the live Inveniam testnet, HTTP end-to-end tests through the MCP protocol layer, k6 load tests for performance, and Docker smoke tests for deployment verification.

**Test inventory** (as of 2026-04-01):

| Metric | Count |
|--------|-------|
| Test files | 23 |
| Test functions | 159 |
| Lines of test code | 4,378 |
| Golden fixture files | 13 |
| Integration test files (build tag) | 6 |
| Packages with tests | 8 of 10 |

## Running Tests

### Quick Reference

```bash
make test              # All unit tests (no integration)
make test-unit         # Unit tests with -short flag
make test-integration  # Integration tests against live testnet (requires network)
make test-coverage     # Unit tests with race detector + HTML coverage report
make test-verbose      # Verbose output, no caching
make test-load         # k6 load tests (requires running server + k6 installed)
make docker-smoke      # Build Docker image, start container, verify health + MCP
make seed-test-data    # Create a test registry with phoney records on-chain
```

### Prerequisites

| Command | Prerequisite | Install |
|---------|-------------|---------|
| `make test` | Go 1.26+ | -- |
| `make test-integration` | Network access to `https://evm.inveniam.mantrachain.io` | -- |
| `make test-load` | k6, running server on `:8080` | `brew install k6` |
| `make docker-smoke` | Docker Desktop | -- |
| `make seed-test-data` | `.chain_credentials.txt` in project root | See below |

**Credentials file format** (`.chain_credentials.txt`, git-ignored):

```
Address: 0x...
PrivateKey: 0x...
```

Used by integration write tests and `seed-test-data`. Tests skip gracefully if the file is missing.

## Test Layers

### 1. Unit Tests (no network, no build tags)

Fast, deterministic tests using mocks and stubs. Run with `make test`.

| Package | Test file(s) | Tests | What's covered |
|---------|-------------|-------|----------------|
| `internal/mcp` | `tools_test.go` | 46 | All 16 MCP tool handlers (happy path + error cases), validation helpers (`parseAddress`, `parseHash`, `parseHexData`) |
| `internal/mcp` | `server_test.go` | 6 | HTTP E2E via `httptest.NewServer`: `ListTools` (all 16 tools), `CallTool` (success + error propagation) |
| `internal/config` | `config_test.go` | 10 | Environment variable loading, defaults, validation errors, resilience config |
| `internal/errors` | `errors_test.go` | 5 | Sentinel error distinctness, `IsInputError`, `IsTransientError`, `IsNotFound` |
| `internal/evm` | `tracing_test.go` | 4 | `TracingClient` delegation and error propagation |
| `internal/evm` | `resilient_test.go` | 8 | Retry with backoff, circuit breaker tripping, rate limiting, `SendRawTransaction` non-retry |
| `internal/anchor` | `client_test.go` | 10 | `NewClient` (ABI loading variants), `RequireABI`, mock-based query methods |
| `internal/anchor` | `prepare_test.go` | 9 | `PrepareAddRegistry`, `PrepareAddRecord`, `PrepareGrantRole` validation, gas buffer, `UnsignedTransaction` JSON |
| `internal/telemetry` | `health_test.go` | 4 | `/healthz` and `/readyz` endpoints (healthy, RPC down, ABI missing) |
| `internal/telemetry` | `middleware_test.go`, `metrics_test.go` | 5 | MCP middleware creation, request ID, tool name extraction, metric instruments |
| `internal/logging` | `logger_test.go`, `fanout_test.go`, `redact_test.go` | 12 | Logger creation, JSON output, level filtering, dual-handler fanout, address/URL/data redaction |

**Mock types** used across unit tests:

- `mockEVM` (`internal/mcp/tools_test.go`) -- full `evm.Client` implementation with configurable return values
- `mockAnchor` (`internal/mcp/tools_test.go`) -- full `anchor.Client` implementation
- `stubClient` (`internal/evm/tracing_test.go`) -- minimal `evm.Client` stub
- `failingClient` (`internal/evm/resilient_test.go`) -- `evm.Client` that fails N times then succeeds
- `mockEVMClient` (`internal/anchor/client_test.go`) -- `evm.Client` for anchor-layer tests
- `mockChecker` (`internal/telemetry/health_test.go`) -- readiness probe mock

### 2. Golden Tests (response shape stability)

Golden tests serialize a struct to JSON and compare against a checked-in `.golden.json` file. If the serialized output changes, the test fails -- protecting API response shapes from accidental drift.

**EVM golden files** (`internal/evm/testdata/`):

| File | Type |
|------|------|
| `chain_info.golden.json` | `ChainInfo` |
| `normalized_block.golden.json` | `NormalizedBlock` |
| `normalized_transaction.golden.json` | `NormalizedTransaction` |
| `normalized_receipt.golden.json` | `NormalizedReceipt` |
| `normalized_balance.golden.json` | `NormalizedBalance` |
| `code_result.golden.json` | `CodeResult` |

**Anchor golden files** (`internal/anchor/testdata/`):

| File | Type |
|------|------|
| `registry.golden.json` | `Registry` |
| `record.golden.json` | `Record` |
| `get_registries_response.golden.json` | `GetRegistriesResponse` |
| `get_records_response.golden.json` | `GetRecordsResponse` |
| `empty_records_response.golden.json` | `GetRecordsResponse` (empty) |
| `precompile_info.golden.json` | `PrecompileInfo` |
| `unsigned_transaction.golden.json` | `UnsignedTransaction` |

To update golden files after an intentional change, delete the `.golden.json` file and re-run the test -- it will regenerate.

### 3. Integration Tests (live testnet)

Integration tests connect to the Inveniam EVM RPC at `https://evm.inveniam.mantrachain.io` (chain ID 58887). They are excluded from default `go test ./...` by the `//go:build integration` build tag.

Run with: `make test-integration` or `go test -tags integration ./...`

| Package | Test file | Tests | What's verified |
|---------|----------|-------|-----------------|
| `internal/evm` | `client_integration_test.go` | 8 | `ChainID`, `GetChainInfo`, `LatestBlockNumber`, `BlockByNumber`, `BlockByHash`, `BalanceAt`, `CodeAt` |
| `internal/evm` | `resilient_integration_test.go` | 4 | Resilient wrapper: `ChainID`, `GetChainInfo`, `BalanceAt`, `Ping` |
| `internal/evm` | `logs_integration_test.go` | 2 | `FilterLogs` on precompile address (finds real logs), empty-range query |
| `internal/evm` | `call_integration_test.go` | 2 | `CallContract` against precompile (empty data error path), non-existent address |
| `internal/anchor` | `client_integration_test.go` | 6 | `Info`, `GetRegistries`, `GetRegistry` (by ID/name), `GetRecords` |
| `internal/anchor` | `write_integration_test.go` | 3 | Prepare-sign-submit for `AddRegistry`, `AddRecord`, `GrantRole` |

Write integration tests require `.chain_credentials.txt` and skip if the file is absent.

### 4. MCP End-to-End HTTP Tests

These tests (`internal/mcp/server_test.go`) spin up a real MCP HTTP server using `httptest.NewServer` with mock clients, then connect using the official MCP SDK client (`mcp.NewClient` + `StreamableClientTransport`).

| Test | What's verified |
|------|-----------------|
| `TestE2E_ListTools_Returns16` | Server registers exactly 16 tools (12 read + 4 write) |
| `TestE2E_ListTools_ContainsExpectedNames` | Every expected tool name is present |
| `TestE2E_CallTool_ChainID` | `evm_get_chain_id` returns non-error structured content |
| `TestE2E_CallTool_AnchorInfo` | `anchor_info` returns non-error structured content |
| `TestE2E_CallTool_InvalidAddress` | `evm_get_balance` with bad address returns `IsError=true` |
| `TestE2E_CallTool_MissingRegistryIDAndName` | `anchor_get_registry` with no args returns `IsError=true` |

This layer validates: HTTP transport, SSE/JSON response framing, MCP session management, JSON-RPC 2.0 envelope, tool registration, and error propagation through the full stack.

### 5. k6 Load Tests

The k6 script (`tests/load/k6_mcp_http.js`) exercises the MCP Streamable HTTP endpoint with three scenarios:

| Scenario | Executor | VUs | Duration | Tools exercised |
|----------|----------|-----|----------|-----------------|
| `constant_reads` | constant-vus | 10 | 2 min | `evm_get_chain_id` |
| `burst_reads` | ramping-vus | 0 → 50 → 0 | 3 min | `evm_get_chain_id` |
| `mixed_workload` | constant-vus | 15 | 2 min | `evm_get_chain_id`, `evm_get_block`, `anchor_get_registries` |

**Thresholds:**

- `http_req_duration`: p(95) < 2000ms
- `http_req_failed`: rate < 1%

See `tests/load/README.md` for setup and usage details.

### 6. Docker Smoke Test

`make docker-smoke` performs an automated build-run-verify cycle:

1. Builds the Docker image (`make docker-build`)
2. Starts a container with testnet environment variables on ports 18080/19090
3. Verifies `/healthz` returns `{"status":"ok"}`
4. Verifies `/readyz` returns `{"status":"ready"}` with `evm_rpc: ok` and `abi: loaded`
5. Sends an MCP `initialize` request and verifies HTTP 200
6. Stops the container

### 7. Seed Test Data

`make seed-test-data` runs `cmd/seed-test-data/main.go`, which:

1. Loads credentials from `.chain_credentials.txt`
2. Creates a registry named `mcp-test-data` (skips if it already exists)
3. Adds 3 records with phoney checksums, URIs, and metadata
4. Verifies all data is readable on-chain

This provides a known dataset for integration tests and manual testing.

## Latest Test Results (2026-04-01)

### Unit Tests

```
$ make test
ok  internal/anchor    0.372s
ok  internal/config    0.696s
ok  internal/errors    0.526s
ok  internal/evm       0.753s
ok  internal/logging   1.434s
ok  internal/mcp       1.062s
ok  internal/telemetry 1.275s
```

All 134 unit tests pass. Zero failures.

### Integration Tests

```
$ make test-integration
```

**EVM integration** (4 files, 16 tests):

- `ChainID` = 58887, `LatestBlockNumber` = 828169
- `FilterLogs` found 6 logs from precompile in 1000-block range
- `CallContract` against precompile returns expected error for empty calldata
- All block, balance, and code queries return valid normalized types

**Anchor integration** (2 files, 9 tests):

- `GetRegistries` returns 153+ registries
- `GetRegistry` by ID and name both resolve correctly
- **AddRegistry**: mined successfully, new registry confirmed on-chain
- **AddRecord**: mined successfully, record confirmed with correct checksum/URI
- **GrantRole**: mined successfully in block 828178, 40,547 gas used -- first time this tool has been tested on-chain

### k6 Load Test

```
$ make test-load    # with server running via make run-local
```

| Metric | Value |
|--------|-------|
| Total iterations | 38,178 |
| Throughput | 212 req/s |
| Avg latency | 757µs |
| p90 latency | 1.46ms |
| p95 latency | 1.83ms |
| Max latency | 6.25ms |
| HTTP failure rate | 0.00% |
| VUs (max) | 75 |
| Duration | 3 min |

**Thresholds: ALL PASSED**

- `http_req_duration p(95) < 2000ms` -- actual: 1.83ms
- `http_req_failed rate < 0.01` -- actual: 0.00%

**Known issue**: The k6 script's `tools/call JSON-RPC result` assertion reports failures because the response parser doesn't correctly handle SSE-formatted responses (the server uses `text/event-stream`, not `application/json`). All HTTP 200 responses are valid; this is a k6 script parsing bug, not a server issue.

### Docker Smoke Test

```
$ make docker-smoke
```

- Docker image builds successfully (multi-stage: `golang:1.26-alpine` → `distroless/static-debian12`)
- Container starts, ABI loads from `/app/abi/anchoring.json`
- `/healthz` → `{"status":"ok","version":"0.4.0"}`
- `/readyz` → `{"status":"ready","checks":{"abi":"loaded","evm_rpc":"ok"}}`
- MCP `initialize` → HTTP 200, session established
- Container stops cleanly

## Test Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Test Layers                          │
├─────────────┬───────────────┬───────────────────────────┤
│  Unit Tests │ Golden Tests  │  MCP E2E HTTP Tests       │
│  (mocks)    │ (JSON shapes) │  (httptest + SDK client)  │
│  134 tests  │ 13 fixtures   │  6 tests                  │
├─────────────┴───────────────┴───────────────────────────┤
│              Integration Tests (live testnet)           │
│              25 tests, build tag: integration           │
├─────────────────────────────────────────────────────────┤
│              k6 Load Tests (HTTP transport)             │
│              3 scenarios, 75 VUs, 3 min                 │
├─────────────────────────────────────────────────────────┤
│              Docker Smoke Test (container lifecycle)    │
│              build → start → healthz → readyz → stop   │
└─────────────────────────────────────────────────────────┘
```

## Adding New Tests

**For a new MCP tool**: Add handler tests to `internal/mcp/tools_test.go` using the existing `mockEVM`/`mockAnchor` types. Add the tool name to `TestE2E_ListTools_ContainsExpectedNames` in `server_test.go`.

**For a new EVM client method**: Add a method to `stubClient` in `tracing_test.go` and `failingClient` in `resilient_test.go`. Add a golden fixture if the method returns a new type. Add integration test in a `_integration_test.go` file with `//go:build integration`.

**For a new anchor method**: Add the method to `mockAnchor` in `tools_test.go` and `mockEVMClient` in `anchor/client_test.go`. Add golden fixture for new types.

**Updating golden files**: Delete the `.golden.json` file, run the test, and it will regenerate. Review the diff before committing.
