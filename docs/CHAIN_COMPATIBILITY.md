# Chain compatibility: chain-side gaps and server status

The server targets one chain type: an EVM chain with no native coin,
where fees are charged in a token contract. The chain it was verified
against is the devnet, `nvnm-tempo-devnet-1`, chain ID `787222`,
`https://rpc.nvnm.canary.mantrachain.dev`, node `tempo/v1.16.0` (reth).
The anchor contract is at `0x0000000000000000000000000000000000000A00`
(an EIP-1167 proxy). Everything below was observed live between
2026-11-09 and 2026-11-12.

Send section 1 to the chain team. Section 2 is how the server handles
what the chain does. Section 3 is what was verified and what is still
open.

## 1. For the chain team

Two gaps in `registriesByName`. Offset paging works, so neither blocks
the server. Both capabilities worked in the earlier anchoring precompile
(1.3.1), so callers that relied on them lose them.

### 1.1 Only exact match is implemented

`registriesByName` with `matchMode` 2 (prefix), 3 (suffix), or 4
(contains) reverts:

```text
execution reverted: only exact match is on chain; search prefix/suffix/contains off chain
```

Mode 1 (exact) works and is case-insensitive (`TEMPO-E2E` resolves
`tempo-e2e`, ids 2184 and 2185). Precompile 1.3.1 served all four modes
on chain.

Ask: implement the three modes, or confirm exact-only is permanent.

### 1.2 Name-filter cursor is not implemented

Offset paging works:

| call | result |
|---|---|
| `name=tempo-e2e`, exact, `limit 1` | `[2184]`, no `next_key` |
| same, `offset 1` | `[2185]` |
| same, `offset 2` | `[]` |

Two differences from precompile 1.3.1:

- A full page never returns `next_key`. 1.3.1 returned an 8-byte
  big-endian registry id.
- `pagination.key` is ignored. A key for id 2185 still returned 2184.
  `key` together with a non-zero `offset` is accepted; 1.3.1 rejected
  that combination.

The unfiltered `registries` listing does return and honor `next_key`
(page cap 200, reverse works). The gap is only `registriesByName`.

Ask: honor `key`, return `next_key` when the page filled `limit`, and
reject `key` combined with a non-zero `offset`.

### 1.3 To confirm

- The fee token's on-chain `name()` and `symbol()` are `nUSD`; the chain
  overview calls the asset pathUSD. The server displays pathUSD
  (`NVNM_FEE_TOKEN_SYMBOL` overrides it). Which name is final?
- Is pathUSD (`0x20c0000000000000000000000000000000000000`) the only
  token an account can pay fees in? The server reads that one balance.
  A wallet funded with a different fee token would read as unfunded.
- The public RPC is behind Cloudflare and answered `403` to a client
  with a default Python user agent. The Go client this server uses was
  not blocked. A production deployment needs an endpoint that will not
  filter it.
- Chain IDs and fee tokens for the networks that follow the devnet. The
  server recognizes only `787222`.

### Accepted as the chain's contract

Checked, handled by the server, nothing requested.

- `pagination.total` stays 0 with `countTotal` set.
- No native coin. `eth_getBalance` answers
  `0x9612084f0316e0ebd5182f398e5195a51b5ca47667d4c9b26c9b26c9b26c9b2`
  for every address, including the zero address.
- `eth_maxPriorityFeePerGas` returns 0; `eth_gasPrice` returns the base
  fee (0.6 gwei).
- `eth_estimateGas` with a non-zero `value` fails
  `value transfer not allowed`. Anchor calls carry `value` 0.
- `eth_getLogs` caps a query at 100000 blocks.
- A call at a block the node does not have fails `block not found: 0x…`.

## 2. What the server does

| Chain behavior | Server behavior |
|---|---|
| Chain identity | `787222` is recognized and infers `testnet`; `NVNM_EVM_RPC_URL` and `NVNM_CHAIN_ID` are the only chain settings it needs. Any other chain ID must set `NVNM_CHAIN_ENVIRONMENT` and the fee token, or the server refuses to start. The earlier chain IDs `787111` and `1611` are not recognized. |
| No native coin | `wallet_status`, `nvnm_setup_wizard`, and `evm_get_balance` read `balanceOf` on the fee token and never `eth_getBalance`. Amounts are rendered at the token's decimals (`100.000000 pathUSD`). Responses carry `balance_decimals` and `fee_token_address`. A `balanceOf` that reverts or returns no word is an error, not zero. |
| Fee token config | Built in for `787222`. `NVNM_FEE_TOKEN_ADDRESS` / `_SYMBOL` / `_DECIMALS` are all required on any other chain. A mixed-case address must carry a valid EIP-55 checksum. |
| Chain description | `nvnm_overview` and the `initialize` instructions say the chain has no native coin and name the fee token. `token_native` and `token_wrapped` both carry its symbol. The funding text names `faucet_url` (`NVNM_FAUCET_URL`) or `bridge_url`, and sends the reader to the operator when neither is configured. |
| Exact-only name index (1.1) | `match=prefix/suffix/contains` returns `match mode not supported on this chain: its registry name index serves match=exact only; …`. |
| No name cursor (1.2) | Offset paging works and its stop rules hold. A `key` sent with `name` is rejected with `key is not supported with name on this chain: …`. Detection: the returned page starts below the registry id the cursor names. |
| Zero priority fee | A prepared type-2 transaction takes the node's tip, including 0, with `max_fee_per_gas` at twice the gas price. `prefer_legacy_tx` builds a type-0 transaction at the gas price. |
| Not-found and validation wording | `registry does not exist`, `record does not exist`, `uri cannot be empty`, and `cannot revoke the last registry admin` are returned as `registry not found`, `record not found`, and two stated validation reasons. |
| Log-range and missing-block errors | `query exceeds max block range …`, `block range extends beyond current head block …`, and `block not found …` return curated errors. |
| `anchor_get_registry` argument | `registry_id`, matching the other anchor tools. `id` still works. |
| Live tests | Default to the devnet. Built-in historical-tx fixture for `787222`. The prefix cursor test skips on an exact-only name index. Write tests skip, naming the wallet and the token, when the signing wallet holds none of the fee token. |

The two rejections for 1.1 and 1.2 come from the chain's answer. If the
chain adds the missing support, they stop firing and no server change is
needed.

## 3. Verification and what is still open

### Verified against the devnet (2026-11-12)

The signing wallet was funded with 100 pathUSD.

- **Live integration suite** (`internal/evm`, `internal/anchor`,
  `internal/mcp`, tag `integration`), run on its defaults with no chain
  variables set: every test passes. All five write methods were mined:
  `addRegistry` (type-2 and type-0), `addRecord`, `updateRecordStatus`,
  `grantRole`, `revokeRole` (registry-wide and checksum-scoped).
  `TestIntegration_GetRegistriesByName_Cursor` skips (exact-only name
  index).
- **Deployment hot path** (`TestE2E_HotPath_AnchorDocument`, the test
  `make test-e2e` runs), in-process: passes. Create a registry, anchor a
  record, supersede it, read each back through MCP.
- **Tool sweep** over the MCP endpoint of a server built from this
  branch: 88 checks across all 23 tools, success and error paths, all
  pass.
- **Startup**: a server pointed at `787111` or `1611` refuses to start,
  first for the missing environment and then for the missing fee token.

A type-2 transaction with a zero tip is mined and pays exactly the base
fee: `maxPriorityFeePerGas` 0 and `effectiveGasPrice` equal to the
block's `baseFeePerGas` (0.6 gwei). Creating a registry costs about
0.0025 pathUSD.

### Deployment

- One server instance per chain. A devnet instance needs
  `NVNM_EVM_RPC_URL` and `NVNM_CHAIN_ID=787222`, and optionally
  `NVNM_EXPLORER_URL` (`https://scan.nvnm.canary.mantrachain.dev`) and
  `NVNM_FAUCET_URL`.
- This is a cutover. A deployment still configured for chain `787111` or
  `1611` will not start on this version.
- `787222` is a devnet. A testnet or mainnet with a new chain id needs
  either its id and fee token added next to `devnetChainID` in
  `internal/config/environment.go`, or `NVNM_CHAIN_ENVIRONMENT` plus the
  three `NVNM_FEE_TOKEN_*` variables.

### Left in the code on purpose

- Error wording from the earlier precompile and its node
  (`collections: not found`, `unknown method id`, the cometbft log-range
  messages) is still classified. It is unreachable on this chain and
  harmless; removing it is a separate cleanup.
- The EVM client still exposes `eth_getBalance`. No tool calls it.
- `docs/SECURITY_AUDIT.md` is a dated record and still names the
  earlier chain.
