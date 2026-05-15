# Data Handling Reference — NVNM Chain MCP Server

Technical reference describing what data the NVNM Chain MCP Server
touches at runtime, where it lives, and how long it persists. Paired
with the privacy policy / privacy statement that consumes it — those
documents intentionally omit this level of detail; this is where it
lives.

**Audience:** engineers, security reviewers, operators deploying the
OSS, and counsel pairing the policy with auditable technical detail.
**Currency:** reflects the code as of commit `36b12de`
(Phase 8.12, 2026-05-15). Revise alongside any change to the surfaces
described.

## 1. Scope

Covers the runtime behavior of the server binary built from this
repository. Does not cover:

- the Inveniam Chain blockchain itself (public ledger, outside this
  software);
- the upstream EVM RPC endpoint (separately operated);
- the upstream FusionAuth identity provider when configured.

## 2. Authentication credentials

Two providers, selected at startup via `AUTH_PROVIDER` (default
`apikey`).

### 2.1 API-key provider

Three related env vars govern API-key auth; each has a different
on-disk footprint.

| Env var              | Behavior                                              | On disk?                      |
|----------------------|-------------------------------------------------------|-------------------------------|
| `MCP_API_KEYS_FILE`  | Path to managed multi-key JSON store                  | Yes: hashed entries           |
| `MCP_API_KEY`        | Single-key bypass; key resident in process env        | No                            |
| `ADMIN_API_KEY`      | Gates admin REST API; requires `MCP_API_KEYS_FILE`    | No (env-only)                 |

The multi-key store is the primary case. Each entry contains:

- internal `id` (stable);
- **SHA-256 hash** of the key — raw key never persisted (Phase 8.6);
- `key_prefix` (first 8 characters) for operator identification;
- `enabled` flag, `created_at` (UTC), `roles` slice, `write_approval`
  policy.

Validation: constant-time hash comparison, with a placeholder compare
on the miss path so unknown-key and known-key request timings match
(Phase 8.7).

Persistence semantics: atomic temp + fsync + rename via `SaveKeysFile`
at [internal/mcp/keys.go:188](../internal/mcp/keys.go#L188); advisory
`LOCK_EX` during writes; pre-Phase-8.6 stores are migrated once at
startup with a one-time backup written to
`$MCP_API_KEYS_FILE.pre-migration` that is never overwritten.

The admin REST API (`/admin/keys`, `GET`/`POST`/`PATCH`/`DELETE`) is
gated by `ADMIN_API_KEY` and returns redacted `KeySummary` objects (no
key material) — except on `POST`, which returns the newly minted raw
key exactly once. Each admin operation logs `client_id` and
`remote_addr` at INFO
([internal/mcp/admin.go:138-245](../internal/mcp/admin.go#L138-L245)).

### 2.2 FusionAuth provider

Activated by `AUTH_PROVIDER=fusionauth`. JWKS public keys are fetched
once at startup from `$FUSIONAUTH_JWKS_URL` (or derived from
`$FUSIONAUTH_URL/.well-known/jwks.json`) and cached in process memory
by the `keyfunc` library.

Per request: `jwt.Parse` with `WithLeeway(ClockSkew)` enforces the
standard validity claims; if those pass, the validator manually checks
`iss` and `aud`, then extracts `sub` and the configured roles claim.
Presence of the `automation` role yields `WriteApproval=auto`;
otherwise `required`.

| Claim                | Source            | Action                                           | Persists                                                                                                          |
|----------------------|-------------------|--------------------------------------------------|-------------------------------------------------------------------------------------------------------------------|
| `exp`                | RFC 7519 §4.1.4   | Library-enforced with leeway                     | Nowhere                                                                                                           |
| `nbf`                | RFC 7519 §4.1.5   | Library-enforced with leeway                     | Nowhere                                                                                                           |
| `iat`                | RFC 7519 §4.1.6   | Parsed, unused                                   | Nowhere                                                                                                           |
| `iss`                | RFC 7519 §4.1.1   | Compared via `matchIssuer`                       | Nowhere                                                                                                           |
| `aud`                | RFC 7519 §4.1.3   | Compared via `validateAudience`                  | Nowhere                                                                                                           |
| `sub`                | RFC 7519 §4.1.2   | Extracted as `Claims.ClientID`                   | Request memory; **DEBUG log** at [fusionauth.go:138](../internal/auth/fusionauth.go#L138); `mcp.client.id` span attribute |
| roles (configurable) | Custom            | Extracted as `Claims.Roles`                      | Same as `sub`                                                                                                     |
| `jti`                | RFC 7519 §4.1.7   | Not read                                         | Nowhere                                                                                                           |

**DEBUG-level caveat.** The FusionAuth validator emits
`slog.String("subject", sub)` at DEBUG level. Production should run at
INFO; ad-hoc DEBUG sessions for troubleshooting will write subject
identifiers to stderr. Candidate code change worth weighing during
Phase 9 / 10: redact like `SafeAddr` does for EVM addresses, or drop
entirely.

## 3. Request inputs forwarded to upstream

The following caller-supplied parameters are forwarded to the
configured EVM RPC endpoint and not retained:

- EVM addresses
- Transaction hashes
- Block numbers / tags
- Contract call data (function selector + ABI-encoded args)
- Signed raw transaction hex (passed to `evm_send_raw_transaction`)
- Filter parameters for `evm_get_logs`

Internal logging redacts these before any debug log line:

- `SafeAddr(key, addr)` — first-6 / last-4 chars only;
- `SafeTxData(key, data)` — byte length only, no hex;
- `SafeURL(key, rawURL)` — scheme + host, strips path/query.

See [internal/logging/redact.go](../internal/logging/redact.go).

Tool parameters and tool return values are **never** logged — middleware
exclusion at
[internal/telemetry/middleware.go:36](../internal/telemetry/middleware.go#L36).

## 4. In-memory request state

Per authenticated MCP request, held in process memory for the lifetime
of the single request:

- request correlation UUID (generated at
  [internal/telemetry/middleware.go:46](../internal/telemetry/middleware.go#L46);
  emitted as `request_id` in log lines and as `mcp.request.id` in span
  attributes);
- authenticated `Claims`: `ClientID`, `Roles`, `WriteApproval`.

No cookies. No server-side sessions. No persistent per-caller state
beyond the API-key store described in §2.1.

## 5. Source-IP failure-rate limiter

In-memory map of **failed** authentication attempts keyed by source IP.
Used to throttle credential stuffing. Entries expire after
**15 minutes** of inactivity. Never persisted to disk.

Source-IP derivation: `X-Forwarded-For` is honored only when
`$NVNM_TRUST_PROXY_HEADERS=true`; otherwise the socket peer. Rejected
requests emit a WARN log line containing the derived IP, method, and
path — no token bytes
([internal/mcp/failrate.go](../internal/mcp/failrate.go)).

## 6. Logging

Structured JSON to stderr via `slog`; level set by `$LOG_LEVEL`
(default `info`). Standard fields: timestamp, level, message, plus
request UUID, MCP method, and tool name on tool-call paths.

**Never logged:**

- raw API keys or any byte of an inbound Bearer token;
- raw or decoded JWTs;
- tool input parameters;
- tool return values;
- private keys (none are held).

**Logged at WARN:** rejected unauthenticated requests (remote_addr,
method, path); too-many-failures throttle decisions (remote_addr).

**Logged at INFO:** admin key CRUD operations (client_id, remote_addr).

**Logged at DEBUG only:** FusionAuth subject identifier (see §2.2
caveat).

## 7. Telemetry

Optional. Enabled via `$OTEL_EXPORTER_OTLP_ENDPOINT` (OTLP gRPC),
`$ENABLE_PROMETHEUS` (scrape endpoint at `$METRICS_ADDR`), or
`$ENABLE_STDOUT_TEL` (debug). Trace sampling via
`$OTEL_TRACE_SAMPLE_RATIO`.

### 7.1 Metrics exported

| Metric                            | Type      | Labels                |
|-----------------------------------|-----------|-----------------------|
| `mcp.server.tool.duration`        | histogram | tool, status          |
| `mcp.server.tool.calls`           | counter   | tool, status          |
| `mcp.server.tool.errors`          | counter   | tool, error_type      |
| `mcp.server.active_requests`      | gauge     | —                     |
| `evm.rpc.duration`                | histogram | method                |
| `evm.rpc.errors`                  | counter   | method                |
| `evm.rpc.retries`                 | counter   | —                     |
| `evm.rpc.circuit_breaker.state`   | gauge     | —                     |
| `evm.rpc.rate_limited`            | counter   | —                     |

No per-caller labels in metrics.

### 7.2 Span attributes

`mcp.method`, `mcp.tool.name`, `mcp.request.id`, `mcp.client.id` (from
`ClaimsFromContext`).

Tool parameters and return values are **not** included in any metric or
span.

## 8. Persistence summary

| Item                                | Storage                            | Lifetime                        |
|-------------------------------------|------------------------------------|---------------------------------|
| API-key hash + metadata             | `$MCP_API_KEYS_FILE` (JSON)        | Until deleted                   |
| One-time legacy migration backup    | `$MCP_API_KEYS_FILE.pre-migration` | Indefinite, never overwritten   |
| Authenticated claims                | Process memory                     | Single request                  |
| Request correlation UUID            | Process memory                     | Single request                  |
| Failed-auth IP buckets              | Process memory                     | 15-min inactivity TTL           |
| JWKS public keys                    | Process memory (`keyfunc` cache)   | Process lifetime                |
| Logs                                | stderr (operator-routed)           | Operator-defined                |
| Metrics / traces                    | OTLP / Prometheus sink             | Sink-defined                    |

No other runtime writes to local storage.

## 9. Outbound network destinations

1. **EVM JSON-RPC endpoint** (`$NVNM_EVM_RPC_URL`, optionally
   `$NVNM_EVM_ARCHIVE_RPC_URL`). One outbound per RPC tool call.
2. **FusionAuth JWKS endpoint.** One fetch at startup when
   `AUTH_PROVIDER=fusionauth`.
3. **Telemetry endpoint(s)** when enabled.

No analytics, advertising, or third-party tracking destinations.

## 10. HTTP headers and cookies

**Read:** `Authorization` (parsed; never logged), `Origin` (CORS),
`X-Forwarded-For` (only when `$NVNM_TRUST_PROXY_HEADERS=true`).

**Set:** `Content-Type: application/json`, `Retry-After: 60` on 429.

**Cookies:** none read, none set.

## 11. Cross-references

- [docs/SECURITY_AUDIT.md](SECURITY_AUDIT.md) — point-in-time security
  snapshot; the hashed-at-rest migration is described under "Update
  2026-05-13: Phase 8.6 and 8.7."
- [docs/OWASP_AUDIT.md](OWASP_AUDIT.md) — OWASP Top 10 coverage matrix.
- [CHANGELOG.md](../CHANGELOG.md) — releases that change any of the
  surfaces above.
- [docs/PRIVACY_DISCUSSION.md](PRIVACY_DISCUSSION.md) — working notes
  for the privacy policy and operator privacy statement that consume
  this reference.
