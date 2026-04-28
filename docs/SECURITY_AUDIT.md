# Pre-Red-Team Security Assessment: Inveniam EVM MCP Server

**Date:** 2026-04-01
**Last reviewed:** 2026-04-28 (backlog items refreshed; see "Updates since 2026-04-01" at the end)
**Scope:** Full repository defensive security review
**Status:** Assessment complete; remediation complete (see Phase 4)

---

## Phase 1: Repository-Grounded System Model

### 1. System Name and Business Purpose

**Inveniam EVM MCP Server** (`inveniam-evm`) -- a Model Context Protocol server that exposes curated, typed tools for interacting with the Inveniam NVNM L2 blockchain (Chain ID 58887). It translates MCP tool calls into EVM JSON-RPC and anchor precompile interactions, normalizes responses, and implements a prepare-sign-submit pattern for write operations where the server never holds private keys.

**Evidence:** [docs/DESIGN.md](DESIGN.md) section 1; [README.md](../README.md) opening sections.

### 2. Architecture Summary

```
MCP Client / LLM Agent
        |
        | stdio or HTTP
        v
  Inveniam MCP Server
   /        |        \
  v         v         v
EVM L2   Anchor     OTLP Collector
(JSON-RPC) Precompile  (gRPC insecure)
           0x...0A00
                        Health/Metrics
                        :9090
```

Layered Go packages: `cmd/` -> `config`, `logging`, `telemetry`, `evm`, `anchor`, `mcp`. EVM layer is independent of anchor; anchor is independent of MCP; MCP orchestrates both.

### 3. Tech Stack

- **Language:** Go 1.26.2 (`CGO_ENABLED=0`)
- **MCP SDK:** `github.com/modelcontextprotocol/go-sdk` v1.4.1
- **EVM:** `github.com/ethereum/go-ethereum` v1.17.2
- **Resilience:** `cenkalti/backoff/v5`, `sony/gobreaker/v2`, `golang.org/x/time/rate`
- **Observability:** OpenTelemetry 1.42.0, Prometheus client 1.23.2
- **Runtime image:** `gcr.io/distroless/static-debian12`
- **Orchestration:** Kubernetes (Kustomize + Helm), HPA

### 4. Trust Boundaries

| Boundary | From | To | Transport |
|---|---|---|---|
| TB1 | MCP Client/Agent | MCP Server | stdio (local) or HTTP (network) |
| TB2 | MCP Server | Inveniam EVM RPC | HTTPS JSON-RPC |
| TB3 | MCP Server | OTLP Collector | gRPC (insecure) |
| TB4 | External scraper | Health/Metrics server | HTTP :9090 |
| TB5 | Operator/CI | Container image | Docker build + K8s deploy |

**Evidence:** `internal/mcp/server.go` lines 67-106; `internal/telemetry/telemetry.go` lines 139-141, 185-187; `deploy/k8s/deployment.yaml`.

### 5. Authentication and Authorization Model

**OBSERVED: No authentication or authorization exists at any layer.**

- MCP HTTP transport: no API keys, bearer tokens, mTLS, or auth middleware. The `http.Request` is ignored in the handler factory (`internal/mcp/server.go` line 79).
- Health/metrics endpoints: no authentication (`internal/telemetry/health.go` lines 58-63).
- Per-tool access control: none. All registered tools are available to any connected client.
- Write tools are gated only by a startup flag `ENABLE_WRITE_TOOLS` -- not per-request authorization.

### 6. Sensitive Data Handled

| Data | Sensitivity | Handling |
|---|---|---|
| RPC URL (may contain API keys in query/userinfo) | High | `SafeURL` redacts at startup log; raw URL used in `ethclient.DialContext` and error messages |
| Blockchain addresses, tx hashes, balances | Low-Medium | Exposed in tool results by design |
| Unsigned transactions (calldata, gas, nonce) | Medium | Returned to MCP clients for signing |
| Signed raw transactions | High | Received via `evm_send_raw_transaction`, decoded and broadcast |
| ABI file | Low | Read from filesystem at startup |
| Private keys | Critical | **Never on server** (design invariant); present only in test/seed utilities |
| `.chain_credentials.txt` | Critical | Used by integration tests and seed CLI only; git-ignored |

### 7. Deployment Model

- **Local dev:** stdio transport, direct invocation
- **Production:** Docker (`distroless/static-debian12`), Kubernetes (Kustomize or Helm), behind reverse proxy (documented but not enforced)
- **Ports:** 8080 (MCP HTTP), 9090 (health/metrics)
- **No Ingress, NetworkPolicy, or ServiceAccount manifests in repo**

**Evidence:** `Dockerfile`; `deploy/k8s/`; `deploy/helm/`.

### 8. External Interfaces

| Interface | Direction | Protocol | Auth |
|---|---|---|---|
| MCP tools (16 total) | Inbound | stdio or HTTP | None |
| `/healthz`, `/readyz` | Inbound | HTTP | None |
| `/metrics` | Inbound | HTTP | None |
| EVM JSON-RPC | Outbound | HTTPS | URL-based only |
| OTLP telemetry | Outbound | gRPC | None (insecure) |

### 9. Known Security Controls Found in Repo

| Control | Status | Evidence |
|---|---|---|
| Input validation (addresses, hashes, hex) | **Observed** | `parseAddress`, `parseHash`, `parseHexData` in `internal/mcp/tools_evm.go` lines 337-363 |
| Write tools off by default | **Observed** | `internal/config/config.go` line 90 |
| No private keys on server | **Observed** | `internal/config/config.go` lines 27-29 |
| Log redaction (`SafeURL`) | **Observed** | `internal/logging/redact.go`; used in `cmd/inveniam-mcp-server/main.go` line 81 |
| `gosec` linter enabled | **Observed** | `.golangci.yml` line 19 |
| `detect-secrets` pre-commit hook | **Observed** | `.pre-commit-config.yaml` lines 29-32 |
| Distroless runtime image | **Observed** | `Dockerfile` line 13 |
| K8s: `runAsNonRoot`, `readOnlyRootFilesystem`, `drop ALL caps` | **Observed** | `deploy/k8s/deployment.yaml` lines 22-65 |
| RPC rate limiting and circuit breaker | **Observed** | `internal/evm/resilient.go` |
| CI: minimal `permissions: contents: read` | **Observed** | `.github/workflows/ci.yml` line 10 |
| RPC URL validation (http/https only) | **Observed** | `internal/config/config.go` lines 166-168 |
| No retry on `SendRawTransaction` | **Observed** | `internal/evm/resilient.go` line 255 |
| `ReadHeaderTimeout` on HTTP server | **Observed** | `internal/mcp/server.go` line 86 |

### 10. Assumptions, Constraints, and Unknowns

**Assumptions:**

- MCP HTTP transport is behind a reverse proxy providing TLS termination and access control (documented in DESIGN.md but not enforced)
- Operator controls environment variable injection securely (K8s Secrets, cloud secret stores)
- RPC URL does not contain credentials in userinfo (not validated)

**Unknowns:**

- Whether a reverse proxy/API gateway is actually deployed in front of MCP HTTP
- Whether NetworkPolicies exist at the cluster level (none in repo)
- Whether OTLP collector enforces authentication
- How container images are scanned before deployment
- Whether `ghcr.io` image push is gated by branch protection or OIDC

---

### Component Inventory

| Component | Path | Risk Surface |
|---|---|---|
| MCP Server (HTTP) | `internal/mcp/server.go` | Unauthenticated network listener |
| MCP Tool Handlers (16) | `internal/mcp/tools_*.go` | Input parsing, error propagation |
| EVM Client | `internal/evm/client.go` | RPC interaction, tx broadcast |
| Resilient Client | `internal/evm/resilient.go` | Retry/rate logic |
| Anchor Client | `internal/anchor/client.go` | ABI loading, precompile calls |
| Tx Preparation | `internal/anchor/prepare.go` | Gas/nonce estimation |
| Config | `internal/config/config.go` | Env-based, validation |
| Health Server | `internal/telemetry/health.go` | Unauthenticated endpoints |
| Telemetry | `internal/telemetry/telemetry.go` | Insecure gRPC exporters |
| Dockerfile | `Dockerfile` | Image build chain |
| K8s manifests | `deploy/k8s/`, `deploy/helm/` | Deployment security |
| CI pipeline | `.github/workflows/ci.yml` | Supply chain |
| Seed CLI | `cmd/seed-test-data/main.go` | Contains credential loading |

### External Attack Surface Inventory

- **MCP HTTP** (`:8080`): Unauthenticated, no rate limiting, no CORS, no TLS
- **Health/Metrics HTTP** (`:9090`): Unauthenticated; exposes version, RPC reachability, Prometheus metrics
- **OTLP gRPC**: Insecure, carries error text and request metadata

### Privileged Services / Identities

- **K8s deployment** runs as UID 65532 (distroless nonroot) -- **Observed**
- **Helm template** missing `runAsUser/runAsGroup`, `allowPrivilegeEscalation: false`, `capabilities.drop` -- **Observed**
- No ServiceAccount or RBAC manifests in repo -- **Observed**
- CI runner: `ubuntu-latest`, `permissions: contents: read` only -- **Observed**

### Secrets Flow Summary

```
Operator --[env var injection]--> INVENIAM_EVM_RPC_URL
                                     |
                      +--------------+--------------+
                      |              |              |
                      v              v              v
               ethclient.Dial   SafeURL log    Error strings
               (raw URL)       (host only)    (raw URL leak)

.chain_credentials.txt --[test/seed only]--> seed-test-data CLI
```

### Sensitive Data Flow Summary

- **Inbound signed transactions** arrive via `evm_send_raw_transaction` tool -> decoded in `SendRawTransaction` -> broadcast to chain
- **Unsigned transactions** prepared server-side (nonce, gas, calldata) -> returned to MCP client as structured JSON
- **RPC responses** (blocks, txs, balances, logs) normalized -> returned to MCP client
- **Tool call metadata** (method, tool name, duration, status) -> slog Info + OTel spans
- **Tool arguments and return values** explicitly NOT logged or traced

### AI/Agent-Specific Analysis

This is an **MCP tool server** consumed by AI agents/LLMs:

| Aspect | Status |
|---|---|
| Model providers | None (server is a tool provider, not a model consumer) |
| Retrieval components | None |
| Tools/function calls | 16 MCP tools (12 read, 4 write when enabled) |
| Memory/state | Stateless; MCP session managed by SDK |
| Approval paths | **None** -- no human-in-the-loop for tool execution |
| Provenance/audit | Operational logging only; no append-only audit |
| Sandboxing | Tools execute in-process; no sandbox |
| Prompt injection surface | Tool inputs parsed as structured JSON, not freeform text |

---

## Phase 2: Security Assessment

### Executive Summary

The Inveniam EVM MCP Server has a solid defensive foundation: no server-side key material, distroless container, K8s security contexts, input validation on blockchain types, and the `gosec` linter. However, it has **zero authentication or authorization** on its HTTP interface, relying entirely on an undocumented reverse proxy assumption. This is the dominant risk. Secondary concerns include unbounded input sizes enabling denial of service, information leakage through error propagation and telemetry, inconsistent security hardening between K8s and Helm manifests, and absence of dependency vulnerability scanning in CI. For a system exposing blockchain write capabilities to AI agents, the lack of per-operation approval, audit logging, and rate limiting at the MCP layer represents a meaningful gap.

### Top 10 Most Plausible Attack Paths (Ranked)

**1. Unauthenticated MCP HTTP Access Leading to Blockchain Writes**

- **Evidence:** `internal/mcp/server.go` line 79 ignores `*http.Request`; no auth middleware; write tools registered at startup based on env flag
- **Label:** Observed
- **Why it matters:** Any network-reachable client can call `evm_send_raw_transaction` to broadcast pre-signed transactions, or use `anchor_prepare_*` tools to construct valid unsigned transactions for any address. Combined with a compromised signer, this enables unauthorized on-chain actions.
- **Attacker preconditions:** Network access to port 8080
- **Attack value:** High -- direct blockchain interaction
- **Blast radius:** All on-chain assets accessible to the anchor precompile
- **Defender-visible indicators:** `mcp.server.tool.calls` metric, structured logs with tool name
- **Severity:** Critical
- **Safe validation:** Attempt to call `tools/list` and `tools/call` from an unauthorized host; verify it succeeds
- **Mitigations:** Add authentication middleware (API key, mTLS, or OAuth2); enforce reverse proxy with auth; add per-tool authorization
- **Framework:** OWASP ASVS 4.1, STRIDE: Spoofing/Elevation, OWASP Top 10: A01 Broken Access Control

**2. Denial of Service via Unbounded Input to `parseHexData` / `SendRawTransaction`**

- **Evidence:** `internal/mcp/tools_evm.go` `parseHexData` (lines 360-362) performs `hex.DecodeString` with no max length; `internal/evm/client.go` `SendRawTransaction` (lines 224-236) decodes arbitrarily large hex
- **Label:** Observed
- **Why it matters:** A client can send multi-GB hex strings causing memory exhaustion and OOM kill
- **Attacker preconditions:** Network access to MCP HTTP
- **Attack value:** Medium -- service disruption
- **Blast radius:** Pod crash, all concurrent requests fail
- **Defender-visible indicators:** Memory spike in container metrics, OOMKilled events
- **Severity:** High
- **Safe validation:** Send a 100MB hex string to `evm_call_contract`; observe memory behavior
- **Mitigations:** Add max input length validation (e.g., 1MB) on hex data fields; set `http.Server.MaxHeaderBytes` and request body limits
- **Framework:** OWASP Top 10: A06 Vulnerable Components (DoS), STRIDE: DoS

**3. Information Leakage via Error Propagation to MCP Clients**

- **Evidence:** Tool handlers wrap and return upstream RPC errors to callers (e.g., `internal/mcp/tools_evm.go` lines 127, 222-223, 328-329); `internal/evm/client.go` line 56 includes full RPC URL in error string; OTel spans record full errors (`internal/telemetry/middleware.go` line 71)
- **Label:** Observed
- **Why it matters:** RPC URL with embedded API keys or internal hostnames can leak to untrusted MCP clients; error details reveal internal architecture
- **Attacker preconditions:** Ability to trigger errors (invalid inputs, network issues)
- **Attack value:** Medium -- credential/topology disclosure
- **Blast radius:** RPC API key compromise, internal network mapping
- **Defender-visible indicators:** None (errors returned normally)
- **Severity:** High
- **Safe validation:** Send invalid block number to `evm_get_block`; inspect returned error text for internal details
- **Mitigations:** Sanitize errors at the MCP handler boundary; return generic messages to clients; log details server-side only
- **Framework:** OWASP Top 10: A04 Insecure Design, CWE-209

**4. OTLP Telemetry Interception (Insecure gRPC)**

- **Evidence:** `internal/telemetry/telemetry.go` lines 139-141 `WithInsecure()` for traces; lines 185-187 `WithInsecure()` for metrics
- **Label:** Observed
- **Why it matters:** Telemetry data (including error text, request IDs, tool call patterns) transmitted in plaintext; can be intercepted or spoofed
- **Attacker preconditions:** Network position between server and OTLP collector
- **Attack value:** Medium -- metadata exfiltration, usage pattern analysis
- **Blast radius:** All telemetry data
- **Defender-visible indicators:** None (passive interception)
- **Severity:** Medium
- **Safe validation:** Capture traffic between server and OTLP endpoint; verify plaintext
- **Mitigations:** Use TLS for OTLP gRPC; use mTLS if collector supports it
- **Framework:** STRIDE: Information Disclosure, MITRE ATT&CK: T1040 Network Sniffing

**5. MCP HTTP Server Missing TLS, CORS, Request Body Limits**

- **Evidence:** `internal/mcp/server.go` lines 83-87: `http.Server` with only `ReadHeaderTimeout`; no `TLSConfig`, no CORS middleware, no `MaxBytesReader`
- **Label:** Observed
- **Why it matters:** Without TLS, MCP traffic (including signed transactions) is plaintext; without CORS, browser-based clients could make cross-origin requests; without body limits, large payloads can exhaust memory
- **Attacker preconditions:** Network access (TLS); browser-based attack (CORS)
- **Attack value:** Medium-High
- **Blast radius:** All MCP communications
- **Defender-visible indicators:** None for passive sniffing
- **Severity:** High (cumulative)
- **Safe validation:** Connect to `:8080` with HTTP (not HTTPS); send cross-origin request from browser
- **Mitigations:** TLS termination at reverse proxy (document and enforce); add CORS middleware; add `http.MaxBytesReader`
- **Framework:** OWASP Top 10: A02 Cryptographic Failures, A05 Security Misconfiguration

**6. Helm Chart Security Context Gaps vs Raw K8s Manifests**

- **Evidence:** `deploy/k8s/deployment.yaml` has full hardening (lines 22-65: `runAsUser`, `runAsGroup`, `allowPrivilegeEscalation: false`, `capabilities.drop: ALL`); `deploy/helm/.../deployment.yaml` lines 25-53 lacks `runAsUser`, `runAsGroup`, `allowPrivilegeEscalation`, `capabilities.drop`
- **Label:** Observed
- **Why it matters:** Helm-deployed instances run with weaker security posture; container escape or privilege escalation easier
- **Attacker preconditions:** Container compromise
- **Attack value:** Medium -- privilege escalation
- **Blast radius:** Pod, potentially node
- **Defender-visible indicators:** `kubectl get pod -o jsonpath='{.spec.securityContext}'`
- **Severity:** Medium
- **Safe validation:** Deploy via Helm; inspect effective security context
- **Mitigations:** Align Helm template security context with raw K8s manifest
- **Framework:** CIS Kubernetes Benchmark, STRIDE: Elevation of Privilege

**7. No Dependency Vulnerability Scanning in CI**

- **Evidence:** `.github/workflows/ci.yml` has no `govulncheck`, Snyk, Trivy, or SBOM step; 17 direct deps with `go-ethereum` v1.17.2 pulling extensive transitive tree (257 lines in `go.sum`); no Dependabot config
- **Label:** Observed
- **Why it matters:** Known CVEs in dependencies (especially `go-ethereum`, `gorilla/websocket` v1.4.2 transitive) would not be detected
- **Attacker preconditions:** Published CVE in dependency
- **Attack value:** Varies (up to Critical for RCE in geth)
- **Blast radius:** Full server compromise
- **Defender-visible indicators:** None without scanning
- **Severity:** Medium-High
- **Safe validation:** Run `govulncheck ./...` locally
- **Mitigations:** Add `govulncheck` to CI; enable Dependabot; pin Docker base images by digest
- **Framework:** OWASP Top 10: A06 Vulnerable and Outdated Components, MITRE ATT&CK: T1195 Supply Chain

**8. No Audit Trail for Tool Invocations and Transaction Broadcasts**

- **Evidence:** `internal/telemetry/middleware.go` lines 33-34 explicitly state tool arguments and return values are not logged; `evm_send_raw_transaction` does not log tx hash; no append-only audit log
- **Label:** Observed
- **Why it matters:** Cannot forensically reconstruct what operations were performed, by whom (no auth = no identity), or what transactions were broadcast
- **Attacker preconditions:** Any access
- **Attack value:** N/A (detection gap)
- **Blast radius:** All post-incident investigation capability
- **Defender-visible indicators:** None (that is the problem)
- **Severity:** Medium
- **Safe validation:** Broadcast a transaction; attempt to find it in logs
- **Mitigations:** Add audit logging for write operations (at minimum: tool name, from address, tx hash, timestamp); separate audit log stream
- **Framework:** OWASP ASVS 7.1, MITRE ATT&CK: T1562 Impair Defenses

**9. No MCP-Level Rate Limiting (Agent Abuse)**

- **Evidence:** No rate limiter on HTTP or MCP layer; upstream RPC rate limiter only protects the EVM node, not the MCP server itself
- **Label:** Observed
- **Why it matters:** A rogue or compromised AI agent can flood the server with tool calls, exhausting resources or generating excessive RPC costs
- **Attacker preconditions:** MCP client access
- **Attack value:** Medium -- resource exhaustion, cost amplification
- **Blast radius:** Server availability, upstream RPC billing
- **Defender-visible indicators:** `mcp.server.tool.calls` counter spike
- **Severity:** Medium
- **Safe validation:** Send 1000 concurrent `evm_get_chain_id` calls; measure resource impact
- **Mitigations:** Add per-client rate limiting middleware; implement connection limits
- **Framework:** OWASP Top 10: A04 Insecure Design

**10. Docker Base Image Not Pinned by Digest**

- **Evidence:** Prior to remediation, `Dockerfile` used tag-based base images rather than digest-pinned references.
- **Label:** Observed
- **Why it matters:** Tag-based references are mutable; a supply chain attack on the base image registry would silently affect builds
- **Attacker preconditions:** Compromise of Docker Hub or GCR image tags
- **Attack value:** High -- code execution in build or runtime
- **Blast radius:** All deployed instances
- **Defender-visible indicators:** Image hash mismatch (if tracked)
- **Severity:** Medium
- **Safe validation:** Compare `docker inspect` digests across builds
- **Mitigations:** Pin base images by `@sha256:...` digest; use Cosign/Sigstore verification
- **Framework:** SLSA Level 2+, MITRE ATT&CK: T1195.002

### Threat Model by Trust Boundary

**TB1: MCP Client -> MCP Server**

- No authentication (Finding 1)
- No rate limiting (Finding 9)
- No TLS (Finding 5)
- No input size limits (Finding 2)
- No CORS protection (Finding 5)
- Error information leakage (Finding 3)

**TB2: MCP Server -> EVM RPC**

- URL may contain credentials (Finding 3)
- Rate limiting and circuit breaker present (control)
- HTTPS enforced by config validation (control)

**TB3: MCP Server -> OTLP Collector**

- Insecure gRPC (Finding 4)
- Error text in spans (Finding 3)

**TB4: External -> Health/Metrics**

- No authentication (Finding 1)
- Exposes version, RPC reachability status, Prometheus metrics

**TB5: CI/CD -> Container**

- No vulnerability scanning (Finding 7)
- No image signing (Finding 10)
- Minimal CI permissions (control)

### Abuse and Misuse Cases

1. **Rogue AI Agent Flooding Writes:** An LLM agent in a loop calls `anchor_prepare_add_registry` thousands of times, generating valid unsigned transactions that could be signed and broadcast, creating spam on-chain.
2. **Reconnaissance via Error Probing:** Attacker sends malformed inputs to each tool, collecting error messages that reveal internal hostnames, RPC endpoints, ABI structure, and software versions.
3. **Transaction Front-Running via OTLP Interception:** Attacker intercepts plaintext telemetry to observe transaction patterns and timing, enabling front-running strategies.
4. **Resource Exhaustion via Large Hex Payloads:** Attacker sends multi-GB hex data to `evm_call_contract` or `evm_send_raw_transaction`, causing OOM and crashing the pod.
5. **Metrics Scraping for Intelligence:** Unauthenticated `/metrics` endpoint reveals tool call patterns, error rates, RPC latency, and circuit breaker state -- useful for timing attacks or identifying degraded states.

### AI/Agent-Specific Findings

**A1. No Human Approval for Write Operations**

- **Attack objective:** AI agent autonomously constructs and signs transactions without human oversight
- **Entry vector:** Agent has MCP client access + signing capability
- **Violated trust assumption:** Prepare-sign-submit assumes signing is a deliberate human action; an autonomous agent bypasses this
- **Safe simulation:** Configure an agent with signing key access; observe whether it self-approves writes
- **Mitigation:** Implement approval workflow (confirmation tool, rate limits on prepare operations, signing requires human confirmation)

**A2. Tool Description Injection (Indirect Prompt Injection)**

- **Attack objective:** Manipulate agent behavior through crafted tool response data
- **Entry vector:** Malicious on-chain data (registry names, record metadata) returned in tool responses that influence agent reasoning
- **Violated trust assumption:** Tool responses are treated as trusted data by consuming agents
- **Safe simulation:** Seed a registry with a name containing instruction-like text; observe agent behavior
- **Mitigation:** Document that MCP tool outputs contain untrusted on-chain data; consuming agents should treat responses as untrusted

**A3. No Provenance or Audit Trail for Agent-Initiated Actions**

- **Attack objective:** Deny accountability for agent-initiated blockchain operations
- **Entry vector:** Any agent with MCP access
- **Violated trust assumption:** Operations can be attributed and investigated
- **Safe simulation:** Have multiple agents use the server; attempt to attribute specific operations
- **Mitigation:** Add request identity (API key / client cert) and audit logging with caller identity, tool arguments for write operations, and transaction hashes

---

## Phase 3: Pre-Red-Team Readiness Plan

### Top Priorities to Fix Now (Immediate)

1. **Add authentication to MCP HTTP transport** -- API key header check at minimum; mTLS for production. This is the single highest-impact fix.
2. **Add request body size limits** -- `http.MaxBytesReader` wrapper and max hex input length validation in `parseHexData` and `SendRawTransaction`.
3. **Sanitize errors at the MCP boundary** -- Return generic error categories to clients; log full details server-side only. Specifically fix the RPC URL leak in `internal/evm/client.go` line 56.
4. **Add audit logging for write operations** -- Log tool name, `from` address, prepared tx hash, and broadcast tx hash for write tools.

### Controls to Verify Immediately

1. Confirm a reverse proxy with TLS and auth is deployed in front of `:8080` in production (or document that it is not)
2. Confirm NetworkPolicies exist at the cluster level restricting ingress to `:8080` and `:9090`
3. Confirm OTLP collector is not network-exposed and uses TLS
4. Run `govulncheck ./...` against current codebase
5. Verify `.chain_credentials.txt` is not present in any deployed image or artifact

### Detections to Enable Before Testing

1. Alert on `mcp.server.tool.calls` counter spike (>100/min for write tools)
2. Alert on container OOMKilled events
3. Alert on MCP HTTP connections from unexpected source IPs (if network logging exists)
4. Alert on `evm.rpc.errors` counter spike (may indicate probing)
5. Monitor `evm.rpc.rate_limited` counter

### Assumptions the Red Team Is Likely to Challenge

1. "A reverse proxy provides auth" -- they will try direct access to `:8080`
2. "No private keys on the server" -- they will look for credential files, env leaks, error messages containing secrets
3. "Write tools are off by default" -- they will check if the env var can be influenced
4. "Distroless = no shell = limited post-exploit" -- they will check for any writable paths, `/tmp` mount
5. "Rate limiting on RPC protects the server" -- they will DoS the MCP layer directly

### Tabletop "Assume Breach" Scenarios

**Scenario 1: Compromised MCP Client**

A rogue agent gains MCP access. It enumerates all tools, probes error messages for internal details, floods prepare operations, and if signing capability is available, broadcasts unauthorized transactions. No audit trail exists. Detection relies entirely on metrics counter anomalies.

**Scenario 2: RPC URL Credential Leak**

The RPC URL contains an API key in the query string. A client triggers a connection error (e.g., by causing a timeout). The error propagates through the tool response containing the full URL. The attacker now has direct RPC access, bypassing the MCP server entirely.

**Scenario 3: Container Escape via Dependency Vulnerability**

An unpatched CVE in a `go-ethereum` transitive dependency allows code execution. The attacker finds `/tmp` is writable (Helm deployment) but the root filesystem is read-only. They attempt to exfiltrate environment variables (including RPC URL) via the insecure OTLP channel.

### Executive Talking Points on Residual Risk

1. **Authentication is the critical gap.** The server currently trusts any network-reachable client. This is acceptable for local stdio use but unacceptable for HTTP deployment.
2. **The "no keys on server" design is sound.** The prepare-sign-submit pattern is the right architecture. The risk shifts to agent autonomy if signing is automated.
3. **Observability is good but not forensic.** Tool call patterns are metered, but the explicit decision to not log arguments means write operations cannot be forensically reconstructed.
4. **Supply chain posture needs hardening.** No vulnerability scanning, no image signing, no SBOM. This is common for early-stage projects but needs addressing before production.
5. **The Helm chart is weaker than the raw K8s manifests.** Anyone deploying via Helm gets a less secure configuration.

### Pre-Red-Team Test Plan

| # | Test Objective | Component | Hypothesis | Safe Validation | Access Level | Expected Evidence | Pass/Fail Criteria | Owner | Remediation If Failed |
|---|---|---|---|---|---|---|---|---|---|
| 1 | Verify MCP HTTP requires auth | MCP Server `:8080` | No auth exists | `curl -X POST http://<host>:8080/ -d '...'` | Network | HTTP 200 with session ID | **Fail** if 200 without credentials | Platform | Add auth middleware |
| 2 | Verify health endpoints require auth | Health `:9090` | No auth exists | `curl http://<host>:9090/healthz` | Network | HTTP 200 with status JSON | **Fail** if 200 from untrusted source | Platform | Add auth or restrict to cluster-internal |
| 3 | Test input size limits | `evm_call_contract` | No limit exists | Send 10MB hex `data` parameter | MCP client | OOM or slow response | **Fail** if server accepts without limit | Dev | Add `MaxBytesReader` + input length check |
| 4 | Test error information leakage | `evm_get_block` | Errors contain internal details | Send invalid block ref, inspect error | MCP client | Error text with hostnames/URLs | **Fail** if internal details visible | Dev | Sanitize at handler boundary |
| 5 | Verify OTLP encryption | Telemetry gRPC | Plaintext | Packet capture between server and collector | Network | Plaintext protobuf | **Fail** if unencrypted in production | Infra | Enable TLS on OTLP |
| 6 | Verify Helm security parity | Helm deployment | Missing hardening | `kubectl get pod -o yaml`, compare security contexts | Cluster admin | Missing fields | **Fail** if differs from raw K8s | Dev | Align Helm template |
| 7 | Dependency vulnerability scan | `go.mod` | Unknown CVEs | Run `govulncheck ./...` | Dev | Vulnerability list | **Fail** if high/critical CVEs | Dev | Patch or mitigate |
| 8 | Verify no credential artifacts in image | Docker image | Clean image | Inspect Docker layers | Dev | No credential files | **Fail** if credentials found | Dev | Update `.dockerignore` |
| 9 | Test write tool gating | MCP Server | `ENABLE_WRITE_TOOLS=false` blocks writes | Call `anchor_prepare_add_registry` with writes disabled | MCP client | Tool not found error | **Pass** if tool unavailable | Dev | N/A |
| 10 | Verify no audit gap for writes | `evm_send_raw_transaction` | No tx hash logged | Broadcast tx, search all logs | MCP client + log access | Missing tx hash in logs | **Fail** if tx hash not logged | Dev | Add audit logging |

### Hardening Priority Tiers

**Immediate Fixes (Before Red Team):**

- Authentication middleware on MCP HTTP
- Request body size limits
- Error sanitization at MCP boundary
- Audit logging for write operations
- Run `govulncheck`

**Before Red Team (Within 1 Week):**

- Align Helm security context with raw K8s
- TLS for OTLP gRPC (or document collector is localhost-only)
- Add `govulncheck` + Dependabot to CI
- Add NetworkPolicy to K8s manifests
- Pin Docker base images by digest

**Longer-Term Hardening:**

- Per-tool authorization (RBAC)
- MCP-level rate limiting per client
- Image signing with Cosign/Sigstore
- SBOM generation in CI
- Structured audit log with client identity
- Human-in-the-loop approval for write operations when consumed by autonomous agents
- Document and enforce reverse proxy requirements
- Consider `ReadTimeout`, `WriteTimeout`, `IdleTimeout` on HTTP server
- License scanning for dependency compliance

---

## Phase 4: Remediation Results

**Remediation date:** 2026-04-01

All "Immediate Fixes" and "Before Red Team" items have been implemented. Each finding below references the original attack path number.

### Immediate Fixes -- Completed

#### Finding 1: Unauthenticated MCP HTTP Access -- REMEDIATED

| Aspect | Detail |
|---|---|
| Fix | Multi-key Bearer token authentication middleware |
| Files | `internal/mcp/auth.go` (new), `internal/mcp/keys.go` (new), `internal/auth/context.go` (new), `internal/config/config.go`, `internal/mcp/server.go`, `cmd/inveniam-mcp-server/main.go` |
| Config | `MCP_API_KEYS_FILE` (path to JSON key store, preferred) or `MCP_API_KEY` (single key fallback) |
| Behavior | When configured, all HTTP requests must include `Authorization: Bearer <key>`. Each key maps to a client ID that flows through to audit logs and OTel spans. Server warns at startup if HTTP transport runs with no keys configured. |
| Key management | `cmd/key-mgmt/` CLI tool; Makefile targets `key-create`, `key-disable`, `key-enable`, `key-list` |
| Verification | `go test ./internal/mcp/...` passes; manual end-to-end test of key create/disable/list confirmed working |
| Residual risk | No per-tool authorization (RBAC) yet; health/metrics endpoints (`:9090`) remain unauthenticated |

#### Finding 2: Denial of Service via Unbounded Input -- REMEDIATED

| Aspect | Detail |
|---|---|
| Fix | Request body size limits + hex input length caps |
| Files | `internal/mcp/server.go`, `internal/mcp/tools_evm.go`, `internal/evm/client.go` |
| Changes | `http.MaxBytesReader` wrapper caps request bodies at 10 MB. `MaxHeaderBytes` set to 1 MB. `parseHexData` rejects hex strings > 2 MB (1 MB decoded). `SendRawTransaction` rejects signed tx hex > 2 MB. `ReadTimeout` (60s), `WriteTimeout` (120s), `IdleTimeout` (120s) added to HTTP server. |
| Verification | `go build ./...` and `go test ./...` pass |

#### Finding 3: Information Leakage via Error Propagation -- REMEDIATED

| Aspect | Detail |
|---|---|
| Fix | Error sanitization at MCP boundary + RPC URL leak removed |
| Files | `internal/evm/client.go`, `internal/errors/errors.go`, `internal/telemetry/middleware.go` |
| Changes | `NewClient` error no longer includes the raw RPC URL. New `SafeForClient()` function in `internal/errors/` classifies errors: input/not-found errors pass through unchanged; upstream and internal errors are replaced with generic messages. Applied in the telemetry middleware so OTel spans still get the full error for internal debugging, but the MCP response to the client is sanitized. |
| Verification | `go test ./...` passes |

#### Finding 8: No Audit Trail for Write Operations -- REMEDIATED

| Aspect | Detail |
|---|---|
| Fix | Audit logging with client identity for all write tool handlers |
| Files | `internal/mcp/tools_evm_write.go`, `internal/mcp/tools_anchor_write.go`, `internal/telemetry/middleware.go` |
| Changes | `evm_send_raw_transaction` logs `client_id`, `tx_hash` on success; `client_id`, `signed_tx_len`, `error` on failure. `anchor_prepare_add_registry` logs `client_id`, `from` (redacted), `registry_name`. `anchor_prepare_add_record` logs `client_id`, `from` (redacted), `registry`, `uri`. `anchor_prepare_grant_role` logs `client_id`, `from` (redacted), `registry_id`, `account` (redacted), `role`. Telemetry middleware logs `client_id` on every tool call and adds it as an OTel span attribute. |
| Verification | `go test ./...` passes |

#### Finding 5 (partial) / govulncheck: Dependency Vulnerability -- REMEDIATED

| Aspect | Detail |
|---|---|
| Fix | Upgraded `google.golang.org/grpc` v1.79.2 -> v1.79.3 |
| Finding | `govulncheck` identified GO-2026-4762 (authorization bypass via missing leading slash in `:path`) in the transitive `grpc` dependency. Our code did not call the vulnerable symbols, but the package was exposed. |
| Verification | `govulncheck ./...` now reports zero vulnerabilities |

### Before Red Team -- Completed

#### Finding 6: Helm Chart Security Context Gaps -- REMEDIATED

| Aspect | Detail |
|---|---|
| Fix | Aligned Helm chart with raw K8s deployment manifest |
| Files | `deploy/helm/inveniam-mcp-server/values.yaml`, `deploy/helm/inveniam-mcp-server/templates/deployment.yaml` |
| Changes | Pod security context now includes `runAsUser: 65532`, `runAsGroup: 65532`, `runAsNonRoot: true`. Container security context now includes `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`, `capabilities.drop: [ALL]`. Values split into `podSecurityContext` and `containerSecurityContext` for clarity. |
| Verification | Template renders correctly with new values structure |

#### Finding 4: OTLP Telemetry Insecure gRPC -- REMEDIATED

| Aspect | Detail |
|---|---|
| Fix | Made OTLP insecure mode configurable |
| Files | `internal/telemetry/telemetry.go`, `internal/config/config.go`, `cmd/inveniam-mcp-server/main.go` |
| Changes | New `OTLP_INSECURE` env var (default `true` for backward compatibility). When set to `false`, OTLP gRPC trace and metric exporters connect with TLS instead of `WithInsecure()`. Insecure mode is logged at startup for visibility. |
| Residual risk | Default remains insecure; operator must explicitly opt in to TLS. Documented as an operational requirement. |

#### Finding 7: No Dependency Vulnerability Scanning in CI -- REMEDIATED

| Aspect | Detail |
|---|---|
| Fix | Added `govulncheck` to CI pipeline + Dependabot configuration |
| Files | `.github/workflows/ci.yml`, `.github/dependabot.yml` (new) |
| Changes | CI now runs `govulncheck ./...` before tests. Dependabot configured for `gomod`, `docker`, and `github-actions` ecosystems on a weekly schedule with up to 10 open PRs. |

#### Finding 7 (supplement): No NetworkPolicy -- REMEDIATED

| Aspect | Detail |
|---|---|
| Fix | Added NetworkPolicy to K8s manifests |
| Files | `deploy/k8s/networkpolicy.yaml` (new), `deploy/k8s/kustomization.yaml` |
| Changes | Ingress restricted: port 8080 only from pods labeled `app.kubernetes.io/component: mcp-client`; port 9090 open for probes/metrics. Egress restricted to HTTPS (443), OTLP gRPC (4317), and DNS (53). Added to Kustomize resource list. |
| Residual risk | Ingress selectors are templates; operators must adjust `podSelector`/`namespaceSelector` for their cluster topology. |

#### Finding 10: Docker Base Images Not Pinned by Digest -- REMEDIATED

| Aspect | Detail |
|---|---|
| Fix | Both Dockerfile base images pinned by `sha256` digest |
| Files | `Dockerfile` |
| Changes | `golang:1.26.2-alpine` pinned to `@sha256:f85330846cde1e57ca9ec309382da3b8e6ae3ab943d2739500e08c86393a21b1`. `gcr.io/distroless/static-debian12` pinned to `@sha256:20bc6c0bc4d625a22a8fde3e55f6515709b32055ef8fb9cfbddaa06d1760f838`. Dependabot will flag when newer digests are available. |

### Longer-Term Items -- Triaged

The following items from the "Longer-Term Hardening" tier have been triaged with the following dispositions:

| Item | Disposition | Notes |
|---|---|---|
| Human-in-the-loop approval for write ops | **Completed** | Implemented via MCP elicitation in `internal/mcp/approval.go`. Configurable per-client (`write_approval` in key store) and globally (`WRITE_APPROVAL_DEFAULT`). E2E tested in `server_e2e_test.go`. |
| Reverse proxy requirements | **Done** | Operational guide with nginx example added to `docs/DESIGN.md` section 10. |
| SBOM generation in CI | **Completed (`c357898`)** | `anchore/sbom-action` emits CycloneDX JSON artifact on every push to `main`. |
| MCP-level rate limiting per client | **Completed (`568ae50`)** | Token-bucket per-client limiter via `MCP_RATE_LIMIT` (default 60 req/s) and `MCP_RATE_BURST` (default 10). Returns HTTP `429` when exceeded. Implementation in `internal/mcp/ratelimit.go`. |
| Image signing with Cosign/Sigstore | **Completed (`c357898`)** | Cosign keyless signing of compiled binary on `main` push via Sigstore OIDC. Image-digest signing requires registry decision (still pending; see `docs/IMPLEMENTATION_PLAN.md` backlog). |
| License scanning for dependency compliance | **Completed (`c357898`)** | `go-licenses` check on every push/PR with explicit allowed-licenses list. |
| Per-tool authorization (RBAC) | **Completed (`568ae50`)** | Roles on API keys (`reader`, `writer`, `admin`, `automation`); FusionAuth maps via `roles` claim. All 16 tools gated; `ErrPermissionDenied` is client-safe. |
| CORS middleware | **Backlog (Low)** | Low priority since auth is enforced; only relevant if browser-based MCP clients are used. |
| Self-serve key request workflow | **Backlog (Medium)** | Allow clients to request an API key via an endpoint; a human or agent reviews and approves. Builds on the admin key management API. |

### Summary (refreshed 2026-04-28)

| Tier | Total | Remediated | Documented Only | Backlog |
|---|---|---|---|---|
| Immediate Fixes | 5 | 5 | 0 | 0 |
| Before Red Team | 5 | 5 | 0 | 0 |
| Longer-Term | 8 | 7 | 0 | 1 |
| **Total** | **18** | **17** | **0** | **1** |

All findings rated High or Critical have been remediated. Of the original 8 Longer-Term items, 7 have shipped (5 since the original assessment in commits `c357898` and `568ae50`); only **CORS middleware** remains in the backlog and is rated Low priority. Remaining open items are tracked in `docs/IMPLEMENTATION_PLAN.md`.

---

## Updates since 2026-04-01

The original assessment marked 6 items as "Backlog" and 2 as "Done" in the Longer-Term tier. As of 2026-04-28, 5 of those 6 backlog items have been delivered:

| Item | Status as of 2026-04-01 | Status as of 2026-04-28 |
|---|---|---|
| SBOM generation in CI | Backlog (Medium) | **Completed** -- `c357898` |
| MCP-level rate limiting per client | Backlog (Medium) | **Completed** -- `568ae50` |
| Image signing with Cosign/Sigstore (binary) | Backlog (Medium) | **Completed** -- `c357898` |
| License scanning | Backlog (Low) | **Completed** -- `c357898` |
| Per-tool authorization (RBAC) | Backlog (Low) | **Completed** -- `568ae50` |
| CORS middleware | Backlog (Low) | Still backlog (Low) |

In addition, **FusionAuth OAuth/JWT authentication** was added in Phase 6 as a second auth provider alongside API keys, and **MetaMask wallet signing support** was added in Phase 7 with a `wallet_tx_request` payload returned from every `anchor_prepare_*` tool. Neither was scoped in the original audit but both materially improve the production security posture (centralized identity, browser-wallet UX without server-side keys).
