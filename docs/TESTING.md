# End-to-End Testing — Local Environment Setup

This document covers **manual end-to-end testing**: standing a real server up
on your machine and driving it the way a real MCP client will. It is the
companion to [`TESTING_UNIT.md`](TESTING_UNIT.md), which owns the automated
suite (unit, golden, integration, in-process MCP E2E, k6, Docker smoke) and
the CI coverage gate.

Use this document when you need to answer "does a Claude-class client actually
connect and call tools against my build?" — a question the automated suite
deliberately does not answer, because client behavior is black-box (see
`SECURITY_AUDIT.md` § *Update 2026-06-11*).

## Three loops

E2E here means three progressively wider loops. Each is independently useful;
run the narrowest one that covers what you changed.

| Loop | What it exercises | Needs |
|---|---|---|
| 1. Loopback | Real binary, real HTTP transport, real middleware chain, real chain RPC — driven by `curl` / `make mcp-probe` | Nothing beyond the repo |
| 2. Local client | Loop 1 + a real MCP client (Claude Code, Claude Desktop) over stdio or HTTP to `localhost` | A Claude client |
| 3. Remote connector | Loop 2 + public HTTPS ingress, Origin allowlist, bearer auth as a hosted deployment sees it | A tunnel (`cloudflared` / `ngrok`) |

Loops 1 and 2 are the everyday path. Loop 3 is for changes to auth, the
Origin/CORS guards, the well-known handling, or anything that only manifests
when the client is not on your machine.

---

## 1. Prerequisites

| Loop | Requirement |
|---|---|
| All | Go 1.26+, network access to the configured EVM RPC (`https://evm.testnet.nvnmchain.io` by default) |
| All | `curl`; `jq` optional (`make mcp-probe` pretty-prints with it when present) |
| 2 | Claude Code CLI (`claude`) and/or Claude Desktop |
| 3 | A tunnel that terminates TLS — `cloudflared` or `ngrok` |
| Optional | Docker + Compose v2 (§ 3b stack, container smoke), `k6` (load), Postgres 16 (audit/key-store surfaces) |

---

## 2. Configure the environment

```sh
cp .env.example .env
```

Fill in at minimum:

| Variable | Local value |
|---|---|
| `NVNM_EVM_RPC_URL` | `https://evm.testnet.nvnmchain.io` |
| `NVNM_CHAIN_ID` | `787111` |
| `NVNM_CHAIN_ENVIRONMENT` | `testnet` |
| `ANCHOR_ABI_PATH` | `abi/anchoring.json` (absolute path when launched by a client — see § 5) |
| `MCP_TRANSPORT` | `http` for loops 1 and 3; `stdio` for the stdio client path |
| `MCP_HTTP_ADDR` | `:8180` (the `.env.example` / Makefile default) |
| `METRICS_ADDR` | `:9190` |

The `NVNM_*` prefix is the only chain-config family the server reads. Legacy
`INVENIAM_*` variables are **hard-rejected at startup** — see
[`RUNBOOK.md`](RUNBOOK.md) `#env-var-migration`. `.env` is gitignored; keep it
that way.

### Authentication is mandatory on HTTP

The HTTP transport **fails closed at boot** with `ErrHTTPAuthRequired` when
neither `MCP_API_KEYS_FILE` (holding at least one *enabled* key) nor
`MCP_API_KEY` is configured. This holds even with `MCP_KEYLESS_READS=true`:
keyless reads relax the *request* path, not the boot-time requirement that a
validator exists.

Pick one:

**Multi-key store (matches production):**

```sh
make key-create NAME=local-dev ROLES=reader,writer
```

The raw key is printed **once** — copy it now. It is stored hashed in
`.mcp-keys.json` (gitignored, mode `0600`). `make key-list` shows status,
`make key-disable NAME=local-dev` revokes it.

**Single throwaway key (fastest):**

```sh
export MCP_API_KEYS_FILE=""            # the file path wins when both are set
export MCP_API_KEY=local-e2e-throwaway-key
export MCP_API_KEY_ROLES=reader,writer # REQUIRED; boot fails with ErrMissingAPIKeyRoles without it
```

Authorization is default-deny: a key authorizes only what its roles permit, and
a key with no roles authorizes nothing.

### stdio needs no auth

Under `--transport stdio` the transport itself is the trust boundary (a local
subprocess the client owns), so no validator is required and none of the HTTP
middleware runs.

---

## 3. Start the server

```sh
make run-local     # HTTP transport; sources .env; ports from MCP_HTTP_ADDR / METRICS_ADDR
make run           # stdio transport; reads config from the exported environment (does NOT source .env)
```

Verify liveness and readiness (the metrics port, not the MCP port):

```sh
make healthz       # {"status":"ok","version":"dev"}
make readyz        # {"status":"ready","checks":{"abi":"loaded","evm_rpc":"ok"}}
```

`version: dev` is correct for a `make`-built binary — the real version is
injected at release time via `-ldflags`. `abi: loaded` failing means
`ANCHOR_ABI_PATH` is wrong; `evm_rpc` failing means the RPC URL is unreachable.

### 3b. Alternative: start via Docker Compose

[`docker-compose.yml`](../docker-compose.yml) runs the same binary in a
container, fronted by a `caddy` service that terminates TLS on
`https://localhost:8443` with a local self-signed CA (the MCP port is not
published on the host directly — see the header comment in
`docker-compose.yml` for why). This is the closer analog of a hosted
deployment and is useful when you want TLS in the loop without standing up a
tunnel (Loop 3).

```sh
cp .env.example .env                              # if not already done
make key-create NAME=local-dev ROLES=reader,writer # compose bind-mounts .mcp-keys.json
make compose-up                                    # build + start; prints the endpoints
```

`make compose-up` fails fast with a clear error if `.env` or `.mcp-keys.json`
is missing — the latter matters because Docker will otherwise create the
bind-mount path as an empty **directory**, not a file.

```sh
curl -sk https://localhost:8443/.well-known/oauth-protected-resource -o /dev/null -w "%{http_code}\n"  # 404
curl -sSf http://localhost:9190/healthz | python3 -m json.tool
```

`-k` (or trusting Caddy's root CA — see § 5d) is required against `:8443`
since the cert is self-signed. `make compose-logs` follows both containers;
`make compose-down` tears the stack down. After `make key-create` /
`make key-disable` while the stack is already up, run `make compose-restart`
so the server re-reads `.mcp-keys.json`.

The optional Postgres profile (`KEY_STORE_BACKEND=postgres`) is started with
`docker compose --profile postgres up --build` — see the `docker-compose.yml`
header comment for the accompanying `.env` variables.

---

## 4. Loop 1 — drive the server without an MCP client

### `make mcp-probe`

```sh
make mcp-probe TOOL=evm_get_chain_id ARGS='{}'
make mcp-probe TOOL=nvnm_overview ARGS='{}'
make mcp-probe TOOL=evm_get_balance ARGS='{"address":"0x0000000000000000000000000000000000000000"}'
make mcp-probe-help
```

The target inlines the `initialize` → `notifications/initialized` →
`tools/call` handshake. It reads `MCP_HTTP_ADDR` (default `:8180`); override it
inline for a server on another port.

### Raw curl

The handler runs `Stateless: true`, so it mints no session map and a single
POST is sufficient — no `Mcp-Session-Id` round-trip required (the header is
still returned on `initialize` for spec-compliant clients):

```sh
curl -sS -X POST http://localhost:8180/ \
  -H "Authorization: Bearer $MCP_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","method":"tools/call","id":1,
       "params":{"name":"evm_get_chain_id","arguments":{}}}'
```

Responses are `text/event-stream` framed (`event: message` / `data: {...}`).
Both `Accept` values are required, and `Content-Type: application/json` is
enforced. `GET` returns `405` with `Allow: POST` under the stateless handler —
that is expected, not a defect.

### MCP-authorization preflight

These two behaviors are exactly what make a Claude-class client report
"Needs authentication" when they regress. Check them before blaming the client:

```sh
# OAuth discovery paths must 404 ("no OAuth here; use configured credentials")
curl -sS -o /dev/null -w "%{http_code}\n" \
  http://localhost:8180/.well-known/oauth-protected-resource       # 404

# Unauthenticated requests must carry a Bearer challenge
curl -sS -i -X POST http://localhost:8180/ \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1,"params":{}}'  # 401 + WWW-Authenticate: Bearer
```

Regression coverage lives in `internal/mcp/wellknown_test.go` and
`internal/mcp/auth_test.go`; this is the manual confirmation that the deployed
binary behaves the same.

### Origin guard

With no `NVNM_ALLOWED_ORIGINS` set, only loopback origins are accepted (any
port). Requests with **no** `Origin` header — `curl`, CLI clients,
server-to-server — pass through untouched.

```sh
# Browser-style origin, default allowlist:
curl -sS -o /dev/null -w "%{http_code}\n" -X POST http://localhost:8180/ \
  -H "Origin: https://claude.ai" -H "Authorization: Bearer $MCP_API_KEY" \
  -H "Content-Type: application/json" -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1,"params":{}}'   # 403
```

That `403` is the guard working. Loop 3 covers allowlisting the origin.

---

## 5. Loop 2 — connect a local Claude client

### 5a. Claude Code over stdio

The client launches the binary, so the chain config must reach the child
process and **all paths must be absolute** (the working directory is not
guaranteed):

```sh
claude mcp add --transport stdio \
  --env NVNM_EVM_RPC_URL=https://evm.testnet.nvnmchain.io \
  --env NVNM_CHAIN_ID=787111 \
  --env NVNM_CHAIN_ENVIRONMENT=testnet \
  --env ANCHOR_ABI_PATH=/abs/path/to/nvnm-mcp-server/abi/anchoring.json \
  nvnm-local -- /abs/path/to/nvnm-mcp-server/bin/nvnm-mcp-server --transport stdio
```

Everything after `--` is the launch command, untouched. Build the binary first
(`make build`).

### 5b. Claude Code over HTTP

Point the client at the already-running `make run-local` server:

```sh
claude mcp add --transport http nvnm-local http://localhost:8180/ \
  --header "Authorization: Bearer <raw key from make key-create>"
```

Claude Code sends no browser `Origin`, so the default loopback allowlist is
sufficient. Do not use `--scope project` for this: the header holds a
credential and `.mcp.json` is committed.

### 5c. Claude Desktop over stdio

Edit `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS)
and restart the app:

```json
{
  "mcpServers": {
    "nvnm-local": {
      "command": "/abs/path/to/nvnm-mcp-server/bin/nvnm-mcp-server",
      "args": ["--transport", "stdio"],
      "env": {
        "NVNM_EVM_RPC_URL": "https://evm.testnet.nvnmchain.io",
        "NVNM_CHAIN_ID": "787111",
        "NVNM_CHAIN_ENVIRONMENT": "testnet",
        "ANCHOR_ABI_PATH": "/abs/path/to/nvnm-mcp-server/abi/anchoring.json"
      }
    }
  }
}
```

### 5d. Claude Code against the Docker Compose stack (HTTPS)

The compose stack (§ 3b) fronts the server with Caddy's self-signed cert, so a
real client needs to trust it before `https://localhost:8443` will connect
(unlike `curl -k`, `claude mcp add` has no insecure-TLS flag). Export and
trust Caddy's local CA once per machine:

```sh
docker compose cp caddy:/data/caddy/pki/authorities/local/root.crt ./caddy-local-root.crt
sudo security add-trusted-cert -d -r trustRoot \
  -k /Library/Keychains/System.keychain ./caddy-local-root.crt   # macOS
```

Then add the client exactly as in § 5b, pointed at the Caddy port instead of
the bare server port:

```sh
claude mcp add --transport http nvnm-local https://localhost:8443/ \
  --header "Authorization: Bearer <raw key from make key-create>"
```

### 5e. Claude Desktop against the Docker Compose stack (via a stdio bridge)

`claude_desktop_config.json` only validates the stdio shape (`command` /
`args` / `env`) — there is no `url` or `transport` field, so pasting an
HTTPS URL into it does nothing (some builds silently drop the whole
`mcpServers` block on next save). Settings → Connectors → *Add custom
connector* **is not the fix either**: that path is brokered through
Anthropic's cloud, which opens the connection to your server itself — it
requires a URL reachable from Anthropic's public IP ranges, so
`https://localhost:8443` can never work there (that UI is for Loop 3 § 6,
over a real tunnel).

To reach the compose stack from Desktop, bridge it with the `mcp-remote`
stdio↔HTTP proxy so Desktop launches a subprocess exactly like any other
stdio server. Export the Caddy CA (§ 5d) and point `NODE_EXTRA_CA_CERTS` at
it, since Node does not consult the OS trust store the way `curl`/`security
add-trusted-cert` does:

```json
{
  "mcpServers": {
    "nvnm-local": {
      "command": "npx",
      "args": [
        "-y", "mcp-remote", "https://localhost:8443/",
        "--header", "Authorization:${AUTH_HEADER}"
      ],
      "env": {
        "AUTH_HEADER": "Bearer <raw key from make key-create>",
        "NODE_EXTRA_CA_CERTS": "/abs/path/to/nvnm-mcp-server/caddy-local-root.crt"
      }
    }
  }
}
```

Fully quit and restart Claude Desktop after editing the config (closing the
window does not reload it). If this is more machinery than a given change
needs, stdio (§ 5c) exercises the same tool handlers without any of the
Caddy/TLS plumbing — reach for that first unless you're specifically testing
the HTTP/TLS path.

### Verify the connection

```sh
claude mcp list          # expect: nvnm-local ✔ Connected
claude mcp get nvnm-local
```

Inside a session, `/mcp` shows status and tool count. **A configured server is
not a connected server** — always confirm before drawing conclusions.

### Smoke prompts

Exercise the surfaces an agent actually walks, in the order the onboarding
tools recommend:

1. "Use nvnm-local's `nvnm_overview` tool to describe this chain."
2. "Call `evm_get_chain_id`." — confirms RPC reachability end to end.
3. "Call `wallet_status` for `0x...`." — confirms the balance/nonce read path.
4. "Call `anchor_get_registries`." — confirms ABI decode against live chain
   state (run `make seed-test-data` first if the chain is fresh).

Confirm the answer is labeled as coming from the server's tool, not from model
knowledge.

---

## 6. Loop 3 — remote connector (claude.ai / Messages API)

Remote clients cannot reach `localhost`. Both paths below need a public HTTPS
URL and both treat the server exactly as a hosted deployment.

> **This exposes a development server to the internet.** Use a throwaway key
> with the narrowest roles that cover the test, leave `ENABLE_WRITE_TOOLS=false`
> unless you are specifically testing writes, never point it at mainnet config,
> and tear the tunnel down when you're done.

**Step 1 — allowlist the origin and restart.** The default loopback-only
allowlist rejects `https://claude.ai` with `403`:

```sh
NVNM_ALLOWED_ORIGINS="https://claude.ai" make run-local
```

**Step 2 — open a tunnel:**

```sh
cloudflared tunnel --url http://localhost:8180
# or
ngrok http 8180
```

**Step 3a — claude.ai custom connector.** In claude.ai → Settings →
Connectors → *Add custom connector*, use the tunnel's HTTPS URL and supply the
API key as the bearer token. The server answers `404` on the OAuth well-known
paths by design, which tells the client to use the configured credential rather
than starting a discovery flow.

**Step 3b — Messages API MCP connector.** Beta header
`anthropic-beta: mcp-client-2025-11-20`:

```sh
curl https://api.anthropic.com/v1/messages \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $ANTHROPIC_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "anthropic-beta: mcp-client-2025-11-20" \
  -d '{
    "model": "claude-opus-5",
    "max_tokens": 1024,
    "messages": [{"role":"user","content":"What is the chain ID?"}],
    "mcp_servers": [{
      "type": "url",
      "url": "https://<your-tunnel-host>/",
      "name": "nvnm-local",
      "authorization_token": "<raw API key>"
    }],
    "tools": [{"type":"mcp_toolset","mcp_server_name":"nvnm-local"}]
  }'
```

Only MCP **tool calls** are supported by that connector, and stdio servers
cannot be attached — which is why this loop needs the HTTP transport.

---

## 7. Write-path e2e (prepare → sign → broadcast)

The write flow is the one surface where manual e2e is genuinely load-bearing,
because the signing step lives outside the server by design.

```sh
export ENABLE_WRITE_TOOLS=true      # write tools are not registered without it
# key must hold writer / admin / automation; grant_role additionally requires admin
```

The loop: call an `anchor_prepare_*` tool → sign `raw_tx` (or the
`wallet_tx_request`) **client-side** → broadcast via `evm_send_raw_transaction`
→ confirm with `evm_get_transaction_receipt`.

Non-obvious constraints:

- **The server never holds a key.** Do not add one to `.env` for the server's
  benefit; testnet signing credentials belong to the test harness
  (`.chain_credentials.txt` or `NVNM_TEST_PRIVATE_KEY`), both gitignored.
- **`evm_send_raw_transaction` is a scoped relay.** It decodes the signed
  transaction and refuses anything not destined for the anchor precompile.
  `MCP_RELAY_ALLOW_ANY=true` lifts that on the authenticated path only, and is
  a **boot error** when combined with `MCP_KEYLESS_WRITES=true`.
- **Human confirmation is the client's job.** The server issues no approval
  prompt; the caller-side signature is the security boundary.
- `make seed-test-data` creates the `mcp-test-data` registry plus three records
  so read-back has something to find.

Wallet-side specifics are in [`METAMASK_GUIDE.md`](METAMASK_GUIDE.md); per-tool
schemas are in [`TOOL_REFERENCE.md`](TOOL_REFERENCE.md).

---

## 8. Optional surfaces

| Surface | How to exercise locally |
|---|---|
| Admin key API | Set `ADMIN_API_KEY`; binds `127.0.0.1:8081` by default. `curl -H "Authorization: Bearer $ADMIN_API_KEY" http://localhost:8081/admin/keys` |
| Postgres-backed stores | `MCP_KEYLESS_PG_DSN` (write/admin audit, quota, blacklist) and `KEY_STORE_DSN` + `KEY_HMAC_PEPPER` (Postgres key store). Migrations run at boot |
| Keyless reads / writes | `MCP_KEYLESS_READS=true` admits requests with **no** `Authorization` header (a present-but-invalid token is still `401`). `MCP_KEYLESS_WRITES=true` requires `MCP_KEYLESS_PG_DSN` |
| Container parity | `make docker-smoke` — build, run on `18080`/`19090`, check `/healthz`, `/readyz`, `initialize`, tear down |
| Load | `make test-load` (k6 against a running server; see `tests/load/README.md`) |

---

## 9. Teardown and hygiene

```sh
claude mcp remove nvnm-local        # drop the client entry
make key-disable NAME=local-dev     # revoke the local key
# stop the tunnel; stop the server (Ctrl-C)
```

Never commit `.env`, `.mcp-keys.json`, `.chain_credentials.txt`, or a
`.mcp.json` containing a bearer header. `detect-secrets` runs in pre-commit and
CI; do not extend `.secrets.baseline` to get a value past it.

---

## 10. Troubleshooting

| Symptom | Cause and fix |
|---|---|
| Boot: `ErrHTTPAuthRequired` | HTTP transport with no enabled key. Set `MCP_API_KEY` (+ roles) or point `MCP_API_KEYS_FILE` at a store with an enabled entry |
| Boot: `ErrMissingAPIKeyRoles` | `MCP_API_KEY` set without `MCP_API_KEY_ROLES` |
| Boot: legacy-var rejection | An `INVENIAM_*` var or `WRITE_APPROVAL_DEFAULT` is present. See `RUNBOOK.md#env-var-migration` and `#write-approval-removal` |
| Client reports "Needs authentication" | Run the § 4 preflight: well-known must be `404`, unauthenticated must be `401` + `WWW-Authenticate: Bearer`. If both pass, the bearer token is wrong, disabled, or expired |
| `401 missing Authorization header` | No bearer sent, and `MCP_KEYLESS_READS` is off |
| `403` on every request from a browser-based or hosted client | Origin not allowlisted. Add it to `NVNM_ALLOWED_ORIGINS` (exact match including port, except loopback) |
| `permission denied` on a tool | Default-deny RBAC. Reads need `reader`+; writes need `writer`/`admin`/`automation`; `anchor_prepare_grant_role` / `revoke_role` need `admin` |
| Write tools missing from `tools/list` | `ENABLE_WRITE_TOOLS` is not `true` — 17 read-only tools are registered instead of 23 |
| `405 Method Not Allowed` on `GET` | Expected: the stateless handler serves `POST` only |
| `415` / `400 Accept must contain...` | Send `Content-Type: application/json` and `Accept: application/json, text/event-stream` |
| `readyz` reports `abi` not loaded | `ANCHOR_ABI_PATH` is wrong or unreadable — use an absolute path when a client launches the binary |
| `429` | Rate limited: per-client (`MCP_RATE_LIMIT`/`BURST`), anonymous per-IP (`MCP_ANON_RATE_*`), or the pre-auth IP failure limiter after repeated `401`s |
| Port already in use | Another server (or `make docker-smoke`) holds `:8180`/`:9190`. Override `MCP_HTTP_ADDR` / `METRICS_ADDR`; the Makefile targets honor both |
