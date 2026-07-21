# Data Handling Reference — NVNM Chain MCP Server

Technical reference describing what data the NVNM Chain MCP Server
touches at runtime, where it lives, and how long it persists. Paired
with the privacy policy / privacy statement that consumes it — those
documents intentionally omit this level of detail; this is where it
lives.

**Audience:** engineers, security reviewers, operators deploying the
OSS, and counsel pairing the policy with auditable technical detail.
**Currency:** reflects the code as of commit `52e2357` (admin_audit
provisioning + admin-server start-on-key-or-file boot wiring,
2026-07-07). Revise alongside any change to the surfaces described.
Re-stamp this anchor to the eventual squash-merge commit once this
branch lands on `main`, rather than to a later doc-only fix.

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
| `MCP_API_KEYS_FILE`  | Path to the managed multi-key JSON store — the production path. Holds many issued keys, each with roles; hot-reloaded.  | Yes: hashed entries     |
| `MCP_API_KEY`        | Single-key dev-mode override: accepts exactly one Bearer token, value taken directly from this env var. No roles, no admin API. Local-run only. | No                      |
| `ADMIN_API_KEY`      | Bearer token that gates the admin REST API at `/admin/keys` (mint / list / patch / revoke caller-facing keys). Requires `MCP_API_KEYS_FILE`.    | No (env-only)           |

A **Bearer token** in this context is a hex string the caller puts in
the `Authorization: Bearer <token>` HTTP header. The server compares
its versioned hash digest against the on-disk store (or the single
`MCP_API_KEY` value) and accepts or rejects. The token is the
caller's only credential — there is no password, no challenge, no
multi-factor step.

The multi-key store (`MCP_API_KEYS_FILE`) is the primary path. Each
entry contains:

- internal `id` (stable);
- **versioned hash digest** of the key — raw key never written to disk
  (Phase 8.6); `hash_version 0` = plain SHA-256 (legacy, default when
  `KEY_HMAC_PEPPER` is unset); `hash_version 1` = HMAC-SHA256 under a
  server-held pepper (`KEY_HMAC_PEPPER`, optional). The pepper is a
  server-side secret that makes a database-only key-store dump
  non-reversible offline; it is opt-in and supplied via env. The
  `KeyEntry` struct can transiently hold a raw key in process memory
  during creation or one-time migration, but `NewKeyEntry` (the only
  production constructor) hashes immediately and never retains the raw
  value. Persisted re-hashing of legacy `v0` keys to `v1` lands with
  the Postgres backend (Phase 3); until then, `v0` keys continue to
  authenticate unchanged via versioned candidate lookup;
- `key_prefix` (first 8 characters) for operator identification;
- `enabled` flag, `created_at` (UTC), `roles` slice.

Validation: constant-time hash comparison, with a placeholder compare
on the miss path so unknown-key and known-key request timings match
(Phase 8.7). See `.env.example` and `docs/RUNBOOK.md §Authentication`
for the canonical `KEY_HMAC_PEPPER` / `KEY_HMAC_PEPPER_PREVIOUS`
env-var reference.

Persistence semantics: atomic temp + fsync + rename via `SaveKeysFile`
at [internal/mcp/keys.go:188](../internal/mcp/keys.go#L188); advisory
`LOCK_EX` during writes; pre-Phase-8.6 stores are migrated once at
startup with a one-time backup written to
`$MCP_API_KEYS_FILE.pre-migration` that is never overwritten.

**Postgres backend (Phase 3, opt-in).** When `KEY_STORE_BACKEND=postgres`
(see `.env.example`, Postgres key-store backend block), API-key entries
are stored in an operator-provisioned Postgres database instead of the
JSON file above. Keys at rest live in the `api_keys` table as a `BYTEA`
versioned digest — the same `hash_version` scheme (`v0` = plain SHA-256,
`v1` = HMAC-SHA256 under `KEY_HMAC_PEPPER`). `KEY_HMAC_PEPPER` is **required** when `KEY_STORE_BACKEND=postgres`
**and** `AUTH_PROVIDER=apikey` (boot fails with `ErrPepperRequired`
without it). FusionAuth deployments are exempt — they do not use the
key store. Legacy `v0` entries are lazily rehashed to `v1` on first
authenticated use and persisted atomically. The `api_keys` table includes
an `expires_at` column; expiry enforcement is not implemented until Phase
4 — keys do not expire today. `KEY_STORE_BACKEND=file` (the default) is
unchanged.

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
Roles are extracted from the token and used for RBAC gating; no write-approval policy is derived from the `automation` role (server-side write approval was removed in the Option 0 stateless migration).

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
loud (`ErrMissingClientIDHMACKey`) if it is unset.

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
`$NVNM_TRUST_PROXY_HEADERS=true`; otherwise the socket peer
(`RemoteAddr`) is used directly. When header-trust is enabled, the
server does **not** take the leftmost `X-Forwarded-For` entry — that
value is client-supplied and forgeable. Instead it walks a
configured number of trusted hops in from the right of
`X-Forwarded-For ++ RemoteAddr`, so a forged left-prefix cannot mint
its own rate-limit bucket. The hop count is
**`$NVNM_TRUSTED_PROXY_HOPS`** (int, default `1`): the number of
trusted proxy hops in front of the server, including the direct
socket peer. Set it to the real chain depth (`1` = single ingress,
`2` = CDN + ingress); `config.Load()` rejects `< 1` at boot
(`ErrInvalidTrustedProxyHops`) since 0 (or negative) trusted hops is
a meaningless configuration when proxy-header trust is enabled —
there is always at least the one proxy that set the headers. If the
computed index falls outside the observed chain (a missing or
shorter-than-expected `X-Forwarded-For`), derivation falls back to
`RemoteAddr` rather than ever trusting an unverified value. Rejected
requests emit a WARN log line containing the derived source IP and
(for missing-Authorization rejections) the request method; the
request path, token bytes, and request body are never logged
([internal/mcp/auth.go:49-84](../internal/mcp/auth.go#L49-L84) and
[internal/mcp/failrate.go:150-153](../internal/mcp/failrate.go#L150-L153)).
Deploy-side invariants for this control (proxy strips inbound `XFF`;
setting `NVNM_TRUSTED_PROXY_HOPS` to match real topology) are
documented in `docs/RUNBOOK.md` § "Trusted-proxy header invariants
(C3/C5)".

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
the log stream beyond the absent `client_id`. This follows from the
keyless-read auth-middleware design (Phase 9.16).

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
| `mcp.server.tool.errors`          | counter   | tool                  |
| `mcp.server.active_requests`      | gauge     | —                     |
| `evm.rpc.duration`                | histogram | method                |
| `evm.rpc.errors`                  | counter   | method                |
| `evm.rpc.retries`                 | counter   | —                     |
| `evm.rpc.circuit_breaker.state`   | gauge     | —                     |
| `evm.rpc.rate_limited`            | counter   | —                     |
| `mcp.write.broadcasts`            | counter   | outcome               |
| `mcp.write.relay_scope_rejected`  | counter   | cause                 |

No per-caller labels in metrics. `mcp.write.broadcasts`'s `outcome`
label is `ok` or `failed`. `mcp.write.relay_scope_rejected`'s `cause`
label is a closed, typed enum
([`internal/telemetry.RelayRejectCause`](../internal/telemetry/metrics.go)) —
never a free string — so a signer address or other caller-derived
value cannot compile into a metric label; this is the structural
defense against label-cardinality abuse and signer-address leakage on
the unauthenticated `/metrics` endpoint. The seven values:

| `cause` | Meaning |
|---------|---------|
| `decode` | The signed-tx hex failed to decode. |
| `anchor_misconfig` | Server misconfiguration (invalid `ANCHOR_ADDRESS`). Boot-time validation makes this **provably unreachable** at runtime — see `docs/RUNBOOK.md` § "Anonymous writes" for the guard chain. |
| `relay_scope` | The transaction's destination is not the anchor precompile. |
| `signer_blacklist` | The recovered signer is on the per-signer ban list (§ 8.2). |
| `signer_quota` | The recovered signer exceeded `MCP_SIGNER_WRITE_RATE` within `MCP_SIGNER_WRITE_WINDOW` (§ 8.2). |
| `quota_store_error` | The `signer_quota` Postgres store was unreachable and the fail-closed default rejected the write (§ 8.2). |
| `blacklist_store_error` | The `signer_blacklist` Postgres store was unreachable and the fail-closed default rejected the write (§ 8.2). |

Investigation playbooks for each cause live in
[`docs/INCIDENT_RUNBOOK.md`](INCIDENT_RUNBOOK.md#relay-scope-rejections-spiking).

### 7.2 Span attributes

`mcp.method`, `mcp.tool.name`, `mcp.request.id`, and `mcp.client.id`
(from `ClaimsFromContext`). The `mcp.client.id` attribute is
**omitted entirely** — not set to empty-string — on anonymous calls
under `MCP_KEYLESS_READS=true`. Authed traffic carries it unchanged.

Tool parameters and return values are **not** included in any metric or
span.

## 8. Persistence summary

| Item                                | Storage                                                                | Lifetime                        |
|-------------------------------------|------------------------------------------------------------------------|---------------------------------|
| API-key hash + metadata (file backend, default) | `$MCP_API_KEYS_FILE` (JSON)                            | Until deleted                   |
| API-key hash + metadata (Postgres backend, opt-in) | `api_keys` table, `BYTEA` versioned digest — see §2.1 | Until deleted                   |
| One-time legacy migration backup    | `$MCP_API_KEYS_FILE.pre-migration`                                     | Indefinite, never overwritten   |
| Authenticated claims                | Process memory                                                         | Single request                  |
| Request correlation UUID            | Process memory                                                         | Single request                  |
| Failed-auth IP buckets              | Process memory                                                         | 15-min inactivity TTL           |
| JWKS public keys                    | Process memory (`keyfunc` cache)                                       | Process lifetime                |
| Write-audit log (keyless writes, Phase 4a) | `write_audit` Postgres table (opt-in via `MCP_KEYLESS_PG_DSN`) | **Retained indefinitely unless `MCP_WRITE_AUDIT_RETENTION` is set.** When set, enforced in-process by the retention purge; `grantRole` broadcasts take the separate, longer `MCP_WRITE_AUDIT_GRANT_ROLE_RETENTION` window. See § 8.3. |
| Per-signer quota counters (Phase 5) | `signer_quota` Postgres table (same `MCP_KEYLESS_PG_DSN`)               | **Retained indefinitely unless `MCP_SIGNER_QUOTA_RETENTION` is set.** Note the quota logic itself never deletes a row — an expired window stops *counting*, but the row remains. See § 8.2. |
| Per-signer blacklist (Phase 5)      | `signer_blacklist` Postgres table (same `MCP_KEYLESS_PG_DSN`)           | Until an admin removes the entry. `MCP_SIGNER_BLACKLIST_RETENTION` can expire bans automatically but is **unset by default — deleting a ban un-bans that signer**. See § 8.2. |
| Admin mutation audit (F2/F5)        | `admin_audit` Postgres table (same `MCP_KEYLESS_PG_DSN`)                | **Retained indefinitely unless `MCP_ADMIN_AUDIT_RETENTION` is set.** Logs-only (not persisted) when `MCP_KEYLESS_PG_DSN` is unset; see "Audit-trail scope and known limitations" below. |
| Logs                                | stderr (operator-routed)                                               | Operator-defined                |
| Metrics / traces                    | OTLP / Prometheus sink                                                 | Sink-defined                    |

No other runtime writes to local storage.

### 8.1 Write-audit table

The `write_audit` table records attempted keyless broadcasts (when `MCP_KEYLESS_WRITES=true`). Each row captures the on-chain signer address, destination, value, calldata length, transaction hash, outcome (success/failure/queued), and timestamp. The table is append-only and populated when `MCP_KEYLESS_PG_DSN` is configured; without it, the server logs broadcast attempts but does not persist them.

Each row also records the 4-byte ABI **method selector** (`method_selector`, migration 0005). Under keyless writes every relayed transaction shares one destination — `checkRelayScope` permits only the anchor precompile — so `to_addr` cannot distinguish an administrative `grantRole` call from a routine anchor write. The selector is what allows the two retention windows below to be applied to the right rows. A selector is a public function identifier and carries no caller data.

Retention windows are **operator-configured and enforced in-process** — see § 8.3. They default to *unset*, which means **retain indefinitely**. An operator who publishes a retention period (in a privacy policy, DPA, or contract) must set the corresponding window here; a period promised in a document and enforced by nothing is not a retention period.

### Audit-trail scope and known limitations

`write_audit` and `admin_audit` are separate tables covering separate surfaces — broadcasts vs. admin mutations, respectively. Know the boundary between them before relying on either for compliance or forensics:

- **Covered by `write_audit` — every broadcast (F1):** `evm_send_raw_transaction` calls in **both** modes. Under `MCP_KEYLESS_WRITES=true` the keyless path decodes the signed transaction, recovers the signer, enforces relay-scope, and audits. Under keyless writes **off** (authed/self-host) the handler decodes the tx and enforces the same anchor-precompile relay-scope gate as the keyless path (`decodeAndScope` in [`internal/mcp/tools_evm_write.go`](../internal/mcp/tools_evm_write.go), `resolveBroadcast`), broadcasting the caller's raw bytes on success — so a permitted broadcast writes a signer-keyed row. (The legacy behaviour — best-effort decode, **no** relay-scope enforcement, raw passthrough — survives only when the operator sets `MCP_RELAY_ALLOW_ANY=true`.) The persisted row is keyed on the recovered on-chain **signer**; in authed mode the authenticated caller's `client_id` is carried in the broadcast's structured-log line (§ 6), not in the table (there is no `client_id` column). Persistence requires a store: `write_audit` exists only when `MCP_KEYLESS_PG_DSN` is configured — without it, broadcasts are logged (§ 6) but not persisted, in either mode.
- **Covered by `admin_audit` — admin mutations, when a DSN is configured (F2/F5):** the 7 admin mutation types (`key.create`, `key.update`, `key.delete`, `blacklist.add`, `blacklist.remove`, `pending.approve`, `pending.reject`) are now recorded in the `admin_audit` table, attributed to the acting `actor_id` (the shared `admin` identity, or a per-admin id from `ADMIN_API_KEYS_FILE` — see `docs/RUNBOOK.md` § "Admin identities and per-admin audit attribution"), whenever `MCP_KEYLESS_PG_DSN` is configured. This closes the F2 gap **when Postgres is configured**.
- **Not covered — structured logs only, no persisted table:** admin mutations when `MCP_KEYLESS_PG_DSN` is **not** configured fall back to attributed `INFO` log lines only — the same 7 actions, same `actor_id`, but never written to `admin_audit` (there is no table to write to). This is still a real gap in a no-DSN deployment; do not represent it as covered by a persisted audit trail. And in the rare case an authed broadcast's bytes do not decode under `MCP_RELAY_ALLOW_ANY=true` (the only mode that still broadcasts undecodable bytes), there is no recovered signer to key a `write_audit` row on ([`recordAudit`](../internal/mcp/tools_evm_write.go) returns without writing) — the tx still broadcasts (the RPC is the arbiter) and the attempt is captured in logs only. Without that flag, an undecodable authed tx is rejected before broadcast (`cause="decode"`), not relayed.

The F2 gap (previously: admin mutations were never persisted, logs-only, full stop) is now closed **conditionally** — it depends on `MCP_KEYLESS_PG_DSN` being configured, same precondition as `write_audit`. An operator who needs a persisted record of **admin actions** and has not configured `MCP_KEYLESS_PG_DSN` must still rely on their log-shipping pipeline (§ 6), not a table — no table exists to query.

### Per-signer write analysis (query `write_audit`, not Prometheus) <a id="per-signer-write-analysis-query-write_audit-not-prometheus"></a>

Prometheus write counters (`mcp_write_broadcasts_total{outcome}`,
`mcp_write_relay_scope_rejected_total{cause}` — see
[`docs/INCIDENT_RUNBOOK.md`](INCIDENT_RUNBOOK.md#relay-scope-rejections-spiking))
are intentionally aggregate — no signer label — to keep cardinality
bounded and signer addresses off the unauthenticated `/metrics`
endpoint. Per-signer and new-signer detection is served from the
signer-keyed `write_audit` table instead (columns `signer`,
`created_at` per the schema above and
[`internal/mcp/migrations/0002_init_write_audit.sql`](../internal/mcp/migrations/0002_init_write_audit.sql)):

**Per-signer write volume:**

```sql
SELECT signer, count(*) AS writes FROM write_audit GROUP BY signer ORDER BY writes DESC;
```

**New signers over time (first-seen per signer, bucketed by day):**

```sql
SELECT date_trunc('day', first_seen) AS day, count(*) AS new_signers
  FROM (SELECT signer, min(created_at) AS first_seen FROM write_audit GROUP BY signer) s
 GROUP BY day ORDER BY day;
```

Anonymous writes are live as of Phase 5 (gated by `MCP_KEYLESS_WRITES`;
§ 8.2), so new-signer flooding is now a meaningful sybil signal —
correlate a rising new-signer rate here with
`mcp_write_relay_scope_rejected_total{cause="signer_quota"}` (§ 7.1) to
distinguish routine onboarding from a coordinated flood.

### 8.2 Signer quota and blacklist (Phase 5)

Two additional Postgres tables enforce per-signer abuse limits on the keyless (`MCP_KEYLESS_WRITES=true`) write path: `signer_quota` throttles broadcast volume per signer, and `signer_blacklist` lets an operator permanently ban a signer address. Both live in the same database as `write_audit` (`MCP_KEYLESS_PG_DSN`) and are consulted before every keyless broadcast — blacklist first, since a banned signer is rejected outright and never consults or consumes quota. Neither table is consulted, or exists as a meaningful gate, when keyless writes are off.

**`signer_quota`** ([`internal/mcp/migrations/0003_init_signer_quota_blacklist.sql`](../internal/mcp/migrations/0003_init_signer_quota_blacklist.sql)):

| Column | Type | Purpose |
|--------|------|---------|
| `signer` | `TEXT` | Recovered signer address. Part of the composite primary key. |
| `window_start` | `TIMESTAMPTZ` | Start of the fixed counting window this row belongs to (`WindowStart(now, MCP_SIGNER_WRITE_WINDOW)` — a boundary-aligned bucket, not a sliding window). Part of the composite primary key. |
| `count` | `INTEGER` | Broadcasts by this signer within the window. Incremented only after a **successful** broadcast — a failed or errored broadcast never consumes quota. |

A signer's writes are permitted while `count < MCP_SIGNER_WRITE_RATE` (default `500`) within the current `MCP_SIGNER_WRITE_WINDOW` (default `24h`). Exceeding it rejects the broadcast (`ErrSignerQuotaExceeded`, `cause="signer_quota"` — § 7.1). Env-var reference: `docs/RUNBOOK.md` § "Anonymous writes."

**`signer_blacklist`:**

| Column | Type | Purpose |
|--------|------|---------|
| `signer` | `TEXT` (primary key) | Banned signer address. |
| `reason` | `TEXT` | Operator-supplied free-text reason; defaults to empty string. |
| `created_at` | `TIMESTAMPTZ` | Ban timestamp; defaults to `now()`. |

Managed exclusively via the admin API (`GET`/`POST /admin/signer-blacklist`, `DELETE /admin/signer-blacklist/{signer}` — see `docs/RUNBOOK.md` § "Signer blacklist (Phase 5)"). There is no equivalent admin surface for `signer_quota`: it is auto-managed counter state, never operator-edited directly.

**Fail-open knobs and default-closed posture.** `MCP_SIGNER_QUOTA_FAIL_OPEN` and `MCP_SIGNER_BLACKLIST_FAIL_OPEN` (both default `false`) govern what happens when the respective store itself is unreachable (e.g. the keyless Postgres pool is down) — **not** what happens on a legitimate quota/blacklist hit, which always rejects. Default fail-closed: a store error rejects the write (`cause="quota_store_error"` / `cause="blacklist_store_error"` — § 7.1) rather than silently admitting a signer the server could not actually check. Flipping either to `true` trades that safety check for availability during a store outage; see [`docs/INCIDENT_RUNBOOK.md`](INCIDENT_RUNBOOK.md#relay-scope-rejections-spiking) for when an operator might reach for that lever, and `docs/RUNBOOK.md` § "Anonymous writes" for the canonical env-var reference.

**Retention.** Both tables default to **retain indefinitely**; see § 8.3 for the windows that change that.

A correction worth stating plainly, because earlier revisions of this document got it wrong: `signer_quota` rows are **not** "effectively transient." The 500-per-24h quota is enforced by a **read-time timestamp comparison** — `Count` selects only rows matching the *current* `window_start`, and a new window **inserts a new row** (the primary key is `(signer, window_start)`). An expired window therefore stops *counting* against a signer, but its row is **never deleted by the quota logic**. Absent `MCP_SIGNER_QUOTA_RETENTION`, these rows accumulate one per signer per window, indefinitely. Do not describe this data as transient in any policy document.

`signer_blacklist` rows persist until an admin removes them. `MCP_SIGNER_BLACKLIST_RETENTION` can expire them automatically, but it is **unset by default and should usually stay that way**: deleting a ban row *un-bans* that signer, silently restoring an abuser's access when the window elapses.

### 8.3 Retention purge

The retention purge is the **only** mechanism in this server that deletes rows from `write_audit`, `signer_quota`, `signer_blacklist`, or `admin_audit`. Nothing else — no TTL, no partition drop, no background job — ever removes them. Prior to its introduction, every one of these tables grew without bound, and the retention periods this document previously cited were enforced by nothing.

Every window is **operator-configured and defaults to unset (= retain indefinitely)**. That default is deliberate: a self-hosting operator's retention obligations are theirs to determine, and a server that silently deleted their audit trail on our schedule would be worse than one that keeps everything. The trade is that an operator who *publishes* a retention period must configure it here for it to be true.

| Env var | Table | Default |
|---------|-------|---------|
| `MCP_WRITE_AUDIT_RETENTION` | `write_audit` (ordinary broadcasts) | unset — retain indefinitely |
| `MCP_WRITE_AUDIT_GRANT_ROLE_RETENTION` | `write_audit` (`grantRole` broadcasts only) | unset — falls under the ordinary window |
| `MCP_SIGNER_QUOTA_RETENTION` | `signer_quota` | unset — retain indefinitely |
| `MCP_SIGNER_BLACKLIST_RETENTION` | `signer_blacklist` | unset — bans are permanent (recommended) |
| `MCP_ADMIN_AUDIT_RETENTION` | `admin_audit` | unset — retain indefinitely |
| `MCP_RETENTION_PURGE_INTERVAL` | how often the purge runs | `1h` |

Values are Go durations; days and months are not units (90 days = `2160h`, 12 months = `8760h`).

**Boot guards.** The server refuses to start rather than enforce a policy an operator plainly did not intend:

- A **negative** window is rejected (`ErrRetentionNegative`) — it is always a typo, and reading it as "delete everything" would be catastrophic.
- A window set with a **non-positive purge interval** is rejected (`ErrRetentionIntervalInvalid`) — the purge would never fire and the window would be silently unenforced, which is the exact failure this feature exists to end.
- A **`grantRole` window shorter than the ordinary window** is rejected (`ErrRetentionGrantRoleShorter`). The administrative carve-out is only meaningful as a *longer* window; inverting it would purge the admin audit trail before the routine traffic it exists to outlive. Note that an *unset* ordinary window counts as **infinite** here, so setting only the `grantRole` window is the same inversion by another route and is likewise rejected.
- A `grantRole` window configured while the **anchor ABI is unavailable** is rejected: the selector cannot be derived, `grantRole` rows could not be told apart from ordinary ones, and every one of them would be purged on the shorter window while the server reported success.

**Operational notes.** Deletes are batched (5,000 rows per statement, capped per table per tick) so a large backlog cannot hold long row locks or starve live traffic; the remainder is picked up on the next tick. A failing sweep is logged and retried rather than killing the goroutine. Rows written before migration 0005 carry an empty `method_selector` and are treated as ordinary writes.

## 9. Outbound network destinations

1. **EVM JSON-RPC endpoint** (`$NVNM_EVM_RPC_URL`, optionally
   `$NVNM_EVM_ARCHIVE_RPC_URL`). One outbound per RPC tool call.
2. **FusionAuth JWKS endpoint.** One fetch at startup when
   `AUTH_PROVIDER=fusionauth`.
3. **Telemetry endpoint(s)** when enabled.

No analytics, advertising, or third-party tracking destinations.

## 10. HTTP headers and cookies

**Read:** `Authorization` (parsed; never logged), `Origin` (CORS),
`X-Forwarded-For` (only when `$NVNM_TRUST_PROXY_HEADERS=true`; see
§5), `X-Forwarded-Proto` (only when `$NVNM_TRUST_PROXY_HEADERS=true`;
used by the https-enforcement check, see §11).

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

When `$NVNM_TRUST_PROXY_HEADERS=true`, the server additionally
performs an app-layer defense-in-depth check: it reads
`X-Forwarded-Proto` (see §10) and rejects a request carrying an
explicit non-`https` value with `403`, catching a plaintext-downgrade
path inside the trust boundary that the ingress alone might miss.
This check is deliberately lenient when `X-Forwarded-Proto` is
**absent** — the ingress remains the primary, fail-closed TLS gate;
see `docs/RUNBOOK.md` § "Trusted-proxy header invariants (C3/C5)" for
the fail-open rationale.

Data at rest is covered above: API-key entries are stored as a
versioned hash digest (§2.1 — `v0` = plain SHA-256, `v1` = HMAC-SHA256
under `KEY_HMAC_PEPPER` when set); logs and metrics are
operator-routed to operator-chosen sinks (§6, §7); no other runtime
data is written to local storage. The server itself does not encrypt
files it writes — the only file it writes is the API-key store, whose
entries are already hashed.

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
