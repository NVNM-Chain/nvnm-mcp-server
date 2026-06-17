# NVNM Chain MCP Server -- Design & Architecture

## 1. System Context

The NVNM Chain MCP Server sits between MCP-capable clients (LLMs, developer tools, agents) and the NVNM Chain (Inveniam's L2 on MANTRA). It translates high-level tool calls into EVM JSON-RPC requests, normalizes the responses, and returns structured, typed JSON.

For write operations, the server constructs unsigned transactions but never holds private keys. Signing is the caller's responsibility.

### Target Chain

NVNM Chain is Inveniam's Layer 2 blockchain, secured by MANTRA's validator set through Interchain Security (ICS). It is purpose-built for document anchoring and provenance verification. The chain runs as two networks -- testnet and mainnet -- and this server is deployed as one instance per network (see § 8 Multi-chain).

| Property | Testnet | Mainnet |
|---|---|---|
| Cosmos chain ID | `nvnm-testnet-1` | `nvnm-1` |
| EVM chain ID | `787111` (`0xc02a7`) | `1611` (`0x64b`) |
| Native token | `mantraUSD` | `mmUSD` |
| Gas token (EVM, wrapped) | `wmantraUSD` | `wmmUSD` |
| EVM RPC | `https://evm.testnet.nvnmchain.io` | `https://evm.nvnmchain.io` |
| Cosmos RPC | `https://rpc.testnet.nvnmchain.io` | `https://rpc.nvnmchain.io` |
| EVM explorer | `https://explorer.evm.testnet.nvnmchain.io` | `https://evm.explorer.nvnmchain.io` |
| MCP service (hosted) | `https://mcp-testnet.nvnmchain.io` | `https://mcp.nvnmchain.io` |
| Anchor precompile | `0x0000000000000000000000000000000000000A00` | `0x0000000000000000000000000000000000000A00` |

> **Privacy-by-design.** The chain stores only hash fingerprints of data (one-way SHA-256), never the underlying data itself. Anchor writes are publicly observable: `add_record` logs the SHA-256 hash and `add_registry` logs the registry name in public on-chain logs. A hash is one-way, so the underlying document is not derivable — counterparties exchange the file off-band and verify by recomputing the hash; encode or salt sensitive registry names before anchoring. The chain is a neutral notary that never reads documents.
>
> *Design implication for this server:* the onboarding wizard and `wallet_status` surface only activity-level signals (e.g., "has this wallet sent any transactions?") and never content-level signals (e.g., "which registries did it touch"). Decoding receipts is out of scope; it would expose nothing beyond what the public logs already carry (the hash and registry name).

```
                                          ┌─────────────────────┐
                                          │  MCP Client / Agent │
                                          │                     │
                                          │  1. Request prepare │
                                          │  2. Sign locally    │
                                          │  3. Submit signed   │
                                          └─────────┬───────────┘
                                                    │
                                              MCP (stdio/HTTP)
                                                    │
                                          ┌─────────▼───────────┐
                                          │  Inveniam MCP       │
                                          │  Server             │
                                          │                     │
                                          │  • ABI encoding     │
                                          │  • Tx construction  │
                                          │  • Response norm.   │
                                          │  • NO private keys  │
                                          └─────────┬───────────┘
                                                    │
                                              JSON-RPC (HTTPS)
                                                    │
                                          ┌─────────▼───────────┐
                                          │  NVNM Chain (EVM) │
                                          │  (RPC + Precompile) │
                                          └─────────────────────┘
```

### Goals

1. Expose a curated, typed MCP tool surface -- not raw JSON-RPC passthrough
2. Normalize all responses into consistent, documented shapes
3. Cleanly separate generic EVM logic from Inveniam-specific anchor logic
4. Support both local development (stdio) and production deployment (HTTP)
5. Handle all blockchain complexity (ABI, gas, nonces) so callers don't need web3 libraries
6. Never hold or require private keys -- signing is always external

### Non-Goals (v1)

- Cosmos API integration
- WebSocket subscriptions
- Multi-chain routing
- Automatic ABI discovery
- Explorer/Blockscout as data source
- Server-side transaction signing or key management

## 2. Architecture

### Package Dependency Graph

```
cmd/nvnm-mcp-server/main.go
    │
    ├── internal/config      (env loading, validation)
    ├── internal/logging     (slog wrapper, redaction helpers)
    ├── internal/telemetry   (OTel providers, MCP middleware, health server, metrics)
    ├── internal/evm         (defiweb/go-eth RPC wrapper, normalized types, tracing wrapper)
    ├── internal/anchor      (anchor adapter, prepare-sign-submit, address validation)
    ├── internal/auth         (claims, token validation, FusionAuth JWT, API key adapter)
    ├── internal/mcp         (MCP server, tool handlers, auth middleware, key store)
    └── internal/version     (canonical version constant)
            │
            ├── internal/evm
            ├── internal/anchor
            ├── internal/auth
            └── internal/errors

cmd/key-mgmt/main.go
    │
    └── internal/mcp         (KeyStore, key file I/O)
```

Key constraint: `internal/evm` knows nothing about anchors. `internal/anchor` knows nothing about MCP. `internal/mcp` orchestrates both. `internal/auth` owns authentication: unified `Claims` type, `TokenValidator` interface, FusionAuth JWT/JWKS validation, and API key validation adapter. It is depended on by `internal/mcp` (middleware) and `internal/telemetry` (client identity in spans) without import cycles.

### Package Responsibilities

#### `cmd/nvnm-mcp-server`

Application entrypoint. Responsibilities:
- Parse CLI flags (`--transport`)
- Load and validate configuration
- Initialize logger
- Initialize OpenTelemetry providers and MCP middleware
- Construct EVM client (with tracing wrapper) and anchor client
- Start health/metrics server on `:9090`
- Register MCP tools with telemetry middleware
- Select and start MCP transport
- Handle graceful shutdown on SIGINT/SIGTERM (including telemetry flush)

#### `internal/config`

Environment-based configuration loading and validation.

- Reads from `os.Getenv`; no config files, no frameworks
- `Config` struct with typed fields
- `Load()` function returns `(*Config, error)` -- fails fast on missing required fields
- `Validate()` checks invariants (chain ID > 0, timeout > 0, etc.)
- Safe defaults only for non-sensitive settings (timeout, log level)

#### `internal/logging`

Wrapper over `log/slog` (Go stdlib) with redaction utilities.

- `New(level string) *slog.Logger` -- creates a configured JSON logger
- `NewText(level string)` -- text logger for local development
- Redaction helpers: `SafeAddr` (truncate addresses), `SafeURL` (hostname only), `SafeTxData` (length only)
- Fanout handler utility for duplicating log records to multiple handlers

#### `internal/errors`

Shared sentinel errors and error classification.

Sentinel errors:
- `ErrInvalidAddress` -- malformed Ethereum address
- `ErrInvalidBlockRef` -- malformed block number or hash
- `ErrInvalidTxHash` -- malformed transaction hash
- `ErrBlockNotFound` -- block does not exist
- `ErrTxNotFound` -- transaction not found
- `ErrContractCallFailed` -- eth_call reverted or failed
- `ErrRegistryNotFound` -- registry not found
- `ErrRecordNotFound` -- anchor record not found
- `ErrAnchorABIMissing` -- anchor precompile ABI not loaded
- `ErrWriteDisabled` -- write tools not enabled
- `ErrUpstreamRPC` -- upstream RPC error (timeout, rate limit, etc.)

Error taxonomy (for MCP responses):
1. **Input errors** -- malformed address, hash, block ref
2. **Not-found errors** -- tx, block, anchor not found
3. **Upstream errors** -- RPC timeout, rate limit, method not supported
4. **Internal errors** -- ABI decode failure, normalization bug

#### `internal/evm`

Generic EVM JSON-RPC client layer. Wraps `defiweb/go-eth` (MIT-licensed).

Interface:

```go
type Client interface {
    // Read methods
    ChainID(ctx context.Context) (*big.Int, error)
    LatestBlockNumber(ctx context.Context) (uint64, error)
    GetChainInfo(ctx context.Context) (*ChainInfo, error)
    BlockByNumber(ctx context.Context, number *big.Int, fullTx bool) (*NormalizedBlock, error)
    BlockByHash(ctx context.Context, hash common.Hash, fullTx bool) (*NormalizedBlock, error)
    TransactionByHash(ctx context.Context, hash common.Hash) (*NormalizedTransaction, error)
    TransactionReceipt(ctx context.Context, hash common.Hash) (*NormalizedReceipt, error)
    BalanceAt(ctx context.Context, address common.Address, block *big.Int) (*NormalizedBalance, error)
    CodeAt(ctx context.Context, address common.Address, block *big.Int) (*CodeResult, error)
    CallContract(ctx context.Context, msg ethereum.CallMsg, block *big.Int) ([]byte, error)
    FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]NormalizedLog, error)

    // Write support methods
    PendingNonceAt(ctx context.Context, address common.Address) (uint64, error)
    SuggestGasPrice(ctx context.Context) (*big.Int, error)
    EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error)
    SendRawTransaction(ctx context.Context, signedTxHex string) (string, error)

    Ping(ctx context.Context) error // readiness probe
    Close()
}
```

Design decisions:
- Methods return **normalized types** directly, not raw upstream RPC types. Normalization happens at the EVM layer boundary.
- The client wraps a `defiweb/go-eth` RPC transport and holds a configured timeout.
- A **tracing wrapper** (`NewTracingClient`) implements the same `Client` interface, adding OTel spans and duration/error metrics to every RPC call without modifying the core client.
- If an archive RPC URL is configured, a second client is available for historical queries.

#### `internal/anchor`

Inveniam-specific anchoring adapter targeting the EVM precompile at `0x0000000000000000000000000000000000000A00`.

The anchoring module provides a framework for creating and managing decentralized registries of data records. It allows users to "anchor" data by storing a checksum and metadata, creating an immutable, timestamped history of records. The module features a granular Role-Based Access Control (RBAC) system.

**Data model:**

- **Registry** -- a logical container for records (id, name, description, creator, createdAt, metadata). The creator automatically becomes admin.
- **Record** -- an anchored data entry with versioning. Multiple records can share the same RecordID but differ by Index (version number). Fields: registry, recordId, index, checksum, checksumAlgo, uri, status, isLatest, timestamp, metadata.
- **RBAC** -- hierarchical permission system: `admin` (full control) and `editor` (add/update records). Scopes: Record -> Registry -> Global.

Interface:

```go
type Client interface {
    Info() PrecompileInfo
    Available() bool

    // Read methods
    GetRegistry(ctx context.Context, req GetRegistryRequest) (*Registry, error)
    GetRegistries(ctx context.Context, req GetRegistriesRequest) (*GetRegistriesResponse, error)
    GetRecords(ctx context.Context, req GetRecordsRequest) (*GetRecordsResponse, error)

    // Write preparation (prepare-sign-submit pattern)
    PrepareAddRegistry(ctx context.Context, req PrepareAddRegistryRequest) (*UnsignedTransaction, error)
    PrepareAddRecord(ctx context.Context, req PrepareAddRecordRequest) (*UnsignedTransaction, error)
    PrepareGrantRole(ctx context.Context, req PrepareGrantRoleRequest) (*UnsignedTransaction, error)
}
```

Design decisions:
- The precompile address is known (`0x...0A00`) and defaulted in config.
- ABI is loaded from a JSON file at startup via `ANCHOR_ABI_PATH`. If not set, anchor tools are registered but return informative errors.
- The client uses `defiweb/go-eth`'s ABI pack/unpack to encode `eth_call` requests against the precompile.
- Write preparation methods return `UnsignedTransaction` containing the fully constructed transaction (with nonce, gas, chain ID) but no signature. Signing is the caller's responsibility.
- The `Available()` method allows MCP tools to provide clear status about whether the ABI is loaded.

#### `internal/mcp`

MCP tool registration and request handling using the official Go MCP SDK.

Responsibilities:
- Construct `mcp.Server` with tool definitions (`server.go`, `tools_evm.go`, `tools_anchor.go`, `tools_evm_write.go`, `tools_anchor_write.go`)
- Each tool handler: validate input -> call EVM/anchor client -> return normalized result wrapped in a `next_actions`-bearing envelope (`envelopes.go`, `next_actions.go`)
- Tool annotations (`annotations.go`) -- every tool carries an MCP `ToolAnnotations` payload (ReadOnly / Destructive / OpenWorld hints) so clients can tell read-only from state-changing tools without inferring spec defaults
- Map internal errors to MCP-safe responses (human-readable message + error flag)
- HTTP middleware chain (outer → inner): `originGuard` → `limitRequestBody` → `AuthMiddleware` → `ClientRateLimiter` → `mcpHandler`. Origin guard sits outermost so disallowed requests short-circuit before auth/rate-limit work runs.
- Origin-header validation (`origin.go`) -- DNS-rebinding defense per the MCP spec; allowlist via `NVNM_ALLOWED_ORIGINS`. Empty Origin (server-to-server, CLI) always passes; localhost variants accept any port.
- HTTP authentication middleware (`auth.go`) -- delegates to `internal/auth.TokenValidator` (API key or FusionAuth JWT)
- Per-client token-bucket rate limiting (`ratelimit.go`) -- returns HTTP `429` when exceeded
- Per-tool role-based authorization (`rbac.go`) -- gates each handler on `reader` / `writer` / `admin` / `automation` roles
- API key store (`keys.go`, `managed_keys.go`) -- file-backed JSON store with hot-reload semantics
- Admin REST API (`admin.go`) -- runtime key CRUD on a separate port (`:8081`), guarded by `ADMIN_API_KEY`
- Onboarding-tool runtime info (`runtime.go`) -- bundles operator-config values (`ChainEnvironment`, anchor address, default URLs) passed once at server construction so handlers do not reach into `*config.Config` directly

## 3. MCP Tool Design

### Naming Convention

Tools use a `{domain}_{verb}_{noun}` pattern:
- `evm_get_chain_id` -- read chain info
- `anchor_get_registry` -- read registry
- `anchor_prepare_add_registry` -- prepare a write transaction
- `evm_send_raw_transaction` -- broadcast a signed transaction

Onboarding tools (Phase 8.8) use an `nvnm_*` / `wallet_*` prefix instead of `{domain}_{verb}_{noun}`. They are concierge-style entry points -- prose-guided, multi-state, surfaced to first-time agents -- and don't map to a single RPC verb the way the EVM and anchor reads do.

### Tool Inventory

21 tools after Phase 8.8. Each registers a `ToolAnnotations` payload (ReadOnly / Destructive / OpenWorld) and returns a response envelope carrying a `next_actions[]` array whose `tool` hints are AST-verified at test time to point at registered tool names.

| Group | Count | Tools |
|---|---|---|
| Onboarding | 5 | `nvnm_overview`, `wallet_status`, `nvnm_setup_wizard`, `nvnm_setup_verify_hash`, `nvnm_setup_verify_signature` |
| EVM reads | 8 | `evm_get_chain_id`, `evm_get_block`, `evm_get_transaction`, `evm_get_transaction_receipt`, `evm_get_balance`, `evm_get_code`, `evm_get_logs`, `evm_call_contract` |
| Anchor reads | 4 | `anchor_info`, `anchor_get_registry`, `anchor_get_registries`, `anchor_get_records` |
| Anchor + EVM writes | 4 | `anchor_prepare_add_registry`, `anchor_prepare_add_record`, `anchor_prepare_grant_role`, `evm_send_raw_transaction` |

### Input Validation

All tools validate inputs at the MCP boundary before calling downstream:
- Ethereum addresses: 0x-prefixed, 20 bytes, valid hex
- Transaction hashes: 0x-prefixed, 32 bytes, valid hex
- Block references: "latest", decimal number, or 0x-prefixed hash
- String lengths capped to prevent abuse

### Response Shape

Every tool returns structured JSON with consistent field naming:
- `snake_case` field names
- Numeric values as both decimal strings and hex where useful
- Explicit `null` rather than omitting fields
- `status` field as human-readable string ("success", "reverted"), not raw integer

### Error Responses

Tools return errors via the MCP `isError` flag with a human-readable text message. Internal details (stack traces, raw RPC errors) are logged but not exposed to callers.

## 4. Write Transaction Architecture

Write operations use a **prepare-sign-submit** pattern that keeps private keys entirely outside the MCP server. The server handles all blockchain complexity (ABI encoding, nonce, gas estimation, chain ID); the caller signs using their own key infrastructure.

Two signing paths are supported, both originating from the same `anchor_prepare_*` tool call:

- **MetaMask / EIP-1193 (browser wallets):** use `wallet_tx_request` from the response.
- **Local / headless signers (CLI, HSM, WalletConnect):** use `raw_tx` from the response.

### Flow A: MetaMask / Browser Wallet

```
┌──────────────────┐         ┌──────────────────┐         ┌──────────┐  ┌──────────────────┐
│   Browser / UI   │         │    MCP Server     │         │ MetaMask │  │  Inveniam Chain  │
└────────┬─────────┘         └────────┬──────────┘         └────┬─────┘  └────────┬─────────┘
         │                            │                          │                 │
         │  anchor_prepare_add_record │                          │                 │
         │ ─────────────────────────► │                          │                 │
         │                            │  ABI-encode + gas est.  │                 │
         │  ◄──────────────────────── │                          │                 │
         │  {raw_tx, wallet_tx_req}   │                          │                 │
         │                            │                          │                 │
         │  eth_sendTransaction       │                          │                 │
         │  (wallet_tx_request) ─────────────────────────────── ►│                 │
         │                            │                          │  Sign + broadcast ──────────►
         │                            │                          │  ◄── tx_hash                │
         │  ◄──────────────────────────────────────────────────  │                 │
         │  tx_hash                   │                          │                 │
         │                            │                          │                 │
         │  evm_get_transaction_receipt(tx_hash)                 │                 │
         │ ─────────────────────────► │                          │                 │
         │                            │  eth_getTransactionReceipt ───────────────►
         │  ◄──────────────────────── │  ◄── receipt            │                 │
         │  {status, gas_used, ...}   │                          │                 │
         └────────────────────────────┘                          └─────────────────┘
```

**JavaScript client example:**

```js
const prepared = await callMCPTool("anchor_prepare_add_record", { from, registry, uri, checksum });

const txHash = await window.ethereum.request({
  method: "eth_sendTransaction",
  params: [prepared.wallet_tx_request],
});

const receipt = await callMCPTool("evm_get_transaction_receipt", { tx_hash: txHash });
```

### Flow B: Local / Headless Signer

```
┌──────────────────┐         ┌──────────────────┐         ┌──────────────────┐
│   Calling App    │         │    MCP Server     │         │  Inveniam Chain  │
└────────┬─────────┘         └────────┬──────────┘         └────────┬─────────┘
         │                            │                             │
         │  anchor_prepare_add_       │                             │
         │  registry(from, name, ...) │                             │
         │ ─────────────────────────► │                             │
         │                            │  ABI-encode calldata        │
         │                            │  eth_getTransactionCount ──►│
         │                            │  ◄── nonce                  │
         │                            │  eth_estimateGas ──────────►│
         │                            │  ◄── gas estimate           │
         │                            │  eth_gasPrice ─────────────►│
         │                            │  ◄── gas price              │
         │                            │  Construct unsigned tx      │
         │  ◄──────────────────────── │                             │
         │  {raw_tx, wallet_tx_req,   │                             │
         │   nonce, gas, chain_id...} │                             │
         │                            │                             │
         │  Sign raw_tx locally       │                             │
         │  (ECDSA with private key)  │                             │
         │                            │                             │
         │  evm_send_raw_transaction  │                             │
         │  (signed_tx_hex)           │                             │
         │ ─────────────────────────► │                             │
         │                            │  eth_sendRawTransaction ───►│
         │                            │  ◄── tx_hash                │
         │  ◄──────────────────────── │                             │
         │  {tx_hash}                 │                             │
         └────────────────────────────┘                             │
```

### UnsignedTransaction type

The `anchor_prepare_*` tools return an `UnsignedTransaction` with fields for both signing paths. Phase 8.4 made EIP-1559 (type-2) the default; type-0 (legacy) is still produced when the caller opts in via `prefer_legacy_tx: true` on the tool input.

```go
type UnsignedTransaction struct {
    // Local/headless signer path
    RawTx                string `json:"raw_tx"`                            // RLP-encoded unsigned tx (hex, 0x-prefixed)
    Type                 uint8  `json:"type,omitempty"`                    // EIP-2718 type: 2 (EIP-1559 default) or 0 (legacy opt-out)
    To                   string `json:"to"`                                // Target address (precompile)
    Data                 string `json:"data"`                              // ABI-encoded calldata (hex)
    Nonce                uint64 `json:"nonce"`                             // Sender's current nonce
    Gas                  uint64 `json:"gas"`                               // Estimated gas limit (with 20% buffer)
    GasPrice             string `json:"gas_price"`                         // Wei, decimal string. On type-2 responses GasPrice equals MaxFeePerGas so legacy-only signers still have a usable value.
    MaxFeePerGas         string `json:"max_fee_per_gas,omitempty"`         // EIP-1559 fee cap; type-2 only
    MaxPriorityFeePerGas string `json:"max_priority_fee_per_gas,omitempty"` // EIP-1559 miner tip; type-2 only
    Value                string `json:"value"`                             // Always "0" for precompile calls
    ChainID              int64  `json:"chain_id"`                          // For EIP-155 replay protection

    // MetaMask / EIP-1193 path
    WalletTxRequest *WalletTransactionRequest `json:"wallet_tx_request,omitempty"`
}

type WalletTransactionRequest struct {
    From                 string `json:"from"`                              // Sender address
    To                   string `json:"to"`                                // Precompile address
    Data                 string `json:"data"`                              // ABI-encoded calldata
    Value                string `json:"value"`                             // "0x0"
    ChainID              string `json:"chainId"`                           // 0x-prefixed hex (e.g. "0xc02a7")
    Gas                  string `json:"gas"`                               // 0x-prefixed hex quantity
    GasPrice             string `json:"gasPrice,omitempty"`                // Type-0 path; omitted for EIP-1559
    MaxFeePerGas         string `json:"maxFeePerGas,omitempty"`            // EIP-1559 path
    MaxPriorityFeePerGas string `json:"maxPriorityFeePerGas,omitempty"`    // EIP-1559 path
}
```

The type-2 path fetches the miner tip via `evm.Client.SuggestGasTipCap` (added in 8.4 alongside the existing `SuggestGasPrice`) and sets `MaxFeePerGas = 2 * SuggestGasPrice` for headroom against base-fee inflation. When the RPC does not implement `eth_maxPriorityFeePerGas` (or returns zero) the server falls back to a 1-gwei tip rather than failing the prepare call.

### Why prepare-sign-submit

| Concern | Server-held keys | Prepare-sign-submit |
|---|---|---|
| Private key exposure | Key in env var or config; single breach exposes signing | Key never leaves caller's domain |
| HSM / Vault support | Requires server integration with each key store | Caller uses their own key infrastructure |
| Audit trail | Server signs on behalf of caller | Caller is cryptographic author of every tx |
| MCP server scope | Becomes a trusted signing service | Stays a stateless translation layer |
| Caller complexity | Minimal (just call tool) | Sign one hash (3-4 lines, no web3 library) |

### Nonce management

The MCP server fetches the pending nonce via `eth_getTransactionCount(address, "pending")` when preparing each transaction. This is correct for serial write patterns (one write at a time per address), which is the expected usage for document anchoring workflows.

If concurrent writes from the same address are needed in the future, the server can add a nonce reservation mechanism. This is not needed for v1.

### Gas estimation

Gas is estimated via `eth_estimateGas` with a safety buffer (1.2x the estimate). The buffer accounts for state changes between estimation and execution. The exact buffer multiplier will be tuned during testnet validation.

## 5. Transport Strategy

The server supports two transports, selected by `--transport` flag or `MCP_TRANSPORT` env var:

### stdio (default)

- Used for local development and direct MCP client integration (e.g. Claude Desktop, Cursor)
- Server reads JSON-RPC from stdin, writes to stdout
- Simplest deployment: just run the binary

### Streamable HTTP

- Used for production/remote deployment (AWS, MANTRA nodes)
- Server listens on `MCP_HTTP_ADDR` (default `:8080`)
- Supports session management via `Mcp-Session-Id` headers
- Can sit behind a reverse proxy (nginx, ALB)

Both transports use the same `mcp.Server` instance; only the transport wrapper differs.

## 6. Configuration Design

Environment-variable-first. No config files, no YAML, no TOML.

```go
type Config struct {
    EVMRPCURL        string        // required
    EVMArchiveRPCURL string        // optional
    ChainID          int64         // required
    AnchorAddress    string        // default "0x0000000000000000000000000000000000000A00"
    AnchorABIPath    string        // path to precompile ABI JSON (required for anchor tools)
    RequestTimeout   time.Duration // default 15s
    LogLevel         string        // default "info"
    EnableWriteTools bool          // default false; gates anchor_prepare_* tools
    Transport        string        // default "stdio"
    HTTPAddr         string        // default ":8080"

    // Authentication -- provider selection
    AuthProvider     string        // AUTH_PROVIDER; "apikey" (default) or "fusionauth"

    // API key authentication
    APIKey           string        // MCP_API_KEY; single key for simple deployments
    APIKeysFile      string        // MCP_API_KEYS_FILE; path to JSON key store (preferred)

    // FusionAuth authentication
    FusionAuthURL           string        // FUSIONAUTH_URL; base URL of FusionAuth instance
    FusionAuthApplicationID string        // FUSIONAUTH_APPLICATION_ID
    FusionAuthIssuer        string        // FUSIONAUTH_ISSUER; defaults to FUSIONAUTH_URL
    FusionAuthJWKSURL       string        // FUSIONAUTH_JWKS_URL; defaults to ${URL}/.well-known/jwks.json
    JWTClockSkew            time.Duration // JWT_CLOCK_SKEW; default 60s
    JWTRolesClaim           string        // JWT_ROLES_CLAIM; default "roles"

    // Admin key management API
    AdminAPIKey      string        // ADMIN_API_KEY; required to start admin server
    AdminAPIAddr     string        // ADMIN_API_ADDR; default ":8081"

    // MCP-level rate limiting (per client)
    MCPRateLimit     float64       // MCP_RATE_LIMIT; default 60 req/s per client
    MCPRateBurst     int           // MCP_RATE_BURST; default 10

    // Upstream RPC resilience
    RPCMaxRetries     int           // RPC_MAX_RETRIES; default 3
    RPCInitialBackoff time.Duration // RPC_INITIAL_BACKOFF; default 500ms
    RPCMaxBackoff     time.Duration // RPC_MAX_BACKOFF; default 10s
    RPCRateLimit      float64       // RPC_RATE_LIMIT; default 100 req/s
    RPCRateBurst      int           // RPC_RATE_BURST; default 20
    BreakerThreshold  uint          // CIRCUIT_BREAKER_THRESHOLD; default 5
    BreakerTimeout    time.Duration // CIRCUIT_BREAKER_TIMEOUT; default 30s

    // Telemetry
    OTELEndpoint     string        // OTEL_EXPORTER_OTLP_ENDPOINT; enables OTLP export
    OTELServiceName  string        // default "nvnm-mcp-server"
    OTLPInsecure     bool          // OTLP_INSECURE; default true; set false for TLS
    TraceSampleRatio float64       // OTEL_TRACE_SAMPLE_RATIO; default 1.0
    EnablePrometheus bool          // default true; exposes /metrics
    EnableStdoutTel  bool          // default false; for dev debugging
    MetricsAddr      string        // default ":9090"; health + metrics server

}
```

Validation rules:
- `EVMRPCURL` must be non-empty and start with `http://` or `https://`
- `ChainID` must be > 0
- `RequestTimeout` must be > 0
- `Transport` must be "stdio" or "http"

Note: there are no private key or signing configuration fields. The MCP server never holds keys.

## 7. Logging & Observability

### Structured Logging

Via `log/slog` (Go stdlib) with OTel bridge:
- JSON format in production, text format for local development
- Every log record includes `trace_id` and `span_id` when an OTel trace context is active
- Unique `request_id` per MCP method call for correlation
- Redaction helpers ensure sensitive data never appears in logs:
  - Addresses: truncated to `0x9f8a...A9CD`
  - URLs: hostname only, no path/credentials
  - Transaction data: byte length only, no content
  - Private keys: never logged (never reach the server)

### OpenTelemetry Instrumentation

Vendor-agnostic telemetry via OpenTelemetry SDK:

**Exporters (configurable via env vars):**
- **OTLP** (gRPC) -- sends traces and metrics to an OTel Collector (sidecar or service), which routes to CloudWatch/X-Ray, Grafana Cloud, etc.
- **Prometheus** -- exposes `/metrics` endpoint for direct scraping
- **Stdout** -- for local dev/debugging

**MCP middleware (auto-instruments every tool call):**
- Trace span per method call with `mcp.method`, `mcp.tool.name`, `mcp.request.id`
- `mcp.server.tool.duration` histogram -- latency distribution per tool
- `mcp.server.tool.calls` counter -- call count by tool name and status
- `mcp.server.tool.errors` counter -- error count by tool name
- `mcp.server.active_requests` gauge -- concurrent in-flight requests

**RPC client tracing (auto-instruments every upstream RPC call):**
- Child span per RPC call: `rpc.method`, `rpc.target` (hostname only)
- `evm.rpc.duration` histogram -- upstream RPC latency
- `evm.rpc.errors` counter -- errors by method

**Privacy:** tool arguments, return values, and private keys are never recorded in traces or metrics; error messages are attached to internal trace span events for debugging only and are sanitized before reaching clients.

### Health Check Endpoints

Separate HTTP server on `METRICS_ADDR` (default `:9090`):

| Endpoint | Purpose |
|---|---|
| `GET /healthz` | Liveness probe -- always `200 OK` if the process is running |
| `GET /readyz` | Readiness probe -- `200 OK` if EVM RPC is reachable and ABI is loaded; `503` otherwise |
| `GET /metrics` | Prometheus scrape endpoint |

Compatible with Kubernetes probes, AWS ALB health checks, and Azure health probes.

#### `internal/telemetry`

OpenTelemetry initialisation, MCP middleware, health/metrics server, and metric definitions.

- `New(ctx, cfg, logger)` -- creates `TracerProvider` and `MeterProvider` with configured exporters
- `Shutdown(ctx)` -- flushes pending telemetry before exit
- `NewMCPMiddleware(metrics, logger)` -- returns `mcp.Middleware` that auto-instruments all tool calls
- `NewHealthServer(addr, promHandler, checker, abiLoaded, logger)` -- serves `/healthz`, `/readyz`, `/metrics`
- `NewMetrics(provider)` -- creates all metric instruments
- Readiness check polls EVM RPC every 30s (cached result)

## 8. Deployment Topology

### Local Development

```
[Developer Mac] ─── stdio ──► [nvnm-mcp-server] ─── HTTPS ──► [NVNM Chain RPC]
```

### AWS (ECS/Fargate)

```
[MCP Client] ─── HTTPS ──► [ALB] ──► [ECS Task: nvnm-mcp-server] ─── HTTPS ──► [NVNM Chain RPC]
```

- Docker image based on `gcr.io/distroless/static-debian12`
- Health check via HTTP GET on `/` or dedicated `/health` endpoint
- Environment variables from ECS task definition or Secrets Manager

### MANTRA Validator Node

```
[MCP Client] ─── HTTP ──► [nvnm-mcp-server] ─── localhost ──► [Local EVM RPC]
```

- Binary runs directly on the node (or in a container)
- Connects to `http://localhost:8545` or equivalent local RPC
- Minimal latency for anchor queries

### Multi-chain (testnet + mainnet): two instances, not per-session

Once mainnet is live alongside testnet, the server supports **both** by
**running two independent instances**, one per chain. Each instance is
pinned to its chain at startup via `NVNM_EVM_RPC_URL`,
`NVNM_CHAIN_ID`, and `NVNM_CHAIN_ENVIRONMENT`. (The Phase 8.9 hard
cut renamed these from the legacy `INVENIAM_*` prefix; see
`docs/RUNBOOK.md#env-var-migration`.)

```
                    ┌── [server-testnet] ─── HTTPS ──► [testnet RPC]
[MCP Client] ──HTTPS┤
                    └── [server-mainnet] ─── HTTPS ──► [mainnet RPC]
```

Per-session chain selection inside a single instance was **considered
and rejected** for v1. The decision and reasoning are recorded here so
future contributors don't relitigate it.

#### Why two instances

1. **Blast-radius isolation.** A mainnet write is real provenance and
   real money; a testnet write is disposable. A single server that
   can do either has more attack surface and more chances for an
   operator or agent to accidentally broadcast to the wrong chain.
   Per-instance means the chain choice is decided at deploy time by
   an operator, not at request time by an agent. The chain selection
   is encoded in *which URL the client connects to*, which is the
   simplest possible invariant.
2. **Audit trail is implicit.** "Which chain did this write go to?"
   is answered by "which server received the call." No per-call
   chain dimension to forget on a log line. Forensic reconstruction
   stays cleanly partitioned.
3. **Per-chain RBAC is natural.** A testnet-only API key is just a
   key on the testnet server. A mainnet-only operator role lives in
   the mainnet auth provider. The same identity surfacing on both
   chains would require either per-chain role scoping (`writer.testnet`
   vs `writer.mainnet` in every role check) or a global role that
   spans both -- both options are surface-area we don't need to
   maintain.
4. **Different operational tiers.** Mainnet probably wants stricter
   rate limits, separate alerting, a different on-call rotation,
   maybe a different Grafana dashboard. Per-instance lets all of
   these be independent config; per-session would force conditional
   logic at every observability seam.
5. **Existing config model already supports it.** Chain identity is
   already startup-bound via three env vars. Two instances = two env
   sets. **Zero code change** to go multi-chain.
6. **Agent UX cost is small.** Clients configure one MCP endpoint
   per chain, mirroring how every other blockchain tool works.
   Workflows that want to "promote from testnet to mainnet" land on
   the client side (their MCP manifest knows both URLs), not on the
   server.

#### What per-session selection would have required (rejected design)

For the record, here is what would have changed if we'd taken the
per-session path:

| Surface | Currently | Per-session would need |
|---|---|---|
| EVM RPC client | one, built in `main.go` | a map keyed by chain, picked from request context |
| Anchor client + ABI | one | one per chain |
| `NewServer(..., chainEnvironment, ...)` | one chain label | resolved per call |
| Chain ID in tx validation | one chain ID | per-call chain ID |
| Audit logs | one chain implicit | every line carries an explicit `chain=` dimension |
| Auth / RBAC | one role set | per-chain role scoping (e.g. `writer:mainnet` ≠ `writer:testnet`) |
| Write gating (`ENABLE_WRITE_TOOLS` + RBAC role) | per-instance | per-instance-per-chain |
| Resilient client (retry/breaker) | one set of state | one set per chain (testnet flakes must not trip the mainnet breaker) |

Effort estimate: ~1–2 weeks. Not large in LOC; large in design
decisions (RBAC scoping dominates). Rejected because the operational
benefits of per-instance isolation outweigh the marginal client-side
convenience.

#### Revisit triggers

Reconsider per-session selection only if:
- A concrete agent workflow needs to bounce between testnet and
  mainnet in a single user-level interaction (and that workflow
  cannot route through two MCP client configurations on the agent
  side).
- A new auth provider arrives where per-chain role scoping is
  cheap (e.g. JWT claims that include a `chain` field natively).

In the absence of either trigger, the two-instances model is the
intended long-term shape.

## 9. Graceful Shutdown

The server handles `SIGINT` and `SIGTERM`:
1. Stop accepting new MCP connections/requests
2. Wait for in-flight tool calls to complete (with a deadline)
3. Shut down health/metrics server
4. Flush pending telemetry (traces and metrics)
5. Close EVM client connections
6. Exit 0

Timeout: 5 seconds for telemetry flush and health server shutdown.

## 10. Security Considerations

> See `docs/SECURITY_AUDIT.md` for the full pre-red-team threat model, ranked attack paths, remediation log, and tabletop scenarios.

- **No raw RPC passthrough**: Callers cannot send arbitrary JSON-RPC. Only curated tools are exposed.
- **Input validation**: All inputs validated at tool boundary. Hex data and signed transaction inputs are capped at 2 MB to prevent memory exhaustion.
- **No private keys**: The server never holds, receives, or logs private keys. All transaction signing is external to the server. This eliminates an entire class of key-management vulnerabilities.
- **Write protection**: Write (prepare) tools disabled by default; require explicit opt-in via `ENABLE_WRITE_TOOLS=true`.
- **RPC isolation**: Upstream RPC errors are mapped to safe error types; raw error details stay in logs. `errors.SafeForClient()` sanitizes all errors at the MCP boundary before returning to callers.
- **Unsigned tx transparency**: Prepare tools return the full transaction breakdown (to, data, nonce, gas, chain ID) so callers can verify exactly what they're signing.
- **HTTPS**: Production RPC endpoints should use HTTPS.
- **Rate limiting**: Token-bucket rate limiter on upstream RPC calls. See [Resilience](#11-resilience).
- **HTTP request limits**: Request bodies are capped at 10 MB via `http.MaxBytesReader`. HTTP server enforces `ReadTimeout`, `WriteTimeout`, `IdleTimeout`, and `MaxHeaderBytes`.

### Authentication (HTTP transport)

When using HTTP transport, the server supports Bearer token authentication via a configurable auth provider. The provider is selected by the `AUTH_PROVIDER` env var.

| Value | Description |
|---|---|
| `apikey` (default) | Self-managed API keys. Simple, no external dependencies. |
| `fusionauth` | FusionAuth OAuth/JWT with JWKS validation. Production multi-tenant use. |

Stdio transport is always unauthenticated (local-only, trusted), regardless of provider setting.

#### API Key Provider (`AUTH_PROVIDER=apikey`)

**Configuration (choose one):**

| Variable | Description |
|---|---|
| `MCP_API_KEYS_FILE` | Path to a JSON key store file with multiple keys and client IDs (recommended for production) |
| `MCP_API_KEY` | Single API key for simple deployments (no client identity tracking) |

**Key store format** (`.mcp-keys.json`):

```json
[
  {
    "id": "my-agent",
    "key": "mcp_...",
    "enabled": true,
    "created_at": "2026-04-01T12:00:00Z"
  }
]
```

**Behaviour:**
- When keys are configured, all HTTP requests must include `Authorization: Bearer <key>`.
- Each key maps to a client ID that flows through to structured logs and OTel spans (`client_id` attribute), enabling per-client audit trails.
- Disabled keys are rejected.
- The server warns at startup if HTTP transport runs with no keys configured.

#### FusionAuth Provider (`AUTH_PROVIDER=fusionauth`)

**Configuration:**

| Variable | Required | Description |
|---|---|---|
| `FUSIONAUTH_URL` | Yes | Base URL of the FusionAuth instance (e.g. `https://auth.example.com`) |
| `FUSIONAUTH_APPLICATION_ID` | Yes | Application UUID in FusionAuth |
| `FUSIONAUTH_ISSUER` | No | Expected JWT `iss` claim (defaults to `FUSIONAUTH_URL`) |
| `FUSIONAUTH_JWKS_URL` | No | JWKS endpoint (defaults to `FUSIONAUTH_URL/.well-known/jwks.json`) |
| `JWT_CLOCK_SKEW` | No | Leeway for token expiry checks (default `60s`) |
| `JWT_ROLES_CLAIM` | No | Name of the roles claim in JWT (default `roles`) |

**Behaviour:**
- JWTs are validated locally using JWKS (public key signature verification). No token introspection.
- Issuer and audience (application ID) are verified on every request.
- The JWT `sub` claim becomes the client identity for logs and OTel spans.
- Roles are extracted from the `roles` claim (top-level or nested under the application ID key, FusionAuth-style).
- The admin key management API does not start in FusionAuth mode (user management is external).
- FusionAuth's scheme-stripping quirk (issuer without `http://` prefix) is handled.

**Expected roles:** `reader`, `writer`, `admin`, `automation`

**FusionAuth setup (external):**
1. Create a tenant in the existing FusionAuth instance
2. Create an application within the tenant
3. Define roles: `reader`, `writer`, `admin`, `automation`
4. Configure JWT template if custom claims are needed beyond default `roles`
5. Set `FUSIONAUTH_URL` and `FUSIONAUTH_APPLICATION_ID` env vars

**Key management** is provided by the `cmd/key-mgmt/` CLI and Makefile targets (for local/dev use):

```bash
make key-create NAME=my-agent    # Create key
make key-list                    # List keys (ID, enabled, created)
make key-disable NAME=my-agent   # Disable key
make key-enable NAME=my-agent    # Re-enable key
```

**Admin Key Management API** (for production/DevOps use):

A dedicated REST API on a separate port (default `:8081`) enables runtime key management without server restarts. Changes take effect immediately -- no hot-reload delay.

| Env Var | Default | Purpose |
|---|---|---|
| `ADMIN_API_KEY` | _(none)_ | Admin bearer token. Admin server only starts when set. |
| `ADMIN_API_ADDR` | `:8081` | Admin API listen address. |

The admin API is authenticated by its own bearer token (`ADMIN_API_KEY`), separate from the client keys it manages. This ensures that compromise of a client key does not grant admin access.

**Endpoints:**

| Method | Path | Description |
|---|---|---|
| `POST` | `/admin/keys` | Create a new client key. Returns the raw key **once**. |
| `GET` | `/admin/keys` | List all keys (redacted -- prefix only, no raw keys). |
| `PATCH` | `/admin/keys/{id}` | Update enabled status. |
| `DELETE` | `/admin/keys/{id}` | Permanently remove a key. |

**Security controls:**
- Separate port -- firewalled/NetworkPolicy'd independently from MCP and metrics ports.
- Separate credential -- admin token is distinct from client keys.
- Constant-time token comparison.
- Audit logging on every mutation (action, client ID, remote address).
- Raw key shown once on creation, never retrievable after.
- Request body size limit (1 MB).
- Only starts on HTTP transport (not stdio).

### Write Gating

Write tools are gated at two levels; both must be satisfied for `evm_send_raw_transaction` to execute:

1. **`ENABLE_WRITE_TOOLS=true`** -- write tools are not registered unless this env var is set. Default is `false`. See `docs/RUNBOOK.md` for deployment guidance.
2. **RBAC role** -- the authenticated caller must hold `writer`, `admin`, or `automation`. `reader` role callers are denied at the tool handler boundary (`requireRole` in `internal/mcp/rbac.go`).

**Human confirmation is the client/agent's responsibility.** The server no longer issues an MCP elicitation prompt before broadcasting. The caller-side signature is the security boundary: the server broadcasts exactly the signed bytes it receives and cannot alter them. It is the client's or agent's duty to obtain human confirmation before submitting a signed transaction, and to verify the transaction being signed matches what the user intended.

The server handler (`StreamableHTTPOptions{Stateless: true}`) runs stateless — no per-pod session map, no load-balancer affinity required. See `docs/SESSION_AFFINITY.md` for the full rationale.

**Fail-loud migration guards** -- if `WRITE_APPROVAL_DEFAULT` is present in the environment, or if a key store entry carries a `write_approval` field, startup fails with `ErrLegacyWriteApproval` / `ErrLegacyKeyWriteApproval` and a pointer to `docs/RUNBOOK.md#write-approval-removal`.

### Error Sanitization

Errors returned to MCP clients are sanitized via `errors.SafeForClient()`:
- Known application errors (input validation, not-found, write-disabled) are passed through with their original message.
- Unknown/internal errors (RPC failures, ABI decode errors, etc.) are replaced with a generic "internal error" message.
- The original error is always logged server-side with full context.
- RPC URLs are never included in error messages to prevent information leakage.

### Reverse Proxy Requirements (Production)

In production, the MCP HTTP server (`:8080`) should sit behind a reverse proxy (nginx, HAProxy, AWS ALB, etc.) that handles:

1. **TLS termination** -- the MCP server itself does not terminate TLS. The reverse proxy should present a valid TLS certificate and forward plaintext HTTP to `:8080`.
2. **Rate limiting** -- while the server has auth and upstream RPC rate limiting, a reverse proxy can provide connection-level rate limiting, IP-based throttling, and DDoS protection.
3. **Request size limits** -- the server enforces a 10 MB body limit, but the proxy should also cap body size to prevent network-level abuse.
4. **Access logging** -- the proxy should log source IPs, request sizes, and TLS versions for forensic purposes.

**Example nginx configuration:**

```nginx
upstream mcp_backend {
    server 127.0.0.1:8080;
}

server {
    listen 443 ssl;
    server_name mcp.example.com;

    ssl_certificate     /etc/ssl/certs/mcp.crt;
    ssl_certificate_key /etc/ssl/private/mcp.key;
    ssl_protocols       TLSv1.2 TLSv1.3;

    client_max_body_size 10m;

    location / {
        proxy_pass http://mcp_backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 120s;
        proxy_send_timeout 120s;
    }
}
```

**Note:** The health/metrics port (`:9090`) should NOT be exposed externally. It should only be accessible within the cluster for Kubernetes probes and Prometheus scraping.

## 11. Resilience

The EVM client stack includes a resilience wrapper (`internal/evm/resilient.go`) that decorates the tracing client with three production-hardening mechanisms.

### Composition

```
defiweb/go-eth RPC → TracingClient → ResilientClient → (used by anchor + MCP handlers)
```

Each retry attempt passes through the tracing layer, producing its own OTel span. The resilient wrapper adds metrics for retry count, circuit breaker state, and rate-limit rejections.

### Retry with Exponential Backoff

Transient RPC errors (timeouts, network errors, connection resets) are retried using exponential backoff with jitter. Configuration:

- `RPC_MAX_RETRIES` (default 3) -- maximum retry attempts
- `RPC_INITIAL_BACKOFF` (default 500ms) -- initial wait between retries
- `RPC_MAX_BACKOFF` (default 10s) -- maximum wait between retries

`SendRawTransaction` is explicitly excluded from retries due to idempotency risk.

### Rate Limiting

There are **two independent** rate limiters:

**Upstream RPC rate limit** -- token-bucket on outbound EVM JSON-RPC calls (`internal/evm/resilient.go`):

- `RPC_RATE_LIMIT` (default 100 req/s) -- sustained request rate
- `RPC_RATE_BURST` (default 20) -- burst capacity
- When exceeded and the context expires waiting for a token, the call returns `ErrRateLimited`.

**Per-client MCP rate limit** -- token-bucket on inbound MCP requests, keyed by authenticated client ID (`internal/mcp/ratelimit.go`):

- `MCP_RATE_LIMIT` (default 60 req/s) -- per-client sustained rate
- `MCP_RATE_BURST` (default 10) -- per-client burst
- When exceeded, the server returns HTTP `429 Too Many Requests`.
- Auth middleware runs first so the client ID is available to key the bucket.

### Circuit Breaker

A circuit breaker (sony/gobreaker) protects against cascading failures from a degraded upstream RPC. Configuration:

- `CIRCUIT_BREAKER_THRESHOLD` (default 5) -- consecutive failures to trip the breaker
- `CIRCUIT_BREAKER_TIMEOUT` (default 30s) -- time in open state before half-open probe

States:
- **Closed** -- normal operation, all calls pass through
- **Open** -- upstream is failing, all calls rejected immediately with `ErrCircuitOpen`
- **Half-Open** -- one probe call allowed; success closes the breaker, failure reopens it

State transitions are logged at WARN level and recorded in the `evm.rpc.circuit_breaker.state` metric.

### Trace Sampling

`OTEL_TRACE_SAMPLE_RATIO` (default 1.0) configures the fraction of root traces sampled. Uses `ParentBased(TraceIDRatioBased)` to respect upstream sampling decisions while controlling cost for high-volume deployments.

## 12. Key Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| MCP SDK | Official `go-sdk` v1.5.0 | Maintained by Google/Anthropic, typed tool binding, both transports |
| EVM client | `defiweb/go-eth` | MIT-licensed (no LGPL/GPL exposure); covers RPC + ABI + secp256k1 in one tree; suitable for proprietary distribution |
| Logging | `log/slog` + redaction | Structured, zero-dep base, safe redaction utilities |
| Telemetry | OpenTelemetry SDK | Vendor-agnostic; native Prometheus + OTLP export; follows OTel env var conventions |
| Health checks | Separate `:9090` server | Decoupled from MCP transport; compatible with K8s, ALB, Azure probes |
| RPC tracing | Decorator pattern | `TracingClient` wraps `Client` interface; no changes to core client code |
| Config | Plain env vars | Simplest possible; no framework overhead; 12-factor compliant |
| Normalization | Inside `evm/` package | Reduces package count; normalization is a concern of the EVM layer |
| Anchor isolation | Adapter pattern in `internal/anchor` | Must be swappable without touching EVM or MCP layers |
| Write key management | Prepare-sign-submit (no server keys) | Keys never leave caller's domain; MCP server stays a stateless translator; supports MetaMask, HSM/Vault, CLI signers without server changes |
| Nonce management | Fetch-at-prepare-time | Correct for serial writes; sufficient for document anchoring volumes; avoids statefulness in the server |
| No `normalize/` package | Merged into `evm/` and `anchor/` | Original plan had too much indirection for the value |
| MCP middleware | Custom (~100 lines) | Avoided 1-star `mcp-otel-go` dependency; full control over privacy and metric naming |
| Retry logic | cenkalti/backoff v5 | Exponential backoff with jitter; transitive dep promoted to direct |
| Rate limiting | golang.org/x/time/rate | Stdlib-adjacent token bucket; no external dependency complexity |
| Circuit breaker | sony/gobreaker v2 | Well-maintained (5k+ stars), simple API, generic type support |
| Trace sampling | OTel SDK ParentBased | Respects upstream decisions, configurable ratio for cost control |
| Auth provider | Dual: API key + FusionAuth | API keys for simplicity/dev/agents; FusionAuth for production multi-tenant OAuth. Provider-selectable via `AUTH_PROVIDER` |
| JWT validation | `keyfunc/v3` + `golang-jwt/jwt/v5` | Same stack as TraceChain API; JWKS auto-refresh; proven in production |
| Write gating | RBAC role + `ENABLE_WRITE_TOOLS` | No server-side approval state; stateless handler; signature is the security boundary |
