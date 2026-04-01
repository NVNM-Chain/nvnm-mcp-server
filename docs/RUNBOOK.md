# Operational runbook: Inveniam EVM MCP Server

This document covers production deployment and day-two operations for the Go MCP server that exposes the Inveniam EVM chain (NVNM L2, chain ID **58887**) via MCP tools, with HTTP transport, separate health/metrics port, OpenTelemetry traces, and Prometheus metrics.

---

## 1. Deployment checklist

### Required environment variables

| Variable | Purpose |
|----------|---------|
| `INVENIAM_EVM_RPC_URL` | Primary EVM JSON-RPC URL (`http://` or `https://` only). May include query parameters for provider API keys; treat as secret if it does. |
| `INVENIAM_CHAIN_ID` | Expected chain ID; must be a positive integer (e.g. `58887`). Startup fails validation if missing or invalid. |

Production default RPC for this network: `https://evm.inveniam.mantrachain.io`.

### Strongly recommended for production

| Variable | Purpose |
|----------|---------|
| `ANCHOR_ABI_PATH` | Filesystem path to the anchor precompile ABI JSON. Without it, anchor tools are registered but return errors at call time; `anchor_info` reports ABI not loaded. |

Set to `/app/abi/anchoring.json` when that file is baked into the image (see below).

### Optional environment variables (with defaults)

| Variable | Default | Purpose |
|----------|---------|---------|
| `INVENIAM_EVM_ARCHIVE_RPC_URL` | _(empty)_ | Reserved for archive RPC when historical-query routing is implemented; not consumed by the current binary for routing. |
| `ANCHOR_ADDRESS` | `0x0000000000000000000000000000000000000A00` | Anchor precompile address. |
| `REQUEST_TIMEOUT` | `15s` | Per-upstream-call context timeout on the EVM client. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |
| `ENABLE_WRITE_TOOLS` | `false` | Must be `true` to register prepare / submit tools (`anchor_prepare_*`, `evm_send_raw_transaction`). |
| `MCP_TRANSPORT` | `stdio` | Use `http` in production. |
| `MCP_HTTP_ADDR` | `:8080` | MCP HTTP listen address. |
| `METRICS_ADDR` | `:9090` | Health + metrics listen address. |
| `ENABLE_PROMETHEUS` | `true` | When `true`, serves `GET /metrics` on the metrics port. |
| `ENABLE_STDOUT_TELEMETRY` | `false` | Emit OTel spans/metrics to stdout (debug only). |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | _(empty)_ | OTLP gRPC endpoint (e.g. `otel-collector:4317`); enables trace and metric export to a collector. |
| `OTEL_SERVICE_NAME` | `inveniam-mcp-server` | Service name in OTel resource. |

### Authentication (HTTP transport)

| Variable | Default | Purpose |
|----------|---------|---------|
| `MCP_API_KEYS_FILE` | _(empty)_ | Path to JSON key store file (preferred). Contains multiple keys with client IDs. |
| `MCP_API_KEY` | _(empty)_ | Single API key (dev/test fallback). No client identity tracking. |
| `OTLP_INSECURE` | `true` | Use plaintext connection to OTLP endpoint. Set `false` for TLS. |

When either key variable is set, all HTTP requests must include `Authorization: Bearer <key>`. The server warns at startup if HTTP transport runs with no keys configured.

Manage keys via Makefile targets:

```bash
make key-create NAME=my-agent                       # Create key, prints key to stdout
make key-create NAME=pipeline APPROVAL=auto          # Create key with auto write approval
make key-list                                        # List all keys (ID, enabled, approval, created)
make key-disable NAME=my-agent                       # Disable a key
make key-enable NAME=my-agent                        # Re-enable a key
make key-set-approval NAME=my-agent APPROVAL=auto    # Set write approval policy for a client
```

### Write approval (human-in-the-loop)

| Variable | Default | Purpose |
|----------|---------|---------|
| `WRITE_APPROVAL_DEFAULT` | `required` | Global default for write approval. `required` prompts users via MCP elicitation before broadcasting signed transactions; `auto` broadcasts without prompting. |

Per-client overrides are set via `write_approval` in the key store (see `make key-set-approval`). Resolution: per-client > global default > `"required"`.

When approval is `required`:
- The server decodes the signed transaction and presents details (to, value, gas, nonce, chain ID, data length) to the user via MCP elicitation.
- The user must accept to proceed; decline or cancel returns an error.
- If the MCP client does not support elicitation, the request is rejected.

### Secrets management

- Store `INVENIAM_EVM_RPC_URL` in Kubernetes Secrets, AWS Secrets Manager/SSM, or equivalent when it contains API keys or signed tokens.
- Store `MCP_API_KEYS_FILE` contents or `MCP_API_KEY` value in the platform secret store. The key store file (`.mcp-keys.json`) is gitignored.
- Prefer injecting secrets via environment from the platform secret store; avoid committing URLs or API keys to Git.
- Logs redact full RPC URLs to host-only where configured; still treat the env value as sensitive.

### ABI file in the container

- Production images should include `abi/anchoring.json` at a known path (commonly `/app/abi/anchoring.json`) and set `ANCHOR_ABI_PATH` accordingly.
- Verify the production Dockerfile or image build copies the `abi/` tree; the repository’s root `Dockerfile` documents the distroless layout but operators must ensure the ABI layer is added if not already present in their build pipeline.

### Port mapping

| Port | Purpose |
|------|---------|
| **8080** | MCP HTTP transport (`MCP_HTTP_ADDR`). |
| **9090** | Health and metrics (`METRICS_ADDR`): `GET /healthz`, `GET /readyz`, `GET /metrics`. |

Container image exposes 8080 and 9090. Map both in Kubernetes Services, ECS task definitions, and load balancers as required.

### Transport and process args

Production typically runs:

```bash
/inveniam-mcp-server --transport http
```

Ensure `MCP_TRANSPORT=http` (or the CLI flag) so the server listens for MCP on the HTTP address.

---

## 2. Health check interpretation

Endpoints are served on the **metrics** port (default `:9090`), not on the MCP port.

### `GET /healthz` (liveness)

- Returns **200** with JSON: `status`, `version`.
- Indicates the process is running and the health HTTP server is accepting requests.
- If this fails repeatedly, the container or task is unhealthy or crashed; restart or replace the workload.

### `GET /readyz` (readiness)

- Returns **200** when the cached readiness state is healthy; **503** when not ready.
- Response JSON includes `status` (`ready` / `not_ready`) and `checks`:
  - `evm_rpc`: `ok` or `unreachable` (from `eth_chainId`-style connectivity check).
  - `abi`: `loaded` if `ANCHOR_ABI_PATH` was loaded at startup, else `not_configured`.

**Important:** Readiness **503** is driven **only** by the EVM RPC probe failing. A missing ABI is reported in `checks.abi` as `not_configured` but **does not** by itself flip readiness to 503.

Background probes run every **30 seconds** (`readinessCheckInterval` in `internal/telemetry/health.go`), with a **5 second** timeout per probe. Kubernetes `periodSeconds: 10` (see `deploy/k8s/deployment.yaml`) may observe stale readiness for up to one probe interval after recovery.

### `GET /metrics`

- Prometheus scrape endpoint when `ENABLE_PROMETHEUS=true`.
- If disabled, the route is not registered.

---

## 3. Metrics reference

Metrics are registered on the OpenTelemetry `Meter` named `inveniam-mcp-server` and exported through the OTel Prometheus exporter. Exact Prometheus series names and histogram layout follow the exporter and `prometheus/otlptranslator` naming rules (including `otel_scope_*` labels on exported metrics). After deployment, confirm live names with:

```bash
curl -sS "http://<pod-ip>:9090/metrics" | grep -E 'mcp|evm_rpc|tool|active'
```

### MCP tool metrics

| OTel instrument name | Type | Labels / attributes | Meaning |
|----------------------|------|---------------------|---------|
| `mcp.server.tool.duration` | Float64 histogram (unit: seconds) | `mcp.method`, `mcp.tool.name` | End-to-end time per MCP method/tool invocation (middleware). |
| `mcp.server.tool.calls` | Int64 counter | `mcp.method`, `mcp.tool.name`, `status` (`ok` / `error`) | Count of tool calls by outcome. |
| `mcp.server.tool.errors` | Int64 counter | `mcp.method`, `mcp.tool.name` | Count of calls that returned an error. |
| `mcp.server.active_requests` | Int64 up-down counter | `mcp.method`, `mcp.tool.name` | In-flight MCP requests (increment on entry, decrement on exit). |

### EVM RPC metrics

| OTel instrument name | Type | Labels / attributes | Meaning |
|----------------------|------|---------------------|---------|
| `evm.rpc.duration` | Float64 histogram (unit: seconds) | `rpc.method` (e.g. `eth_chainId`, `eth_getBlockByNumber`) | Upstream JSON-RPC call latency in the tracing client. |
| `evm.rpc.errors` | Int64 counter | `rpc.method` | Upstream RPC errors observed by the tracing wrapper. |

### Resilience metrics

The resilient EVM client wrapper records retry attempts, circuit breaker state transitions, and rate-limit rejections via OTel metrics and structured log entries. These can be observed in `evm.rpc.errors` counters (which include retried attempts) and `evm.rpc.duration` histograms (which include retry latency). Circuit breaker state changes are logged at WARN level.

---

## 4. Alert response procedures

Define Prometheus alert rules in **`deploy/prometheus/alerts.yaml`** (add this file to your repo or overlay if it is not present yet). Use the metric series from section 3 after verifying exported names on `/metrics`.

### `InveniamMCPHighErrorRate`

- **Likely cause:** Upstream RPC errors, timeouts, or tool-level failures.
- **Actions:** Inspect `mcp.server.tool.errors` and `evm.rpc.errors` by label; search logs for `"status":"error"` and `level` `ERROR`; verify `INVENIAM_EVM_RPC_URL` reachability and provider status.

### `InveniamMCPCriticalErrorRate`

- **Actions:** Same as high error rate, with higher urgency; check for sustained RPC outage, TLS/DNS issues, and recent config changes. Scale or roll back if a bad release is suspected.

### `InveniamMCPHighP99Latency`

- **Actions:** Compare `mcp.server.tool.duration` and `evm.rpc.duration` high quantiles; check network path to RPC; review `REQUEST_TIMEOUT`; consider horizontal scale if CPU saturation correlates.

### `InveniamMCPHealthCheckFailing`

- **Actions:** Confirm pod/task liveness (`/healthz`) vs readiness (`/readyz`). For 503 on `/readyz`, treat as RPC probe failure first. Confirm ABI path and file only if anchor tools misbehave or `checks.abi` is `not_configured` and that is unacceptable for the environment.

### `InveniamMCPCircuitBreakerOpen`

- **Actions:** The circuit breaker (`sony/gobreaker`) is implemented in `internal/evm/resilient.go`. When triggered, all RPC calls fail fast with `ErrCircuitOpen`. State transitions are logged at WARN level. Check upstream RPC provider health. The breaker auto-recovers after `CIRCUIT_BREAKER_TIMEOUT` (default 30s) via a half-open probe.

### `InveniamMCPHighRetryRate`

- **Actions:** Retries are implemented with exponential backoff and jitter on idempotent read RPCs. High retry rates indicate upstream instability. Check `evm.rpc.errors` by method; verify RPC provider status. Consider increasing `RPC_INITIAL_BACKOFF` or reducing `RPC_MAX_RETRIES` if retries are amplifying load.

### `InveniamMCPRateLimiting`

- **Actions:** The in-process token-bucket rate limiter (`golang.org/x/time/rate`) caps upstream RPC calls at `RPC_RATE_LIMIT` req/s with `RPC_RATE_BURST` burst. If clients are being throttled, increase the rate limit, add replicas with fair routing, or negotiate higher quotas with the RPC provider.

---

## 5. Resilience configuration

### Implemented

The EVM client stack includes a resilience wrapper (`internal/evm/resilient.go`) that decorates the tracing client:

```
raw ethclient → TracingClient → ResilientClient → (used by anchor + MCP handlers)
```

| Feature | Config variable | Default | Description |
|---------|----------------|---------|-------------|
| Timeouts | `REQUEST_TIMEOUT` | `15s` | Per-call context deadline on the ethclient |
| Retry with exponential backoff | `RPC_MAX_RETRIES` | `3` | Maximum retry attempts for transient RPC errors |
| | `RPC_INITIAL_BACKOFF` | `500ms` | Initial wait between retries |
| | `RPC_MAX_BACKOFF` | `10s` | Maximum wait between retries |
| Rate limiting | `RPC_RATE_LIMIT` | `100` | Upstream RPC rate limit (requests per second) |
| | `RPC_RATE_BURST` | `20` | Burst capacity for token-bucket rate limiter |
| Circuit breaker | `CIRCUIT_BREAKER_THRESHOLD` | `5` | Consecutive failures to trip the breaker |
| | `CIRCUIT_BREAKER_TIMEOUT` | `30s` | Time in open state before half-open probe |

### `eth_sendRawTransaction` and retries

`evm_send_raw_transaction` ultimately calls `eth_sendRawTransaction`. This method is explicitly excluded from retries due to idempotency risk: a submission may succeed on the wire but the client can still see a timeout, and a retry can double-submit. The current code performs a single attempt per call.

---

## 6. Common failure modes

| Symptom | Likely cause | Mitigation |
|---------|--------------|------------|
| `/readyz` 503, `evm_rpc: unreachable` | Network, DNS, TLS, or provider outage | Check URL, certificates, firewall egress, provider dashboard; test RPC with `curl` JSON-RPC from a debug pod. |
| Anchor tools error; `anchor_info` shows ABI missing | `ANCHOR_ABI_PATH` unset, wrong path, or file not in image | Fix path; rebuild image with `abi/anchoring.json` included. |
| Elevated latency | Slow upstream or undersized CPU | Inspect `evm.rpc.duration` and `mcp.server.tool.duration`; increase `REQUEST_TIMEOUT` only if appropriate; scale replicas. |
| OOMKilled / memory growth | Limits too low for concurrency | Raise memory limits; see section 8. Example manifest: requests `64Mi`, limits `256Mi`. |
| Sporadic failures under load | Provider rate limits or connection limits | Reduce concurrency from clients; add replicas; contact RPC provider. |

---

## 7. Log query examples

Structured logs are JSON on **stderr** (`slog` JSON handler). Each MCP tool invocation logs at **info** with fields including: `method`, `tool`, `request_id`, `duration`, `status` (`ok` / `error`).

### AWS CloudWatch Logs Insights

Replace log group with yours (e.g. `/ecs/inveniam-mcp-server`):

```
fields @timestamp, tool, status, duration, request_id, msg
| filter status = "error"
| sort @timestamp desc
| limit 100
```

Slow tool calls (duration in log field; values are structured):

```
fields @timestamp, tool, duration, request_id
| filter msg = "tool call" and duration > 5000000000
| sort @timestamp desc
```

Specific tool:

```
fields @timestamp, tool, status, request_id
| filter tool = "evm_get_block"
| sort @timestamp desc
```

### Grafana Loki (LogQL)

```logql
{job="inveniam-mcp-server"} | json | status = "error"
```

```logql
{job="inveniam-mcp-server"} | json | tool = `anchor_get_registries`
```

### Correlating logs with traces

- Logs include **`request_id`** (UUID) per MCP invocation (`internal/telemetry/middleware.go`).
- OpenTelemetry spans include attribute **`mcp.request.id`** with the same value.
- **`trace_id` is not injected into log lines** by default in this codebase. Correlate by:
  - Searching the trace backend (Jaeger, Tempo, X-Ray) for `mcp.request.id`, or
  - Adding a collector processor or slog bridge that attaches trace context to logs if your platform requires `trace_id` in every line.

---

## 8. Scaling guidance

### Horizontal Pod Autoscaler

Example in **`deploy/k8s/hpa.yaml`**: CPU average utilization target **70%**, min **2**, max **10** replicas. Adjust for your cluster metrics server and SLOs.

```yaml
# Reference: deploy/k8s/hpa.yaml
metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
```

Consider custom metrics (e.g. from Prometheus) on `mcp.server.active_requests` or RPC latency once your monitoring stack exposes them to the metrics API.

### When to scale out

- Sustained high **`mcp.server.active_requests`** per replica with growing latency.
- **p99** growth in `mcp.server.tool.duration` or `evm.rpc.duration` not explained by RPC alone.
- CPU throttling or memory pressure visible in kubelet / ECS metrics.

### Resource sizing (starting points)

| Profile | Notes |
|---------|--------|
| Low traffic | Near `deploy/k8s/deployment.yaml` defaults (`100m` CPU request, `256Mi` limit) may suffice. |
| Higher concurrency | Increase CPU and memory; watch for Go GC and JSON-RPC connection usage. |
| Many concurrent MCP clients | Prefer more replicas over a single oversized pod to isolate failure domains. |

### Trace sampling

The server supports configurable trace sampling via `OTEL_TRACE_SAMPLE_RATIO` (default `1.0`, meaning sample all traces). Uses `ParentBased(TraceIDRatioBased)` to respect upstream sampling decisions while controlling cost for high-volume deployments.

Set `OTEL_TRACE_SAMPLE_RATIO=0.1` to sample 10% of root traces, or use collector-side tail sampling for more advanced policies.

---

## 9. Disaster recovery

### Stateless service

The MCP server holds **no durable application state**. Recovery is redeploy and reconnect to the same (or failover) RPC endpoint.

### Redeployment

1. Apply updated image or manifest (Kubernetes rolling rollout, ECS new deployment).
2. Confirm **`/healthz`** then **`/readyz`** on a pod/task.
3. Smoke-test MCP over HTTP (e.g. `initialize` / `tools/list`) against port **8080**.

### RPC endpoint failover

- If you operate a secondary RPC URL, update `INVENIAM_EVM_RPC_URL` (and restart workloads). Archive URL env exists for future use; confirm code support before relying on split routing.
- Verify chain ID still matches `INVENIAM_CHAIN_ID`.

### Post-recovery verification

```bash
curl -sS "http://<host>:9090/healthz"
curl -sS "http://<host>:9090/readyz"
curl -sS "http://<host>:9090/metrics" | head
```

Then run an MCP client against `http://<host>:8080` for a minimal tool call (e.g. `evm_get_chain_id`).

---

## References in this repository

- Configuration: `internal/config/config.go`, `README.md`
- Authentication: `internal/mcp/auth.go`, `internal/mcp/keys.go`, `internal/auth/context.go`
- Write approval: `internal/mcp/approval.go`, `internal/mcp/tools_evm_write.go`
- Key management CLI: `cmd/key-mgmt/main.go`
- Health server: `internal/telemetry/health.go`
- Metrics instruments: `internal/telemetry/metrics.go`, `internal/telemetry/middleware.go`, `internal/evm/tracing.go`
- Resilience: `internal/evm/resilient.go`
- Kubernetes samples: `deploy/k8s/` (including `networkpolicy.yaml`)
- Design / roadmap: `docs/DESIGN.md`, `docs/IMPLEMENTATION_PLAN.md`
- Security: `docs/SECURITY_AUDIT.md`
