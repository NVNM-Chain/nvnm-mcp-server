# k6 load tests (MCP HTTP)

Load tests for the Inveniam EVM MCP server using [k6](https://k6.io/) against the **Streamable HTTP** transport. Requests are JSON-RPC 2.0 `POST` with `Content-Type: application/json`.

## Prerequisites

- [k6](https://k6.io/docs/get-started/installation/) installed (`k6 version`).
- MCP server running with HTTP transport and reachable JSON-RPC URL (see below).
- Valid chain configuration: `INVENIAM_EVM_RPC_URL`, `INVENIAM_CHAIN_ID`, and `ANCHOR_ABI_PATH` as required by the server (see project `README.md`).
- **Authentication**: the current k6 script does **not** send `Authorization` headers. To run load tests, start the server with **no** keys configured (`MCP_API_KEYS_FILE` and `MCP_API_KEY` both unset, `AUTH_PROVIDER=apikey` default) so the server warns at startup but accepts all requests. If you need authenticated load testing, extend `k6_mcp_http.js` to read a token from `__ENV.MCP_AUTH_TOKEN` and add it to the request headers in `setup()` and the VU code.
- **Rate limiting**: when `MCP_RATE_LIMIT` is set (default 60 req/s per client), all unauthenticated requests share a single bucket and high-VU scenarios will receive HTTP `429`. Either raise `MCP_RATE_LIMIT`/`MCP_RATE_BURST` for the load-test run, or unset them.

## Starting the server for load testing

From the repository root, with environment variables set:

```bash
make run-http
```

This builds the binary and runs it with `--transport http`. By default the MCP HTTP handler listens on `MCP_HTTP_ADDR` (default `:8080`) at the **server root** (for example `http://localhost:8080/`). Health and Prometheus metrics are on `METRICS_ADDR` (default `:9090`), separate from MCP.

If you terminate TLS or mount the handler under a path (for example `/mcp`), set `MCP_URL` to that full URL when running k6.

## Running the tests

All scenarios run **in parallel** when you execute the script without filters. That stresses the server with combined VUs from `constant_reads`, `burst_reads`, and `mixed_workload`.

```bash
k6 run tests/load/k6_mcp_http.js
```

Run a **single** scenario:

```bash
k6 run --scenario constant_reads tests/load/k6_mcp_http.js
k6 run --scenario burst_reads tests/load/k6_mcp_http.js
k6 run --scenario mixed_workload tests/load/k6_mcp_http.js
```

### Custom MCP URL

```bash
k6 run -e MCP_URL=http://host:port/mcp tests/load/k6_mcp_http.js
```

For local `make run-http` without a path prefix:

```bash
k6 run -e MCP_URL=http://localhost:8080/ tests/load/k6_mcp_http.js
```

## What the script does

1. **setup** (once): sends `initialize`, reads `Mcp-Session-Id` from the response (k6 exposes it as `mcp-session-id`), then sends `notifications/initialized` with that header on follow-up requests. Requests send `Accept: application/json, text/event-stream`, which the MCP Go SDK requires for POST.
2. **VU code**: sends `tools/call` for the tools configured per scenario. Metrics include tags `mcp_method`, `mcp_tool`, and per-scenario tags from k6 (`scenario` on built-in HTTP metrics).
3. **Responses**: the script accepts either a JSON body or `text/event-stream` with JSON-RPC payloads in SSE `data:` lines (default SDK behavior without `JSONResponse`).
4. **Thresholds**: `http_req_duration` p(95) under 2000 ms; `http_req_failed` rate under 1%.

Scenarios:

| Scenario         | Executor      | Load shape |
|------------------|---------------|------------|
| `constant_reads` | constant-vus  | 10 VUs for 2 minutes; `evm_get_chain_id` |
| `burst_reads`    | ramping-vus   | 0 to 50 VUs in 1m, hold 1m, ramp to 0 in 1m; `evm_get_chain_id` |
| `mixed_workload` | constant-vus  | 15 VUs for 2 minutes; mix of `evm_get_chain_id`, `evm_get_block` (latest), `anchor_get_registries` |

## Interpreting results

- **Summary (stdout)**: k6 prints iteration rate, request duration percentiles, failed request rate, and whether **thresholds** passed or failed.
- **Checks**: Lines such as `initialize JSON-RPC result` or `tools/call JSON-RPC result` reflect application-level correctness (status, JSON-RPC envelope, MCP tool result shape). Failed checks often mean wrong URL, missing session handling, RPC/backend errors, or non-JSON responses (e.g. proxy HTML).
- **http_req_failed**: Non-2xx HTTP responses or network errors. Tune sleep durations or VUs if the server or upstream RPC is overloaded.
- **Tags**: Filter in k6 outputs or downstream backends using `mcp_tool`, `mcp_method`, or `scenario` to see which calls dominate latency or failures.

Note: **One shared session** from `setup()` is reused by all VUs. If the server serializes or rejects concurrent use of a single session, run a single scenario with lower VUs or adapt the script to initialize per VU.

## Exporting results

### Grafana Cloud k6

1. Create a token in your Grafana Cloud k6 project.
2. Set the environment variable `K6_CLOUD_TOKEN` to that token.
3. Run:

```bash
k6 cloud tests/load/k6_mcp_http.js
```

Alternatively, pipe a local run to the cloud (see [k6 Cloud documentation](https://grafana.com/docs/grafana-cloud/testing/k6/) for the option set supported by your k6 version).

### InfluxDB

Send time series to InfluxDB (v1 example; adjust URL, database, and credentials for your install):

```bash
k6 run --out influxdb=http://localhost:8086/k6 tests/load/k6_mcp_http.js
```

For InfluxDB 2.x or other backends, use the corresponding k6 output extension or `--out` option described in the [k6 results output](https://k6.io/docs/results-output/) documentation.
