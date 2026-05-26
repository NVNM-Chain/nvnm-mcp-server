# Data Handling Reference — NVNM Chain MCP Server

Technical reference describing what data the NVNM Chain MCP Server
touches at runtime, where it lives, and how long it persists. Paired
with the privacy policy / privacy statement that consumes it — those
documents intentionally omit this level of detail; this is where it
lives.

**Audience:** engineers, security reviewers, operators deploying the
OSS, and counsel pairing the policy with auditable technical detail.
**Currency:** reflects the code as of commit `5927adb` (Phase 8
close-out, 2026-05-15). Revise alongside any change to the surfaces
described.

## 1. Scope

Covers the runtime behavior of the server binary built from this
repository. Does not cover:

- the Inveniam Chain blockchain itself (public ledger, outside this
  software);
- the upstream EVM RPC endpoint (separately operated);
- the upstream FusionAuth identity provider when configured.

**Privacy posture in one sentence.** By design, the server holds no
personal data about end-users; the only identity information it sees
is what the operator's chosen auth provider supplies — an
operator-issued API key (which is not tied to a user identity unless
the operator chooses to label it so) or the `sub` claim from a
FusionAuth JWT (which is the FusionAuth user's identifier, not their
PII). The server also holds **zero blockchain private keys** and
performs **zero signing** — see
[docs/KEY_CUSTODY_THREAT_MODEL.md](KEY_CUSTODY_THREAT_MODEL.md) for
the threat-model rationale.

## 2. Authentication credentials

By default every inbound MCP request must authenticate. When the
operator sets `MCP_KEYLESS_READS=true` (HTTP transport only, Phase
9.16), requests with **no** `Authorization` header are admitted
anonymously and pass through a per-IP rate limiter
(`MCP_ANON_RATE_LIMIT` / `MCP_ANON_RATE_BURST`); per-tool gating is
then enforced fail-closed by an MCP receiving middleware against the
exempt registry in [`internal/mcp/authpolicy.go`](../internal/mcp/authpolicy.go).
The only auth-gated tool today is `evm_send_raw_transaction`; all
other tools (anchor reads, EVM reads, onboarding helpers) are exempt.
A present-but-invalid token is still rejected — only a fully absent
header triggers anonymous admission. See § 6 / § 7 for the
anonymous-traffic telemetry posture.

The server supports two interchangeable providers, chosen at startup
via the `AUTH_PROVIDER` environment variable (default `apikey`):

- **`apikey`** — operator-managed Bearer tokens minted by the
  server's own admin REST API. The operator is the sole key issuer;
  there is no user-facing signup flow. Best for automation, internal
  deployments, and small operator-controlled caller populations.
- **`fusionauth`** — JSON Web Tokens (JWTs) minted by an external
  FusionAuth identity provider the operator already runs. Callers
  must already have a registered user account (userid + password) at
  that FusionAuth instance; they authenticate there, receive a JWT,
  and present the JWT to this server. The MCP server never sees the
  password and never authenticates the user itself — it only
  validates the JWT signature against FusionAuth's public keys and
  trusts the identity claims inside.

All credentials and configuration described below live on the
**server host** (wherever the operator runs this binary), not on
caller machines. Callers hold exactly one secret: the Bearer token
(API key or JWT) they present in the `Authorization: Bearer <token>`
HTTP header on every request.

### 2.1 API-key provider

Three environment variables govern API-key authentication. **All
three are read from the server's environment** (the host running the
MCP server binary), not from the caller's machine:

| Env var              | What it controls                                                                                                                                | On disk?                |
|----------------------|-------------------------------------------------------------------------------------------------------------------------------------------------|-------------------------|
| `MCP_API_KEYS_FILE`  | Path to the managed multi-key JSON store — the production path. Holds many issued keys, each with roles + write-approval policy; hot-reloaded.  | Yes: hashed entries     |
| `MCP_API_KEY`        | Single-key dev-mode override: accepts exactly one Bearer token, value taken directly from this env var. No roles, no admin API. Local-run only. | No                      |
| `ADMIN_API_KEY`      | Bearer token that gates the admin REST API at `/admin/keys` (mint / list / patch / revoke caller-facing keys). Requires `MCP_API_KEYS_FILE`.    | No (env-only)           |

A **Bearer token** in this context is a hex string the caller puts in
the `Authorization: Bearer <token>` HTTP header. The server compares
its SHA-256 hash against the on-disk store (or the single
`MCP_API_KEY` value) and accepts or rejects. The token is the
caller's only credential — there is no password, no challenge, no
multi-factor step.

The multi-key store (`MCP_API_KEYS_FILE`) is the primary path. Each
entry contains:

- internal `id` (stable);
- **SHA-256 hash** of the key — raw key never written to disk (Phase
  8.6); the `KeyEntry` struct can transiently hold a raw key in
  process memory during creation or one-time migration, but
  `NewKeyEntry` (the only production constructor) hashes immediately
  and never retains the raw value;
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

Activated by `AUTH_PROVIDER=fusionauth`. **The MCP server does not
authenticate users itself in this mode** — it delegates entirely to
the operator's FusionAuth installation. A caller must already have a
registered user account (userid + password, optionally with
multi-factor authentication configured at FusionAuth) on that
FusionAuth instance. They log in to FusionAuth out-of-band, receive a
JWT, and present that JWT to this server on every request. The MCP
server never sees the userid or password.

JWKS public keys are fetched once at startup from
`$FUSIONAUTH_JWKS_URL` (or derived from
`$FUSIONAUTH_URL/.well-known/jwks.json`) and cached in process memory
by the `keyfunc` library. JWKS = JSON Web Key Set, the set of public
keys FusionAuth publishes so downstream verifiers can validate JWT
signatures without contacting FusionAuth on every request.

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
| `sub`                | RFC 7519 §4.1.2   | Hashed into `Claims.ClientID` via keyed HMAC-SHA256 (`hmacClientID`, key `MCP_CLIENT_ID_HMAC_KEY`) | Raw `sub` lives in request-scope memory only and is **never logged**. The HMAC'd value appears as `client_id` in logs and as the `mcp.client.id` span attribute. |
| roles (configurable) | Custom            | Extracted as `Claims.Roles`                      | Request memory                                                                                                    |
| `jti`                | RFC 7519 §4.1.7   | Not read                                         | Nowhere                                                                                                           |

**Sub handling (resolved Phase 9.16, 2026-05-20).** The validator no
longer logs the `sub` at any level: the former DEBUG `subject` line was
removed, and the `sub` reaches logs and traces only as a keyed HMAC
(`client_id`), which is stable for audit correlation but not reversible
to a real-world identity without the server-held `MCP_CLIENT_ID_HMAC_KEY`.
The key is mandatory under `AUTH_PROVIDER=fusionauth` — startup fails
loud (`ErrMissingClientIDHMACKey`) if it is unset. See
[`docs/PRIVACY_DISCUSSION.md`](PRIVACY_DISCUSSION.md) § 2.1 D4/D9.

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
requests emit a WARN log line containing the derived source IP and
(for missing-Authorization rejections) the request method; the
request path, token bytes, and request body are never logged
([internal/mcp/auth.go:49-84](../internal/mcp/auth.go#L49-L84) and
[internal/mcp/failrate.go:150-153](../internal/mcp/failrate.go#L150-L153)).

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

**Anonymous reads (`MCP_KEYLESS_READS=true`):** the per-request
INFO `tool call` log line has the `client_id` field **absent**
(not empty-string), so structured-log queries cleanly distinguish
"anonymous read" from "broken auth that nulled the field." Authed
write traffic continues to emit `client_id`. The anonymous per-IP
rate limiter logs `remote_addr` on 429 (and only on 429); successful
anonymous reads are otherwise indistinguishable from authed reads in
the log stream beyond the absent `client_id`. See the Phase 9.16
design doc `docs/superpowers/specs/2026-05-21-keyless-read-auth-middleware-design.md`
§6 for the full data-flow rationale.

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

`mcp.method`, `mcp.tool.name`, `mcp.request.id`, and `mcp.client.id`
(from `ClaimsFromContext`). The `mcp.client.id` attribute is
**omitted entirely** — not set to empty-string — on anonymous calls
under `MCP_KEYLESS_READS=true`. Authed traffic carries it unchanged.

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

## 11. Transport security

TLS termination is the **operator's responsibility**, not the
server's. The server binary speaks plaintext HTTP/JSON on its
configured listen address and expects to be deployed behind a reverse
proxy, ingress controller, or load balancer that terminates TLS
upstream of it. This is a deliberate design choice — wire encryption
is a deployment concern that varies by operator (Let's Encrypt
managed certificates, internal CA, mTLS) and lives at a different
layer than this binary. The Helm chart in `deploy/helm/` shows the
Kubernetes ingress pattern; operators running outside Kubernetes
should terminate at nginx, Caddy, an AWS ALB, or equivalent.

Data at rest is covered above: API-key entries are SHA-256-hashed
(§2.1); logs and metrics are operator-routed to operator-chosen
sinks (§6, §7); no other runtime data is written to local storage.
The server itself does not encrypt files it writes — the only file
it writes is the API-key store, whose entries are already hashed.

## 12. Data subject perspective

For privacy-policy and regulatory framing (GDPR, CCPA, equivalents):

- **There is no end-user account database in this server.** Nothing
  to subject to access, portability, rectification, or deletion
  requests directly on the MCP server. The only persisted identity
  data is the hashed API-key entries described in §2.1, which are
  operator-managed administrative records, not end-user records.
- **For FusionAuth-authenticated callers,** the data controller for
  the caller's identity is the operator's FusionAuth installation,
  not this server. Data-subject requests for that identity route to
  FusionAuth. The MCP server only observes the `sub` claim per
  request (held in process memory for the lifetime of the request;
  see §4) and emits it to logs only at DEBUG level (§2.2).
- **For API-key callers,** the operator is the data controller. The
  operator's revocation of a key via the admin REST API (§2.1)
  terminates this server's recognition of that identity. There is no
  separate "delete user" operation because there is no user record
  beyond the key.
- **Logs may contain caller-supplied data** at the levels described
  in §6 (source IP at WARN; FusionAuth `sub` at DEBUG). Log retention
  is operator-controlled — the server emits to stderr; the operator's
  log shipper and storage policy determine how long log data lives.

## 13. Cross-references

- [docs/SECURITY_AUDIT.md](SECURITY_AUDIT.md) — point-in-time security
  snapshot; the hashed-at-rest migration is described under "Update
  2026-05-13: Phase 8.6 and 8.7."
- [docs/OWASP_AUDIT.md](OWASP_AUDIT.md) — OWASP Top 10 coverage matrix.
- [docs/KEY_CUSTODY_THREAT_MODEL.md](KEY_CUSTODY_THREAT_MODEL.md) —
  rationale for the server holding zero blockchain private keys and
  performing zero signing; canonical rebuttal to any "let the server
  hold the key" proposal.
- [CHANGELOG.md](../CHANGELOG.md) — releases that change any of the
  surfaces above.
- [docs/PRIVACY_DISCUSSION.md](PRIVACY_DISCUSSION.md) — working notes
  for the privacy policy and operator privacy statement that consume
  this reference.
