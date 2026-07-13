# OWASP Top 10 Self-Audit — NVNM Chain MCP Server

**Edition audited:** OWASP Top 10:2021
**Created:** 2026-05-14 (Phase 8.12 close-out)
**Last updated:** 2026-07-08 (Phase 5 anonymous writes + F1-F5 hardening)
**Scope:** Security posture of `main` as of the Phase 8 close-out — the MCP
server process, its HTTP/stdio transports, the admin REST API, the
prepare-sign-submit write path, configuration, and the CI/deployment surface.
**Status:** **Living document.** Phase 9 (OSS Readiness) and Phase 10 (DevOps
Foundations) extend it as new surface lands; each deferral below names the
phase that owns the follow-up.
**Companion:** [`docs/SECURITY_AUDIT.md`](SECURITY_AUDIT.md) — the point-in-time
pre-red-team assessment and its append-only dated update log. That document
records *what changed and when*; this one records *how the current posture maps
to a standard framework*. Where a control traces to a specific finding there,
it is cross-referenced inline.

---

## How to read this document

For each OWASP category:

- **Surface** — what attack surface this category actually has in this codebase.
- **Controls** — what is implemented, cited as `file` + symbol (line-drift-resistant).
- **Residual risk / deferrals** — what is *not* covered, and which phase owns it.
- **Disposition** — one of:
  - **COVERED** — the surface is mitigated; residual risk is low or is an explicit, documented design trade-off.
  - **PARTIAL** — meaningful controls exist, but a named gap is deferred to a later phase or to operator responsibility.
  - **DEFERRED** — the surface exists and is not yet mitigated in-repo; the owning phase is named.

A disposition of COVERED is **not** a claim of zero risk — it is a claim that
the residual risk is understood and accepted, or pushed to a documented
boundary (operator, reverse proxy, consuming agent).

---

## Coverage summary

| # | Category | Disposition | One-line |
|---|---|---|---|
| A01 | Broken Access Control | **PARTIAL** | Three write-auth modes: (1) authed-only (default), (2) `MCP_KEYLESS_READS=true` with writes still authed, (3) `MCP_KEYLESS_WRITES=true` exempts `evm_send_raw_transaction` from auth (`internal/mcp/authpolicy.go` `RequiresAuth`), governed instead by a per-signer blacklist + fixed-window quota (default 500/24h); health/metrics endpoints rely on network isolation; RBAC enforcement is conditional on roles being assigned. |
| A02 | Cryptographic Failures | **PARTIAL** | API keys stored as versioned hash digest at rest (v0 = plain SHA-256; v1 = HMAC-SHA256 under `KEY_HMAC_PEPPER`, opt-in) with constant-time compare; admin key sha256-hashed; OTLP TLS default-on; MCP HTTP transport TLS is a reverse-proxy responsibility. |
| A03 | Injection | **COVERED** | No SQL/shell/template surface; calldata is typed ABI-encoded, never concatenated. Indirect prompt injection via on-chain data is documented as a consumer-side concern. |
| A04 | Insecure Design | **COVERED** | Prepare-sign-submit keeps zero keys server-side; pre-mortem-driven design; honest wizard state names; `ENABLE_WRITE_TOOLS` still gates tool registration, but under `MCP_KEYLESS_WRITES=true` the RBAC-role precondition is bypassed and the gate becomes decode → relay-scope → blacklist → quota → broadcast; caller-side signature is the security boundary. |
| A05 | Security Misconfiguration | **PARTIAL** | Secure defaults + fail-loud config validation; K8s/Helm hardened and aligned; K8s `:latest` image tag pinning deferred to the Phase 10 release pipeline. |
| A06 | Vulnerable and Outdated Components | **COVERED** | `govulncheck` + license scan + Dependabot in CI; deps vendored and built `-mod=vendor`; Docker bases digest-pinned; GPL-exposed `go-ethereum` removed. |
| A07 | Identification and Authentication Failures | **COVERED** | Two providers (hashed API keys, FusionAuth JWT/JWKS); 256-bit random keys; pre-auth per-IP failure limiter; constant-time compare with miss-path timing flattening; F5 added `ADMIN_API_KEYS_FILE` for per-admin identity + attribution alongside the legacy single `ADMIN_API_KEY`. |
| A08 | Software and Data Integrity Failures | **PARTIAL** | Cosign binary signing + SBOM + vendored deps + atomic/backed-up key-file migration; container-image-digest signing deferred to Phase 10. |
| A09 | Security Logging and Monitoring Failures | **PARTIAL** | Append-only Postgres `admin_audit` table (F2) records all 7 admin mutation handlers with per-admin `actor_id`; `write_audit` now covers both keyless and authed broadcasts (F1). Persistence requires `MCP_KEYLESS_PG_DSN`; see `docs/DATA_HANDLING.md`. Alerting maturity remains Phase 10. |
| A10 | Server-Side Request Forgery | **COVERED** | No client-controlled outbound request target; all outbound URLs (RPC, OTLP, JWKS) are operator-configured. The anchor `uri` field is encoded, never fetched. |

---

## A01:2021 — Broken Access Control

### Surface
The MCP HTTP transport (`:8080`), per-tool authorization, the admin REST API
(`:8081`), and the health/metrics endpoints (`:9090`). The stdio transport is
in-scope only as a local-process boundary.

### Controls
- **Transport authentication.** `AuthMiddleware` in `internal/mcp/auth.go`
  requires `Authorization: Bearer <key>` on every HTTP request and attaches
  `auth.Claims` to the request context.
- **Fail-closed HTTP.** `loadAPIKeys` returns `config.ErrHTTPAuthRequired` when
  the transport is `http` and no validator can be constructed — the server
  refuses to start rather than serving unauthenticated. (SECURITY_AUDIT.md
  2026-05-12 review, item 3.)
- **Per-tool RBAC.** `requireRole` in `internal/mcp/rbac.go` is the first call
  in every tool handler. Read tools accept `reader|writer|admin|automation`;
  `evm_send_raw_transaction` in `tools_evm_write.go` excludes `reader`. A role
  mismatch returns `apperrors.ErrPermissionDenied`, which `SafeForClient`
  renders as a client-safe 403.
- **Three write-auth modes.** Write authorization is not a single on/off
  switch; `RequiresAuth` in `internal/mcp/authpolicy.go` selects between:
  1. **Authed-only (default).** `MCP_KEYLESS_WRITES=false` — every call to
     `evm_send_raw_transaction` requires a valid bearer key/JWT and RBAC role,
     same as any other write.
  2. **Keyless reads, authed writes.** `MCP_KEYLESS_READS=true` exempts the
     read-only tools in `authExemptTools` from auth, but
     `evm_send_raw_transaction` is deliberately kept **out** of that map — a
     keyless-reads deployment does not silently become a keyless-writes one.
  3. **Keyless writes.** `MCP_KEYLESS_WRITES=true` (which requires
     `MCP_KEYLESS_READS=true` — `config.ErrKeylessWritesRequiresReads`) exempts
     `evm_send_raw_transaction` itself from authentication. In this mode RBAC
     is replaced by two runtime gates enforced in `checkSignerGates`
     (`internal/mcp/tools_evm_write.go`): a per-signer **blacklist**
     (`internal/mcp/signer_blacklist.go`, checked first — a banned signer never
     consumes quota) and a per-signer, fixed-window **quota**
     (`internal/mcp/signer_quota.go`, `MCP_SIGNER_WRITE_RATE` default 500 per
     `MCP_SIGNER_WRITE_WINDOW` default 24h). Both gates fail closed by default
     (`MCP_SIGNER_QUOTA_FAIL_OPEN` / blacklist-fail-open default `false`) if
     their backing Postgres store is unreachable.
- **Write tools off by default.** `ENABLE_WRITE_TOOLS` (`internal/config`)
  defaults to `false`; `internal/mcp/server.go` only registers write tools when
  it is `true`.
- **Admin API isolation.** `ADMIN_API_ADDR` defaults to `127.0.0.1:8081`;
  `adminAuth` in `internal/mcp/admin.go` compares sha256 hashes under
  `subtle.ConstantTimeCompare`.

### Residual risk / deferrals
- **RBAC enforcement is conditional.** `requireRole` is a no-op when claims are
  absent or carry no roles — the stdio path, and any API key issued without a
  `roles` list. This is deliberate (stdio is a local-process trust boundary;
  role-free keys preserve the pre-RBAC behavior) but it means **RBAC is only as
  strong as the operator's key provisioning discipline.** Operators issuing
  HTTP keys should always assign roles.
- **Health/metrics endpoints are unauthenticated** (`internal/telemetry/health.go`).
  Mitigated today by the `deploy/k8s/networkpolicy.yaml` ingress rules; a
  dedicated auth layer for `:9090` is a **Phase 10** consideration.

### Disposition
**PARTIAL.** The MCP transport is authenticated and RBAC-gated by default,
with a fail-closed posture across all three write-auth modes described above.
When an operator opts into `MCP_KEYLESS_WRITES=true`, RBAC is intentionally
replaced by the blacklist + quota gates rather than silently dropped. The two
named gaps — conditional RBAC (mode 1/2) and unauthenticated `:9090` — are
documented operator-boundary / Phase 10 items, not silent holes.

---

## A02:2021 — Cryptographic Failures

### Surface
API keys and the admin key at rest; signed transactions and tool traffic in
transit; OTLP telemetry; the signature-verification helper.

### Controls
- **API keys stored as versioned hash digest at rest.** `HashKey` in
  `internal/auth/keyhash.go` produces a `hash_version`-tagged digest: `v0` =
  plain SHA-256 (legacy default when `KEY_HMAC_PEPPER` is unset); `v1` =
  HMAC-SHA256 under a server-held pepper (`KEY_HMAC_PEPPER`, optional). The
  pepper is a server-side secret that makes a key-store dump non-reversible
  offline; it is opt-in and wired via env. `KeyEntry` on disk holds `key_hash` +
  `key_prefix`, never the raw token (Phase 8.6 — SECURITY_AUDIT.md update
  2026-05-13). The keys file is written `0o600` via atomic tmp+fsync+rename
  under an advisory `flock`. Legacy `v0` keys continue to authenticate via
  versioned candidate lookup; persisted re-hashing to `v1` lands with the
  Postgres backend (Phase 3). See `.env.example` for the `KEY_HMAC_PEPPER` /
  `KEY_HMAC_PEPPER_PREVIOUS` env-var reference.
- **Constant-time validation.** `internal/auth/apikey_validator.go` compares
  fixed-length hash digests under `subtle.ConstantTimeCompare`; the miss path
  burns `missPathPlaceholder` to flatten hit/miss timing (Phase 8.7).
- **Admin key.** Hashed + constant-time compared in `adminAuth`
  (`internal/mcp/admin.go`).
- **Key generation.** `GenerateKey` in `internal/mcp/keys.go` draws 32 bytes
  (256 bits) from `crypto/rand`. Because the secret is full-entropy random — not
  a user-chosen password — a fast hash (SHA-256 / HMAC-SHA256) is the correct
  primitive; a slow KDF would add cost without adding meaningful brute-force
  resistance.
- **OTLP transport.** `OTLP_INSECURE` defaults to `false` — gRPC exporters
  connect with TLS unless an operator explicitly opts into the localhost-sidecar
  insecure mode (SECURITY_AUDIT.md 2026-05-12 review, item 9).
- **Signature verification.** `nvnm_setup_verify_signature`
  (`internal/mcp/tools_setup_verify.go`) recovers the signer via the vendored
  `defiweb` EIP-191 path; no homemade crypto.

### Residual risk / deferrals
- **The MCP HTTP transport has no TLS of its own.** `internal/mcp/server.go`
  configures timeouts and body limits but no `TLSConfig`; TLS termination is a
  documented reverse-proxy responsibility (`docs/DESIGN.md` §10). This is the
  deployment model, not an oversight — but it *is* a residual risk if the proxy
  assumption fails in a given deployment. Operator-owned; revisited for the
  public deployment in **Phase 10**.

### Disposition
**PARTIAL.** Secrets at rest and the OTLP channel are cryptographically sound.
In-transit protection of the MCP transport is delegated to the reverse proxy by
design and documented as such.

---

## A03:2021 — Injection

### Surface
Tool inputs (addresses, hashes, hex blobs, registry names, URIs, metadata
strings); construction of EVM calldata; on-chain string data flowing back into
a consuming agent's context.

### Controls
- **No classic injection surface.** The server runs no database, no shell-out,
  and no template engine. There is no SQL, OS-command, or template-injection
  sink to reach.
- **Structured, validated inputs.** Tool arguments arrive as typed JSON, not
  freeform text. `parseAddress`, `parseHash`, and `parseHexData` in
  `internal/mcp/tools_evm.go` reject malformed input at the handler boundary;
  `parseHexData` additionally caps decoded size.
- **Typed ABI encoding.** Calldata is built with `defiabi` `Method.EncodeArgs`
  in `internal/anchor/prepare.go` — arguments are encoded positionally into the
  ABI tuple, never string-concatenated into a call payload.
- **Deserialization.** Inbound JSON is unmarshalled into concrete typed structs;
  there is no polymorphic / gadget-deserialization surface.
- **Free-text control/bidi-char rejection (F3).** The public key-request HTTP
  endpoint's free-text fields (`company`, `intended_use`) are rejected outright
  if they contain Unicode `Cc` control characters (NUL, CRLF injection) or `Cf`
  format/bidi-override characters (e.g. U+202E RLO, the Trojan-Source class,
  zero-width spaces, BOM) — `internal/mcp/keys_request_http.go`.

### Residual risk / deferrals
- **Indirect prompt injection.** On-chain string fields (registry names, record
  metadata) are returned verbatim in tool responses and could carry
  instruction-like text aimed at the consuming LLM. The server's deliberate
  stance is that it returns on-chain *truth* and does not sanitize it; the
  threat and the consumer-side defenses are documented in
  `docs/SECURITY_CONSUMER_GUIDANCE.md` (SECURITY_AUDIT.md 2026-05-12 AI/agent
  findings). This is a consumer-boundary concern by design, not a server-side
  gap.

### Disposition
**COVERED.** Traditional injection is structurally absent. The one real vector —
prompt injection via on-chain data — is explicitly documented and assigned to
the consumer boundary.

---

## A04:2021 — Insecure Design

### Surface
The architecture itself: key custody, the write path, the onboarding wizard's
state model, and whether security was designed in or bolted on.

### Controls
- **Zero server-side key custody.** The prepare-sign-submit pattern means the
  server constructs *unsigned* transactions only; signing happens caller-side.
  There is no private key on the server to steal — the single highest-leverage
  design decision in the system, and an invariant called out in
  `internal/config` and `docs/DESIGN.md`.
- **Pre-mortem-driven design.** The Phase 8 design enumerated ten
  failure modes *before* implementation and recorded the refinements applied
  in response (atomic key-file writes, legacy-tx opt-out, honest wizard states,
  fail-loud env-var migration).
- **Honest state naming.** The wizard uses `funded_active` rather than the
  misleading `ready_to_anchor` — a deliberate
  design choice against a state name that would over-claim what the server can
  actually observe.
- **Write gating.** `ENABLE_WRITE_TOOLS=true` (off by default) still gates
  whether the write tool is *registered* at all. On top of that, authorization
  differs by mode: under the default authed configuration a write additionally
  requires an authenticated caller with `writer`, `admin`, or `automation`
  role; under `MCP_KEYLESS_WRITES=true` that RBAC-role precondition is
  deliberately bypassed and replaced by decode → relay-scope → blacklist →
  quota → broadcast (see A01). The caller-side signature is the security
  boundary in every mode: the server broadcasts exactly the signed bytes it
  receives. Human confirmation before submitting a signed transaction is the
  client/agent's responsibility.
- **Defense in depth.** Origin guard → per-IP failure limiter → body limit →
  auth → per-client rate limiter, layered in `internal/mcp/server.go`.

### Residual risk / deferrals
- **Autonomous-agent abuse.** An agent that *also* holds signing capability can
  submit writes without human review (SECURITY_AUDIT.md AI/agent finding A1).
  The server no longer gates writes via elicitation; the residual is inherent to
  giving an autonomous agent a signer and is a property of the deployment, not
  a server defect. Rate limiting narrows the blast radius. Documented for
  consumers in `docs/SECURITY_CONSUMER_GUIDANCE.md`.

### Disposition
**COVERED.** Insecure Design is the category Phase 8 most directly strengthened:
the architecture removes the highest-value target (keys), and the design process
itself (pre-mortem, honest naming) is part of the control set.

---

## A05:2021 — Security Misconfiguration

### Surface
Environment-variable configuration, default values, the `INVENIAM_*` → `NVNM_*`
migration, and the K8s / Helm deployment manifests.

### Controls
- **Fail-loud on legacy config.** `Config.Load` rejects any `INVENIAM_*`
  chain-config key with `ErrLegacyEnvVars` and a pointer to the runbook
  migration table — even when the matching `NVNM_*` is also set (Phase 8.9;
  the project's migration-hygiene principle). Stale config cannot drift silently.
- **Secure defaults.** `ENABLE_WRITE_TOOLS=false`, `OTLP_INSECURE=false`,
  `ADMIN_API_ADDR=127.0.0.1:8081`.
- **No silent fallback.** Config validation refuses to start when the chain
  environment cannot be resolved (SECURITY_AUDIT.md 2026-05-12 Go review) — a
  private-fork operator must set `NVNM_CHAIN_ENVIRONMENT` explicitly.
- **Hardened, aligned manifests.** `deploy/k8s/deployment.yaml` and
  `deploy/helm/nvnm-mcp-server/values.yaml` both set `runAsNonRoot`,
  `runAsUser/Group: 65532`, `allowPrivilegeEscalation: false`,
  `readOnlyRootFilesystem: true`, `capabilities.drop: [ALL]`. The Helm/raw-K8s
  drift from the original audit (Finding 6) is closed.
- **Network isolation.** `deploy/k8s/networkpolicy.yaml` restricts ingress
  (8080 from labelled MCP clients only; 8081 intentionally not exposed) and
  egress (443 / OTLP / DNS only).
- **Minimal image.** `gcr.io/distroless/static-debian12`, digest-pinned in
  `Dockerfile`.

### Residual risk / deferrals
- **K8s `Deployment` still references the `:latest` image tag.** The Dockerfile
  digest-pins its *base* images, but the deployed image is tag-mutable. A
  comment block in `deployment.yaml` documents the substitution; the real fix
  needs the release pipeline to emit a digest-stable image — **Phase 10**
  (DevOps Foundations). (SECURITY_AUDIT.md 2026-05-12 review, item 6.)
- **CORS middleware** is intentionally not implemented (by design)
  — only relevant once browser-based MCP clients hit a public deployment.
  **Phase 9.**

### Disposition
**PARTIAL.** Configuration hygiene and manifest hardening are strong. The
`:latest` tag is the one concrete misconfiguration left, and it is blocked on a
Phase 10 release-pipeline change rather than an in-repo edit.

---

## A06:2021 — Vulnerable and Outdated Components

### Surface
The Go dependency tree, the Docker base images, and the EVM client library.

### Controls
- **`govulncheck` in CI** (`.github/workflows/ci.yml`), run before tests.
  Verified clean at Phase 8.12 close-out: zero vulnerabilities affecting called
  code.
- **License + supply-chain scanning.** `go-licenses` enforces a permissive-only
  allowlist on every push/PR; `anchore/sbom-action` emits a CycloneDX SBOM.
- **Dependabot** (`.github/dependabot.yml`) covers `gomod`, `docker`, and
  `github-actions` weekly.
- **Vendored dependencies.** `vendor/` is committed and CI builds/tests with
  `-mod=vendor`, so a compromised upstream module cannot affect a build that
  passed locally.
- **GPL exposure removed.** `github.com/ethereum/go-ethereum` (GPL-3.0 /
  LGPL-3.0 consumed packages) was fully removed and replaced with
  `defiweb/go-eth` (MIT) — SECURITY_AUDIT.md update 2026-05-13.
- **Digest-pinned bases.** Both Dockerfile stages pin `@sha256:` digests.

### Residual risk / deferrals
- **`defiweb/go-eth` is a small-org dependency** (bus-factor risk). The
  committed `vendor/` copy is the mitigation: the source is forkable in place if
  upstream is abandoned (SECURITY_AUDIT.md update 2026-05-13, operational
  callout).

### Disposition
**COVERED.** Scanning, pinning, vendoring, and license enforcement are all in CI
and were re-verified green at close-out.

---

## A07:2021 — Identification and Authentication Failures

### Surface
The authentication providers, credential strength, brute-force / credential-
stuffing resistance, and session handling.

### Controls
- **Two providers.** `apikey` — file-backed, versioned-hash-at-rest (v0 = plain SHA-256; v1 = HMAC-SHA256 under `KEY_HMAC_PEPPER`, opt-in), hot-reloadable
  bearer keys with an admin REST API. `fusionauth` — JWT validation against
  JWKS with issuer / audience / clock-skew checks (`internal/auth/fusionauth.go`).
  Selected via `AUTH_PROVIDER`.
- **Strong credentials by construction.** API keys are 256-bit `crypto/rand`
  values (`GenerateKey`) — there is no user-chosen-password or weak-credential
  surface.
- **Credential-stuffing defense.** `IPFailRateLimiter` in
  `internal/mcp/failrate.go` is a *pre-auth*, per-source-IP failure budget;
  `AuthMiddleware` calls `Penalize` on every 401 (SECURITY_AUDIT.md 2026-05-12
  review, item 1).
- **Timing-safe comparison.** Both the apikey and admin paths compare
  fixed-length hash digests under `subtle.ConstantTimeCompare` (apikey: versioned
  SHA-256 or HMAC-SHA256 per `hash_version`; admin: plain SHA-256), with a
  miss-path placeholder to flatten hit/miss timing.
- **Correct failure semantics.** Bearer failures return `401` per RFC 7235 with
  a plain `WWW-Authenticate: Bearer` challenge (RFC 6750), and do not disclose
  whether a key exists versus is wrong (`internal/mcp/auth.go`). The OAuth
  discovery well-known paths return `404` (`wellKnownGuard`,
  `internal/mcp/wellknown.go`), so MCP clients fall back to the configured
  Bearer credential instead of attempting an OAuth flow the server does not
  offer. The challenge carries no `resource_metadata` parameter by design.
- **No session fixation surface.** The server is stateless; MCP session
  lifecycle is handled by the SDK.
- **Per-admin identity (F5).** `ADMIN_API_KEYS_FILE` (`internal/config`) loads
  a JSON map of per-admin keys alongside the legacy single `ADMIN_API_KEY`.
  `adminAuth` in `internal/mcp/admin.go` resolves `sha256(admin-key) -> admin
  id` under `subtle.ConstantTimeCompare`, so admin mutations are attributable
  to an individual actor rather than one shared credential.

### Residual risk / deferrals
- **JWKS is fetched at validator initialization.** Key rotation on the
  FusionAuth side may therefore require a server restart to be picked up. Low
  impact for the current internal deployment; a refresh cadence is a reasonable
  **Phase 9** hardening item if the FusionAuth path goes to production use.

### Disposition
**COVERED.** Credentials are strong by construction, comparison is timing-safe,
and credential stuffing is rate-limited before auth even runs.

---

## A08:2021 — Software and Data Integrity Failures

### Surface
The CI/CD pipeline and its artifacts, the integrity of the on-disk key store
across the Phase 8.6 migration, and deserialization paths.

### Controls
- **Artifact signing.** CI performs Cosign keyless (Sigstore OIDC) signing of
  the compiled binary; an SBOM is published alongside.
- **Build integrity.** `vendor/` is committed and builds use `-mod=vendor`; CI
  runs with `permissions: contents: read`.
- **Key-file migration integrity.** The Phase 8.6 migration is the principal
  *data*-integrity control: `SaveKeysFile` does atomic tmp+fsync+rename under an
  advisory `flock`; a one-shot `<path>.pre-migration` backup is written before
  any mutation and never overwritten; `LoadKeysFile` falls back to `<path>.tmp`
  to recover an interrupted write. Regression-covered in
  `internal/mcp/keys_migration_test.go`.
- **No unsafe deserialization.** Inbound data is JSON unmarshalled into typed
  structs only.

### Residual risk / deferrals
- **Container-image-digest signing is not yet in place.** Cosign currently signs
  the *binary blob*, not the published container image by digest; image-digest
  signing is blocked on a registry decision. **Phase 10.**
- The `:latest` deployment tag (also under A05) undermines deploy-time artifact
  integrity for the same reason and is owned by the same Phase 10 release-pipeline
  work.

### Disposition
**PARTIAL.** Binary signing, SBOM, vendored builds, and the key-file migration
integrity are all in place. The container-image integrity story (digest signing
+ digest-pinned deploy) is the named gap, deferred to the Phase 10 release
pipeline.

---

## A09:2021 — Security Logging and Monitoring Failures

### Surface
Audit logging of write operations, per-request telemetry, and the detection /
alerting posture.

### Controls
- **Write-operation audit logs.** `evm_send_raw_transaction` and the
  `anchor_prepare_*` handlers emit structured `slog.Group("audit", ...)` lines
  with stable `tool`, `phase`, and `client_id` keys; the broadcast path logs the
  `tx_hash` (SECURITY_AUDIT.md Finding 8, plus the 2026-05-12 consistency fix).
- **Append-only admin mutation audit (F2/F5).** The `admin_audit` Postgres
  table (migration `internal/mcp/migrations/0004_admin_audit.sql`) records all
  7 admin mutation actions — `key.create`, `key.update`, `key.delete`,
  `blacklist.add`, `blacklist.remove`, `pending.approve`, `pending.reject`
  (`internal/mcp/admin_audit.go`) — with a per-admin `actor_id` resolved from
  `ADMIN_API_KEYS_FILE` (F5), so mutations are attributable to an individual
  admin rather than a shared credential.
- **Broadcast audit closed for authed mode (F1).** `write_audit` previously
  only captured keyless-mode broadcasts. `evm_send_raw_transaction` now
  audits **both** modes: under `MCP_KEYLESS_WRITES=true` the handler decodes,
  enforces relay-scope, and audits; with keyless writes **off**, the handler
  still decodes the signed transaction best-effort solely to record a
  signer-keyed `write_audit` row (no relay-scope enforcement, raw passthrough)
  — see `internal/mcp/tools_evm_write.go` `resolveBroadcast`.
- **Per-call telemetry.** The middleware in `internal/telemetry/middleware.go`
  logs every tool call with `method`, `tool`, `request_id`, `client_id`,
  `duration`, and `status`, and mirrors it onto OTel spans + Prometheus metrics.
- **Privacy-aware logging.** Tool arguments and return values are deliberately
  *not* recorded; errors are kept in full on the internal span but sanitized via
  `SafeForClient` before reaching the client. Credential material is never
  logged — `SafeURL` / `SafeAddr` / `SafeTxData` in `internal/logging/redact.go`
  redact at the boundary. F4 gates raw-key-in-logs behind an explicit
  `NVNM_ALLOW_KEY_IN_LOGS` opt-in (default `false`).
- **Alerting scaffolding.** `deploy/prometheus/alerts.yaml` ships baseline
  alerts.

### Residual risk / deferrals
This section intentionally does not restate the audit-trail scope and gaps —
`docs/DATA_HANDLING.md`'s "Audit-trail scope and known limitations" section is
the single source of truth for what is and is not persisted. In short: both
`admin_audit` and `write_audit` require `MCP_KEYLESS_PG_DSN` to be configured —
without it, mutations/broadcasts are logged but not persisted, in either mode.
`write_audit` has no `client_id` column (rows are signer-keyed; the authed
caller's `client_id` lives only in the structured-log line, not the table).
Retention/pruning for both tables is implemented in-process (see
[`DATA_HANDLING.md`](DATA_HANDLING.md) § 8.3) but is **opt-in**: every window
defaults to unset, which means retain indefinitely. An operator who publishes a
retention period must set the corresponding env var for it to be enforced.
- **No dedicated, append-only *log* stream (as distinct from the Postgres
  audit tables above).** General `tool call` telemetry still shares the common
  `slog` stream; stream separation and alerting maturity for that log line
  remain **Phase 10** (DevOps Foundations).
- **stdio transport has no client identity** — audit lines on that path carry an
  empty `client_id`. Acceptable for the local-process trust boundary.

### Disposition
**PARTIAL.** Admin mutations (F2/F5) and both keyless and authed broadcasts
(F1) now write to append-only Postgres audit tables with per-actor/per-signer
attribution, closing the gap this section previously called out as
"no dedicated, append-only audit stream." The remaining maturity gap —
persistence without `MCP_KEYLESS_PG_DSN` configured, retention, and a
segregated/alert-wired general log stream — is tracked in
`docs/DATA_HANDLING.md` and owned by Phase 10.

---

## A10:2021 — Server-Side Request Forgery (SSRF)

### Surface
Every outbound request the server makes: EVM JSON-RPC, the OTLP collector, and
(on the FusionAuth path) the JWKS endpoint.

### Controls
- **No client-controlled outbound target.** The EVM RPC URL is the
  operator-supplied `NVNM_EVM_RPC_URL`; the OTLP endpoint and the FusionAuth /
  JWKS URL are likewise operator-configured. **No tool accepts a URL, hostname,
  or port from the caller** — tool inputs are addresses, hashes, and hex blobs.
  A client cannot redirect where the server connects.
- **URL validation.** The configured RPC URL is restricted to `http`/`https`
  schemes at config-load time (`internal/config`).
- **The anchor `uri` field is not an SSRF vector.** `anchor_prepare_add_record`
  accepts a `uri` string, but the server only ABI-encodes it into calldata for
  the caller to sign — **it is never dereferenced or fetched.** This is called
  out explicitly because the field *looks* like an outbound-request surface and
  is not.

### Residual risk / deferrals
- None identified. The category is structurally inapplicable: the outbound
  request set is fixed at startup and operator-owned.

### Disposition
**COVERED.** SSRF is designed out — there is no path from client input to an
outbound-request destination.

---

## Maintenance

This document is a **living coverage matrix**, not a point-in-time snapshot.
When new surface lands:

- **Phase 9 (OSS Readiness)** should revisit A05 (CORS for browser MCP clients),
  A07 (JWKS refresh cadence if the FusionAuth path goes to production), and add
  any categories touched by the public-facing OSS scaffolding.
- **Phase 10 (DevOps Foundations)** owns the deferrals in A05 / A08 (digest-pinned
  and digest-signed images via the release pipeline), A01 (`:9090` auth posture),
  and A09 (retention/pruning for the `admin_audit`/`write_audit` Postgres
  tables, persistence when `MCP_KEYLESS_PG_DSN` is unset, and general-log-stream
  segregation + alerting maturity).
- Any change that moves a disposition (e.g. PARTIAL → COVERED) should update both
  the per-category section and the **Coverage summary** table, and note the
  change in `docs/SECURITY_AUDIT.md`'s update log so the dated history stays
  authoritative.

Disposition legend: **COVERED** = residual risk understood/accepted or pushed to
a documented boundary · **PARTIAL** = controls exist, named gap deferred ·
**DEFERRED** = surface exists, mitigation owned by a named later phase.
