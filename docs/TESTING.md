# Testing

This document describes the testing strategy and framework for the NVNM Chain MCP Server. Current test results live in CI, not here.

## Overview

The project uses a layered testing approach: unit tests with mocks for fast feedback, golden tests for response shape stability, integration tests against the live NVNM testnet, HTTP end-to-end tests through the MCP protocol layer, k6 load tests for performance, and Docker smoke tests for deployment verification.

The suite runs via `make test`; CI enforces green on every PR. Exact test counts aren't tracked here — they drift every release. Run `go test ./...` (or check the CI job output) for current numbers.

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
| `make test-integration` | Network access to `https://evm.testnet.nvnmchain.io` | -- |
| `make test-load` | k6, running server on `:8080` | `brew install k6` |
| `make docker-smoke` | Docker Desktop | -- |
| `make seed-test-data` | `.chain_credentials.txt` in project root | See below |
| Postgres-backed `internal/mcp` tests | `NVNM_TEST_PG_DSN` env var | See below |

**Credentials file format** (`.chain_credentials.txt`, git-ignored):

```
Address: 0x...
PrivateKey: 0x...
```

Used by integration write tests and `seed-test-data`. Tests skip gracefully if the file is missing.

**Postgres-backed tests.** A subset of `internal/mcp` tests exercise the audit log, signer-quota, signer-blacklist, write-audit, and migration surface against a real Postgres database, gated on `NVNM_TEST_PG_DSN` (e.g. `postgres://.../nvnm?sslmode=disable`). They call `t.Skip` cleanly when the variable is unset, so `make test` passes without it — set `NVNM_TEST_PG_DSN` to actually exercise that surface.

## Test Layers

Note: a subset of `internal/mcp` unit tests are Postgres-backed and gated on `NVNM_TEST_PG_DSN` (see Prerequisites above); they skip cleanly when it's unset.

### 1. Unit Tests (no network, no build tags)

Fast, deterministic tests using mocks and stubs. Run with `make test`.

- **`internal/mcp`** — MCP tool dispatch (EVM + anchor read/write handlers, onboarding tools), HTTP/E2E protocol server, API key auth, RBAC/default-deny, admin key-management API + hot reload, `ManagedKeyStore` CRUD, write-audit + admin audit logging, signer quota/blacklist, Postgres-backed store and migrations, fail-loud legacy-config migration guards.
- **`internal/auth`** — API-key hashing/validation (including legacy-hash back-compat), FusionAuth JWT/JWKS validation (issuer/audience/expiry/role extraction, app-scoped roles), claims propagation via context.
- **`internal/config`** — environment variable loading, defaults, validation errors, resilience config, fail-loud migration guard for removed legacy settings.
- **`internal/errors`** — sentinel error distinctness and classification helpers (`IsInputError`, `IsTransientError`, `IsNotFound`).
- **`internal/evm`** — tracing client delegation, resilient wrapper (retry/backoff, circuit breaker, rate limiting, non-retry on send).
- **`internal/anchor`** — client construction/ABI loading, mock-based query methods, prepare-transaction validation and gas buffering.
- **`internal/telemetry`** — `/healthz` and `/readyz` endpoints, MCP middleware, request ID and tool-name extraction, metric instruments.
- **`internal/logging`** — logger creation, JSON output, level filtering, dual-handler fanout, address/URL/data redaction.

See each package's `*_test.go` files for the current set of test functions and cases; this list intentionally omits per-file counts since they drift every release.

**Mock types** used across unit tests:

- `mockEVM` (`internal/mcp/tools_test.go`) -- full `evm.Client` implementation with configurable return values
- `mockAnchor` (`internal/mcp/tools_test.go`) -- full `anchor.Client` implementation
- `stubClient` (`internal/evm/tracing_test.go`) -- minimal `evm.Client` stub
- `failingClient` (`internal/evm/resilient_test.go`) -- `evm.Client` that fails N times then succeeds
- `mockEVMClient` (`internal/anchor/client_test.go`) -- `evm.Client` for anchor-layer tests
- `mockChecker` (`internal/telemetry/health_test.go`) -- readiness probe mock
- `bearerTransport` (`internal/mcp/server_e2e_test.go`) -- `http.RoundTripper` that injects `Authorization: Bearer` headers for API key auth E2E tests

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

Integration tests connect to the NVNM Chain testnet EVM RPC at `https://evm.testnet.nvnmchain.io` (chain ID 787111). They are excluded from default `go test ./...` by the `//go:build integration` build tag.

Run with: `make test-integration` or `go test -tags integration ./...`

| Package | Test file | Tests | What's verified |
|---------|----------|-------|-----------------|
| `internal/evm` | `client_integration_test.go` | 8 | `ChainID`, `GetChainInfo`, `LatestBlockNumber`, `BlockByNumber`, `BlockByHash`, `BalanceAt`, `CodeAt` |
| `internal/evm` | `resilient_integration_test.go` | 4 | Resilient wrapper: `ChainID`, `GetChainInfo`, `BalanceAt`, `Ping` |
| `internal/evm` | `logs_integration_test.go` | 2 | `FilterLogs` on precompile address (finds real logs), empty-range query |
| `internal/evm` | `call_integration_test.go` | 2 | `CallContract` against precompile (empty data error path), non-existent address |
| `internal/anchor` | `client_integration_test.go` | 6 | `Info`, `GetRegistries`, `GetRegistry` (by ID/name), `GetRecords` |
| `internal/anchor` | `write_integration_test.go` | 3 | Prepare-sign-submit for `AddRegistry`, `AddRecord`, `GrantRole` |
| `internal/anchor` | `prepare_integration_test.go` | 2 | `PrepareAddRegistry` round-trips: EIP-1559 (type-2 default) and legacy (type-0 opt-out) |
| `internal/mcp` | `wallet_status_integration_test.go` | 1 | `eth_account` round-trip: `wallet_status` before → `PrepareAddRegistry` → sign → broadcast → receipt → `wallet_status` reflects the new nonce |

Write and round-trip integration tests require testnet credentials --
`.chain_credentials.txt` (`write_integration_test.go`,
`wallet_status_integration_test.go`) or `NVNM_TEST_PRIVATE_KEY` from
`.env` (`prepare_integration_test.go`) -- and skip if absent.

The anchor read tests depend on a stable registry named `mcp-test-data` (one registry, three records) seeded by `cmd/seed-test-data`. Re-run that command against a fresh testnet before running the anchor integration suite.

**`count_total` behavioral note.** The `nvnm-testnet-1` anchor precompile returns `pagination.total = 0` for `registries` and `records` queries even though the client sets `countTotal: true`. The registry/record rows themselves decode correctly; only the count is unpopulated. The integration tests therefore assert on the returned slice length, not on `pagination.total`. MCP tool responses surface whatever the chain returns for `total`, so a downstream consumer should treat it as best-effort, not authoritative, on this network.

### 4. MCP End-to-End HTTP Tests

These tests spin up a real MCP HTTP server using `httptest.NewServer` with mock clients, then connect using the official MCP SDK client (`mcp.NewClient` + `StreamableClientTransport`). Tests are split across `server_test.go` (basic tool registration and calls) and `server_e2e_test.go` (write path, API key auth, stateless behavior).

**Basic E2E** (`server_test.go`):

| Test | What's verified |
|------|-----------------|
| `TestE2E_ListTools_Returns21` | Server registers exactly 21 tools (5 onboarding + 8 EVM reads + 4 anchor reads + 4 writes) |
| `TestE2E_ListTools_ContainsExpectedNames` | Every expected tool name is present |
| `TestE2E_CallTool_ChainID` | `evm_get_chain_id` returns non-error structured content |
| `TestE2E_CallTool_AnchorInfo` | `anchor_info` returns non-error structured content |
| `TestE2E_CallTool_InvalidAddress` | `evm_get_balance` with bad address returns `IsError=true` |
| `TestE2E_CallTool_MissingRegistryIDAndName` | `anchor_get_registry` with no args returns `IsError=true` |

**Write path E2E** (`server_e2e_test.go`):

| Test | What's verified |
|------|-----------------|
| `TestE2E_SendRawTx_DirectBroadcast_NoElicitation` | Writer key broadcasts directly; no elicitation round-trip; RPC result returned |

**API key authentication E2E** (`server_e2e_test.go`):

| Test | What's verified |
|------|-----------------|
| `TestE2E_Auth_ValidKey_ToolCallSucceeds` | Valid Bearer token grants access |
| `TestE2E_Auth_InvalidKey_ConnectionFails` | Wrong Bearer token rejected |
| `TestE2E_Auth_MissingKey_ConnectionFails` | Missing Authorization header rejected |
| `TestE2E_Auth_DisabledKey_ConnectionFails` | Disabled key rejected (while active keys exist) |
| `TestE2E_Auth_NoKeysConfigured_NoAuthRequired` | No keys configured = auth bypassed |

**RBAC / default-deny E2E** (`server_e2e_test.go`):

| Test | What's verified |
|------|-----------------|
| `TestE2E_RBAC_ReaderCannotCallWriteTool` | Key with `reader` role is denied on a write tool (`evm_send_raw_transaction`) |
| `TestE2E_RBAC_NoRolesDeniedAll` | Authenticated key with no roles is denied all tools (default-deny: no roles = no access) |
| `TestE2E_RBAC_GrantRoleRequiresAdmin` | Key with `writer` role is denied `anchor_prepare_grant_role` (requires `admin`) |

**Stateless handler E2E** (`server_e2e_test.go`):

| Test | What's verified |
|------|-----------------|
| `TestE2E_StatelessHandler_ServesUnknownSession` | Stateless handler (`Stateless: true`) serves a request with an unknown session ID without error, confirming no per-pod session map is required |

**Fail-loud migration E2E** (`server_e2e_test.go` / `config_test.go` / `managed_keys_test.go`):

| Test | What's verified |
|------|-----------------|
| `TestLoad_RejectsLegacyWriteApprovalDefault` | `WRITE_APPROVAL_DEFAULT` in environment causes startup failure with `ErrLegacyWriteApproval` |
| `TestLoadKeysFile_RejectsLegacyWriteApproval` | Key store entry carrying `write_approval` field causes load failure with `ErrLegacyKeyWriteApproval` |

This layer validates: HTTP transport, SSE/JSON response framing, MCP session management, JSON-RPC 2.0 envelope, tool registration, error propagation, Bearer token authentication with `AuthMiddleware` (API key and FusionAuth providers), stateless handler behavior, fail-loud migration guards, and client identity propagation.

**Admin key management E2E** (`admin_test.go`):

| Test | What's verified |
|------|-----------------|
| `TestAdmin_Auth_MissingToken` | Unauthenticated request returns 401 |
| `TestAdmin_Auth_InvalidToken` | Wrong admin key returns 403 |
| `TestAdmin_Auth_ValidToken` | Correct admin key grants access |
| `TestAdmin_Create_Success` | Key creation returns raw key, correct metadata |
| `TestAdmin_Create_Duplicate` | Duplicate client ID returns 409 |
| `TestAdmin_Create_MissingClientID` | Missing client_id returns 400 |
| `TestAdmin_List_Empty` | Empty store returns `[]` |
| `TestAdmin_List_WithKeys` | Listed keys are redacted (no raw keys) |
| `TestAdmin_Update_DisableAndEnable` | Disable/enable via PATCH affects Lookup |
| `TestAdmin_Update_NotFound` | PATCH for unknown client returns 404 |
| `TestAdmin_Update_EmptyBody` | PATCH with no fields returns 400 |
| `TestAdmin_Delete_Success` | DELETE removes key |
| `TestAdmin_Delete_NotFound` | DELETE for unknown client returns 404 |
| `TestAdmin_FullLifecycle` | Create → list → disable → enable → delete (immediate effect at each step) |
| `TestAdmin_HotReload_CreatedKeyImmediatelyUsable` | Key created via admin API is immediately findable by `ManagedKeyStore.Lookup` |
| `TestAdmin_HotReload_DisabledKeyImmediatelyRejected` | Key disabled via admin API is immediately nil on `ManagedKeyStore.Lookup` |

**ManagedKeyStore unit tests** (`managed_keys_test.go`):

| Test | What's verified |
|------|-----------------|
| `TestManagedKeyStore_CreateAndLookup` | Create returns key, Lookup finds it |
| `TestManagedKeyStore_CreateDuplicate` | Duplicate client ID returns error |
| `TestManagedKeyStore_List` | List returns summaries with redacted key prefixes |
| `TestManagedKeyStore_UpdateEnabled` | Disable → Lookup=nil, enable → Lookup=entry |
| `TestManagedKeyStore_UpdateMissing` | Update for unknown client returns error |
| `TestManagedKeyStore_Delete` | Delete removes from store and file |
| `TestManagedKeyStore_DeleteMissing` | Delete for unknown client returns error |
| `TestManagedKeyStore_PersistenceAcrossReloads` | Key survives NewManagedKeyStore from same file |
| `TestManagedKeyStore_Counters` | ActiveCount/TotalCount track enable/disable |
| `TestManagedKeyStore_EmptyOnNewFile` | New file → Empty()=true, TotalCount=0 |
| `TestManagedKeyStore_FilePermissions` | Keys file written with 0600 permissions |

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

## Test Results

Current results live in CI — see the GitHub Actions run for this branch/PR for pass/fail status, coverage, and timing. This document doesn't hand-maintain a results snapshot because it goes stale every release.

## Test Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    Test Layers                                  │
├─────────────┬───────────────┬───────────────────────────────────┤
│  Unit Tests │ Golden Tests  │  MCP E2E HTTP Tests               │
│  (mocks)    │ (JSON shapes) │  (httptest + SDK client)          │
├─────────────┴───────────────┴───────────────────────────────────┤
│              Integration Tests (live testnet)                   │
│              build tag: integration                             │
├─────────────────────────────────────────────────────────────────┤
│              k6 Load Tests (HTTP transport)                     │
├─────────────────────────────────────────────────────────────────┤
│              Docker Smoke Test (container lifecycle)            │
│              build → start → healthz → readyz → stop           │
└─────────────────────────────────────────────────────────────────┘
```

## Adding New Tests

**For a new MCP tool**: Add handler tests to `internal/mcp/tools_test.go` using the existing `mockEVM`/`mockAnchor` types. Add the tool name to `TestE2E_ListTools_ContainsExpectedNames` in `server_test.go`.

**For write path or auth features**: Add E2E tests to `internal/mcp/server_e2e_test.go`. Use `startTestServerWithConfig` for write-path tests. Use `startAuthTestServer` for auth tests (configurable `KeyEntry` list and Bearer token via `bearerTransport`). Use `buildSignedTxHex` to generate real signed transactions for write path tests.

**For FusionAuth-related code**: Add unit tests to `internal/auth/auth_test.go` for JWT/JWKS validation (issuer matching, audience checks, expiry, role extraction, app-scoped roles, signature failures). The existing tests use `httptest.NewServer` to serve a JWKS endpoint and `golang-jwt/jwt/v5` to construct test tokens with controlled keys.

**For admin key management**: Add tests to `internal/mcp/admin_test.go` for API endpoint tests (use `startAdminTestServer` and `adminRequest` helpers). Add tests to `internal/mcp/managed_keys_test.go` for `ManagedKeyStore` CRUD operations (use `tempKeysFile` helper for isolated test files).

**For a new EVM client method**: Add a method to `stubClient` in `tracing_test.go` and `failingClient` in `resilient_test.go`. Add a golden fixture if the method returns a new type. Add integration test in a `_integration_test.go` file with `//go:build integration`.

**For a new anchor method**: Add the method to `mockAnchor` in `tools_test.go` and `mockEVMClient` in `anchor/client_test.go`. Add golden fixture for new types.

**Updating golden files**: Delete the `.golden.json` file, run the test, and it will regenerate. Review the diff before committing.
