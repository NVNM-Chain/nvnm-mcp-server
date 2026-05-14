# OWASP Top 10 Self-Audit — NVNM Chain MCP Server

**Edition audited:** OWASP Top 10:2021
**Created:** 2026-05-14 (Phase 8.12 close-out)
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
| A01 | Broken Access Control | **PARTIAL** | MCP transport authenticated + RBAC-gated; health/metrics endpoints rely on network isolation; RBAC enforcement is conditional on roles being assigned. |
| A02 | Cryptographic Failures | **PARTIAL** | API keys + admin key sha256-hashed at rest with constant-time compare; OTLP TLS default-on; MCP HTTP transport TLS is a reverse-proxy responsibility. |
| A03 | Injection | **COVERED** | No SQL/shell/template surface; calldata is typed ABI-encoded, never concatenated. Indirect prompt injection via on-chain data is documented as a consumer-side concern. |
| A04 | Insecure Design | **COVERED** | Prepare-sign-submit keeps zero keys server-side; pre-mortem-driven design; honest wizard state names; human-approval elicitation on writes. |
| A05 | Security Misconfiguration | **PARTIAL** | Secure defaults + fail-loud config validation; K8s/Helm hardened and aligned; K8s `:latest` image tag pinning deferred to the Phase 10 release pipeline. |
| A06 | Vulnerable and Outdated Components | **COVERED** | `govulncheck` + license scan + Dependabot in CI; deps vendored and built `-mod=vendor`; Docker bases digest-pinned; GPL-exposed `go-ethereum` removed. |
| A07 | Identification and Authentication Failures | **COVERED** | Two providers (hashed API keys, FusionAuth JWT/JWKS); 256-bit random keys; pre-auth per-IP failure limiter; constant-time compare with miss-path timing flattening. |
| A08 | Software and Data Integrity Failures | **PARTIAL** | Cosign binary signing + SBOM + vendored deps + atomic/backed-up key-file migration; container-image-digest signing deferred to Phase 10. |
| A09 | Security Logging and Monitoring Failures | **PARTIAL** | Write ops audit-logged with client identity + tx hash; per-call telemetry. A dedicated append-only audit stream and alerting maturity are Phase 10. |
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
- **Write tools off by default.** `ENABLE_WRITE_TOOLS` (`internal/config`)
  defaults to `false`; `internal/mcp/server.go` only registers write tools when
  it is `true`.
- **Write approval.** `internal/mcp/approval.go` resolves a per-client or global
  approval policy and routes through MCP elicitation when approval is
  `required` (the default).
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
**PARTIAL.** The MCP transport is authenticated and RBAC-gated with a
fail-closed posture. The two named gaps — conditional RBAC and unauthenticated
`:9090` — are documented operator-boundary / Phase 10 items, not silent holes.

---

## A02:2021 — Cryptographic Failures

### Surface
API keys and the admin key at rest; signed transactions and tool traffic in
transit; OTLP telemetry; the signature-verification helper.

### Controls
- **API keys hashed at rest.** `HashKey` in `internal/auth/keyhash.go`
  (sha256 → hex). `KeyEntry` on disk holds `key_hash` + `key_prefix`, never the
  raw token (Phase 8.6 — SECURITY_AUDIT.md update 2026-05-13). The keys file is
  written `0o600` via atomic tmp+fsync+rename under an advisory `flock`.
- **Constant-time validation.** `internal/auth/apikey_validator.go` compares
  fixed-length sha256 digests under `subtle.ConstantTimeCompare`; the miss path
  burns `missPathPlaceholder` to flatten hit/miss timing (Phase 8.7).
- **Admin key.** Hashed + constant-time compared in `adminAuth`
  (`internal/mcp/admin.go`).
- **Key generation.** `GenerateKey` in `internal/mcp/keys.go` draws 32 bytes
  (256 bits) from `crypto/rand`. Because the secret is full-entropy random — not
  a user-chosen password — a fast hash (sha256) is the correct primitive; a slow
  KDF would add cost without adding meaningful brute-force resistance.
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
- **Pre-mortem-driven design.** `docs/PHASE_8_DESIGN.md` §4 enumerates ten
  failure modes *before* implementation and §5 records the refinements applied
  in response (atomic key-file writes, legacy-tx opt-out, honest wizard states,
  fail-loud env-var migration).
- **Honest state naming.** The wizard uses `funded_active` rather than the
  misleading `ready_to_anchor` (`PHASE_8_DESIGN.md` §3.7.1) — a deliberate
  design choice against a state name that would over-claim what the server can
  actually observe.
- **Human-in-the-loop by default.** `WRITE_APPROVAL_DEFAULT` defaults to
  `required`; the `automation` role / `auto` approval is an explicit,
  per-client opt-out for trusted pipelines.
- **Defense in depth.** Origin guard → per-IP failure limiter → body limit →
  auth → per-client rate limiter, layered in `internal/mcp/server.go`.

### Residual risk / deferrals
- **Autonomous-agent abuse.** An agent that *also* holds signing capability can
  self-approve writes (SECURITY_AUDIT.md AI/agent finding A1). The approval
  elicitation and rate limiters narrow this, but the residual is inherent to
  giving an autonomous agent a signer — it is a property of the deployment, not
  a server defect. Documented for consumers.

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
  migration table — even when the matching `NVNM_*` is also set (Phase 8.9; see
  `CLAUDE.md` "Migration hygiene principle"). Stale config cannot drift silently.
- **Secure defaults.** `ENABLE_WRITE_TOOLS=false`, `OTLP_INSECURE=false`,
  `ADMIN_API_ADDR=127.0.0.1:8081`, `WRITE_APPROVAL_DEFAULT=required`.
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
- **CORS middleware** is intentionally not implemented (`PHASE_8_DESIGN.md` §2)
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
- **Two providers.** `apikey` — file-backed, sha256-hashed, hot-reloadable
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
  fixed-length sha256 digests under `subtle.ConstantTimeCompare`, with a
  miss-path placeholder to flatten hit/miss timing.
- **Correct failure semantics.** Bearer failures return `401` per RFC 7235 and
  do not disclose whether a key exists versus is wrong (`internal/mcp/auth.go`).
- **No session fixation surface.** The server is stateless; MCP session
  lifecycle is handled by the SDK.

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
- **Per-call telemetry.** The middleware in `internal/telemetry/middleware.go`
  logs every tool call with `method`, `tool`, `request_id`, `client_id`,
  `duration`, and `status`, and mirrors it onto OTel spans + Prometheus metrics.
- **Privacy-aware logging.** Tool arguments and return values are deliberately
  *not* recorded; errors are kept in full on the internal span but sanitized via
  `SafeForClient` before reaching the client. Credential material is never
  logged — `SafeURL` / `SafeAddr` / `SafeTxData` in `internal/logging/redact.go`
  redact at the boundary.
- **Alerting scaffolding.** `deploy/prometheus/alerts.yaml` ships baseline
  alerts.

### Residual risk / deferrals
- **No dedicated, append-only audit stream.** Audit lines are structured but
  share the general `slog` stream; the original Finding 8 mitigation envisioned
  a *separate* audit log stream. Stream separation, retention, and
  tamper-evidence are observability concerns owned by **Phase 10** (DevOps
  Foundations).
- **stdio transport has no client identity** — audit lines on that path carry an
  empty `client_id`. Acceptable for the local-process trust boundary.

### Disposition
**PARTIAL.** Write operations are attributable (identity + tx hash) and every
call is metered. The maturity gap — a segregated, retained, alert-wired audit
stream — is a named Phase 10 deliverable.

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
  and A09 (segregated append-only audit stream + alerting maturity).
- Any change that moves a disposition (e.g. PARTIAL → COVERED) should update both
  the per-category section and the **Coverage summary** table, and note the
  change in `docs/SECURITY_AUDIT.md`'s update log so the dated history stays
  authoritative.

Disposition legend: **COVERED** = residual risk understood/accepted or pushed to
a documented boundary · **PARTIAL** = controls exist, named gap deferred ·
**DEFERRED** = surface exists, mitigation owned by a named later phase.
