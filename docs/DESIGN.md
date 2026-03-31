# Inveniam EVM MCP Server -- Design & Architecture

## 1. System Context

The Inveniam EVM MCP Server sits between MCP-capable clients (LLMs, developer tools, agents) and the Inveniam EVM chain (NVNM). It translates high-level tool calls into EVM JSON-RPC requests, normalizes the responses, and returns structured, typed JSON.

For write operations, the server constructs unsigned transactions but never holds private keys. Signing is the caller's responsibility.

### Target Chain

NVNM Chain is Inveniam's Layer 2 blockchain, secured by MANTRA's validator set through Interchain Security (ICS). It is purpose-built for document anchoring and provenance verification.

| Property | Value |
|---|---|
| Network | NVNM Chain (Inveniam L2) |
| Chain ID | `58887` (`0xe607`) |
| Cosmos chain ID | `manveniam-1` |
| Native currency | mUSD (MANTRA US Dollars) -- pays gas fees |
| EVM RPC | `https://evm.inveniam.mantrachain.io` |
| Cosmos RPC | `https://rpc.inveniam.mantrachain.io` |
| Explorer | `https://explorer.inveniam.mantrachain.io/` |
| Anchor precompile | `0x0000000000000000000000000000000000000A00` |

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
                                          │  Inveniam EVM Chain │
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
cmd/inveniam-mcp-server/main.go
    │
    ├── internal/config      (env loading, validation)
    ├── internal/logging     (slog wrapper)
    ├── internal/evm         (ethclient wrapper, normalized types)
    ├── internal/anchor      (anchor adapter, prepare-sign-submit)
    └── internal/mcp         (MCP server, tool handlers)
            │
            ├── internal/evm
            ├── internal/anchor
            └── internal/errors
```

Key constraint: `internal/evm` knows nothing about anchors. `internal/anchor` knows nothing about MCP. `internal/mcp` orchestrates both.

### Package Responsibilities

#### `cmd/inveniam-mcp-server`

Application entrypoint. Responsibilities:
- Parse CLI flags (`--transport`)
- Load and validate configuration
- Initialize logger
- Construct EVM client and anchor client
- Register MCP tools
- Select and start MCP transport
- Handle graceful shutdown on SIGINT/SIGTERM

#### `internal/config`

Environment-based configuration loading and validation.

- Reads from `os.Getenv`; no config files, no frameworks
- `Config` struct with typed fields
- `Load()` function returns `(*Config, error)` -- fails fast on missing required fields
- `Validate()` checks invariants (chain ID > 0, timeout > 0, etc.)
- Safe defaults only for non-sensitive settings (timeout, log level)

#### `internal/logging`

Thin wrapper over `log/slog` (Go stdlib).

- `New(level string) *slog.Logger` -- creates a configured logger
- JSON handler for production, text handler for development
- No external dependencies

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

Generic EVM JSON-RPC client layer. Wraps `go-ethereum/ethclient`.

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

    Close()
}
```

Design decisions:
- Methods return **normalized types** directly, not raw go-ethereum types. Normalization happens at the EVM layer boundary.
- The client wraps `ethclient.Client` and holds a configured timeout.
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
- The client uses `go-ethereum`'s ABI pack/unpack to encode `eth_call` requests against the precompile.
- Write preparation methods return `UnsignedTransaction` containing the fully constructed transaction (with nonce, gas, chain ID) but no signature. Signing is the caller's responsibility.
- The `Available()` method allows MCP tools to provide clear status about whether the ABI is loaded.

#### `internal/mcp`

MCP tool registration and request handling using the official Go MCP SDK.

Responsibilities:
- Construct `mcp.Server` with tool definitions
- Each tool handler: validate input -> call EVM/anchor client -> return normalized result
- Map internal errors to MCP-safe responses (human-readable message + error flag)

## 3. MCP Tool Design

### Naming Convention

Tools use a `{domain}_{verb}_{noun}` pattern:
- `evm_get_chain_id` -- read chain info
- `anchor_get_registry` -- read registry
- `anchor_prepare_add_registry` -- prepare a write transaction
- `evm_send_raw_transaction` -- broadcast a signed transaction

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

Write operations use a **prepare-sign-submit** pattern that keeps private keys entirely outside the MCP server. The server handles all blockchain complexity; the caller only needs to perform a single ECDSA signature.

### Flow

```
┌──────────────────┐         ┌──────────────────┐         ┌──────────────────┐
│   Calling App    │         │    MCP Server     │         │  Inveniam Chain  │
└────────┬─────────┘         └────────┬──────────┘         └────────┬─────────┘
         │                            │                             │
         │  anchor_prepare_add_       │                             │
         │  registry(from, name,      │                             │
         │  description, metadata)    │                             │
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
         │  {unsigned_tx, nonce,      │                             │
         │   gas, chain_id, to, ...}  │                             │
         │                            │                             │
         │  Sign tx bytes locally     │                             │
         │  (ECDSA with private key)  │                             │
         │                            │                             │
         │  evm_send_raw_transaction  │                             │
         │  (signed_tx_hex)           │                             │
         │ ─────────────────────────► │                             │
         │                            │  eth_sendRawTransaction ───►│
         │                            │  ◄── tx_hash                │
         │  ◄──────────────────────── │                             │
         │  {tx_hash}                 │                             │
         │                            │                             │
         │  evm_get_transaction_      │                             │
         │  receipt(tx_hash)          │                             │
         │ ─────────────────────────► │                             │
         │                            │  eth_getTransactionReceipt─►│
         │                            │  ◄── receipt                │
         │  ◄──────────────────────── │                             │
         │  {status, gas_used, ...}   │                             │
         └────────────────────────────┘                             │
```

### UnsignedTransaction type

The `anchor_prepare_*` tools return an `UnsignedTransaction` containing everything the caller needs to sign and submit:

```go
type UnsignedTransaction struct {
    RawTx   string `json:"raw_tx"`    // RLP-encoded unsigned tx (hex, 0x-prefixed)
    To      string `json:"to"`        // Target address (precompile)
    Data    string `json:"data"`      // ABI-encoded calldata (hex)
    Nonce   uint64 `json:"nonce"`     // Sender's current nonce
    Gas     uint64 `json:"gas"`       // Estimated gas limit (with buffer)
    GasPrice string `json:"gas_price"` // Current gas price (wei, decimal string)
    Value   string `json:"value"`     // Always "0" for precompile calls
    ChainID int64  `json:"chain_id"`  // For EIP-155 replay protection
}
```

The `raw_tx` field is a serialized `types.Transaction` ready for signing. The other fields are provided for caller transparency -- the caller can verify what they're signing before applying their key.

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
}
```

Validation rules:
- `EVMRPCURL` must be non-empty and start with `http://` or `https://`
- `ChainID` must be > 0
- `RequestTimeout` must be > 0
- `Transport` must be "stdio" or "http"

Note: there are no private key or signing configuration fields. The MCP server never holds keys.

## 7. Logging & Observability

### Phase 1-3: slog

Structured logging via `log/slog` (Go stdlib):
- JSON format in production, text format for local development
- Logged events: server start/stop, tool calls (name + duration), RPC errors, config validation
- Sensitive fields (RPC URLs with credentials) are redacted

### Phase 4: Metrics (future)

- Prometheus endpoint on HTTP transport
- Counters: tool calls by name, errors by type
- Histograms: tool call duration, RPC latency
- Gauges: active sessions, latest block number

## 8. Deployment Topology

### Local Development

```
[Developer Mac] ─── stdio ──► [inveniam-mcp-server] ─── HTTPS ──► [Inveniam EVM RPC]
```

### AWS (ECS/Fargate)

```
[MCP Client] ─── HTTPS ──► [ALB] ──► [ECS Task: inveniam-mcp-server] ─── HTTPS ──► [Inveniam EVM RPC]
```

- Docker image based on `gcr.io/distroless/static-debian12`
- Health check via HTTP GET on `/` or dedicated `/health` endpoint
- Environment variables from ECS task definition or Secrets Manager

### MANTRA Validator Node

```
[MCP Client] ─── HTTP ──► [inveniam-mcp-server] ─── localhost ──► [Local EVM RPC]
```

- Binary runs directly on the node (or in a container)
- Connects to `http://localhost:8545` or equivalent local RPC
- Minimal latency for anchor queries

## 9. Graceful Shutdown

The server handles `SIGINT` and `SIGTERM`:
1. Stop accepting new MCP connections/requests
2. Wait for in-flight tool calls to complete (with a deadline)
3. Close EVM client connections
4. Flush logs
5. Exit 0

Timeout: 10 seconds for graceful shutdown, then force exit.

## 10. Security Considerations

- **No raw RPC passthrough**: Callers cannot send arbitrary JSON-RPC. Only curated tools are exposed.
- **Input validation**: All inputs validated at tool boundary.
- **No private keys**: The server never holds, receives, or logs private keys. All transaction signing is external to the server. This eliminates an entire class of key-management vulnerabilities.
- **Write protection**: Write (prepare) tools disabled by default; require explicit opt-in via `ENABLE_WRITE_TOOLS=true`.
- **RPC isolation**: Upstream RPC errors are mapped to safe error types; raw error details stay in logs.
- **Unsigned tx transparency**: Prepare tools return the full transaction breakdown (to, data, nonce, gas, chain ID) so callers can verify exactly what they're signing.
- **HTTPS**: Production RPC endpoints should use HTTPS.
- **Rate limiting**: Future consideration for HTTP transport (Phase 4).

## 11. Key Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| MCP SDK | Official `go-sdk` v1.4.1 | Maintained by Google/Anthropic, typed tool binding, both transports |
| EVM client | `go-ethereum/ethclient` | Industry standard, well-tested, directly wraps JSON-RPC |
| Logging | `log/slog` (stdlib) | Zero dependencies, structured, good enough for Phase 1-3 |
| Config | Plain env vars | Simplest possible; no framework overhead; 12-factor compliant |
| Normalization | Inside `evm/` package | Reduces package count; normalization is a concern of the EVM layer |
| Anchor isolation | Adapter pattern in `internal/anchor` | Must be swappable without touching EVM or MCP layers |
| Write key management | Prepare-sign-submit (no server keys) | Keys never leave caller's domain; MCP server stays a stateless translator; supports HSM/Vault/any signer without server changes |
| Nonce management | Fetch-at-prepare-time | Correct for serial writes; sufficient for document anchoring volumes; avoids statefulness in the server |
| No `normalize/` package | Merged into `evm/` and `anchor/` | Original plan had too much indirection for the value |
| No `observability/` package (Phase 1) | Deferred to Phase 4 | slog is sufficient; Prometheus adds complexity before it's needed |
