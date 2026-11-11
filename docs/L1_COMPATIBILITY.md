# L1 compatibility: chain gaps and MCP status

The L1 is `nvnm-tempo-devnet-1`, chain ID `787222`,
`https://rpc.nvnm.canary.mantrachain.dev`, node `tempo/v1.16.0` (reth).
The anchor contract is at `0x0000000000000000000000000000000000000A00`
(an EIP-1167 proxy). Everything below was observed live between
2026-11-09 and 2026-11-11 and compared with the L2 testnet `787111` at
precompile 1.3.1.

Send section 1 to the chain team. Section 2 is what this server does
about each difference. Section 3 is what is still open before an L1
deployment.

## 1. For the chain team

Two gaps in `registriesByName`. Offset paging works, so neither blocks
the server; both block parity with L2 testnet 1.3.1.

### 1.1 Only exact match is implemented

`registriesByName` with `matchMode` 2 (prefix), 3 (suffix), or 4
(contains) reverts:

```text
execution reverted: only exact match is on chain; search prefix/suffix/contains off chain
```

Mode 1 (exact) works and is case-insensitive (`TEMPO-E2E` resolves
`tempo-e2e`, ids 2184 and 2185). L2 1.3.1 serves all four modes on
chain.

Ask: implement the three modes, or confirm exact-only is permanent.

### 1.2 Name-filter cursor is not implemented

Offset paging works:

| call | result |
|---|---|
| `name=tempo-e2e`, exact, `limit 1` | `[2184]`, no `next_key` |
| same, `offset 1` | `[2185]` |
| same, `offset 2` | `[]` |

Two differences from L2 1.3.1:

- A full page never returns `next_key`. L2 returns an 8-byte
  big-endian registry id.
- `pagination.key` is ignored. A key for id 2185 still returned 2184.
  `key` together with a non-zero `offset` is accepted; L2 rejects that
  combination.

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

### Accepted as the chain's contract

Checked, handled by the server, nothing requested.

- `pagination.total` stays 0 with `countTotal` set (same as L2).
- No native coin. `eth_getBalance` answers
  `0x9612084f0316e0ebd5182f398e5195a51b5ca47667d4c9b26c9b26c9b26c9b2`
  for every address, including the zero address.
- `eth_maxPriorityFeePerGas` returns 0; `eth_gasPrice` returns the base
  fee (0.6 gwei).
- `eth_estimateGas` with a non-zero `value` fails
  `value transfer not allowed`. Anchor calls carry `value` 0.
- `eth_getLogs` caps a query at 100000 blocks (the L2 caps at 10000).

## 2. What the server does

Branch `feature/l1-migration`. Verified against the live L1 through the
MCP endpoint and with the integration suite; the L2 testnet was re-run
as a regression, including a mined type-2 write.

| Difference | Server behavior |
|---|---|
| Chain identity | `787222` is recognized and infers `testnet`. `NVNM_EVM_RPC_URL` and `NVNM_CHAIN_ID` are the only chain settings needed. |
| No native coin | `wallet_status`, `nvnm_setup_wizard`, and `evm_get_balance` read `balanceOf` on the fee token and never `eth_getBalance`. Amounts are rendered at 6 decimals (`100.000000 pathUSD`). Responses carry `balance_decimals` and `fee_token_address`. A `balanceOf` that reverts or returns no word is an error, not zero. |
| Fee token config | Built in for `787222`. `NVNM_FEE_TOKEN_ADDRESS` / `_SYMBOL` / `_DECIMALS` describe it on any other chain and must be set together. |
| Chain description | `nvnm_overview` and the `initialize` instructions describe an EVM L1 with no native coin. Both token names are pathUSD. The funding text names the token and `faucet_url` (`NVNM_FAUCET_URL`), not a bridge. |
| Exact-only name index (1.1) | `match=prefix/suffix/contains` returns `match mode not supported on this chain: its registry name index serves match=exact only; …`. It is not reported as a missing `registriesByName`, and no longer collapses to `upstream operation failed`. |
| No name cursor (1.2) | Offset paging is unchanged and its stop rules hold. A `key` sent with `name` is rejected with `key is not supported with name on this chain: …`. Detection: the returned page starts below the registry id the cursor names. |
| Zero priority fee | A prepared type-2 transaction takes the node's tip, including 0, with `max_fee_per_gas` at twice the gas price. Before, a 1 gwei default tip was added and mined L1 transactions paid 1.2 gwei on a 0.6 gwei base fee. `prefer_legacy_tx` still builds a type-0 transaction at the gas price. |
| reth log-range errors | `query exceeds max block range …` and `block range extends beyond current head block …` return the same two curated errors as on the L2. |
| `anchor_get_registry` argument | `registry_id`, matching the other anchor tools. `id` still works. |
| Live tests | Target chain from `NVNM_EVM_RPC_URL` / `NVNM_CHAIN_ID`. Built-in historical-tx fixture for `787222`. The prefix cursor test skips on an exact-only chain. Write tests skip, naming the wallet and the asset, when the signing wallet holds no fee asset. |

Unchanged and working on both chains: the unfiltered
`anchor_get_registries` listing with both paging styles,
`anchor_get_records`, `anchor_info`, `evm_get_block`,
`evm_get_transaction`, `evm_get_transaction_receipt` (the extra
`feeToken` / `feePayer` receipt fields are ignored), `evm_get_code`,
`evm_get_logs`, `evm_call_contract`, and `anchor_prepare_add_registry`.
The other four `anchor_prepare_*` tools need a registry the signer
administers, so on the L1 they are covered by the funded-wallet run in
section 3.

## 3. Still open

### Needs a funded wallet

No transaction was broadcast on the L1 from this branch: the test
wallet holds no pathUSD. What is established without one:

- `eth_estimateGas` accepts the prepared call with
  `maxPriorityFeePerGas` 0 and `maxFeePerGas` 1.2 gwei, and rejects a
  fee cap below the base fee.
- Type-2 transactions are mined on the L1 (35 of 41 sampled anchor
  transactions), and type-0 transactions paying exactly the base fee
  are mined, so a zero effective tip is included.

Not established: that a type-2 transaction with a zero tip from this
server is mined. To close it, fund the signing wallet with pathUSD and
run:

```sh
NVNM_EVM_RPC_URL=https://rpc.nvnm.canary.mantrachain.dev NVNM_CHAIN_ID=787222 \
  make test-integration
```

then `make test-e2e` against a server started with the same two
variables. If a zero-tip transaction is not included, `prefer_legacy_tx`
is the working path today.

### Deployment

- One server instance per chain. An L1 instance needs its own
  `NVNM_EVM_RPC_URL`, `NVNM_CHAIN_ID=787222`, and optionally
  `NVNM_EXPLORER_URL` (`https://scan.nvnm.canary.mantrachain.dev`) and
  `NVNM_FAUCET_URL`. `NVNM_BRIDGE_URL` should stay unset; there is no
  bridge.
- `787222` is a devnet. An L1 testnet or mainnet with a new chain id
  needs either its id and fee token added next to `l1DevnetChainID` in
  `internal/config/environment.go`, or `NVNM_CHAIN_ENVIRONMENT` plus the
  three `NVNM_FEE_TOKEN_*` variables.

### Documentation not updated on this branch

These still describe only the L2 and should be revised when the L1
becomes a supported deployment target: the README introduction,
`AGENTS.md` ("an Inveniam L2 on MANTRA"), `docs/DESIGN.md`, and
`docs/METAMASK_GUIDE.md` (network parameters and currency symbol for
adding the chain to a wallet).

### Chain side

Items 1.1 and 1.2. When the chain serves the other match modes and the
name cursor, no server change is needed: the two rejections in section 2
only fire on the chain's current answers.
