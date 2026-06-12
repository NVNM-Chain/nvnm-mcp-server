# Incident Runbook — NVNM Chain MCP Server

Per-alert investigation playbooks for the Prometheus rules in
[`deploy/prometheus/alerts.yaml`](../deploy/prometheus/alerts.yaml).
Each section is anchor-pinned by alert name and is what an on-call
engineer should consult when paged. The companion
[`docs/RUNBOOK.md`](RUNBOOK.md) covers deployment, configuration, and
day-two operations; this document covers **incident response only**.

**Audience.** Operators on call; SRE; engineers diagnosing production
issues. Assumes familiarity with the MCP server's request flow
([README § Request Flow](../README.md)) and the metrics taxonomy in
[`docs/DATA_HANDLING.md`](DATA_HANDLING.md) § 7.

**Currency.** Mirrors the alert rules shipped to Prometheus as of the
[`deploy/prometheus/alerts.yaml`](../deploy/prometheus/alerts.yaml)
HEAD. Refresh when alert rules are added or thresholds change.

---

## How to use this document

When a Prometheus alert fires, the `runbook_url` annotation on the
rule links directly to the section below. Each section follows the
same template:

- **What it means** — the literal alert condition in human terms.
- **Likely causes** — ordered roughly by frequency, most-likely first.
- **First-look queries** — PromQL the on-call should run inside the
  first two minutes.
- **Quick mitigations** — actions that contain blast radius without
  needing root-cause certainty.
- **Escalation** — when to page deeper, and who.

The first-look queries assume an environment where `mcp_*` and
`evm_rpc_*` metrics are scraped from the server's `/metrics`
endpoint. The Prometheus instance, dashboards, and labels follow the
operator's deployment topology — adapt selectors where needed.

---

## NvnmMCPServerFaultRate <a id="nvnm-mcp-server-fault-rate"></a>

**Severity:** warning.
**Threshold:** HTTP server-fault ratio > 1% for 5 minutes.

### What it means

The Phase 10 RD3 SLI numerator. The fraction of HTTP responses
classified `server_fault` (5xx, computed by
[`internal/telemetry/http_responses.go`](../internal/telemetry/http_responses.go))
has crossed 1% over the 5-minute window. This is the canonical
"the server broke" error rate.

### Likely causes

1. Upstream EVM RPC degraded (5xx propagating through tool handlers).
   Check `evm_rpc_errors_total` and the circuit-breaker state metric
   side-by-side with the HTTP-class counter.
2. A specific tool handler returning 500 due to a Go panic recovery
   or an unhandled error path. Filter by `tool` on the MCP-method
   counter to localize.
3. Anchor ABI failing to load (`ANCHOR_ABI_PATH`) causing anchor
   tools to return 5xx at call time. The `anchor_info` tool would
   surface `abi_loaded: false`.

### First-look queries

```promql
# Ratio + raw rates
sum(rate(mcp_http_responses_total{class="server_fault"}[5m]))
  / sum(rate(mcp_http_responses_total[5m]))

# Breakdown by HTTP method
sum by (method) (rate(mcp_http_responses_total{class="server_fault"}[5m]))

# Correlate with upstream RPC
sum by (method) (rate(evm_rpc_errors_total[5m]))
```

### Quick mitigations

- If upstream EVM RPC is the source, the resilient client's circuit
  breaker should already be tripping. Confirm
  `evm_rpc_circuit_breaker_state == 2` (open) and let the breaker
  shed load. See also
  [`NvnmMCPCircuitBreakerOpen`](#nvnm-mcp-circuit-breaker-open).
- If a single tool dominates the fault distribution, consider
  temporarily disabling it: write tools can be turned off by setting
  `ENABLE_WRITE_TOOLS=false` and restarting; read tools are
  individually gated only via per-tool RBAC at the API-key level.
- Scale-up via the HPA absorbs transient surges but does not fix
  upstream degradation; resist treating it as a fix.

### Escalation

If the ratio breaches 5% the
[`NvnmMCPServerFaultRateCritical`](#nvnm-mcp-server-fault-rate-critical)
alert fires; treat that as a production incident and page the EVM RPC
provider in parallel.

---

## NvnmMCPServerFaultRateCritical <a id="nvnm-mcp-server-fault-rate-critical"></a>

**Severity:** critical.
**Threshold:** HTTP server-fault ratio > 5% for 5 minutes.

### What it means

The same SLI as
[`NvnmMCPServerFaultRate`](#nvnm-mcp-server-fault-rate) but at the
critical threshold. Five percent 5xx is customer-visible — agents
will be retrying and giving up.

### Likely causes

Same as the warning rule, but with higher prior on:

1. **Sustained upstream outage.** The resilient client's circuit
   breaker has fully opened and is failing fast for the breaker
   timeout window.
2. **Pod-level failure.** A subset of replicas is crashlooping or
   responding 503 due to misconfiguration after a recent deploy.
3. **Auth-store corruption.** The hashed key file has become
   unreadable; all authenticated requests now fail. Check the
   `MCP_API_KEYS_FILE` mount and contents.

### First-look queries

```promql
# Per-pod ratio -- isolates whether the failure is whole-fleet or pod-local
sum by (pod) (rate(mcp_http_responses_total{class="server_fault"}[5m]))
  / sum by (pod) (rate(mcp_http_responses_total[5m]))

# Recent deploys to correlate
kube_deployment_status_observed_generation{deployment="nvnm-mcp-server"}
```

### Quick mitigations

- Roll back to the previous image tag if a deploy correlates with the
  uptick. Helm: `helm rollback nvnm-mcp-server <previous-revision>`.
- If a single pod is the offender, delete it and let the Deployment
  recreate.
- Failing fast back at the customer is preferable to silent
  degradation; do not disable the HTTP-response middleware (it would
  also blind the SLI).

### Escalation

Page the EVM RPC provider's NOC if upstream is the source. If
upstream is healthy, page the Phase 10 on-call engineer for a
production incident.

---

## NvnmMCPCustomerImpactRate <a id="nvnm-mcp-customer-impact-rate"></a>

**Severity:** warning.
**Threshold:** customer-impact ratio (5xx + 429 + 408) > 5% for 5 minutes.

### What it means

The wider SLI that captures "customer experienced failure regardless
of server fault." Includes throttling (429) and request timeouts
(408) on top of 5xx. A spike here without a corresponding server-fault
spike usually means rate limits are biting or a customer is sending
slow / oversized requests.

### Likely causes

1. **Per-client rate-limit bucket exhausted.** A particular customer
   is hammering an endpoint and the `ClientRateLimiter` is throttling
   them. The 429s are correct behavior.
2. **Anonymous-rate limit triggering** under `MCP_KEYLESS_READS=true`.
   Inspect anonymous traffic patterns.
3. **`ReadTimeout` exhaustion** (60s by default, set in
   `internal/mcp/server.go`). Slow upstream RPC causes 408s.

### First-look queries

```promql
# Breakdown by class
sum by (class) (rate(mcp_http_responses_total[5m]))

# Combined ratio
sum(rate(mcp_http_responses_total{class=~"server_fault|customer_impact"}[5m]))
  / sum(rate(mcp_http_responses_total[5m]))
```

### Quick mitigations

- If a single client is causing 429s, treat as healthy — the rate
  limit is doing its job. Contact the customer if the rate is
  pathological.
- If 408s dominate, look at upstream RPC latency
  (`evm_rpc_duration_seconds_bucket`); the server-side fix is
  bumping `REQUEST_TIMEOUT` cautiously or routing some queries to an
  archive node.

---

## NvnmMCPHighErrorRate <a id="inveniam-mcp-high-error-rate"></a>

**Severity:** warning.
**Threshold:** MCP tool error ratio > 5% for 5 minutes.

### What it means

The **legacy MCP-method-level SLI** (kept for backward compatibility
with dashboards built before Phase 10 RD3). Measures
`mcp_server_tool_errors_total / mcp_server_tool_calls_total` — i.e.
tool calls that returned an MCP-protocol error, not HTTP-level
failures. This SLI is per-tool; the
[`NvnmMCPServerFaultRate`](#nvnm-mcp-server-fault-rate) HTTP SLI is
operator-side and should be the primary error signal going forward.

### Likely causes

1. A specific tool is returning errors at a high rate. `tools/call`
   metrics are labeled by `tool`; isolate the offender.
2. Authentication or RBAC errors propagating through the tool layer
   (`unauthorized`, `forbidden`).
3. Caller-supplied input failing validation (`evm_get_*` rejecting
   malformed addresses, anchor tools rejecting bad hashes).

### First-look queries

```promql
# Per-tool error ratio
sum by (tool) (rate(mcp_server_tool_errors_total[5m]))
  / sum by (tool) (rate(mcp_server_tool_calls_total[5m]))

# Top error types
topk(10, sum by (tool, error_type) (rate(mcp_server_tool_errors_total[5m])))
```

### Quick mitigations

- A wave of validation errors from a single client is a customer-side
  bug and not a service incident. The metric won't differentiate;
  cross-reference logs for `client_id` to confirm.
- If the offending tool is server-faulting (not validation), see
  [`NvnmMCPServerFaultRate`](#nvnm-mcp-server-fault-rate).

---

## NvnmMCPCriticalErrorRate <a id="inveniam-mcp-critical-error-rate"></a>

**Severity:** critical.
**Threshold:** MCP tool error ratio > 15% for 5 minutes.

### What it means

Same SLI as [`NvnmMCPHighErrorRate`](#inveniam-mcp-high-error-rate),
crit threshold. Fifteen percent of tool calls returning errors is
severe; either a major upstream outage or the server is broken at the
tool-layer level.

### Likely causes

1. **Auth misconfiguration** — `apikey` store empty or unreadable,
   FusionAuth JWKS unreachable, every authenticated call returning
   `unauthorized`.
2. **Anchor precompile unreachable** — the chain itself is degraded
   at the precompile address.
3. **All write tools failing** under `ENABLE_WRITE_TOOLS=false` while
   callers expect them to be available.

### First-look queries

```promql
# Auth-error specific
sum by (tool) (rate(mcp_server_tool_errors_total{error_type="unauthorized"}[5m]))

# Compare to MCP-layer 401/403 vs HTTP-layer 4xx
sum(rate(mcp_http_responses_total{class="client_error"}[5m]))
```

### Escalation

Treat as production incident. Page Phase 10 on-call. The
[`NvnmMCPServerFaultRateCritical`](#nvnm-mcp-server-fault-rate-critical)
HTTP-side SLI may or may not fire concurrently depending on the
failure mode — see HTTP vs MCP-layer error notes in the design doc.

---

## NvnmMCPHighP99Latency <a id="inveniam-mcp-high-p99-latency"></a>

**Severity:** warning.
**Threshold:** p99 tool duration > 5s for 5 minutes.

### What it means

The 99th percentile of MCP tool-call duration has exceeded 5 seconds.
Most tools complete in milliseconds; a sustained p99 over 5s usually
means upstream RPC is slow or contention is showing up somewhere.

### Likely causes

1. **Upstream EVM RPC latency** — the resilient client is retrying
   slow requests within budget. Inspect `evm_rpc_duration_seconds`.
2. **Archive-node queries** for large block ranges in `evm_get_logs`.
3. **GC pauses or CPU starvation** under sustained load. Compare
   container CPU vs requests.

### First-look queries

```promql
# Per-tool p99 latency
histogram_quantile(0.99,
  sum by (tool, le) (rate(mcp_server_tool_duration_milliseconds_bucket[5m])))

# Upstream RPC latency
histogram_quantile(0.99,
  sum by (method, le) (rate(evm_rpc_duration_seconds_bucket[5m])))
```

### Quick mitigations

- If the dominant tool is `evm_get_logs` over a wide block range,
  ask the caller to narrow the range; this is a documented
  performance edge.
- If CPU is saturated, scale up (HPA or replica bump).

---

## NvnmMCPHealthCheckFailing <a id="inveniam-mcp-health-check-failing"></a>

**Severity:** critical.
**Threshold:** scrape target down for 2 minutes.

### What it means

Prometheus cannot scrape the `nvnm-mcp-server` job — `up == 0`. This
fires before any of the application metrics can be observed, so the
underlying state could be anything from "pod crashlooping" to
"network split between Prometheus and the pod" to "wrong
ServiceMonitor selector after a label change."

### Likely causes

1. **Pod crashloop** — most likely cause after a deploy. Check
   `kube_pod_container_status_restarts_total`.
2. **Metrics port misconfigured** — the server binds metrics on
   `$METRICS_ADDR` (`:9090` default). If a recent change moved it,
   the `ServiceMonitor` `endpoints.port` selector also needs the
   move.
3. **ServiceMonitor label mismatch** after the Phase 9.14 follow-up
   manifest cleanups (`part-of: inveniam` → `nvnm-chain`). If the
   Prometheus instance's `ruleSelector` / `serviceMonitorSelector`
   filters on the old label, the new manifests stop being picked up.

### First-look queries

```promql
# Targets observed by Prometheus
up{job="nvnm-mcp-server"}

# Recent pod activity
kube_pod_container_status_restarts_total{pod=~"nvnm-mcp-server-.*"}
```

### Quick mitigations

- Check pod events and last logs (`kubectl logs --previous` for the
  pre-restart container).
- Verify Service and ServiceMonitor exist in the new `nvnm-mcp`
  namespace (Phase 9.14 rollover surface — see
  [`RUNBOOK.md` § "K8s manifest migration"](RUNBOOK.md#k8s-manifest-migration-phase-9-14-follow-up)).
- Health probes themselves are independent of `/metrics`; check
  `/healthz` and `/readyz` from a sidecar or `kubectl port-forward`
  to isolate scrape-vs-server.

### Escalation

If the pod is healthy but the scrape is failing, this is a
Prometheus / cluster-config issue — page cluster ops, not the MCP
server team.

---

## NvnmMCPCircuitBreakerOpen <a id="inveniam-mcp-circuit-breaker-open"></a>

**Severity:** critical.
**Threshold:** `evm_rpc_circuit_breaker_state == 2` for 1 minute.

### What it means

The resilient EVM client's circuit breaker is fully open. The server
is failing RPC calls fast (and surfacing them as MCP tool errors)
rather than queueing requests at the upstream endpoint. The breaker
shed-load behavior is correct under outage, but it must be reset by
upstream recovery — it does not self-heal while upstream is broken.

### Likely causes

1. **EVM RPC endpoint outage or degradation.**
2. **DNS or routing failure** to the configured RPC URL.
3. **Provider-side rate limiting** triggering enough failures fast
   enough to trip the breaker.

### First-look queries

```promql
# Breaker state
evm_rpc_circuit_breaker_state

# Pre-breaker failure rate
sum(rate(evm_rpc_errors_total[5m]))

# Rate-limit hits from the upstream provider
sum(rate(evm_rpc_rate_limited_total[5m]))
```

### Quick mitigations

- Try the configured RPC URL directly via `curl` from a debug pod to
  confirm reachability.
- If `evm_rpc_rate_limited_total` is high, the provider is throttling
  — slow down by reducing `RPC_RATE_LIMIT` in the deployment.
- Failover to a backup RPC URL if one is configured for the
  environment (operational; not in-server today).

### Escalation

Page the RPC provider's NOC. The breaker will close again on its own
once successful probes resume.

---

## NvnmMCPHighRetryRate <a id="inveniam-mcp-high-retry-rate"></a>

**Severity:** warning.
**Threshold:** RPC retries / tool calls > 20% for 5 minutes.

### What it means

The resilient EVM client is retrying upstream RPC calls at more than
20% the rate of MCP tool calls. Some retry is normal under transient
errors; sustained high retry means upstream is flaky.

### Likely causes

1. **Upstream EVM RPC intermittent errors** — retries are succeeding
   so customer impact is limited but the cost (latency, RPC quota)
   is real.
2. **Network instability** between the server and the RPC endpoint.
3. **Provider-side flapping** during a maintenance window.

### First-look queries

```promql
# Retry rate
sum(rate(evm_rpc_retries_total[5m]))

# Ratio
sum(rate(evm_rpc_retries_total[5m]))
  / sum(rate(mcp_server_tool_calls_total[5m]))
```

### Quick mitigations

- This is a signal, not necessarily an action. If customer-visible
  symptoms are absent (latency normal, error rate clean), monitor.
- If the
  [`NvnmMCPCircuitBreakerOpen`](#inveniam-mcp-circuit-breaker-open)
  alert is approaching, retries are about to give up — prep
  upstream-outage incident response.

---

## NvnmMCPRateLimiting <a id="inveniam-mcp-rate-limiting"></a>

**Severity:** warning.
**Threshold:** `evm_rpc_rate_limited_total` rate > 0 for 5 minutes.

### What it means

The **upstream RPC provider** is rate-limiting us. Not the
server-side rate limiter; this is the provider returning HTTP 429 or
JSON-RPC throttle errors that the resilient client classifies as
rate-limited.

### Likely causes

1. **Traffic surge** beyond the contracted provider quota.
2. **Provider-side quota miscalculation** during their billing reset
   window.
3. **Pool exhaustion** if multiple instances share the same RPC URL
   and the per-account quota is binding rather than per-IP.

### First-look queries

```promql
# Rate of provider-side limits
sum(rate(evm_rpc_rate_limited_total[5m]))

# Compare to our own per-client buckets (if customers are also being throttled)
sum by (mcp_method) (rate(mcp_server_tool_calls_total{status="error"}[5m]))
```

### Quick mitigations

- Cap our outbound rate via `RPC_RATE_LIMIT` to stay under the
  provider's ceiling.
- Stagger replicas — multiple instances sharing the same RPC URL
  multiply the burst.

### Escalation

If sustained, contact the provider for a quota raise.

---

## Anchor patterns

The remaining `*HighErrorRate` legacy alerts (kept for back-compat
with pre-RD3 dashboards) follow the same investigation steps as the
HTTP-level SLI patterns above. Prefer the
[`NvnmMCPServerFaultRate*`](#nvnm-mcp-server-fault-rate) family for
new dashboards.

---

## Cross-references

- [`docs/RUNBOOK.md`](RUNBOOK.md) — deployment + day-two operations.
- [`docs/DATA_HANDLING.md`](DATA_HANDLING.md) § 7 — metrics taxonomy
  + privacy posture.
- [`docs/planning/PHASE_10_DESIGN.md`](planning/PHASE_10_DESIGN.md) — design rationale
  for the Phase 10 SLI class label and capacity targets.
- [`deploy/prometheus/alerts.yaml`](../deploy/prometheus/alerts.yaml) —
  the alert rules this document indexes.
