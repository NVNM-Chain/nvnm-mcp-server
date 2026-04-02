# Inveniam EVM MCP Server -- Capabilities Overview

## What It Is

A production-grade [Model Context Protocol](https://modelcontextprotocol.io/) server that gives AI agents, LLMs, and developer tools native access to the Inveniam NVNM blockchain -- the purpose-built L2 for AI Agent and document anchoring and provenance verification, secured by MANTRA's validator set.

This is not a raw JSON-RPC passthrough. It exposes **16 curated, typed tools** with normalized responses designed for both human comprehension and LLM consumption. All blockchain complexity -- ABI encoding, gas estimation, nonce management, transaction construction -- is handled server-side. Callers pass plain parameters and get structured JSON back.

### Key Numbers

| | |
|---|---|
| **16** MCP tools | 8 chain reads, 4 anchor reads, 4 writes |
| **0** private keys on the server | Prepare-sign-submit by design |
| **248** automated tests | Unit, E2E, integration, load, golden |
| **<2ms** p95 latency | Under sustained 75-VU load |

---

## Why It Matters

**No Web3 Required.** ABI encoding, gas estimation, nonce management, and transaction construction are handled server-side. Callers just pass plain parameters and get structured JSON back. No `ethers.js`, no `go-ethereum`, no web3 libraries needed on the client.

**Keys Never Leave You.** The prepare-sign-submit pattern means private keys never touch the server. The MCP server constructs the transaction, your infrastructure signs it (HSM, Vault, local keystore -- your choice), and the server broadcasts it. The server is a stateless translator, not a custodian.

**AI-Native Interface.** Built for the Model Context Protocol from the ground up. Every tool returns typed, normalized JSON with consistent field naming, explicit nulls, and human-readable status strings. Designed for both human developers and LLM tool-calling.

**Human-in-the-Loop Controls.** Configurable write approval via MCP elicitation. Before any transaction broadcasts, the server can prompt the human user with decoded transaction details (recipient, value, gas, chain ID) and require explicit approval. Set per client: `required` for interactive agents, `auto` for trusted automation pipelines.

**Observable by Default.** OpenTelemetry traces and Prometheus metrics on every tool call and upstream RPC request. Per-client identity flows through audit logs and OTel spans. Health and readiness probes for Kubernetes and cloud ALB health checks.

**Production-Grade Security.** Security hardened with a comprehensive security assessment. Bearer token authentication with per-client identity, error sanitization, input size limits, rate limiting, circuit breakers, distroless container, non-root execution, and dependency vulnerability scanning in CI.

---

## Tool Capabilities

### EVM Chain Reads (8 tools)

| Tool | Capability |
|---|---|
| `evm_get_chain_id` | Chain identity and latest block number |
| `evm_get_block` | Block by number or hash, with optional full transaction details |
| `evm_get_transaction` | Transaction details including pending status detection |
| `evm_get_transaction_receipt` | Receipt with status, gas used, and decoded event logs |
| `evm_get_balance` | Address balance in both wei and ether |
| `evm_get_code` | Contract bytecode and smart contract detection |
| `evm_get_logs` | Filtered event logs by address, topics, and block range |
| `evm_call_contract` | Read-only contract calls with hex calldata |

### Document Anchoring -- Read (4 tools)

| Tool | Capability |
|---|---|
| `anchor_info` | Precompile status: address, ABI loaded, available methods |
| `anchor_get_registry` | Fetch a document registry by numeric ID or unique name |
| `anchor_get_registries` | Paginated list of all registries with optional filters |
| `anchor_get_records` | Flexible record query: by version, checksum, registry, or global search |

### Document Anchoring -- Write (4 tools)

| Tool | Capability |
|---|---|
| `anchor_prepare_add_registry` | Construct an unsigned transaction to create a new registry |
| `anchor_prepare_add_record` | Construct an unsigned transaction to anchor a document (checksum + URI) |
| `anchor_prepare_grant_role` | Construct an unsigned transaction to assign admin or editor permissions |
| `evm_send_raw_transaction` | Broadcast a signed transaction with human-in-the-loop approval |

---

## Write Flow: Prepare -- Sign -- Submit

```
Your Agent          MCP Server           Your Signer          NVNM Chain
    |                   |                     |                    |
    |  prepare_add_     |                     |                    |
    |  record(from,     |                     |                    |
    |  registry, ...)   |                     |                    |
    |------------------>|                     |                    |
    |                   |  ABI encode         |                    |
    |                   |  Fetch nonce    --->|                    |
    |                   |  Estimate gas   --->|                    |
    |                   |  Build unsigned tx  |                    |
    |<------------------|                     |                    |
    |  {unsigned_tx,    |                     |                    |
    |   nonce, gas,     |                     |                    |
    |   chain_id}       |                     |                    |
    |                   |                     |                    |
    |  Sign tx bytes  --|-------------------->|                    |
    |  (ECDSA)          |                     |                    |
    |<------------------|---------------------|                    |
    |                   |                     |                    |
    |  send_raw_tx      |                     |                    |
    |  (signed_hex)     |                     |                    |
    |------------------>|                     |                    |
    |                   |  [Human approval]   |                    |
    |                   |  Broadcast      ----|---=--------------->|
    |<------------------|                     |                    |
    |  {tx_hash}        |                     |                    |
```

---

## Security Posture

- Bearer token authentication with per-client identity and full audit trail
- Human-in-the-loop write approval -- configurable per client (`required` or `auto`)
- Private keys never touch the server -- prepare-sign-submit by design
- Error sanitization prevents internal details from leaking to callers
- Request body limits, hex input caps, and HTTP timeouts against DoS
- Rate limiting and circuit breaker on upstream RPC calls
- Distroless container, non-root, read-only filesystem, all capabilities dropped
- Dependency vulnerability scanning (`govulncheck`) in CI with Dependabot

---

## Deployment Options

**Local / stdio** -- Direct integration with Claude Desktop, Cursor, or any MCP client. Single binary, zero config beyond the RPC URL.

**Kubernetes** -- Kustomize manifests and Helm chart included. HPA, NetworkPolicy, ServiceMonitor, Grafana dashboard, and Prometheus alerting rules ready to deploy.

**AWS / Cloud** -- ECS/Fargate-ready Docker image (distroless). ALB health checks on `:9090`, OTel Collector sidecar for CloudWatch/X-Ray, structured JSON logs for CloudWatch Insights.

---

**Stack:** Go 1.26 -- MCP SDK v1.4.1 -- go-ethereum -- OpenTelemetry -- Prometheus -- Distroless
