# Deployment hot-path e2e

This suite hits a **running MCP server** — typically a deployment — and
walks the human-facing write journey. It is not an all-tools net.
Tool-level regressions belong in the MCP tools suite
(`TestMCP_Tools` in
`internal/mcp/mcp_tools_test.go`). Precompile packing and
grant/revoke mining belong in tagged live client tests. Layer purpose,
what each can catch, and the 23-tool coverage matrix are in
[docs/TESTING.md](../../docs/TESTING.md).

`make test-e2e` runs only
`TestE2E_HotPath_AnchorDocument`: onboard → create a registry →
anchor a document → supersede it → observe that write through EVM
tools. After confirm the new registry is listed with
`anchor_get_registries` by name (full-table scan; HTTP wait 90s,
latency budget 60s) then confirmed with `anchor_get_registry`.
Record read-back asserts `uri`, `is_latest`, and `registry_id`.
Grant/revoke stay out of this path. Decode uses published JSON field
names. Set `NVNM_MCP_TEST_SERVER_URL` at the deployment. Without a URL
the harness falls back to in-process against testnet (developer
convenience).

This is **not** a pull-request gate.

## Run

```bash
NVNM_MCP_TEST_SERVER_URL=https://mcp-testnet.nvnmchain.io make test-e2e
```

Local in-process fallback (needs `NVNM_EVM_RPC_URL` and a funded key):

```bash
make test-e2e
```

## Credentials

Resolved **environment first, file second**. The address is always derived
from the key; a disagreeing `Address` line fails loudly.

```
NVNM_TEST_PRIVATE_KEY=0x...          # wins when set (release CI secret)
NVNM_TEST_ADDRESS=0x...              # optional; cross-checked against the key
```

Or `.chain_credentials.txt` at the repo root (git-ignored):

```
Address: 0x...
PrivateKey: 0x...
```

The wallet must be funded. Without a key the suite **skips** locally.
Set `NVNM_E2E_REQUIRE_CHAIN=1` to turn that skip into a failure.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `NVNM_MCP_TEST_SERVER_URL` | *(unset → in-process)* | Deployed MCP server under test |
| `NVNM_EVM_RPC_URL` | *(required for in-process)* | Chain RPC used to build real clients |
| `NVNM_CHAIN_ID` | `787111` | In-process server chain ID |
| `NVNM_MCP_TEST_API_KEY` | *(unset)* | Bearer token; omit against a keyless server |
| `NVNM_TEST_PRIVATE_KEY` | *(file fallback)* | Signing key |
| `NVNM_MCP_TEST_CREDENTIALS` | `<repo>/.chain_credentials.txt` | Signing credentials file |
| `NVNM_E2E_REQUIRE_CHAIN` | unset | `1`/`true` turns skip into fail |

If the deployment enforces auth, the key needs the `writer` role.

A `mainnet` `chain_environment` from `nvnm_overview` skips writes so the
same journey can smoke production read-only.

## Layout

```
tests/e2e/
  hotpath_test.go   TestE2E_HotPath_AnchorDocument
  harness.go        Call helpers, sign → broadcast → confirm
  resolve.go        O(1) registry-id lookup after create
  fixture.go        Discovery and chain-liveness preflight
  target.go         URL vs in-process target
  credentials.go    Env-then-file key loading
  wire.go           JSON shapes used by the hot path
```
