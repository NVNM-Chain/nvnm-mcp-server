# NVNM Chain MCP Server

[![CI](https://github.com/NVNM-Chain/nvnm-mcp-server/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/NVNM-Chain/nvnm-mcp-server/actions/workflows/ci.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Latest Release](https://img.shields.io/github/v/release/NVNM-Chain/nvnm-mcp-server?include_prereleases&sort=semver)](https://github.com/NVNM-Chain/nvnm-mcp-server/releases)
[![Cosign Signed](https://img.shields.io/badge/cosign-signed-2496ed)](.github/workflows/release.yml)

A typed [Model Context Protocol](https://modelcontextprotocol.io/) bridge between AI agents and the NVNM Chain (Inveniam's L2 on MANTRA). It exposes 21 curated tools — EVM reads, anchor reads, prepare-sign-submit writes, and guided onboarding — with normalized responses, per-tool authorization, and zero key custody. Intended for application developers, LLM-agent authors, and pipeline operators who need a stable, audited surface against an EVM chain rather than raw JSON-RPC.

A Go-based [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) server that exposes the NVNM Chain (Inveniam's L2 on MANTRA) through a curated set of typed tools, with special emphasis on the chain's built-in anchoring interface.

This is **not** a generic JSON-RPC passthrough. It provides stable, typed, high-value MCP tools with normalized responses designed for both human and LLM consumers.

## Request Flow

The HTTP transport layers defense-in-depth middleware around the MCP SDK. Order is outermost first; each layer can short-circuit before the request reaches a tool handler.

```
                  ┌─────────────────────────────────────────────┐
   MCP client ──▶ │ originGuard         DNS-rebinding defense   │
                  │ failGuarded         pre-auth IP rate limit  │
                  │ limitRequestBody    body size cap           │
                  │ AuthMiddleware      apikey or fusionauth    │
                  │ rateLimitMiddleware per-client bucket       │
                  │ mcp.Server (SDK)    JSON-RPC dispatch       │
                  │ tool handler        ABI encode / decode     │
                  │ evm client          retry, breaker, trace   │
                  └──────────────────────┬──────────────────────┘
                                         ▼
                                  EVM JSON-RPC
                            (https://evm.testnet.nvnmchain.io)
```

Source: [`internal/mcp/server.go`](internal/mcp/server.go).

## Documentation

This README is the technical entry point. For deeper context, follow the links below.

**OSS foundation (repo root):**

| File | Purpose |
|---|---|
| [`LICENSE`](LICENSE) | Apache 2.0 license text |
| [`NOTICE`](NOTICE) | Required attribution notice |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | Contribution workflow, DCO, branch / PR conventions |
| [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md) | Community standards |
| [`SECURITY.md`](SECURITY.md) | Vulnerability disclosure policy |
| [`CHANGELOG.md`](CHANGELOG.md) | Per-release notes (Keep a Changelog format) |

**Technical references (`docs/`):**

| File | Purpose |
|---|---|
| [`docs/DESIGN.md`](docs/DESIGN.md) | Architecture decisions; multi-chain deployment model; target-chain reference |
| [`docs/RUNBOOK.md`](docs/RUNBOOK.md) | Operational guide — startup, env-var migration, admin REST API |
| [`docs/INCIDENT_RUNBOOK.md`](docs/INCIDENT_RUNBOOK.md) | Per-alert investigation playbook; what to do when each Prometheus rule fires |
| [`docs/SECURITY_AUDIT.md`](docs/SECURITY_AUDIT.md) | Frozen-snapshot security assessment with remediation results |
| [`docs/DATA_HANDLING.md`](docs/DATA_HANDLING.md) | Privacy-by-design technical reference (what is and isn't stored) |
| [`docs/TERMS.md`](docs/TERMS.md) | Terms of Service for the hosted Service (Apache 2.0 governs the Software) |
| [`docs/KEY_CUSTODY_THREAT_MODEL.md`](docs/KEY_CUSTODY_THREAT_MODEL.md) | Rationale for the zero-key-custody design — no agent-mediated signing |
| [`docs/TOOL_REFERENCE.md`](docs/TOOL_REFERENCE.md) | Per-tool schema reference for all 21 MCP tools |
| [`docs/IMPLEMENTATION_PLAN.md`](docs/IMPLEMENTATION_PLAN.md) | Phased roadmap with goal / depends-on / tasks / exit criteria |

## What this server is not

Cold-reader assumptions to head off up front. The server is deliberately scoped narrow.

- **Not a chain node.** The server talks JSON-RPC to an upstream EVM RPC endpoint. It does not consensus, mine, or hold state beyond connection caches.
- **Not a wallet.** No private keys are ever held server-side. Write tools build complete unsigned transactions and return both a `raw_tx` (for HSM / CLI signers) and a `wallet_tx_request` (for MetaMask / EIP-1193 wallets); the caller signs and broadcasts.
- **Not a custodian or escrow.** The server stores no per-user balance, no document content, no off-chain user records. The only persistent state is the API key store (hashed at rest). All "onboarding state" surfaced by the wizard tools is derived from on-chain balance + nonce at call time.
- **Not an orchestrator.** Tools return a `next_actions` hint array so agents can chain calls themselves. The server does not call other tools internally and does not run multi-step flows on the caller's behalf.

## Target Chain

| Property | Value (testnet) |
|---|---|
| **Network** | NVNM Chain -- Inveniam L2 (MANTRA-secured consumer chain) |
| **EVM chain ID** | `787111` (`0xc02a7`) |
| **Cosmos chain ID** | `nvnm-testnet-1` |
| **Native token** | `mantraUSD` |
| **Gas token** | `wmantraUSD` (wrapped `mantraUSD`, held in EVM wallets) -- pays gas fees |
| **EVM RPC** | `https://evm.testnet.nvnmchain.io` |
| **Cosmos RPC** | `https://rpc.testnet.nvnmchain.io` |
| **EVM explorer** | `https://explorer.evm.testnet.nvnmchain.io` |
| **Anchor precompile** | `0x0000000000000000000000000000000000000A00` |

> **Mainnet** identifiers (EVM chain ID `1611`, Cosmos `nvnm-1`, `*.nvnmchain.io` endpoints) and the full testnet+mainnet reference live in [`docs/DESIGN.md` § Target Chain](docs/DESIGN.md). The server runs as one instance per network.

## Status

**Phases 0–11 engineering-side complete as of 2026-06-02; current release is `v1.0.0-rc.5` (commit `948ef1a`).** Phase 8 closed out on 2026-05-15: foundation types and tool annotations (8.1–8.5), API-key hashing-at-rest migration and constant-time auth (8.6–8.7), five onboarding tools (8.8), the BREAKING env-var hard cut `INVENIAM_*` → `NVNM_*` plus server-identity rename (8.9 — see [`docs/RUNBOOK.md#env-var-migration`](docs/RUNBOOK.md#env-var-migration)), the BREAKING binary + Docker artifact rename (8.13 — `cmd/inveniam-mcp-server/` → `cmd/nvnm-mcp-server/`, image at `ghcr.io/nvnm-chain/nvnm-mcp-server`), the OWASP Top 10 self-audit (8.12), and the security assessment in [`docs/SECURITY_AUDIT.md`](docs/SECURITY_AUDIT.md). Phase 9 (OSS Readiness) shipped through 9.16 across May 18–27: SPDX headers, mainnet cutover playbook, multi-arch Cosign-signed images, secrets scrub, DCO branch protection, the canonical org migration to `NVNM-Chain/nvnm-mcp-server` (9.14), and the keyless-read auth middleware (9.16). Phase 9.15 (public repo flip) is business-gated — engineering work complete, hand-off awaiting the launch moment. Phase 10 (DevOps Foundations) shipped engineering-side on 2026-06-02: HTTP-level error-rate SLI with a `class` label ([`mcp_http_responses_total`](deploy/prometheus/alerts.yaml)), [`docs/INCIDENT_RUNBOOK.md`](docs/INCIDENT_RUNBOOK.md) with per-alert playbooks, Cosign-verify recipe in [`docs/RUNBOOK.md`](docs/RUNBOOK.md), and Phase 9.14 carried-over k8s manifest cleanups (BREAKING for existing deployments). Phase 11 (Product Launch) shipped engineering-side on 2026-06-02: self-serve API-key request endpoint at `POST /api/v1/keys/request` with admin pending-review endpoints and SMTP integration, wallet wizard hook (`needs_wallet` → wallet generator URL), and the engineering-side Terms of Service draft at [`docs/TERMS.md`](docs/TERMS.md). The remaining Phase 11 exit criteria (counsel sign-offs on ToS / Privacy Policy, Anthropic / OpenAI directory submissions, beta cohort onboarding, mailbox provisioning) are non-engineering scope — see [`docs/IMPLEMENTATION_PLAN.md`](docs/IMPLEMENTATION_PLAN.md) for the live tracking.

HTTP transport supports two auth providers (API keys or FusionAuth JWTs) with per-client identity flowing into all audit logs and OTel spans. API keys are stored sha256-hashed at rest and indexed by hash in memory (Phase 8.6); the validator compares hash bytes under constant time and flattens hit/miss timing with a placeholder compare on the miss path (Phase 8.7). A pre-auth IP failure-rate limiter throttles credential stuffing before the auth check runs; per-client MCP rate limiting (post-auth) returns HTTP `429` when exceeded. Per-tool authorization (RBAC) gates each handler on `reader` / `writer` / `admin` / `automation` roles. Origin-header validation (Phase 8.5) provides DNS-rebinding defense at the outermost middleware position; allowlist via `NVNM_ALLOWED_ORIGINS`. Human-in-the-loop write approval via MCP elicitation is configurable per client (`required` or `auto`); the prompt shows the recovered signer address, the first 4 bytes of calldata (method selector), and the chain environment label so consumers can spot signature-substitution attacks. A dedicated admin REST API (default-bound to `127.0.0.1:8081`) enables runtime key management without server restarts.

Write tools construct complete unsigned transactions with both `raw_tx` (for HSM/CLI signers) and `wallet_tx_request` (for MetaMask / EIP-1193 wallets); private keys never touch the server -- see [Write Architecture](#write-architecture-phase-3). Phase 8.4 made EIP-1559 (type-2) the default transaction format; callers that need legacy type-0 set `prefer_legacy_tx: true` on the prepare-tool input. Every tool response carries a `next_actions` hint array (Phase 8.3) so agents can chain calls from response-embedded affordances rather than server-side orchestration. Every tool carries an MCP `ToolAnnotations` payload (Phase 8.2) so clients can tell read-only tools from state-changing ones without inferring spec defaults. Five Phase 8.8 onboarding tools (`nvnm_overview`, `wallet_status`, `nvnm_setup_wizard`, `nvnm_setup_verify_hash`, `nvnm_setup_verify_signature`) walk first-time agents through wallet generation, funding, and on-chain state derivation; the wizard's `funded_active` state is explicit that "has sent any transaction" is not the same as "has anchored" because the chain emits no events by design. OpenTelemetry instrumentation provides traces, metrics, and health check endpoints -- see [Observability](#observability).

The EVM RPC stack uses `github.com/defiweb/go-eth` (MIT) -- `go-ethereum` was removed in 2026-05-13 to comply with the proprietary commercial license policy in `CLAUDE.md`; see `docs/SECURITY_AUDIT.md` for the migration record. Dependencies are vendored (`vendor/`) and CI builds with `-mod=vendor` for supply-chain safety.

## Prerequisites

- Go 1.26+
- Access to the NVNM Chain EVM RPC endpoint
- (Optional) `golangci-lint` for linting
- (Optional) `pre-commit` for git hooks
- (Optional) Docker for containerized deployment

## Quick Start

```bash
# Clone and enter the project
cd NVNM_mcp_server

# Install dev tools and pre-commit hooks
make setup-dev

# See all available commands
make help

# Build
make build

# Configure (minimum required)
export NVNM_EVM_RPC_URL=https://evm.testnet.nvnmchain.io
export NVNM_CHAIN_ID=787111
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
    "nvnm-chain": {
      "command": "/path/to/nvnm-mcp-server",
      "args": ["--transport", "stdio"]
    }
  }
}
```

**HTTP (remote):**
```bash
# Server runs on :8080 by default
export MCP_HTTP_ADDR=:8080
./nvnm-mcp-server --transport http
```

### Authentication (HTTP transport)

When using HTTP transport, authentication is strongly recommended. The server supports two auth providers, selected by `AUTH_PROVIDER` (default: `apikey`).

#### API Key Provider (default)

```bash
export AUTH_PROVIDER=apikey  # default, can be omitted

# Create an API key for a client
make key-create NAME=my-agent

# List all keys
make key-list

# Configure the server to use keys:
export MCP_API_KEYS_FILE=.mcp-keys.json   # Multi-key file (recommended)
# OR
export MCP_API_KEY=your-secret-key         # Single key (dev/test only)
```

#### FusionAuth Provider (OAuth/JWT)

```bash
export AUTH_PROVIDER=fusionauth
export FUSIONAUTH_URL=https://auth.example.com
export FUSIONAUTH_APPLICATION_ID=your-app-uuid
# Optional:
export FUSIONAUTH_ISSUER=https://auth.example.com  # defaults to FUSIONAUTH_URL
export JWT_CLOCK_SKEW=60s                           # default 60s
export JWT_ROLES_CLAIM=roles                        # default "roles"
```

The `automation` role in the JWT maps to `auto` write approval. All other roles default to `required`.

The authenticated client ID (API key ID or JWT `sub`) flows into all audit logs and OTel spans.

## Configuration

All configuration is via environment variables. No config files required.

### Required

| Variable | Description |
|---|---|
| `NVNM_EVM_RPC_URL` | Primary EVM JSON-RPC endpoint |
| `NVNM_CHAIN_ID` | Expected chain ID (`787111` for NVNM testnet, `1611` for mainnet) |
| `NVNM_CHAIN_ENVIRONMENT` | Chain environment label: `testnet` or `mainnet`. Required to disambiguate the per-instance chain pin; legacy `INVENIAM_*` env vars are hard-rejected at startup with a pointer to `docs/RUNBOOK.md#env-var-migration` |

### Authentication (HTTP transport)

| Variable | Default | Description |
|---|---|---|
| `AUTH_PROVIDER` | `apikey` | Auth provider: `apikey` or `fusionauth` |
| `MCP_API_KEYS_FILE` | _(none)_ | Path to JSON key store file (API key mode). |
| `MCP_API_KEY` | _(none)_ | Single API key for dev/test (API key mode). |
| `FUSIONAUTH_URL` | _(none)_ | FusionAuth base URL (required for `fusionauth` mode). |
| `FUSIONAUTH_APPLICATION_ID` | _(none)_ | FusionAuth application UUID (required for `fusionauth` mode). |
| `FUSIONAUTH_ISSUER` | `FUSIONAUTH_URL` | Expected JWT issuer (FusionAuth mode). |
| `FUSIONAUTH_JWKS_URL` | auto | JWKS endpoint (defaults to `FUSIONAUTH_URL/.well-known/jwks.json`). |
| `JWT_CLOCK_SKEW` | `60s` | Leeway for JWT expiry checks (FusionAuth mode). |
| `JWT_ROLES_CLAIM` | `roles` | JWT claim name for roles (FusionAuth mode). |

When auth is configured, HTTP requests must include `Authorization: Bearer <token>`.

### Admin Key Management API

| Variable | Default | Description |
|---|---|---|
| `ADMIN_API_KEY` | _(none)_ | Admin bearer token. Enables the admin REST API on a separate port for runtime key CRUD. |
| `ADMIN_API_ADDR` | `:8081` | Admin API listen address. |

When set (with HTTP transport), a separate server exposes `POST/GET/PATCH/DELETE /admin/keys` for runtime key management. Changes take effect immediately. See `docs/RUNBOOK.md` for full endpoint reference.

### Optional

| Variable | Default | Description |
|---|---|---|
| `NVNM_EVM_ARCHIVE_RPC_URL` | _(none)_ | Archive node RPC for historical queries |
| `ANCHOR_ADDRESS` | `0x0000000000000000000000000000000000000A00` | Anchor precompile address |
| `ANCHOR_ABI_PATH` | _(none)_ | Path to anchor ABI JSON file |
| `REQUEST_TIMEOUT` | `15s` | Timeout for upstream RPC calls |
| `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `ENABLE_WRITE_TOOLS` | `false` | Enable write (prepare) tools |
| `MCP_TRANSPORT` | `stdio` | Transport: `stdio` or `http` |
| `MCP_HTTP_ADDR` | `:8080` | Listen address for HTTP transport |
| `OTLP_INSECURE` | `false` | Use TLS for the OTLP gRPC connection. Set to `true` only for sidecar / localhost collectors that do not support TLS. |
| `WRITE_APPROVAL_DEFAULT` | `required` | Global default for human-in-the-loop write approval. `required` prompts user via MCP elicitation before broadcasting; `auto` skips approval. Per-client overrides via key store. |

### Observability

| Variable | Default | Description |
|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | _(none)_ | OTel Collector endpoint (e.g. `localhost:4317`). Enables OTLP trace + metric export |
| `OTEL_SERVICE_NAME` | `nvnm-mcp-server` | Service name in traces and metrics |
| `ENABLE_PROMETHEUS` | `true` | Expose `/metrics` endpoint on metrics port |
| `ENABLE_STDOUT_TELEMETRY` | `false` | Dump spans/metrics to stderr (dev only) |
| `METRICS_ADDR` | `:9090` | Listen address for health + metrics endpoints |

### Resilience

| Variable | Default | Description |
|---|---|---|
| `MCP_RATE_LIMIT` | `60` | Per-client MCP request rate limit (requests per second). Returns HTTP 429 when exceeded. |
| `MCP_RATE_BURST` | `10` | Per-client burst capacity for the MCP rate limiter. |
| `RPC_MAX_RETRIES` | `3` | Maximum retry attempts for transient RPC errors |
| `RPC_INITIAL_BACKOFF` | `500ms` | Initial backoff duration between retries |
| `RPC_MAX_BACKOFF` | `10s` | Maximum backoff duration between retries |
| `RPC_RATE_LIMIT` | `100` | Upstream RPC rate limit (requests per second) |
| `RPC_RATE_BURST` | `20` | Burst capacity for upstream RPC rate limiter |
| `CIRCUIT_BREAKER_THRESHOLD` | `5` | Consecutive failures to trip circuit breaker |
| `CIRCUIT_BREAKER_TIMEOUT` | `30s` | Time in open state before half-open probe |
| `OTEL_TRACE_SAMPLE_RATIO` | `1.0` | Fraction of traces sampled (0.0-1.0) |

## MCP Tools

21 tools in total. First-time agents should call `nvnm_overview` first; it returns the canonical 6-step journey across the rest of the surface.

### Phase 8.8: Onboarding (5 tools)

| Tool | Description |
|---|---|
| `nvnm_overview` | Lobby tool. Chain identity, privacy-by-design property, 6-step agent journey, prereqs. No chain calls. |
| `wallet_status` | Balance + nonce for an address; three-state status (`unfunded` / `funded_unused` / `funded_active`). |
| `nvnm_setup_wizard` | Four-state guided onboarding with language-specific samples (Python / JS / Go) that store keys via `keyring` / `.env` / `0o600` files (never `print` them). |
| `nvnm_setup_verify_hash` | Stateless challenge: caller proves they can hash a per-address challenge. |
| `nvnm_setup_verify_signature` | Stateless challenge: caller proves they can EIP-191 sign the same challenge. |

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

1. **Prepare** -- Caller invokes `anchor_prepare_add_registry(from, name, description, metadata)`. The MCP server ABI-encodes the call, fetches the nonce for the sender address, estimates gas, and returns a complete unsigned transaction with two signing paths.

2. **Sign & Submit (choose one):**

**Path A -- MetaMask / browser wallet (recommended for human users):**

```js
const prepared = await callMCPTool("anchor_prepare_add_record", {
  from, registry, uri, checksum
});

// Pass wallet_tx_request directly to MetaMask
const txHash = await window.ethereum.request({
  method: "eth_sendTransaction",
  params: [prepared.wallet_tx_request],
});

// Confirm on-chain
const receipt = await callMCPTool("evm_get_transaction_receipt", { tx_hash: txHash });
```

MetaMask signs and broadcasts directly. The response includes `wallet_tx_request` with all numeric fields as `0x`-prefixed hex quantities ready for EIP-1193 wallets. You do not call `evm_send_raw_transaction` in this path.

**Path B -- Local/headless signer (CLI, HSM, automation):**

```python
prepared = mcp.call("anchor_prepare_add_record", {...})

# Sign the raw_tx bytes externally
signed_hex = my_signer.sign(prepared["raw_tx"])

# Broadcast via MCP server
result = mcp.call("evm_send_raw_transaction", {"signed_tx": signed_hex})
receipt = mcp.call("evm_get_transaction_receipt", {"tx_hash": result["tx_hash"]})
```

3. **Verify** -- Use `evm_get_transaction_receipt(tx_hash)` and `anchor_get_records` to confirm the anchor is on-chain.

This pattern means:
- Private keys never touch the MCP server
- Browser wallet users get native MetaMask confirmation prompts
- Signing can happen in an HSM, Vault, hardware wallet, or any secure enclave
- The MCP server handles all the ABI encoding, nonce, and gas estimation

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

Running `make` with no arguments displays the full help menu.

```bash
make help           # Show all available commands (also the default)
make build          # Build binary
make run            # Run with stdio transport
make run-http       # Run with HTTP transport
make run-local      # Build and run locally with HTTP + testnet config
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
make docker-build   # Build Docker image (host arch only)
make docker-buildx  # Multi-arch Docker build (amd64 + arm64) via buildx -- local manual operation
make docker-push    # Multi-arch build and push to registry -- local manual operation, requires registry login
make docker-run     # Run in Docker
make docker-smoke   # Build, run, verify healthz + MCP, tear down
make test-load      # Run k6 load tests (requires k6)
make test-integration # Integration tests against live testnet
make seed-test-data # Create test registry with phoney records on-chain
make clean          # Remove build artifacts
```

### API Key Management

```bash
make key-create NAME=my-agent                            # Create a new API key
make key-create NAME=pipeline APPROVAL=auto              # Create key with auto write approval
make key-list                                            # List all keys (ID, enabled, approval, created)
make key-disable NAME=my-agent                           # Disable a key (rejected at auth)
make key-enable NAME=my-agent                            # Re-enable a disabled key
make key-set-approval NAME=my-agent APPROVAL=auto        # Set write approval policy for a client
```

Keys are stored in `.mcp-keys.json` (gitignored). Set `MCP_API_KEYS_FILE=.mcp-keys.json` to use them.

For comprehensive testing documentation, including test architecture, framework details, and latest results, see [docs/TESTING.md](docs/TESTING.md).

## Deployment

### Docker

```bash
make docker-build
docker run --rm \
  -e NVNM_EVM_RPC_URL=https://evm.testnet.nvnmchain.io \
  -e NVNM_CHAIN_ID=787111 \
  -e ANCHOR_ABI_PATH=/app/abi/anchoring.json \
  -e MCP_TRANSPORT=http \
  -p 8080:8080 \
  -p 9090:9090 \
  nvnm-mcp-server
```

### Kubernetes

Plain YAML manifests are available in `deploy/k8s/`:

```bash
# Apply with default namespace (nvnm-mcp; renamed from inveniam-mcp in PR #6, 2026-06-02 — see RUNBOOK § "K8s manifest migration")
kubectl apply -k deploy/k8s/

# Apply to a specific namespace
kubectl apply -k deploy/k8s/ -n your-namespace
```

A Helm chart is available in `deploy/helm/nvnm-mcp-server/`:

```bash
helm install nvnm-mcp deploy/helm/nvnm-mcp-server/ \
  --set env.NVNM_EVM_RPC_URL=https://evm.testnet.nvnmchain.io \
  --set env.NVNM_CHAIN_ID=787111
```

Prometheus alerting rules are in `deploy/prometheus/alerts.yaml` and a Grafana dashboard in `deploy/grafana/dashboard.json`.

### AWS (ECS/Fargate)

The Docker image is designed for deployment on ECS/Fargate with HTTP transport. Configure environment variables through ECS task definitions. The server exposes health check endpoints on `:9090` (`/healthz`, `/readyz`). Add an `aws-otel-collector` sidecar for CloudWatch/X-Ray telemetry.

### MANTRA Validator Nodes

The server can run directly on MANTRA validator nodes, connecting to `localhost` RPC endpoints for minimal latency. Use stdio transport if co-located with an MCP client, or HTTP transport for remote access.

## Project Structure

```
cmd/
  nvnm-mcp-server/       Entrypoint
  key-mgmt/                  API key management CLI
internal/
  auth/                      Client identity context propagation
  config/                    Environment-based configuration
  logging/                   slog-based structured logging + redaction
  errors/                    Shared sentinel errors + error sanitization
  evm/                       Generic EVM RPC client layer + tracing wrapper
  anchor/                    Inveniam anchor adapter (with address validation)
  mcp/                       MCP tool registration, handlers, auth middleware, key store, admin API
  telemetry/                 OTel providers, MCP middleware, health server, metrics
  version/                   Canonical version constant (single source of truth)
abi/
  anchoring.json             Anchor precompile ABI
deploy/
  k8s/                       Kubernetes manifests (Deployment, Service, HPA, ServiceMonitor, NetworkPolicy)
  helm/nvnm-mcp-server/      Helm chart
  grafana/                   Grafana dashboard JSON
  prometheus/                Prometheus alerting rules
tests/
  load/                      k6 load test scripts
docs/
  DESIGN.md                       Architecture and design decisions
  IMPLEMENTATION_PLAN.md          Phased implementation plan
  PHASE_8_DESIGN.md               Phase 8 design contract (closed out)
  PHASE_9_DESIGN.md               Phase 9 design contract (OSS Readiness)
  SECURITY_AUDIT.md               Security assessment and remediation results
  OWASP_AUDIT.md                  OWASP Top 10 self-audit (Phase 8.12)
  DATA_HANDLING.md                Privacy-by-design technical reference
  PRIVACY_DISCUSSION.md           Working notes for the privacy policy
  TERMS.md                        Terms of Service for the hosted Service
  INCIDENT_RUNBOOK.md             Per-alert investigation playbook
  KEY_CUSTODY_THREAT_MODEL.md     Rationale for zero-key-custody design
  SECURITY_CONSUMER_GUIDANCE.md   Operator-facing security guidance
  LICENSE_EXCEPTIONS.md           Project-scoped license exception register
  MAINNET_CUTOVER.md              Mainnet cutover playbook (Phase 10 input)
  METAMASK_GUIDE.md               End-user MetaMask integration walkthrough
  OVERVIEW.md                     Product-level overview
  WALLET_GENERATOR_DESIGN.md      Wallet generator page design contract (sibling repo)
  TESTING.md                      Test framework, strategy, and results
  TOOL_REFERENCE.md               MCP tool schema reference
  RUNBOOK.md                      Operational runbook
.github/
  workflows/ci.yml           CI pipeline
  dependabot.yml             Automated dependency updates
.pre-commit-config.yaml     Pre-commit hooks
.golangci.yml               Linter configuration
```

## License

Apache License 2.0 -- see [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE). Contributions are accepted under the same terms; see [`CONTRIBUTING.md`](CONTRIBUTING.md).
