# Testing

This document describes the testing strategy and framework for the NVNM Chain MCP Server. Current test results live in CI, not here.

## Overview

The project uses three chain-facing layers with **distinct claims**. Live
client tests and e2e both mine testnet transactions; that overlap is the
fixture (you cannot prove a write lands with a mock), not a duplicate suite.

| Layer | Question it answers | Entry point | Chain | When |
|---|---|---|---|---|
| **Hermetic MCP integration** | Is the advertised MCP surface still the contract we publish? | MCP SDK → in-process HTTP → **mock** chain | No | Every PR (`go test ./...`; first half of `make test-integration`) |
| **Client live tests** | Will this Go client method pack calldata the precompile mines? | `anchor.Client` / `evm.Client` directly | Testnet | `make test-integration` (tagged `*_integration_test.go`) |
| **Deployment e2e** | Does the operator journey still work on the server we shipped? | MCP SDK → **running server** → real chain | Testnet | `make test-e2e` locally; a dedicated CI job is later work |

`TestMCP_Tools` is the MCP tool-regression net
(all 23 tools, mocks). Do not name it `TestE2E_*` and do not put
`//go:build integration` on it. Auth/RBAC samples stay in
`internal/mcp/server_e2e_test.go` (hermetic HTTP). Deployment e2e is only
`tests/e2e`.

The suite runs via `make test`; CI enforces green on every PR. Exact test
counts aren't tracked here — they drift every release. Run `go test ./...`
(or check the CI job output) for current numbers.

CI additionally enforces a **minimum total statement coverage of 80%** on every PR (`scripts/check_coverage.sh`, run after the test step). Reproduce the exact gate locally with `make coverage-check`; the threshold lives in the Makefile (`COVERAGE_THRESHOLD`) and the CI workflow together.

### Purpose and regressions

**Hermetic MCP** (`TestMCP_Tools`). Catches: a
tool listed but never invoked; JSON tag rename / dropped field;
`next_actions` missing or first hint pointing at the wrong tool; prepare
handler wired to the wrong `Prepare*` method (distinct mock calldata);
by-id cursor vs by-name listing envelope. Does **not** catch: ABI argument
order, gas-estimate reverts, EIP-1559 vs legacy packing, deployed-server
config, or HTTP timeout on a large on-chain registry table.

**Client live tests** (build tag `integration`, next to evm/anchor).
Catches: precompile reject of packed calldata; type-2 vs type-0 round-trip;
`updateRecordStatus` encoding (status actually lands on read-back);
grant-then-revoke (and checksum-scoped revoke); RPC not-found abort vs
hang; resilient wrapper against the live RPC. Does **not** catch: MCP
handler field mapping, tool registration, `next_actions`, API-key RBAC, or
a stale deployed binary.

**Deployment e2e** (`TestE2E_HotPath_AnchorDocument`). One operator
journey: onboard → create registry → anchor a record → supersede it →
observe the write. 21 of 23 advertised tools appear as **steps**, not as a
checklist. Catches: deployed auth / write-tools flag / stale binary;
MCP JSON on a real precompile response; by-name listing slower than 60s or
killed by the 90s HTTP wait; read-back of `uri` / `is_latest` /
`registry_id` after a mined write. Does **not** catch: the two tools it
never calls (`anchor_prepare_grant_role`, `anchor_prepare_revoke_role`);
per-tool error/RBAC (hermetic); ABI order (live client tests).

Grant/revoke stay out of the document journey (admin-only MCP role, no
role-read tool). Live client tests already mine both. A later
`TestE2E_HotPath_ShareRegistry` only if a later CI job has an admin key.

### Tool coverage (23 advertised)

Hermetic = invoked as an MCP tool against mocks. Live = corresponding Go
client method against testnet (not an MCP call). E2E = invoked as an MCP
tool on a running server with a real chain.

| Tool | Hermetic MCP | Live client | E2E hot path |
|---|---|---|---|
| `nvnm_overview` | yes | — | onboard |
| `wallet_status` | yes | nonce round-trip | onboard |
| `nvnm_setup_wizard` | yes | — | onboard |
| `nvnm_setup_verify_hash` | yes | — | onboard |
| `nvnm_setup_verify_signature` | yes | — | onboard |
| `evm_get_chain_id` | yes | `ChainID` | onboard |
| `evm_get_block` | yes | `BlockByNumber` | onboard |
| `evm_get_balance` | yes | `BalanceAt` | onboard |
| `anchor_info` | yes | `Info` | onboard |
| `anchor_prepare_add_registry` | yes | prepare + mine | registry |
| `anchor_get_registries` | yes | `GetRegistries` | registry (by name) |
| `anchor_get_registry` | yes | `GetRegistry` | registry |
| `anchor_prepare_add_record` | yes | prepare + mine | record |
| `anchor_get_records` | yes | `GetRecords` | record + lifecycle |
| `evm_send_raw_transaction` | yes | write tests | every write |
| `evm_get_transaction_receipt` | yes | `TransactionReceipt` | confirm |
| `anchor_prepare_update_record_status` | yes | mine + read-back | lifecycle |
| `evm_get_transaction` | yes | `TransactionByHash` | observe |
| `evm_get_logs` | yes | `FilterLogs` | observe |
| `evm_get_code` | yes | `CodeAt` | observe (address only) |
| `evm_call_contract` | yes | `CallContract` | observe |
| `anchor_prepare_grant_role` | yes | prepare + mine | **not called** |
| `anchor_prepare_revoke_role` | yes | grant then revoke | **not called** |

## Running Tests

### Quick Reference

```bash
make test              # Unit + TestMCP_Tools (all 23 tools, no chain)
make test-unit         # Unit tests with -short flag
make test-integration  # TestMCP_Tools, then live client tests (need network)
make test-e2e          # Deployment hot path (NVNM_MCP_TEST_SERVER_URL)
make test-coverage     # Unit tests with race detector + HTML coverage report
make coverage-check    # test-coverage + enforce the 80% total-coverage gate (same check CI runs)
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
| `make test-e2e` | Funded signing key + RPC (in-process) or `NVNM_MCP_TEST_SERVER_URL` | See `tests/e2e/README.md` |
| `make test-load` | k6, running server on `:8080` | `brew install k6` |
| `make docker-smoke` | Docker Desktop | -- |
| `make seed-test-data` | `.chain_credentials.txt` in project root | See below |
| Postgres-backed `internal/mcp` tests | `NVNM_TEST_PG_DSN` env var | See below |

**Credentials file format** (`.chain_credentials.txt`, git-ignored):

```
Address: 0x...
PrivateKey: 0x...
```

Used by integration write tests, `make test-e2e`, and `seed-test-data`.
Tests skip gracefully if the file and `NVNM_TEST_PRIVATE_KEY` are both missing.

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

### 3. Client live tests (live testnet, build tag `integration`)

These sit next to the client code (`internal/evm`, `internal/anchor`). They
are not the MCP tool-regression net — that is section 4. They connect to
the NVNM Chain testnet EVM RPC at `https://evm.testnet.nvnmchain.io`
(chain ID 787111) and are excluded from default `go test ./...` by the
`//go:build integration` build tag.

`make test-integration` runs `TestMCP_Tools` first, then
the tagged live tests in `internal/evm`, `internal/anchor`, and
`internal/mcp` **one package at a time**. Do not replace that with
`go test -tags integration ./...`: packages run in parallel by default,
so a failure in an early package is hidden under later `ok` / `PASS`
lines, and the write tests share one funded key (nonce collisions).
If you invoke `go test` yourself, pass `-p 1 -failfast` and list those
three packages.

| Package | Test file | Tests | What's verified |
|---------|----------|-------|-----------------|
| `internal/evm` | `client_integration_test.go` | 12 | `ChainID`, `GetChainInfo`, `LatestBlockNumber`, `BlockByNumber`, `BlockByHash`, `BalanceAt`, `CodeAt`, `TransactionByHash` (placement fields + not-found), `TransactionReceipt` (mined + not-found abort) |
| `internal/evm` | `resilient_integration_test.go` | 4 | Resilient wrapper: `ChainID`, `GetChainInfo`, `BalanceAt`, `Ping` |
| `internal/evm` | `logs_integration_test.go` | 2 | `FilterLogs` on precompile address (finds real logs), empty-range query |
| `internal/anchor` | `client_integration_test.go` | 6 | `Info`, `GetRegistries`, `GetRegistry` (by ID), `GetRecords` |
| `internal/anchor` | `write_integration_test.go` | 3 | Prepare-sign-submit for `AddRegistry`, `AddRecord`, `GrantRole` |
| `internal/anchor` | `prepare_integration_test.go` | 2 | `PrepareAddRegistry` round-trips: EIP-1559 (type-2 default) and legacy (type-0 opt-out) |
| `internal/anchor` | `prepare_rolestatus_integration_test.go` | 3 | Prepare-sign-submit for the methods: `UpdateRecordStatus` (record read back to confirm the status change landed) and `RevokeRole` (grant-then-revoke, plus the checksum-scoped pair) |
| `internal/mcp` | `wallet_status_integration_test.go` | 1 | `eth_account` round-trip: `wallet_status` before → `PrepareAddRegistry` → sign → broadcast → receipt → `wallet_status` reflects the new nonce |
| `internal/evm` | `call_integration_test.go` | 1 | `CallContract` against a known EOA with empty calldata returns empty data |

Write and round-trip integration tests require testnet credentials,
resolved environment-first then file: `NVNM_TEST_PRIVATE_KEY` (optional
`NVNM_TEST_ADDRESS`), then `.chain_credentials.txt`. They skip if both
are absent. The wallet must be funded: these tests broadcast real
transactions and wait for receipts.

The anchor read tests depend on a stable registry named `mcp-test-data` (one registry, three records) seeded by `cmd/seed-test-data`. Re-run that command against a fresh testnet before running the anchor integration suite.

Since the anchoring precompile keys registries by numeric ID only, `cmd/seed-test-data` resolves `mcp-test-data` to its `registry_id` by scanning `GetRegistries` for an exact name match client-side (there is no on-chain by-name lookup); it reuses the existing registry if that scan finds one, otherwise it creates a new one. Every downstream call in the script, and in the anchor read tests, then operates on that numeric ID.

**`count_total` behavioral note.** The `nvnm-testnet-1` anchor precompile returns `pagination.total = 0` for `registries` and `records` queries even though the client sets `countTotal: true`. The registry/record rows themselves decode correctly; only the count is unpopulated. The integration tests therefore assert on the returned slice length, not on `pagination.total`. MCP tool responses surface whatever the chain returns for `total`, so a downstream consumer should treat it as best-effort, not authoritative, on this network.

### 3b. Deployment e2e (`tests/e2e`, tag `e2e`)

Hot-path check against a **running MCP server** — typically a deployment.
Set `NVNM_MCP_TEST_SERVER_URL`. `make test-e2e` runs
`TestE2E_HotPath_AnchorDocument` only: onboard → create registry →
anchor a record → supersede it → observe the write through EVM tools.
Registry read-back uses `anchor_get_registries` by name (full-table
scan; HTTP wait 90s, fail if slower than 60s) then
`anchor_get_registry` by id. Record read-back asserts `uri`,
`is_latest`, and `registry_id`. Grant/revoke are not in this journey
(admin-only MCP role, no role-read tool). Decode uses published JSON
field names so a read/prepare contract change fails. It is not the
all-tools net; that is `TestMCP_Tools`.

Run locally with `make test-e2e`. Wiring a dedicated GitHub Action is later
work; it is not a pull-request gate.

| Target | How |
|---|---|
| Deployment (intended) | `NVNM_MCP_TEST_SERVER_URL=https://...` |
| In-process fallback | URL unset; needs `NVNM_EVM_RPC_URL` |

Credentials: `NVNM_TEST_PRIVATE_KEY` then `.chain_credentials.txt`. `NVNM_E2E_REQUIRE_CHAIN=1` turns skip into fail.

See `tests/e2e/README.md`.

**Where a new test belongs**

| What you are pinning | Layer |
|---|---|
| Branch / error-path coverage | Unit tests (`internal/*_test.go`) |
| Response JSON shape | Golden files + schema tests |
| Envelope, schema, `next_actions`, all 23 tools without a chain | Hermetic MCP: `TestMCP_Tools` |
| Precompile packing, one client method vs testnet | Live `*_integration_test.go` next to evm/anchor |
| Operator path on a deployed server | `tests/e2e` journey stage — not a per-tool e2e file |
| Auth / RBAC denials | `internal/mcp/server_e2e_test.go` (hermetic) |

### 4. MCP integration tests (hermetic)

These stand up a real MCP HTTP server with **mock** chain clients and drive
it through the official SDK client. No RPC, no wallet. This is the
tool-regression net.

**MCP tools** (`mcp_tools_test.go`):

| Test | What's verified |
|------|-----------------|
| `TestMCP_Tools` | Every name in `tools/list` invoked through the SDK with mocks; published JSON field names (local wire types); `next_actions` required and first hint pinned; `0x` addresses, 64-char checksum, status enum; type-2 unsigned tx + distinct per-prepare calldata + `wallet_tx_request` hex quantities; by-id cursor and by-name listing; coverage assert vs `tools/list` |

Transport, auth, and RBAC samples stay in `server_test.go` / `server_e2e_test.go`.
Those files still use historical `TestE2E_*` names; they are hermetic HTTP
tests, not the deployment suite in `tests/e2e`.

**Registration samples** (`server_test.go`):

| Test | What's verified |
|------|-----------------|
| `TestE2E_ListTools_Returns23` | Server registers exactly 23 tools (5 onboarding + 8 EVM reads + 4 anchor reads + 5 anchor writes + 1 relay write) |
| `TestE2E_ListTools_ContainsExpectedNames` | Every expected tool name is present |
| `TestE2E_CallTool_InvalidAddress` | `evm_get_balance` with bad address returns `IsError=true` |
| `TestE2E_CallTool_MissingRegistryID` | `anchor_get_registry` with no args returns `IsError=true` |

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

The k6 script (`tests/load/k6_mcp_http.js`) exercises the MCP Streamable HTTP endpoint with four scenarios:

| Scenario | Executor | VUs | Duration | Tools exercised |
|----------|----------|-----|----------|-----------------|
| `constant_reads` | constant-vus | 10 | 2 min | `evm_get_chain_id` |
| `burst_reads` | ramping-vus | 0 → 50 → 0 | 3 min | `evm_get_chain_id` |
| `mixed_workload` | constant-vus | 15 | 2 min | `evm_get_chain_id`, `evm_get_block`, `anchor_get_registries` |
| `hot_path` | constant-vus | 1 | 1 min | Same discover / optional prepare / read-back steps as `TestE2E_HotPath_AnchorDocument`. Set `HOTPATH_FROM` to exercise `wallet_status` + `anchor_prepare_add_registry`. Does not broadcast. |

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
│  Unit Tests │ Golden Tests  │  MCP integration (all 23 tools)   │
│  (mocks)    │ (JSON shapes) │  SDK + httptest + mock chain      │
│             │               │  PR / pipeline tool-regression    │
├─────────────┴───────────────┴───────────────────────────────────┤
│         Client live tests (testnet, tag integration)            │
├─────────────────────────────────────────────────────────────────┤
│         E2E hot path vs deployment (tests/e2e, tag e2e)         │
│         NVNM_MCP_TEST_SERVER_URL; run locally (CI job later)    │
├─────────────────────────────────────────────────────────────────┤
│              k6 Load Tests (HTTP transport)                     │
├─────────────────────────────────────────────────────────────────┤
│              Docker Smoke Test (container lifecycle)            │
│              build → start → healthz → readyz → stop           │
└─────────────────────────────────────────────────────────────────┘
```

## Adding New Tests

**For a new MCP tool**: Add handler tests to `internal/mcp/tools_test.go` using the existing `mockEVM`/`mockAnchor` types. Add the tool name to `TestE2E_ListTools_ContainsExpectedNames` in `server_test.go`. Add a mocked SDK invocation to `TestMCP_Tools` in `mcp_tools_test.go` — a tool that is listed but not invoked fails the coverage subtest. Add a tagged `*_integration_test.go` next to the client code when the change crosses the live-testnet boundary. Do not add a live all-tools MCP file; client-layer tests already cover chain facts. Deployment hot-path stays in `tests/e2e`.

**For write path or auth features**: Add E2E tests to `internal/mcp/server_e2e_test.go`. Use `startTestServerWithConfig` for write-path tests. Use `startAuthTestServer` for auth tests (configurable `KeyEntry` list and Bearer token via `bearerTransport`). Use `buildSignedTxHex` to generate real signed transactions for write path tests.

**For FusionAuth-related code**: Add unit tests to `internal/auth/auth_test.go` for JWT/JWKS validation (issuer matching, audience checks, expiry, role extraction, app-scoped roles, signature failures). The existing tests use `httptest.NewServer` to serve a JWKS endpoint and `golang-jwt/jwt/v5` to construct test tokens with controlled keys.

**For admin key management**: Add tests to `internal/mcp/admin_test.go` for API endpoint tests (use `startAdminTestServer` and `adminRequest` helpers). Add tests to `internal/mcp/managed_keys_test.go` for `ManagedKeyStore` CRUD operations (use `tempKeysFile` helper for isolated test files).

**For a new EVM client method**: Add a method to `stubClient` in `tracing_test.go` and `failingClient` in `resilient_test.go`. Add a golden fixture if the method returns a new type. Add integration test in a `_integration_test.go` file with `//go:build integration`.

**For a new anchor method**: Add the method to `mockAnchor` in `tools_test.go` and `mockEVMClient` in `anchor/client_test.go`. Add golden fixture for new types.

**Updating golden files**: Delete the `.golden.json` file, run the test, and it will regenerate. Review the diff before committing.
