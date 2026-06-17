# Operational runbook: NVNM Chain MCP Server

This document covers production deployment and day-two operations for the Go MCP server that exposes the NVNM Chain (Inveniam L2 on MANTRA, chain ID **787111**) via MCP tools, with HTTP transport, separate health/metrics port, OpenTelemetry traces, and Prometheus metrics.

---

## Env var migration

**Phase 8.9 (2026-05-13, BREAKING):** all chain/RPC config keys moved from the `INVENIAM_*` prefix to `NVNM_*`. There is no compatibility alias. `Config.Load` runs a pre-validation pass at startup; if **any** of the three legacy keys are set in the environment, the server exits immediately with an error pointing back to this section. The strict policy fires even when the matching `NVNM_*` is also set — dual-populated config is the silent-drift trap fail-loud exists to catch.

### Migration table

| Phase 8.9 (current) | Pre-8.9 (rejected) | Notes |
|---------------------|--------------------|-------|
| `NVNM_EVM_RPC_URL` | `INVENIAM_EVM_RPC_URL` | Primary EVM JSON-RPC endpoint. Required. |
| `NVNM_EVM_ARCHIVE_RPC_URL` | `INVENIAM_EVM_ARCHIVE_RPC_URL` | Reserved for archive RPC; optional. |
| `NVNM_CHAIN_ID` | `INVENIAM_CHAIN_ID` | Expected chain ID. Required. |

Already-present `NVNM_*` knobs (`NVNM_CHAIN_ENVIRONMENT`, `NVNM_ALLOWED_ORIGINS`, `NVNM_DOCS_URL`, `NVNM_EXPLORER_URL`, `NVNM_BRIDGE_URL`) are unchanged by the rename — they were always under the `NVNM_*` prefix.

### Steps for an existing deployment

1. **In every ConfigMap, Helm `values.yaml`, `.env`, systemd unit, Compose file, Terraform module, or shell wrapper that sets `INVENIAM_*`:** rename the key to its `NVNM_*` equivalent per the table above. Don't keep the old key alongside the new one — the server treats that as a configuration error and refuses to start.
2. **Diff the change before deploying.** If your secrets manager / values overlay sets either prefix dynamically, search for `INVENIAM_` across all overlay layers.
3. **Restart workloads.** First start under the new binary either succeeds with `NVNM_*` only, or fails loud with a message listing each detected legacy key and a pointer back to this section.

### What the failure looks like

```
config error: legacy INVENIAM_* env vars detected; rename to NVNM_*
per docs/RUNBOOK.md#env-var-migration: found INVENIAM_EVM_RPC_URL,
INVENIAM_CHAIN_ID. Migration table: docs/RUNBOOK.md#env-var-migration
```

No partial startup, no silent fallback. The error names every legacy key it found so a single restart surfaces all of them.

### Server identity changes shipped alongside

Phase 8.9 also renamed three operator-visible identity strings to match the chain rename. None of these are user-configured, but they appear in telemetry, MCP `initialize` responses, and logs:

| Identifier | Pre-8.9 | Phase 8.9 |
|------------|---------|-----------|
| MCP server name (`initialize` response) | `inveniam-evm` | `nvnm-chain` |
| OTel `OTEL_SERVICE_NAME` default | `nvnm-mcp-server` | `nvnm-mcp-server` |
| OTel Tracer / Meter name (internal) | `nvnm-mcp-server` | `nvnm-mcp-server` |
| Helm chart name (`deploy/helm/.../Chart.yaml`) | `nvnm-mcp-server` | `nvnm-mcp-server` |

Dashboards that filter by `service.name`, `tracer`, or `meter` will need their queries updated. Dashboard updates can lag the deploy — the metrics keep flowing, they're just labeled differently — but plan for the cutover in the same change window.

---

## K8s manifest migration (Phase 9.14 follow-up)

**Phase 9.14 follow-up (2026-06-02, BREAKING for existing deployments):** the Kubernetes manifests under `deploy/k8s/` carried four operator-visible identifiers from before the NVNM-Chain rename. They have now been renamed to match the post-9.14 canonical identity. **Existing deployments that adopt the new manifests as a drop-in roll will break** — selectors stop matching, mounts move, and the `part-of` label changes.

### What changed

| Surface | Pre-9.14 follow-up | Now |
|---|---|---|
| Namespace (`namespace.yaml`, `kustomization.yaml`) | `inveniam-mcp` | `nvnm-mcp` |
| API-key Secret mount path (`deployment.yaml`) | `/var/run/secrets/inveniam` | `/var/run/secrets/nvnm` |
| `app.kubernetes.io/part-of` label (every manifest) | `inveniam` | `nvnm-chain` |
| `secret.yaml.example` env var | `INVENIAM_EVM_RPC_URL` | `NVNM_EVM_RPC_URL` (see env-var migration above — this was already a hard failure at startup; the example was just stale) |
| `secret.yaml.example` mount path | `/var/run/secrets/inveniam/keys.json` | `/var/run/secrets/nvnm/keys.json` |
| `networkpolicy.yaml` ingress comment | Phantom `inveniam-keymgmt` workload reference | Removed (use a real `namespaceSelector` + `podSelector` for your ops tooling instead) |

### What this breaks if you roll forward without staging

- A pod from the new Deployment will look for its API-key Secret at `/var/run/secrets/nvnm`. If the Secret is still mounted at the old path (operator-managed Secret manifests not yet updated), the server starts but fails closed on every auth attempt — every request is rejected as if no keys were configured.
- A new Namespace resource cannot rename an existing Namespace; applying `namespace.yaml` with `name: nvnm-mcp` creates a *new* empty namespace next to `inveniam-mcp`. Selectors in Services, NetworkPolicies, and RBAC bindings that hard-code the old namespace name stop matching.
- The `part-of: nvnm-chain` label change affects observability groupings: Grafana dashboards, Prometheus recording rules, or kube-state-metrics queries that filter by `app.kubernetes.io/part-of=inveniam` go quiet.

### Steps for an existing deployment

The safe rollover is *parallel*, not in-place:

1. **Audit operator-managed manifests for the four surfaces above.** Anything in your overlay (Helm values, kustomize patches, External Secrets, ArgoCD app config) that references `inveniam-mcp` / `/var/run/secrets/inveniam` / `part-of: inveniam` / `inveniam-keymgmt` / `INVENIAM_EVM_RPC_URL` needs renaming alongside the in-repo manifests.
2. **Apply the new `nvnm-mcp` namespace alongside the old `inveniam-mcp` namespace.** Don't delete the old one yet.
3. **Apply the renamed manifests to the new namespace.** Verify pod readiness, Secret mount path, ServiceMonitor scrape, and NetworkPolicy posture in the new namespace.
4. **Migrate API-key Secrets** to the new mount path under the new namespace. If you generate keys with `key-mgmt`, regenerate the Secret manifest; if you provision via External Secrets, update the `targetSecretName` / `template.metadata.namespace`.
5. **Cut traffic.** Move Ingress / Service consumers to the new namespace's Service. Validate end-to-end.
6. **Decommission the old namespace.** `kubectl delete namespace inveniam-mcp` after no pods or traffic depend on it.
7. **Update observability.** Re-point dashboards and recording rules that filter by `part-of=inveniam` to `part-of=nvnm-chain`.

### Why a hard rename instead of compatibility

Same rationale as the Phase 8.9 env-var rename. Dual-acceptance (selectors that match both `part-of: inveniam` and `part-of: nvnm-chain`, mounts at both `/var/run/secrets/inveniam` and `/var/run/secrets/nvnm`) compounds — every new operator who picks the old name extends the migration tail, and stale legacy values in deployed manifests stay invisible until something else changes. The migration window is small and one-time; the silent-drift trap from accepting both is permanent.

---

## Verifying signed releases (Cosign)

Every tagged release ships Cosign-keyless signatures for both the
release binaries and the multi-arch container image. Verifying before
deployment confirms the artifact came from the project's GitHub
Actions workflow and was not tampered with on the way to you. The
signatures live in [Sigstore Rekor](https://search.sigstore.dev) and
do not require any pre-shared key.

### Verifying a release binary

Download the binary, certificate, and signature from the GitHub
release page, then verify with `cosign verify-blob`:

```sh
RELEASE=v1.0.0-rc.4
PLATFORM=linux-amd64                     # or linux-arm64, darwin-amd64, darwin-arm64

# Download
BASE=https://github.com/NVNM-Chain/nvnm-mcp-server/releases/download/${RELEASE}
for ext in '' .cert.pem .sig .sbom.cyclonedx.json .sha256; do
  curl -fsSLO "${BASE}/nvnm-mcp-server-${RELEASE}-${PLATFORM}${ext}"
done

# SHA-256 first (catches transport-layer corruption before crypto)
shasum -a 256 -c "nvnm-mcp-server-${RELEASE}-${PLATFORM}.sha256"

# Cosign keyless verify
cosign verify-blob \
  --certificate "nvnm-mcp-server-${RELEASE}-${PLATFORM}.cert.pem" \
  --signature   "nvnm-mcp-server-${RELEASE}-${PLATFORM}.sig" \
  --certificate-identity-regexp 'https://github.com/NVNM-Chain/nvnm-mcp-server/.*' \
  --certificate-oidc-issuer     'https://token.actions.githubusercontent.com' \
  "nvnm-mcp-server-${RELEASE}-${PLATFORM}"
```

The output should end with `Verified OK`. The `--certificate-identity-regexp`
and `--certificate-oidc-issuer` together prove the binary was signed
by *this project's* GitHub Actions workflow — substitute the URL prefix
if you fork.

### Verifying the container image

The image is signed at push time by the `Image` workflow. Verify the
manifest digest:

```sh
cosign verify \
  --certificate-identity-regexp 'https://github.com/NVNM-Chain/nvnm-mcp-server/.*' \
  --certificate-oidc-issuer     'https://token.actions.githubusercontent.com' \
  ghcr.io/nvnm-chain/nvnm-mcp-server:v1.0.0-rc.4 | jq .
```

The signature payload pins the manifest digest, so an attacker who
swaps the image after the verify step still produces a different
digest on next pull. Pair this verification with a digest-pinned
deployment for the strongest posture:

```yaml
# In deploy/k8s/deployment.yaml (and equivalent Helm override):
containers:
  - name: nvnm-mcp-server
    image: ghcr.io/nvnm-chain/nvnm-mcp-server@sha256:<digest-from-verify-output>
```

### Cosign admission policy (cluster-side)

The verification commands above run on the operator's workstation
before deployment. For continuous enforcement, the Phase 10
deployment story includes a Cosign admission policy (Sigstore Policy
Controller or Kyverno equivalent) that rejects unsigned images at
admission time. That layer is operator-managed and lives outside
this chart; see Phase 10 design § Cosign admission for the policy
shape.

---

## 1. Deployment checklist

### Required environment variables

| Variable | Purpose |
|----------|---------|
| `NVNM_EVM_RPC_URL` | Primary EVM JSON-RPC URL (`http://` or `https://` only). May include query parameters for provider API keys; treat as secret if it does. |
| `NVNM_CHAIN_ID` | Expected chain ID; must be a positive integer (e.g. `787111`). Startup fails validation if missing or invalid. |

Default RPC for the testnet network: `https://evm.testnet.nvnmchain.io`.

### Strongly recommended for production

| Variable | Purpose |
|----------|---------|
| `ANCHOR_ABI_PATH` | Filesystem path to the anchor precompile ABI JSON. Without it, anchor tools are registered but return errors at call time; `anchor_info` reports ABI not loaded. |

Set to `/app/abi/anchoring.json` when that file is baked into the image (see below).

### Optional environment variables (with defaults)

| Variable | Default | Purpose |
|----------|---------|---------|
| `NVNM_EVM_ARCHIVE_RPC_URL` | _(empty)_ | Reserved for archive RPC when historical-query routing is implemented; not consumed by the current binary for routing. |
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
| `OTEL_SERVICE_NAME` | `nvnm-mcp-server` | Service name in OTel resource. |

### Additional NVNM-prefixed environment variables

The following `NVNM_*` knobs are additive to the required chain config above. They surface in onboarding-tool responses or gate the HTTP transport's Origin-header allowlist.

| Variable | Default | Purpose |
|----------|---------|---------|
| `NVNM_CHAIN_ENVIRONMENT` | inferred from `NVNM_CHAIN_ID` | `testnet` or `mainnet`. Selects env-aware token naming (`mantraUSD`/`wmantraUSD` vs `mmUSD`/`wmmUSD`) for onboarding-tool responses. Inference falls back to `testnet` for chain IDs the server does not recognize (787111 → testnet; 1611 → mainnet). |
| `NVNM_DOCS_URL` | _(empty)_ | Operator-facing docs URL surfaced in onboarding-tool responses (e.g., the wizard's "where to learn more" hint). Optional. |
| `NVNM_EXPLORER_URL` | _(empty)_ | Block-explorer URL surfaced to agents in onboarding-tool responses. Optional. |
| `NVNM_BRIDGE_URL` | _(empty)_ | Bridge/funding-flow URL surfaced to the wizard's `unfunded` state. Optional. |
| `NVNM_WALLET_GENERATOR_URL` | `https://wallet.nvnmchain.io` | Browser-hosted wallet generator page surfaced to the wizard's `needs_wallet` state. Default points at the canonical Inveniam-hosted instance (`NVNM-Chain/nvnm-wallet-page`); operators self-hosting the wallet page can override. |
| `NVNM_KEY_REQUEST_ENABLED` | `false` | Opt-in flag for the public self-serve API-key request endpoint (`POST /api/v1/keys/request`). When `true`, `NVNM_KEY_PENDING_FILE` is required. Phase 11 L3 / RD3. |
| `NVNM_KEY_PENDING_FILE` | _(empty)_ | Path to the on-disk JSON store of pending key requests. Required when `NVNM_KEY_REQUEST_ENABLED=true`. Operator-controlled persistence; mount on a durable filesystem. |
| `NVNM_KEY_REQUEST_RATE_LIMIT` | `0.5` | Per-source-IP token-bucket rate (requests/sec) on the public key-request endpoint. Default deliberately tight — the endpoint produces durable side effects (a queue row + a reviewer ping) and is not a hot path. |
| `NVNM_KEY_REQUEST_RATE_BURST` | `3` | Per-source-IP burst capacity. |
| `NVNM_KEY_REQUEST_MAX_BODY_BYTES` | `16384` | JSON body cap for the public key-request endpoint (tighter than the global `MaxRequestBodyBytes` outer cap). |
| `NVNM_SMTP_HOST` | _(empty)_ | SMTP relay hostname used by the admin approve/reject flow to email customers. Empty -> approvals fall back to a log-only sender (key material lands in structured logs for the operator to copy out). Phase 11 RD2. |
| `NVNM_SMTP_PORT` | _(empty)_ | SMTP port. **Required** when `NVNM_SMTP_HOST` is set; startup fails loud otherwise. |
| `NVNM_SMTP_USERNAME` | _(empty)_ | SMTP PlainAuth username. Optional; when both username and password are empty, no AUTH is attempted (useful for in-network relays). |
| `NVNM_SMTP_PASSWORD` | _(empty)_ | SMTP PlainAuth password. |
| `NVNM_SMTP_FROM` | _(empty)_ | From address on approval / rejection emails. **Required** when `NVNM_SMTP_HOST` is set. |
| `NVNM_SMTP_FROM_NAME` | _(empty)_ | Optional display name. When set, the From header is formatted as `Name <addr>`. |
| `NVNM_ALLOWED_ORIGINS` | _(empty)_ → localhost-only default | Comma-separated allowlist for the HTTP transport's Origin header (DNS-rebinding defense per the MCP spec). When unset the server permits only the loopback variants (`http://localhost`, `https://localhost`, `http://127.0.0.1`, `https://127.0.0.1`, `http://[::1]`, `https://[::1]`) at any port. Production deployments must enumerate the trusted client origins. |

### Origin-header validation (HTTP transport, Phase 8.5)

The HTTP transport rejects requests whose `Origin` header is not on the allowlist. Requests with no `Origin` header (server-to-server, CLI, curl) pass through unchanged. The check is the outermost middleware so rejection short-circuits before auth or rate-limit work runs.

**Defaults (no `NVNM_ALLOWED_ORIGINS` set):** loopback HTTP and HTTPS variants of `localhost`, `127.0.0.1`, and `[::1]`, on any port. Suitable for local development; everything else gets `403`.

**Production override example:**

```bash
NVNM_ALLOWED_ORIGINS="https://claude.ai,https://mcp.nvnmchain.io"
```

Multiple origins, comma-separated, whitespace tolerated. Matching is case-insensitive and ignores surrounding whitespace. Port-stripping is only applied to loopback hosts -- general allowlist entries require exact-match including port.

### Self-serve key requests (Phase 11 L3)

When `NVNM_KEY_REQUEST_ENABLED=true` and `NVNM_KEY_PENDING_FILE` is set, the server exposes a public endpoint that lets callers request a key without prior credentials. Requests land in a pending-review queue that an admin works through via the existing admin REST API.

#### Public submission endpoint

```text
POST /api/v1/keys/request
Content-Type: application/json

{ "email": "user@example.com",
  "company": "Acme",                                    // optional
  "intended_use": "Building an agent for X" }
```

Response shape (RD3, frozen so the contract can absorb a future transition to auto-approval without breaking clients):

```json
{ "request_id": "<uuid>", "status": "pending" }
```

Validation rejections return 400 with `{"error": "..."}`. The endpoint sits outside `AuthMiddleware` (no Bearer required) but inside `originGuard`, `limitRequestBody`, `IPFailRateLimiter`, and the endpoint's own per-source-IP rate limiter (defaults `0.5 rps`, burst `3`).

#### Admin review queue

All three endpoints below run on the admin REST server (separate port — `ADMIN_API_ADDR`, default `127.0.0.1:8081`) behind `ADMIN_API_KEY` Bearer auth.

**List pending + decided history:**

```sh
curl -H "Authorization: Bearer $ADMIN_API_KEY" \
  http://localhost:8081/admin/keys/pending
```

Returns every request the store knows about, all statuses included — the reviewer's audit view, not just the current queue. Items are JSON objects with `id`, `email`, `company`, `intended_use`, `status`, `created_at`, and (for decided requests) `decided_at`, `decider_id`, `key_id`.

**Approve and issue a key:**

```sh
curl -X POST -H "Authorization: Bearer $ADMIN_API_KEY" \
  http://localhost:8081/admin/keys/pending/<request_id>/approve
```

This mints a credential with the `reader` role only (consistent with the project's default-deny posture; promote post-issuance via `PATCH /admin/keys/{id}` if a customer needs write access), persists the decision under the double-approve guard, then attempts SMTP delivery. The 200 response includes:

```json
{ "request_id": "...", "status": "approved",
  "key_id": "pending:<request_id>",
  "api_key": "<freshly-minted raw key>",
  "email_delivered": true }
```

`api_key` is included so reviewers using the API directly (no SMTP) can deliver the key to the customer manually. The key is also returned in plaintext in the response *exactly once*; the on-disk store keeps only the SHA-256 hash (Phase 8.6 invariant unchanged). If SMTP delivery fails the rest of the operation still commits and `email_delivered: false` is returned — the reviewer knows to deliver out-of-band.

**Reject:**

```sh
curl -X POST -H "Authorization: Bearer $ADMIN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"reason": "applicant not eligible for closed beta"}' \
  http://localhost:8081/admin/keys/pending/<request_id>/reject
```

`reason` is optional and surfaces in the rejection email when SMTP is configured. The body is optional; an empty POST is accepted.

#### SMTP modes

| Mode | Trigger | What happens on approve |
|---|---|---|
| SMTP relay | `NVNM_SMTP_HOST` set + `NVNM_SMTP_PORT` + `NVNM_SMTP_FROM` | Email delivered via `net/smtp`; `email_delivered: true` on success |
| Log-only fallback | `NVNM_SMTP_HOST` unset | Email body (including the raw key) written to structured logs at INFO with `msg=email (log-only, no SMTP configured)`; `email_delivered: true` in the response (the structured-log pipeline is the delivery) |

The log-only mode is intended for OSS evaluators, dev / test deployments, and any operator who hasn't wired SMTP yet. Operators using this path are accepting that their log-shipping pipeline is the de-facto secret store for the duration the key sits there. For production-grade deployments, configure SMTP.

#### Double-approve and double-reject guards

The store guarantees:

- A request that has already been approved or rejected returns `409 Conflict` on a second decide. Two admins clicking *approve* near-simultaneously cannot both trigger key issuance + email.
- Persistence-failure rollback is best-effort: if the underlying `Decide` succeeds but the SMTP send fails, the decision still commits and `email_delivered: false` is reported (the reviewer is the safety net). If the decision itself fails after a key has been minted, the freshly-issued key is best-effort deleted; the original Decide error surfaces to the reviewer.

#### Review cadence

The pending store is the source of truth, not a queue with built-in SLAs. Reviewers should set their own cadence (RD3 specifies a 2–4 week closed beta). Suggested operations:

- Review the queue daily during the closed-beta period.
- Use `GET /admin/keys/pending` filtered client-side by `status: "pending"` to find new submissions.
- Approval emails contain the freshly-minted key in plain text; the customer is responsible for storing it securely.
- The `mcp_key_requests_total{status="pending|approved|rejected"}` metric (bounded cardinality) is the operator dashboard for queue health.

#### Spam and flood threat model

The endpoint produces durable side effects (a queue row + an SMTP send on approve). Mitigations:

- Per-source-IP rate limit (deliberately tight: `0.5 rps`, burst `3` by default).
- Body-size cap (`16 KiB` default, tighter than the global 10 MB outer cap).
- Email validation via `net/mail.ParseAddress` rejects malformed addresses, and reviewer judgement during approve catches obviously bogus requests before any email is sent to a real address.
- `IPFailRateLimiter` (shared with auth-failure tracking) provides a coarser outer ring.

For high-spam threat environments, tighten `NVNM_KEY_REQUEST_RATE_LIMIT` further or front the endpoint with an edge CAPTCHA — the design intentionally does not bake CAPTCHA into the server (per `planning/PHASE_9_DESIGN.md` D3).

Rejected requests produce a structured warning log line with the origin, remote address, method, and path. Operators can audit recent rejections with their log aggregator's filter on `"rejecting request with disallowed Origin"`.

### CORS (cross-origin browser access, Phase 9.5)

CORS and the Origin guard above use the **same** `NVNM_ALLOWED_ORIGINS` allowlist but answer different questions, and **both** run:

- **Origin guard** is a server-side anti-spoof / DNS-rebinding defense — it *rejects* requests from a disallowed `Origin` with `403`.
- **CORS** is the browser-facing permission grant — it tells a compliant browser it is *allowed* to read the response. It is needed only by browser-hosted MCP clients (e.g. an agent running in a web page); server-to-server callers (no `Origin` header) are unaffected.

CORS sits **outermost** in the middleware chain so it can answer `OPTIONS` preflight before the Origin guard or any parser runs. Behavior:

- **Preflight (`OPTIONS` with `Access-Control-Request-Method`):** an allowed origin gets `204` with `Access-Control-Allow-Origin: <origin>`, `Access-Control-Allow-Methods: GET, POST, OPTIONS`, `Access-Control-Allow-Headers: Authorization, Content-Type, Mcp-Session-Id`, and `Access-Control-Max-Age: 600`. A disallowed origin gets `403` with no allow headers.
- **Actual requests:** an allowed origin gains `Access-Control-Allow-Origin: <origin>` and `Access-Control-Expose-Headers: Mcp-Session-Id` (so the browser can read the session id the server issues on `initialize` and echo it back on later requests).
- **Credentials:** `Access-Control-Allow-Credentials: false` — the server uses no cookies; browser clients authenticate writes with a bearer token in the `Authorization` header, not credentialed CORS.
- **`Vary: Origin`** is set whenever the origin is echoed, so a shared cache never serves one origin's permission headers to another.

No configuration beyond `NVNM_ALLOWED_ORIGINS` is required; the same production override shown above enables CORS for those origins. CORS rejections are not separately metered (same cardinality concern as the Origin guard).

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
make key-create NAME=pipeline ROLES=writer           # Create key with the writer role
make key-list                                        # List all keys (ID, enabled, roles, created)
make key-disable NAME=my-agent                       # Disable a key
make key-enable NAME=my-agent                        # Re-enable a key
```

### Admin Key Management API

| Variable | Default | Purpose |
|----------|---------|---------|
| `ADMIN_API_KEY` | _(empty)_ | Admin bearer token for the key management REST API. The admin server only starts when this is set AND transport is `http`. |
| `ADMIN_API_ADDR` | `:8081` | Listen address for the admin API. |

When `ADMIN_API_KEY` is set, a separate HTTP server starts on `ADMIN_API_ADDR` with REST endpoints for runtime key management. Changes take effect immediately -- no server restart needed.

**Endpoints:**

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/admin/keys` | Create a new client key (returns raw key once). Body: `{"client_id": "name", "roles": ["reader","writer"]}` |
| `GET` | `/admin/keys` | List all keys (redacted, no raw keys). |
| `PATCH` | `/admin/keys/{id}` | Update enabled/roles. Body: `{"enabled": false}` or `{"roles": ["reader","writer"]}` |
| `DELETE` | `/admin/keys/{id}` | Permanently remove a key. |

All requests require `Authorization: Bearer <ADMIN_API_KEY>`.

**Example: create a key via curl:**

```bash
curl -X POST http://localhost:8081/admin/keys \
  -H "Authorization: Bearer $ADMIN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"client_id": "new-agent", "roles": ["reader"]}'
```

**Security:** The admin port should be restricted via firewall or Kubernetes NetworkPolicy to ops tooling only. The admin token is separate from client keys.

### Write-approval removal

Server-side write approval was **removed in Option 0** (the stateless
multi-replica migration; see [`SESSION_AFFINITY.md`](SESSION_AFFINITY.md)).
The server previously gated `evm_send_raw_transaction` behind an MCP
elicitation prompt; that was the only server→client request and the sole
reason the handler needed sticky sessions. It is gone. Writes now gate on
**RBAC role** (`writer`/`admin`/`automation`) plus the `ENABLE_WRITE_TOOLS`
flag only. Obtaining human confirmation before submitting a signed
transaction is now the **client/agent's responsibility** (stated in the
server's `initialize` instructions); the signature, produced caller-side,
remains the actual security boundary.

**Migration — the server fails loud on the retired knobs:**

- `WRITE_APPROVAL_DEFAULT` set in the environment → startup aborts with
  `ErrLegacyWriteApproval`. **Remove the variable** from your env / ConfigMap
  / Helm values.
- A `write_approval` field on **any** entry in the API-key store
  (`MCP_API_KEYS_FILE`) → startup aborts with `ErrLegacyKeyWriteApproval`,
  naming the offending key IDs. **Remove the `write_approval` field** from
  every key entry in the JSON file. Admin-created key stores from before
  Option 0 carry this field; strip it once.

Both are deliberate hard cuts (no silent fallback), consistent with the
`INVENIAM_*` env migration above. The admin REST API and the `key-mgmt` CLI
no longer accept or display a write-approval policy.

### Secrets management

- Store `NVNM_EVM_RPC_URL` in Kubernetes Secrets, AWS Secrets Manager/SSM, or equivalent when it contains API keys or signed tokens.
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
| **8081** | Admin key management API (`ADMIN_API_ADDR`). Only active when `ADMIN_API_KEY` is set. |
| **9090** | Health and metrics (`METRICS_ADDR`): `GET /healthz`, `GET /readyz`, `GET /metrics`. |

Container image exposes 8080 and 9090. Map both in Kubernetes Services, ECS task definitions, and load balancers as required. The admin port (8081) should be exposed only to internal ops tooling -- restrict via NetworkPolicy or firewall rules.

### Transport and process args

Production typically runs:

```bash
/nvnm-mcp-server --transport http
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

Metrics are registered on the OpenTelemetry `Meter` named `nvnm-mcp-server` and exported through the OTel Prometheus exporter. Exact Prometheus series names and histogram layout follow the exporter and `prometheus/otlptranslator` naming rules (including `otel_scope_*` labels on exported metrics). After deployment, confirm live names with:

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

Prometheus alert rules ship with the repo at **`deploy/prometheus/alerts.yaml`** (`PrometheusRule` for the Prometheus Operator, with `runbook_url` annotations pointing to the sections below). Verify exported metric names on `/metrics` before tuning thresholds; OTel-exported names follow the `prometheus/otlptranslator` rules and may include `otel_scope_*` labels.

### `NvnmMCPHighErrorRate`

- **Likely cause:** Upstream RPC errors, timeouts, or tool-level failures.
- **Actions:** Inspect `mcp.server.tool.errors` and `evm.rpc.errors` by label; search logs for `"status":"error"` and `level` `ERROR`; verify `NVNM_EVM_RPC_URL` reachability and provider status.

### `NvnmMCPCriticalErrorRate`

- **Actions:** Same as high error rate, with higher urgency; check for sustained RPC outage, TLS/DNS issues, and recent config changes. Scale or roll back if a bad release is suspected.

### `NvnmMCPHighP99Latency`

- **Actions:** Compare `mcp.server.tool.duration` and `evm.rpc.duration` high quantiles; check network path to RPC; review `REQUEST_TIMEOUT`; consider horizontal scale if CPU saturation correlates.

### `NvnmMCPHealthCheckFailing`

- **Actions:** Confirm pod/task liveness (`/healthz`) vs readiness (`/readyz`). For 503 on `/readyz`, treat as RPC probe failure first. Confirm ABI path and file only if anchor tools misbehave or `checks.abi` is `not_configured` and that is unacceptable for the environment.

### `NvnmMCPCircuitBreakerOpen`

- **Actions:** The circuit breaker (`sony/gobreaker`) is implemented in `internal/evm/resilient.go`. When triggered, all RPC calls fail fast with `ErrCircuitOpen`. State transitions are logged at WARN level. Check upstream RPC provider health. The breaker auto-recovers after `CIRCUIT_BREAKER_TIMEOUT` (default 30s) via a half-open probe.

### `NvnmMCPHighRetryRate`

- **Actions:** Retries are implemented with exponential backoff and jitter on idempotent read RPCs. High retry rates indicate upstream instability. Check `evm.rpc.errors` by method; verify RPC provider status. Consider increasing `RPC_INITIAL_BACKOFF` or reducing `RPC_MAX_RETRIES` if retries are amplifying load.

### `NvnmMCPRateLimiting`

- **Actions:** The in-process token-bucket rate limiter (`golang.org/x/time/rate`) caps upstream RPC calls at `RPC_RATE_LIMIT` req/s with `RPC_RATE_BURST` burst. If clients are being throttled, increase the rate limit, add replicas with fair routing, or negotiate higher quotas with the RPC provider.

### `NvnmMCPClientRateLimit429`

- **Actions:** The MCP layer enforces a per-client token-bucket via `MCP_RATE_LIMIT` (req/s, default 60) and `MCP_RATE_BURST` (default 10). When exceeded, the server returns HTTP `429 Too Many Requests` keyed by the authenticated client ID. Investigate by client ID in structured logs (`client_id` attribute). Mitigations: identify the noisy client; raise the per-client limit if legitimate; rotate or disable the client key if abusive.

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
| Per-client MCP rate limit | `MCP_RATE_LIMIT` | `60` | Token-bucket cap on MCP requests per second per authenticated client. Returns HTTP `429 Too Many Requests` when exceeded. |
| | `MCP_RATE_BURST` | `10` | Burst capacity per client. |
| Keyless reads (HTTP only) | `MCP_KEYLESS_READS` | `false` | When `true`, read tools may be called without an `Authorization` header; write tools still require a valid Bearer token (enforced by an MCP receiving middleware that gates on a fail-closed registry — only `evm_send_raw_transaction` is auth-gated today). HTTP-only; under stdio transport the flag is ignored and a startup warning is logged. Anonymous calls leave `client_id` absent from logs and spans (not empty-string). See [`internal/mcp/authpolicy.go`](../internal/mcp/authpolicy.go) for the gated-tool registry. **Inveniam-hosted production invariant: this MUST be `"true"`.** The published NVNM MCP, LLC privacy policy represents the hosted Service as keyless-read ("we generate no per-customer record of read activity"); running `false` in the hosted deployment would make that legal representation false. The Helm `values.yaml` and k8s `configmap.yaml` ship `"false"` (safe self-hoster default) with this invariant noted inline — the hosted overlay must override to `"true"`. Self-hosters operate their own privacy posture and may choose either value. |
| Per-IP anon read rate limit | `MCP_ANON_RATE_LIMIT` | `5` | Token-bucket rate (req/s) for **anonymous** reads, keyed by source IP. Authenticated traffic bypasses this limiter. **Operator-enforced invariant:** must be tighter than `MCP_RATE_LIMIT`; not validated at startup. |
| | `MCP_ANON_RATE_BURST` | `5` | Burst capacity per source IP for anonymous reads. |
| Upstream RPC retry | `RPC_MAX_RETRIES` | `3` | Maximum retry attempts for transient RPC errors |
| | `RPC_INITIAL_BACKOFF` | `500ms` | Initial wait between retries |
| | `RPC_MAX_BACKOFF` | `10s` | Maximum wait between retries |
| Upstream RPC rate limit | `RPC_RATE_LIMIT` | `100` | Upstream RPC rate limit (requests per second) |
| | `RPC_RATE_BURST` | `20` | Burst capacity for token-bucket rate limiter |
| Upstream RPC circuit breaker | `CIRCUIT_BREAKER_THRESHOLD` | `5` | Consecutive failures to trip the breaker |
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

Replace log group with yours (e.g. `/ecs/nvnm-mcp-server`):

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
{job="nvnm-mcp-server"} | json | status = "error"
```

```logql
{job="nvnm-mcp-server"} | json | tool = `anchor_get_registries`
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

### Phase 10 RD1 capacity targets

The Phase 10 OQ walkthrough resolved per-environment capacity targets
(`docs/planning/PHASE_10_DESIGN.md` § 14 RD1). They are *aspirational ceilings
for capacity planning*, not contractual SLOs (the Service is provided
on a "reasonable efforts" basis per `docs/TERMS.md` § 10).

| Environment | Sustained RPS target | Burst RPS target | HPA posture |
|---|---|---|---|
| Testnet (`nvnm-testnet-1`, EVM `787111`) | ~5 RPS | ~25 RPS | Aggressive (low CPU target, fast scale-up) |
| Mainnet (`nvnm-1`, EVM `1611`) | ~50 RPS | ~200 RPS | Aggressive (same posture, more headroom) |

Per RD1, **node sizing is explicitly out of scope** for this design —
the operator's Kubernetes platform handles node-level capacity; this
chart only describes pod-level requests/limits and HPA bounds.

#### Recommended HPA configuration

The shipped Helm defaults in `deploy/helm/nvnm-mcp-server/values.yaml`
err conservative (`hpa.enabled: false`, `targetCPUUtilization: 70`,
`minReplicas: 2`, `maxReplicas: 10`) so that adopting the chart does
not silently change billing. For the RD1 production posture, override:

```yaml
# values-production.yaml fragment
hpa:
  enabled: true
  targetCPUUtilization: 50      # aggressive: scale up before saturation
  minReplicas: 3                # absorbs one node-loss without latency
  maxReplicas: 20               # mainnet ceiling; tune for your quota
```

The 50% CPU target is the "aggressive" posture RD1 calls for — at 50%
average utilisation the cluster has roughly 2x burst headroom per
replica before HPA needs to act, which beats the 200-RPS mainnet
burst target against 50-RPS sustained without needing tail-latency
sacrifice.

The shipped per-pod resource requests (`100m` CPU, `128Mi` memory)
and limits (`500m` CPU, `256Mi` memory) are adequate at the testnet
target out of the box. For mainnet, plan to validate under load —
the working set is dominated by JSON-RPC sockets to the EVM endpoint
plus modest per-request decoding, so memory typically stays well
under the limit but CPU can spike during signature verification on
hot tools.

#### Load-test methodology

To validate the capacity numbers above against your environment:

1. Deploy to a staging cluster with the production HPA profile.
2. Run [`tests/load/`](../tests/load/) (k6 scripts) at the target
   sustained RPS for 30 minutes; observe `mcp_server_active_requests`,
   `mcp_http_responses_total{class="server_fault"}` (Phase 10 RD3 SLI),
   and p99 tool-call duration.
3. Ramp to the burst RPS for 5 minutes; HPA should add replicas
   within the metrics-server lag (default 15s).
4. Pass criteria: server-fault SLI stays under the
   `NvnmMCPServerFaultRate` warning threshold (1%); p99 stays under
   the `NvnmMCPHighP99Latency` threshold (5000ms).

Failing pass criteria do not necessarily mean the targets are wrong —
upstream RPC latency or per-tool work distribution can dominate; the
load test surfaces the bottleneck.

### Trace sampling

The server supports configurable trace sampling via `OTEL_TRACE_SAMPLE_RATIO` (default `1.0`, meaning sample all traces). Uses `ParentBased(TraceIDRatioBased)` to respect upstream sampling decisions while controlling cost for high-volume deployments.

Set `OTEL_TRACE_SAMPLE_RATIO=0.1` to sample 10% of root traces, or use collector-side tail sampling for more advanced policies.

---

## 9. Upgrading across the Phase 8.6 API-key migration

The 2026-05-13 release migrates the API-keys file from raw bearer
tokens to sha256-hashed-at-rest entries. The migration is automatic
on first restart of the new binary against an existing legacy file
and is **irreversible** in the sense that the new binary no longer
reads the raw `key` field as authoritative.

### Before the upgrade

1. **Back up `MCP_API_KEYS_FILE` out-of-band.** The server writes
   a one-shot `<path>.pre-migration` backup automatically before
   any mutation, but an independent copy stored in your operator
   secrets system (Vault, External Secrets, S3, etc.) is cheap
   insurance against a filesystem failure during the upgrade.
2. Confirm `MCP_API_KEYS_FILE` is mode `0o600` and owned by the
   server's run user.
3. Note: the rolling-back path is to redeploy the **previous**
   binary against the `<path>.pre-migration` file (renamed back
   to the primary). The new binary writes the primary file in
   hashed form, which the previous binary cannot read.

### What the server does on first restart

| Step | Behavior |
|---|---|
| 1 | `LoadKeysFile` reads the existing file; entries arrive with raw `key` populated and no `key_hash`. |
| 2 | `migrateLegacyEntries` walks the slice in place: compute `key_hash` (sha256 hex) and `key_prefix` (first 8 chars of the raw key), clear `key`. |
| 3 | **One-shot backup**: if `<path>.pre-migration` does not already exist, write a verbatim copy of the original file with mode `0o600`. The backup is written **before** any mutation of the primary file. If a backup from a prior migration already exists, leave it untouched -- the first backup is the truest "what did we have before hashing ever ran" record. |
| 4 | `SaveKeysFile` rewrites the primary file in hashed form via atomic `tmp + fsync + rename` while holding `flock(LOCK_EX \| LOCK_NB)` on the existing file. |
| 5 | The server logs `legacy keys file migrated to hashed format` at INFO with the path, backup path, and entry count, then continues startup. |

### Log signals to expect

| Severity | Message | Meaning |
|---|---|---|
| INFO | `legacy keys file migrated to hashed format` | Migration succeeded. The primary file is now in hashed shape; the backup exists at `<path>.pre-migration`. |
| WARN | `legacy keys migrated in memory but failed to persist; next admin CRUD will re-save` | In-memory entries are normalized so auth works; the primary file is still in legacy shape. Most likely cause: a transient filesystem issue (read-only mount, permission, quota). Fix and restart, or let the next admin Create / Update / Delete re-persist. |
| WARN | `legacy keys file detected but pre-migration backup write failed; skipping disk re-save to preserve the original file` | Backup write failed. The primary file is **deliberately not mutated** to preserve recoverability; in-memory state is normalized so auth works. Fix the underlying issue (likely a write-permission or quota problem on the directory containing the keys file) and restart. |

### After the upgrade

- `<path>.pre-migration` is the immutable historical record. The
  server will never overwrite it on subsequent migrations. You
  should treat it with the same secret-handling care as the
  active keys file (`0o600`, off-host backups, etc.) and consider
  deleting it after you have verified the new file is healthy.
- The keys file is now safe to share among operators reading via
  the admin REST API (`GET /admin/keys`), since the response
  carries `key_prefix` for visual recognition but never the raw
  key or the hash.
- Existing API keys continue to work unchanged for callers --
  raw bearer tokens are still what clients present, and the
  server hashes them at the validator boundary.

### Multi-process safety during the upgrade

`SaveKeysFile` takes an advisory `flock(LOCK_EX | LOCK_NB)` on the
existing file. If the lock is already held (e.g. another process
is mid-migration), the call returns immediately with an error
rather than blocking indefinitely. Coordinate the upgrade so that
only one process touches `MCP_API_KEYS_FILE` at a time:

- For Kubernetes: a rolling deploy is safe because each pod
  reads the file independently and any concurrent writer (e.g.
  the `key-mgmt` CLI on a sidecar) honors the same lock.
- For bare-metal: do not run `key-mgmt create/update/delete`
  during the first restart of the new binary.

### Rollback

The new binary cannot read the migrated file via the legacy path
(raw `key` is empty post-migration). To roll back:

1. Stop the new binary.
2. `mv "$KEYS_FILE.pre-migration" "$KEYS_FILE"` to restore the
   original.
3. Redeploy the previous binary.

This loses any keys created **after** the migration on the new
binary; coordinate with whoever has been issuing keys to confirm
no admin CRUD happened post-migration before rolling back.

---

## 10. Disaster recovery

### Stateless service

The MCP server holds **no durable application state**. Recovery is redeploy and reconnect to the same (or failover) RPC endpoint.

### Redeployment

1. Apply updated image or manifest (Kubernetes rolling rollout, ECS new deployment).
2. Confirm **`/healthz`** then **`/readyz`** on a pod/task.
3. Smoke-test MCP over HTTP (e.g. `initialize` / `tools/list`) against port **8080**.

### RPC endpoint failover

- If you operate a secondary RPC URL, update `NVNM_EVM_RPC_URL` (and restart workloads). Archive URL env exists for future use; confirm code support before relying on split routing.
- Verify chain ID still matches `NVNM_CHAIN_ID`.

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
- Write path (RBAC-gated; no server-side approval gate since Option 0): `internal/mcp/tools_evm_write.go`
- Key management CLI: `cmd/key-mgmt/main.go`
- Admin key management API: `internal/mcp/admin.go`, `internal/mcp/managed_keys.go`
- Health server: `internal/telemetry/health.go`
- Metrics instruments: `internal/telemetry/metrics.go`, `internal/telemetry/middleware.go`, `internal/evm/tracing.go`
- Resilience: `internal/evm/resilient.go`
- Kubernetes samples: `deploy/k8s/` (including `networkpolicy.yaml`)
- Design / roadmap: `docs/DESIGN.md`, `docs/IMPLEMENTATION_PLAN.md`
- Security: `docs/SECURITY_AUDIT.md`
