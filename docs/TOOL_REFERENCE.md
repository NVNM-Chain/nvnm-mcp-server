# MCP Tool Reference

Complete schema reference for all 21 tools exposed by the Inveniam EVM MCP Server.

> **Write tools require `ENABLE_WRITE_TOOLS=true`.**
> The four write tools (`anchor_prepare_add_registry`, `anchor_prepare_add_record`,
> `anchor_prepare_grant_role`, `evm_send_raw_transaction`) are only registered
> when the `ENABLE_WRITE_TOOLS` environment variable is set to `true`.
> Without it the server exposes 17 read-only tools (5 onboarding, 8 EVM reads, 4 anchor reads).
>
> **HTTP authentication:** When using HTTP transport with auth configured
> (API keys via `MCP_API_KEYS_FILE` / `MCP_API_KEY`, or FusionAuth via
> `AUTH_PROVIDER=fusionauth`), all requests must include an
> `Authorization: Bearer <token>` header. The authenticated client ID is logged
> with all write operations for audit purposes. See `README.md` for key
> management commands and FusionAuth configuration.
>
> **Per-tool authorization (RBAC):** When roles are present on the API key or
> in the JWT, tools are gated by role. Reads require `reader`, `writer`,
> `admin`, or `automation`. Writes require `writer`, `admin`, or `automation`.
> `anchor_prepare_grant_role` requires `admin`. Calls without sufficient role
> return `permission denied`.
>
> **Per-client rate limiting:** When configured (`MCP_RATE_LIMIT`,
> `MCP_RATE_BURST`), requests beyond the per-client token budget receive
> HTTP `429 Too Many Requests`.

---

## Phase 8 cross-cutting changes

The following apply to every tool registered after Phase 8.2–8.5 and are not repeated per-tool below.

### Tool annotations

Every tool carries an MCP `ToolAnnotations` payload so clients (and connector-directory reviewers) can tell read-only tools from state-changing ones without inferring spec defaults. Three profiles cover the surface:

| Profile | `ReadOnlyHint` | `DestructiveHint` | `OpenWorldHint` | Tools |
|---|---|---|---|---|
| `newOpenWorldReadOnly` | true | _(unset)_ | true | `evm_get_*` (8), `anchor_get_*` (3), `anchor_prepare_*` (3), `wallet_status`, `nvnm_setup_wizard` |
| `newClosedWorldReadOnly` | true | _(unset)_ | false | `anchor_info`, `nvnm_overview`, `nvnm_setup_verify_hash`, `nvnm_setup_verify_signature` |
| `newDestructiveWriteTool` | false | true | true | `evm_send_raw_transaction` |

`anchor_prepare_*` tools are annotated read-only because the prepare step itself only reads chain state (nonce, gas, fees); the destructive effect happens at `evm_send_raw_transaction` time after the caller signs and broadcasts. The four `nvnm_*` / `wallet_*` onboarding tools added in Phase 8.8 split between open-world (those that touch chain state -- `wallet_status`, `nvnm_setup_wizard`) and closed-world (pure-compute helpers and the lobby tool -- `nvnm_overview`, `nvnm_setup_verify_hash`, `nvnm_setup_verify_signature`).

### `next_actions` response envelope

Every tool's response now carries an additive `next_actions` array. Each entry is a hint pointing at the next logical tool call:

```json
{
  "tool": "evm_get_transaction_receipt",
  "hint": "Wait ~one block then call this tool with tx_hash=0x... to confirm inclusion and inspect the decoded events.",
  "when": "after broadcast lands"
}
```

The `when` field is optional (omitted when the next call is unconditional). `tool` always names a registered MCP tool -- an AST-level reachability test enforces this. Some builders branch on response data (e.g., `evm_get_transaction_receipt` returns a different hint for `status=reverted` than for `status=success`; `evm_send_raw_transaction` echoes the tx_hash into its hint).

The field is additive: existing JSON consumers that ignore unknown fields see no change. Field-level assertions (`out.ChainID`, `out.Wei`, etc.) keep working via Go's promoted-field access on the envelope structs.

### EIP-1559 default on prepare tools (8.4)

`anchor_prepare_add_registry`, `anchor_prepare_add_record`, and `anchor_prepare_grant_role` now build EIP-1559 (type-2) transactions by default. The response shape gains three new fields:

| Field | Type | Populated when |
|---|---|---|
| `type` | int | Always; omitted in JSON when value is `0` |
| `max_fee_per_gas` | string (decimal wei) | Type-2 only (omitempty) |
| `max_priority_fee_per_gas` | string (decimal wei) | Type-2 only (omitempty) |

On type-2 responses, `gas_price` is dual-populated to equal `max_fee_per_gas` so a legacy-only signer that only reads `gas_price` still has a usable value.

Each prepare tool gains an optional `prefer_legacy_tx` (bool, default `false`) input. Set to `true` to opt back into a type-0 `LegacyTx` for signers that cannot produce type-2 signatures.

---

## Table of Contents

### Onboarding (Phase 8.8)

- [nvnm\_overview](#nvnm_overview)
- [wallet\_status](#wallet_status)
- [nvnm\_setup\_wizard](#nvnm_setup_wizard)
- [nvnm\_setup\_verify\_hash](#nvnm_setup_verify_hash)
- [nvnm\_setup\_verify\_signature](#nvnm_setup_verify_signature)

### Phase 1 -- Generic EVM

1. [evm\_get\_chain\_id](#1-evm_get_chain_id)
2. [evm\_get\_block](#2-evm_get_block)
3. [evm\_get\_transaction](#3-evm_get_transaction)
4. [evm\_get\_transaction\_receipt](#4-evm_get_transaction_receipt)
5. [evm\_get\_balance](#5-evm_get_balance)
6. [evm\_get\_code](#6-evm_get_code)
7. [evm\_get\_logs](#7-evm_get_logs)
8. [evm\_call\_contract](#8-evm_call_contract)

### Phase 2 -- Anchor Reads

9. [anchor\_info](#9-anchor_info)
10. [anchor\_get\_registry](#10-anchor_get_registry)
11. [anchor\_get\_registries](#11-anchor_get_registries)
12. [anchor\_get\_records](#12-anchor_get_records)

### Phase 3 -- Anchor Writes (require ENABLE\_WRITE\_TOOLS=true)

13. [anchor\_prepare\_add\_registry](#13-anchor_prepare_add_registry)
14. [anchor\_prepare\_add\_record](#14-anchor_prepare_add_record)
15. [anchor\_prepare\_grant\_role](#15-anchor_prepare_grant_role)
16. [evm\_send\_raw\_transaction](#16-evm_send_raw_transaction)

---

## nvnm\_overview

Lobby tool. Returns chain identity, the privacy-by-design property, the canonical agent journey, and a one-line prerequisites summary. No chain calls; pure-compute from operator-configured runtime info.

### Input Parameters

_No parameters._

### Output Fields

| Field | Type | Description |
|---|---|---|
| `chain_name` | `string` | Human-readable chain name. |
| `chain_environment` | `string` | One of `mainnet` / `testnet` / `local`. |
| `chain_id` | `int64` | EIP-155 chain ID. |
| `anchor_precompile` | `string` | Anchor precompile address (`0x...0A00`). |
| `explorer_url` | `string` | Operator-configured explorer URL (omitted when empty). |
| `docs_url` | `string` | Operator-configured docs URL (omitted when empty). |
| `bridge_url` | `string` | Operator-configured bridge URL (omitted when empty). |
| `token_native` | `string` | Native gas token symbol (e.g. `INVE`). |
| `token_wrapped` | `string` | Wrapped token symbol (e.g. `WINVE`). |
| `what_is_nvnm_chain` | `string` | 2-3 sentence prose explanation. |
| `privacy_by_design` | `string` | Verbatim caveat: the precompile emits no events, so only the caller knows what their transactions did. |
| `prereqs` | `[]string` | Prerequisites a caller needs before interacting. |
| `canonical_journey` | `[]object` | Recommended first-time-agent sequence; each entry is `{step, tool, hint}`. |
| `next_actions` | `[]NextAction` | Hints toward `wallet_status` / `nvnm_setup_wizard`. |

### Error Conditions

None: pure-compute, no RPC.

---

## wallet\_status

One-shot snapshot for an EVM address. Returns balance, nonce, and a three-state status -- `unfunded` / `funded_unused` / `funded_active`. The status field is intentionally honest: `funded_active` means "has sent any transaction," NOT "has anchored," because the chain emits no events by design.

### Input Parameters

| Field | Type | Required | Description |
|---|---|---|---|
| `address` | `string` | yes | EVM address (`0x...`). |

### Output Fields

| Field | Type | Description |
|---|---|---|
| `address` | `string` | EIP-55 checksummed address. |
| `balance_wei` | `string` | Balance as decimal-wei string (`"0"` when unfunded). |
| `balance_human` | `string` | Balance in the env-aware wrapped-token unit (e.g. `"1.5 WINVE"`). |
| `nonce` | `uint64` | Pending nonce. |
| `has_sent_tx` | `bool` | True iff `nonce > 0`. |
| `status` | `string` | `unfunded` / `funded_unused` / `funded_active`. |
| `chain_id` | `int64` | EIP-155 chain ID (echoed for client convenience). |
| `chain_environment` | `string` | `mainnet` / `testnet` / `local`. |
| `token_native` | `string` | Native gas-token symbol. |
| `token_wrapped` | `string` | Wrapped-token symbol used by `balance_human`. |
| `next_actions` | `[]NextAction` | Status-specific recommendations. |

### Error Conditions

- Invalid address format.
- Upstream RPC error fetching balance or pending nonce.

---

## nvnm\_setup\_wizard

Four-state prose-guided onboarding flow: `needs_wallet` / `unfunded` / `funded_unused` / `funded_active`. Without an address, returns language-specific wallet-generation samples (Python / JS / Go) that store the key via `keyring` / `.env` files / mode-`0600` files -- the samples never `print` the private key. With an address, derives state from the on-chain snapshot and returns concrete next-step prose plus a wallet snapshot.

### Input Parameters

| Field | Type | Required | Description |
|---|---|---|---|
| `address` | `string` | no | Optional EVM address. Omit to receive the `needs_wallet` response. |

### Output Fields

| Field | Type | Description |
|---|---|---|
| `state` | `string` | One of `needs_wallet` / `unfunded` / `funded_unused` / `funded_active`. |
| `message` | `string` | State-specific prose guidance. |
| `wallet` | `object` | Snapshot (`address`, `balance_wei`, `balance_human`, `nonce`, `has_sent_tx`, `chain_id`, `chain_environment`, `token_native`, `token_wrapped`) -- omitted when `state=needs_wallet`. |
| `sample_code` | `[]object` | Language-specific wallet-generation snippets -- only populated when `state=needs_wallet`. Each entry has `language`, `title`, `code`. |
| `bridge_url` | `string` | Bridge URL for funding -- populated when `state=needs_wallet` or `state=unfunded`. |
| `next_actions` | `[]NextAction` | State-specific recommendations. |

### Error Conditions

- Invalid address format (when an address is provided).
- Upstream RPC error fetching balance or pending nonce.

### Important caveat

`funded_active` says "has sent any transaction," NOT "has anchored." The chain emits no events by design, so this server cannot detect anchoring specifically. The response prose repeats this caveat verbatim.

---

## nvnm\_setup\_verify\_hash

Stateless hashing challenge. The server derives a deterministic per-address challenge string and the caller proves they can compute `SHA-256` of it. No on-chain state, no per-session state.

### Input Parameters

| Field | Type | Required | Description |
|---|---|---|---|
| `address` | `string` | yes | EVM address (`0x...`) the challenge is bound to. |
| `hash` | `string` | yes | Caller's SHA-256 of the challenge bytes, 0x-prefixed hex. |

### Output Fields

| Field | Type | Description |
|---|---|---|
| `ok` | `bool` | True iff `hash` matches the expected digest. |
| `address` | `string` | EIP-55 checksummed address. |
| `challenge` | `string` | The deterministic challenge string. |
| `expected` | `string` | The server-computed expected digest (0x-prefixed lowercase hex). |
| `got` | `string` | The caller-supplied digest, normalized (lowercase, 0x-prefixed). |
| `next_actions` | `[]NextAction` | On success, points at `nvnm_setup_verify_signature`. On mismatch, points back at `nvnm_setup_verify_hash` with a recompute hint. |

### Error Conditions

- Invalid address format.
- Missing `hash` (returns `missing_required`).
- Hash mismatch (returns `invalid_hash`; the response body still carries `expected` and `got` so the caller can debug their hashing path).

---

## nvnm\_setup\_verify\_signature

Stateless signing challenge. Uses the same per-address challenge as `nvnm_setup_verify_hash`; the caller produces an EIP-191 `personal_sign` signature over it, and the server recovers the signer address and compares it to the supplied `address`.

### Input Parameters

| Field | Type | Required | Description |
|---|---|---|---|
| `address` | `string` | yes | EVM address the caller claims to control; also the address that should be recovered from the signature. |
| `signature` | `string` | yes | EIP-191 `personal_sign` output over the challenge, 0x-prefixed hex (65 bytes / 132 hex chars). |

### Output Fields

| Field | Type | Description |
|---|---|---|
| `ok` | `bool` | True iff the recovered signer matches `address`. |
| `address` | `string` | EIP-55 checksummed address (what the caller claimed). |
| `challenge` | `string` | The deterministic challenge string. |
| `recovered_address` | `string` | The address ECDSA-recovered from the signature (omitted on parse failure). |
| `next_actions` | `[]NextAction` | On success, points at `anchor_get_registries` / `anchor_prepare_*`. On mismatch, points back at `nvnm_setup_verify_signature` with a re-sign hint. |

### Error Conditions

- Invalid address format.
- Malformed `signature` (wrong length, non-hex, or unparseable as `r||s||v`).
- ECDSA recovery failure (e.g., bad `v` byte).
- Recovered signer does not match the supplied address.

---

## 1. evm\_get\_chain\_id

Returns the chain ID and latest block number for the connected EVM network.

### Input Parameters

_No parameters._

### Output Fields

| Field                | Type     | Description                          |
|----------------------|----------|--------------------------------------|
| `chain_id`           | `int64`  | EIP-155 chain identifier             |
| `latest_block_number`| `uint64` | Most recent block number on the chain|

### Error Conditions

- RPC connection failure or timeout.

### Example

**Request:**

```json
{}
```

**Response:**

```json
{
  "chain_id": 58887,
  "latest_block_number": 185432
}
```

---

## 2. evm\_get\_block

Returns a block by number or hash. Use `block_number` for numeric lookup, `block_hash` for hash lookup. Set `full_transactions` to true to include transaction details.

### Input Parameters

| Name               | Type     | Required | Description                                |
|--------------------|----------|----------|--------------------------------------------|
| `block_number`     | `int64`  | optional | Block number (omit for latest)             |
| `block_hash`       | `string` | optional | Block hash (0x-prefixed, 32 bytes)         |
| `full_transactions`| `bool`   | optional | Include full transaction details (default false)|

Provide either `block_number` or `block_hash`, not both. Omit both to fetch the latest block.

### Output Fields

| Field              | Type                    | Description                                 |
|--------------------|-------------------------|---------------------------------------------|
| `number`           | `uint64`                | Block number                                |
| `hash`             | `string`                | Block hash (0x-prefixed)                    |
| `parent_hash`      | `string`                | Parent block hash (0x-prefixed)             |
| `timestamp_unix`   | `uint64`                | Block timestamp (Unix epoch seconds)        |
| `gas_limit`        | `uint64`                | Block gas limit                             |
| `gas_used`         | `uint64`                | Total gas used by all transactions          |
| `base_fee_per_gas` | `string` or null        | Base fee per gas (EIP-1559), omitted if N/A |
| `miner`            | `string`                | Block producer address (0x-prefixed)        |
| `transaction_count`| `int`                   | Number of transactions in the block         |
| `transactions`     | `NormalizedTxSummary[]` | Transaction summaries (only when `full_transactions` is true)|

**NormalizedTxSummary fields:**

| Field   | Type     | Description                        |
|---------|----------|------------------------------------|
| `hash`  | `string` | Transaction hash (0x-prefixed)     |
| `index` | `uint`   | Transaction index within the block |
| `from`  | `string` | Sender address (0x-prefixed)       |
| `to`    | `string` | Recipient address (0x-prefixed), empty for contract creation |
| `value` | `string` | Value transferred (wei, decimal)   |

### Error Conditions

- Invalid `block_hash` format (not 0x-prefixed, not 32 bytes).
- Block not found for the given number or hash.
- RPC connection failure.

### Example

**Request:**

```json
{
  "block_number": 100,
  "full_transactions": true
}
```

**Response:**

```json
{
  "number": 100,
  "hash": "0xabc123...def",
  "parent_hash": "0x789012...345",
  "timestamp_unix": 1700000000,
  "gas_limit": 30000000,
  "gas_used": 21000,
  "base_fee_per_gas": "1000000000",
  "miner": "0x1234567890abcdef1234567890abcdef12345678",
  "transaction_count": 1,
  "transactions": [
    {
      "hash": "0xdeadbeef...",
      "index": 0,
      "from": "0xaaa...111",
      "to": "0xbbb...222",
      "value": "1000000000000000000"
    }
  ]
}
```

---

## 3. evm\_get\_transaction

Returns transaction details by hash, including block inclusion info if mined.

### Input Parameters

| Name      | Type     | Required | Description                            |
|-----------|----------|----------|----------------------------------------|
| `tx_hash` | `string` | required | Transaction hash (0x-prefixed, 32 bytes)|

### Output Fields

| Field          | Type             | Description                                       |
|----------------|------------------|---------------------------------------------------|
| `hash`         | `string`         | Transaction hash (0x-prefixed)                    |
| `block_number` | `uint64` or null | Block number (omitted if pending)                 |
| `block_hash`   | `string` or null | Block hash (omitted if pending)                   |
| `index`        | `uint64` or null | Transaction index in block (omitted if pending)   |
| `from`         | `string`         | Sender address (0x-prefixed)                      |
| `to`           | `string` or null | Recipient address (omitted for contract creation) |
| `value`        | `string`         | Value transferred (wei, decimal string)           |
| `gas`          | `uint64`         | Gas limit set by the sender                       |
| `gas_price`    | `string`         | Gas price (wei, decimal string)                   |
| `nonce`        | `uint64`         | Sender nonce                                      |
| `data`         | `string`         | Input data (hex, 0x-prefixed)                     |
| `is_pending`   | `bool`           | True if the transaction is not yet mined          |

### Error Conditions

- Invalid `tx_hash` format (not 0x-prefixed, not 32 bytes).
- Transaction not found.
- RPC connection failure.

### Example

**Request:**

```json
{
  "tx_hash": "0x5c504ed432cb51138bcf09aa5e8a410dd4a1e204ef84bfed1be16dfba1b22060"
}
```

**Response:**

```json
{
  "hash": "0x5c504ed432cb51138bcf09aa5e8a410dd4a1e204ef84bfed1be16dfba1b22060",
  "block_number": 46147,
  "block_hash": "0x4e3a3754410177e6937ef1f84bba68ea139e8d1a2258c5f85db9f1cd715a1bdd",
  "index": 0,
  "from": "0xa1e4380a3b1f749673e270229993ee55f35663b4",
  "to": "0x5df9b87991262f6ba471f09758cde1c0fc1de734",
  "value": "31337000000000000000000",
  "gas": 21000,
  "gas_price": "50000000000000",
  "nonce": 0,
  "data": "0x",
  "is_pending": false
}
```

---

## 4. evm\_get\_transaction\_receipt

Returns the receipt for a mined transaction, including status, gas used, logs, and created contract address.

### Input Parameters

| Name      | Type     | Required | Description                            |
|-----------|----------|----------|----------------------------------------|
| `tx_hash` | `string` | required | Transaction hash (0x-prefixed, 32 bytes)|

### Output Fields

| Field                | Type               | Description                                        |
|----------------------|--------------------|----------------------------------------------------|
| `tx_hash`            | `string`           | Transaction hash (0x-prefixed)                     |
| `block_number`       | `uint64`           | Block number containing the transaction            |
| `block_hash`         | `string`           | Block hash (0x-prefixed)                           |
| `transaction_index`  | `uint`             | Position of the transaction within the block       |
| `status`             | `string`           | `"success"` or `"reverted"`                        |
| `gas_used`           | `uint64`           | Gas consumed by this transaction                   |
| `cumulative_gas_used`| `uint64`           | Cumulative gas used in the block up to this tx     |
| `contract_address`   | `string` or null   | Address of created contract (omitted if not a deployment)|
| `logs`               | `NormalizedLog[]`  | Event logs emitted by the transaction              |

**NormalizedLog fields** (same structure as `evm_get_logs` output):

| Field          | Type       | Description                             |
|----------------|------------|-----------------------------------------|
| `address`      | `string`   | Emitting contract address (0x-prefixed) |
| `topics`       | `string[]` | Indexed event topics (0x-prefixed hashes)|
| `data`         | `string`   | Non-indexed event data (hex, 0x-prefixed)|
| `block_number` | `uint64`   | Block number                            |
| `tx_hash`      | `string`   | Transaction hash (0x-prefixed)          |
| `tx_index`     | `uint`     | Transaction index in the block          |
| `log_index`    | `uint`     | Log index in the block                  |
| `removed`      | `bool`     | True if log was removed due to chain reorganization|

### Error Conditions

- Invalid `tx_hash` format (not 0x-prefixed, not 32 bytes).
- Receipt not found (transaction may be pending or nonexistent).
- RPC connection failure.

### Example

**Request:**

```json
{
  "tx_hash": "0x5c504ed432cb51138bcf09aa5e8a410dd4a1e204ef84bfed1be16dfba1b22060"
}
```

**Response:**

```json
{
  "tx_hash": "0x5c504ed432cb51138bcf09aa5e8a410dd4a1e204ef84bfed1be16dfba1b22060",
  "block_number": 46147,
  "block_hash": "0x4e3a3754410177e6937ef1f84bba68ea139e8d1a2258c5f85db9f1cd715a1bdd",
  "transaction_index": 0,
  "status": "success",
  "gas_used": 21000,
  "cumulative_gas_used": 21000,
  "logs": []
}
```

---

## 5. evm\_get\_balance

Returns the balance of an address in both wei and ether. Optionally specify a block number.

### Input Parameters

| Name           | Type     | Required | Description                                  |
|----------------|----------|----------|----------------------------------------------|
| `address`      | `string` | required | Ethereum address (0x-prefixed, 20 bytes)     |
| `block_number` | `int64`  | optional | Block number (omit for latest)               |

### Output Fields

| Field     | Type     | Description                            |
|-----------|----------|----------------------------------------|
| `address` | `string` | Queried address (0x-prefixed)          |
| `wei`     | `string` | Balance in wei (decimal string)        |
| `ether`   | `string` | Balance in ether (decimal string)      |

### Error Conditions

- Invalid address format (not a valid 0x-prefixed hex address).
- RPC connection failure.

### Example

**Request:**

```json
{
  "address": "0x1234567890abcdef1234567890abcdef12345678"
}
```

**Response:**

```json
{
  "address": "0x1234567890abcdef1234567890abcdef12345678",
  "wei": "1000000000000000000",
  "ether": "1.0"
}
```

---

## 6. evm\_get\_code

Returns the contract bytecode at an address, and whether a contract is deployed there.

### Input Parameters

| Name           | Type     | Required | Description                                  |
|----------------|----------|----------|----------------------------------------------|
| `address`      | `string` | required | Ethereum address (0x-prefixed, 20 bytes)     |
| `block_number` | `int64`  | optional | Block number (omit for latest)               |

### Output Fields

| Field         | Type     | Description                                 |
|---------------|----------|---------------------------------------------|
| `address`     | `string` | Queried address (0x-prefixed)               |
| `bytecode`    | `string` | Contract bytecode (hex, 0x-prefixed)        |
| `is_contract` | `bool`   | True if bytecode is non-empty at this address|

### Error Conditions

- Invalid address format (not a valid 0x-prefixed hex address).
- RPC connection failure.

### Example

**Request:**

```json
{
  "address": "0x1234567890abcdef1234567890abcdef12345678"
}
```

**Response:**

```json
{
  "address": "0x1234567890abcdef1234567890abcdef12345678",
  "bytecode": "0x6080604052...",
  "is_contract": true
}
```

---

## 7. evm\_get\_logs

Returns event logs matching a filter. Specify address(es), block range, and/or topics.

### Input Parameters

| Name         | Type       | Required | Description                                |
|--------------|------------|----------|--------------------------------------------|
| `address`    | `string`   | optional | Contract address to filter (0x-prefixed)   |
| `from_block` | `int64`    | optional | Start block number                         |
| `to_block`   | `int64`    | optional | End block number                           |
| `topics`     | `string[]` | optional | Event topics (0x-prefixed hashes) to match |

At least one filter parameter should be provided to avoid unbounded queries.

### Output Fields

Returns an array of log objects. Each log contains:

| Field          | Type       | Description                              |
|----------------|------------|------------------------------------------|
| `address`      | `string`   | Emitting contract address (0x-prefixed)  |
| `topics`       | `string[]` | Indexed event topics (0x-prefixed hashes)|
| `data`         | `string`   | Non-indexed event data (hex, 0x-prefixed)|
| `block_number` | `uint64`   | Block number containing the log          |
| `tx_hash`      | `string`   | Transaction hash (0x-prefixed)           |
| `tx_index`     | `uint`     | Transaction index in the block           |
| `log_index`    | `uint`     | Log index in the block                   |
| `removed`      | `bool`     | True if removed due to chain reorganization|

### Error Conditions

- Invalid `address` format.
- Invalid topic hash format at any index.
- RPC query failure (e.g., block range too large for the node).

### Example

**Request:**

```json
{
  "address": "0x1234567890abcdef1234567890abcdef12345678",
  "from_block": 100,
  "to_block": 200,
  "topics": [
    "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
  ]
}
```

**Response:**

```json
[
  {
    "address": "0x1234567890abcdef1234567890abcdef12345678",
    "topics": [
      "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
      "0x000000000000000000000000aaa...111",
      "0x000000000000000000000000bbb...222"
    ],
    "data": "0x0000000000000000000000000000000000000000000000000de0b6b3a7640000",
    "block_number": 150,
    "tx_hash": "0xdeadbeef...",
    "tx_index": 2,
    "log_index": 5,
    "removed": false
  }
]
```

---

## 8. evm\_call\_contract

Execute a read-only contract call. Provide the contract address and hex-encoded calldata. Returns raw hex output.

### Input Parameters

| Name           | Type     | Required | Description                              |
|----------------|----------|----------|------------------------------------------|
| `to`           | `string` | required | Contract address (0x-prefixed)           |
| `data`         | `string` | required | Hex-encoded calldata (0x-prefixed)       |
| `block_number` | `int64`  | optional | Block number (omit for latest)           |

### Output Fields

| Field    | Type     | Description                         |
|----------|----------|-------------------------------------|
| `result` | `string` | Hex-encoded return data (0x-prefixed)|

### Error Conditions

- Invalid `to` address format.
- Invalid calldata hex encoding.
- Contract call reverted (e.g., require failure).
- RPC connection failure.

### Example

**Request:**

```json
{
  "to": "0x1234567890abcdef1234567890abcdef12345678",
  "data": "0x70a08231000000000000000000000000aaaa...1111"
}
```

**Response:**

```json
{
  "result": "0x0000000000000000000000000000000000000000000000000de0b6b3a7640000"
}
```

---

## 9. anchor\_info

Returns configuration status of the anchoring precompile, including address, whether the ABI is loaded, and method count.

### Input Parameters

_No parameters._

### Output Fields

| Field          | Type     | Description                                   |
|----------------|----------|-----------------------------------------------|
| `address`      | `string` | Precompile contract address (0x-prefixed)     |
| `chain_id`     | `int64`  | Chain ID the precompile is configured for     |
| `abi_loaded`   | `bool`   | Whether the anchoring ABI was successfully loaded|
| `method_count` | `int`    | Number of methods in the loaded ABI (omitted if ABI not loaded)|

### Error Conditions

This tool reads local configuration only and does not make RPC calls. It should not error under normal operation.

### Example

**Request:**

```json
{}
```

**Response:**

```json
{
  "address": "0x0000000000000000000000000000000000000A00",
  "chain_id": 58887,
  "abi_loaded": true,
  "method_count": 12
}
```

---

## 10. anchor\_get\_registry

Fetch a single anchoring registry by its numeric ID or unique name. A registry is a logical container for anchored records.

### Input Parameters

| Name   | Type     | Required | Description                  |
|--------|----------|----------|------------------------------|
| `id`   | `uint64` | optional | Registry numeric ID          |
| `name` | `string` | optional | Registry unique name         |

At least one of `id` or `name` must be provided. If both are provided, the server uses both as lookup criteria.

### Output Fields

| Field         | Type     | Description                                    |
|---------------|----------|------------------------------------------------|
| `id`          | `uint64` | Registry numeric ID                            |
| `name`        | `string` | Registry unique name                           |
| `description` | `string` | Human-readable description                     |
| `creator`     | `string` | Address of the registry creator (0x-prefixed)  |
| `created_at`  | `string` | Creation timestamp                             |
| `metadata`    | `string` | Optional JSON metadata (omitted if empty)      |

### Error Conditions

- Neither `id` nor `name` provided (missing required parameter).
- Registry not found for the given ID or name.
- ABI encoding/decoding failure.
- RPC connection failure.

### Example

**Request:**

```json
{
  "name": "fund-documents"
}
```

**Response:**

```json
{
  "id": 1,
  "name": "fund-documents",
  "description": "Anchored fund documentation",
  "creator": "0xaaa...111",
  "created_at": "2024-01-15T10:30:00Z",
  "metadata": "{\"category\": \"finance\"}"
}
```

---

## 11. anchor\_get\_registries

Fetch a paginated list of anchoring registries. Optionally filter by registry\_id or name.

### Input Parameters

| Name          | Type     | Required | Description                   |
|---------------|----------|----------|-------------------------------|
| `registry_id` | `uint64` | optional | Filter by registry ID         |
| `name`        | `string` | optional | Filter by registry name       |
| `offset`      | `uint64` | optional | Pagination offset             |
| `limit`       | `uint64` | optional | Pagination limit              |

### Output Fields

| Field                   | Type         | Description                              |
|-------------------------|--------------|------------------------------------------|
| `registries`            | `Registry[]` | Array of registry objects                |
| `pagination`            | `object`     | Pagination metadata (omitted if no pagination requested)|
| `pagination.total`      | `uint64`     | Total number of matching registries      |

Each element in `registries` has the same fields as [anchor\_get\_registry](#10-anchor_get_registry) output.

### Error Conditions

- ABI encoding/decoding failure.
- RPC connection failure.

### Example

**Request:**

```json
{
  "offset": 0,
  "limit": 10
}
```

**Response:**

```json
{
  "registries": [
    {
      "id": 1,
      "name": "fund-documents",
      "description": "Anchored fund documentation",
      "creator": "0xaaa...111",
      "created_at": "2024-01-15T10:30:00Z",
      "metadata": ""
    },
    {
      "id": 2,
      "name": "audit-reports",
      "description": "External audit reports",
      "creator": "0xbbb...222",
      "created_at": "2024-02-20T14:00:00Z",
      "metadata": ""
    }
  ],
  "pagination": {
    "total": 2
  }
}
```

---

## 12. anchor\_get\_records

Flexibly query anchored records. Supports multiple lookup modes:

1. **Specific version:** `registry_id` + `record_id` + `index`
2. **Latest version of a record:** `registry_id` + `record_id`
3. **By content hash within a registry:** `registry_id` + `checksum`
4. **All latest records in a registry:** `registry_id` (with optional pagination)
5. **All records matching a checksum across registries:** `checksum`

### Input Parameters

| Name          | Type     | Required | Description                                 |
|---------------|----------|----------|---------------------------------------------|
| `registry_id` | `uint64` | optional | Registry numeric ID                         |
| `record_id`  | `uint64` | optional | Record ID within the registry               |
| `index`      | `uint64` | optional | Version index (starts at 1)                 |
| `checksum`   | `string` | optional | Content hash to search for                  |
| `registry`   | `string` | optional | Registry name                               |
| `offset`     | `uint64` | optional | Pagination offset                           |
| `limit`      | `uint64` | optional | Pagination limit                            |

### Output Fields

| Field                | Type        | Description                                     |
|----------------------|-------------|-------------------------------------------------|
| `records`            | `Record[]`  | Array of record objects                         |
| `pagination`         | `object`    | Pagination metadata (omitted if no pagination requested)|
| `pagination.total`   | `uint64`    | Total number of matching records                |

**Record fields:**

| Field           | Type     | Description                                 |
|-----------------|----------|---------------------------------------------|
| `registry`      | `string` | Registry name                               |
| `record_id`     | `uint64` | Record ID within the registry               |
| `index`         | `uint64` | Version index (1-based)                     |
| `checksum`      | `string` | Content hash of the anchored document       |
| `checksum_algo` | `string` | Hash algorithm used (e.g., `sha256`)        |
| `uri`           | `string` | Document URI                                |
| `status`        | `string` | Record status (e.g., `Active`)              |
| `is_latest`     | `bool`   | True if this is the latest version          |
| `timestamp`     | `string` | Anchoring timestamp                         |
| `metadata`      | `string` | JSON metadata                               |

### Error Conditions

- ABI encoding/decoding failure.
- RPC connection failure.
- No matching records found (returns empty array, not an error).

### Example

**Request:**

```json
{
  "registry_id": 1,
  "record_id": 42
}
```

**Response:**

```json
{
  "records": [
    {
      "registry": "fund-documents",
      "record_id": 42,
      "index": 3,
      "checksum": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
      "checksum_algo": "sha256",
      "uri": "ipfs://QmXyz...",
      "status": "Active",
      "is_latest": true,
      "timestamp": "2024-06-01T12:00:00Z",
      "metadata": "{\"version\": \"3.0\"}"
    }
  ]
}
```

---

## 13. anchor\_prepare\_add\_registry

> Requires `ENABLE_WRITE_TOOLS=true`

Construct an unsigned `addRegistry` transaction. Returns a complete unsigned transaction for the caller to sign and submit via `evm_send_raw_transaction`.

### Input Parameters

| Name          | Type     | Required | Description                    |
|---------------|----------|----------|--------------------------------|
| `from`        | `string` | required | Sender EVM address (0x-prefixed)|
| `name`        | `string` | required | Registry name (must be unique) |
| `description` | `string` | required | Registry description           |
| `metadata`    | `string` | optional | Optional JSON metadata         |

### Output Fields

Returns an [UnsignedTransaction](#unsignedtransaction-fields) object.

### Error Conditions

- Invalid `from` address.
- ABI encoding failure.
- Nonce lookup failure (RPC error).
- Gas estimation failure (RPC error or EVM revert, e.g., duplicate name).

### Example

**Request:**

```json
{
  "from": "0xaaa...111",
  "name": "fund-documents",
  "description": "Anchored fund documentation",
  "metadata": "{\"category\": \"finance\"}"
}
```

**Response:**

```json
{
  "raw_tx": "0xf8a80185...",
  "to": "0x0000000000000000000000000000000000000A00",
  "data": "0x12345678...",
  "nonce": 5,
  "gas": 200000,
  "gas_price": "1000000000",
  "value": "0",
  "chain_id": 58887,
  "wallet_tx_request": {
    "from": "0xaaa...111",
    "to": "0x0000000000000000000000000000000000000A00",
    "data": "0x12345678...",
    "value": "0x0",
    "chainId": "0xe607",
    "gas": "0x30d40",
    "gasPrice": "0x3b9aca00"
  }
}
```

---

## 14. anchor\_prepare\_add\_record

> Requires `ENABLE_WRITE_TOOLS=true`

Construct an unsigned `addRecord` transaction to anchor a document checksum and URI in a registry. Returns a complete unsigned transaction for the caller to sign and submit.

### Input Parameters

| Name            | Type     | Required | Description                                    |
|-----------------|----------|----------|------------------------------------------------|
| `from`          | `string` | required | Sender EVM address (0x-prefixed)               |
| `registry`      | `string` | required | Registry name                                  |
| `uri`           | `string` | required | Document URI                                   |
| `checksum`      | `string` | required | Document checksum hash                         |
| `checksum_algo` | `string` | optional | Hash algorithm (e.g., `sha256`)                |
| `status`        | `string` | optional | Record status (default: `Active`)              |
| `metadata`      | `string` | optional | Optional JSON metadata                         |

### Output Fields

Returns an [UnsignedTransaction](#unsignedtransaction-fields) object.

### Error Conditions

- Invalid `from` address.
- Registry not found by name.
- ABI encoding failure.
- Nonce lookup failure (RPC error).
- Gas estimation failure (RPC error or EVM revert, e.g., insufficient permissions).

### Example

**Request:**

```json
{
  "from": "0xaaa...111",
  "registry": "fund-documents",
  "uri": "ipfs://QmXyz...",
  "checksum": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
  "checksum_algo": "sha256",
  "status": "Active"
}
```

**Response:**

```json
{
  "raw_tx": "0xf8c80185...",
  "to": "0x0000000000000000000000000000000000000A00",
  "data": "0xabcdef01...",
  "nonce": 6,
  "gas": 250000,
  "gas_price": "1000000000",
  "value": "0",
  "chain_id": 58887,
  "wallet_tx_request": {
    "from": "0xaaa...111",
    "to": "0x0000000000000000000000000000000000000A00",
    "data": "0xabcdef01...",
    "value": "0x0",
    "chainId": "0xe607",
    "gas": "0x3d090",
    "gasPrice": "0x3b9aca00"
  }
}
```

---

## 15. anchor\_prepare\_grant\_role

> Requires `ENABLE_WRITE_TOOLS=true`

Construct an unsigned `grantRole` transaction to assign admin or editor permissions on a registry. Returns a complete unsigned transaction for the caller to sign and submit.

### Input Parameters

| Name          | Type     | Required | Description                                          |
|---------------|----------|----------|------------------------------------------------------|
| `from`        | `string` | required | Admin EVM address (0x-prefixed)                      |
| `registry_id` | `uint64` | required | Registry numeric ID                                  |
| `checksum`    | `string` | optional | Scope role to a specific record checksum             |
| `account`     | `string` | required | Address to grant the role to (0x-prefixed)           |
| `role`        | `string` | required | Role to grant: `admin` or `editor`                   |

### Output Fields

Returns an [UnsignedTransaction](#unsignedtransaction-fields) object.

### Error Conditions

- Invalid `from` or `account` address.
- Invalid `role` value (must be `admin` or `editor`).
- ABI encoding failure.
- Nonce lookup failure (RPC error).
- Gas estimation failure (RPC error or EVM revert, e.g., caller is not an admin).

### Example

**Request:**

```json
{
  "from": "0xaaa...111",
  "registry_id": 1,
  "account": "0xbbb...222",
  "role": "editor"
}
```

**Response:**

```json
{
  "raw_tx": "0xf8b80185...",
  "to": "0x0000000000000000000000000000000000000A00",
  "data": "0x87654321...",
  "nonce": 7,
  "gas": 150000,
  "gas_price": "1000000000",
  "value": "0",
  "chain_id": 58887,
  "wallet_tx_request": {
    "from": "0xaaa...111",
    "to": "0x0000000000000000000000000000000000000A00",
    "data": "0x87654321...",
    "value": "0x0",
    "chainId": "0xe607",
    "gas": "0x249f0",
    "gasPrice": "0x3b9aca00"
  }
}
```

---

## 16. evm\_send\_raw\_transaction

> Requires `ENABLE_WRITE_TOOLS=true`
>
> **Write approval:** Before broadcasting, the server checks the write-approval
> policy for the authenticated client. If approval is `required` (the default),
> the server sends an MCP elicitation prompt with decoded transaction details
> (to, value, gas, nonce, chain ID, data length) and waits for the user to
> accept or decline. Set `WRITE_APPROVAL_DEFAULT=auto` or per-client
> `write_approval: "auto"` to skip the prompt for trusted pipelines.

Broadcast a signed transaction to the network. Input is the signed transaction as a hex string (0x-prefixed). Returns the transaction hash.

### Input Parameters

| Name        | Type     | Required | Description                                |
|-------------|----------|----------|--------------------------------------------|
| `signed_tx` | `string` | required | Signed transaction hex (0x-prefixed)       |

### Output Fields

| Field     | Type     | Description                           |
|-----------|----------|---------------------------------------|
| `tx_hash` | `string` | Transaction hash (0x-prefixed)        |

### Error Conditions

- `signed_tx` is empty (missing required parameter).
- Write approval declined by user (`ErrWriteDeclined`).
- Write approval required but MCP client does not support elicitation (`ErrElicitationUnsupported`).
- Invalid hex encoding.
- RLP decoding failure (malformed transaction).
- Nonce too low or too high.
- Insufficient funds for gas.
- Transaction rejected by the node (e.g., invalid signature, chain ID mismatch).
- RPC connection failure.

### Example

**Request:**

```json
{
  "signed_tx": "0xf86c0185...v1b..."
}
```

**Response:**

```json
{
  "tx_hash": "0x5c504ed432cb51138bcf09aa5e8a410dd4a1e204ef84bfed1be16dfba1b22060"
}
```

---

## Appendix: Common Types

### UnsignedTransaction Fields

All `anchor_prepare_*` tools return an `UnsignedTransaction` with two signing
paths in the same response:

| Field               | Type     | Description                                           |
|---------------------|----------|-------------------------------------------------------|
| `raw_tx`            | `string` | RLP-encoded unsigned transaction (hex, 0x-prefixed). Used by **local/headless** signers (CLI, HSM, server-side ECDSA). |
| `to`                | `string` | Target address (precompile, 0x-prefixed).             |
| `data`              | `string` | ABI-encoded calldata (hex, 0x-prefixed).              |
| `nonce`             | `uint64` | Sender's pending nonce.                               |
| `gas`               | `uint64` | Estimated gas limit (includes safety buffer).         |
| `gas_price`         | `string` | Current gas price (wei, decimal string).              |
| `value`             | `string` | Always `"0"` for precompile calls.                    |
| `chain_id`          | `int64`  | EIP-155 chain ID.                                     |
| `wallet_tx_request` | `object` | EIP-1193 / **MetaMask** transaction request payload (see below). Pass directly to `window.ethereum.request({ method: "eth_sendTransaction", params: [...] })`. |

**`wallet_tx_request` object fields** (all values are `0x`-prefixed hex
quantities suitable for EIP-1193 wallets):

| Field      | Type     | Description                                  |
|------------|----------|----------------------------------------------|
| `from`     | `string` | Sender address (0x-prefixed).                |
| `to`       | `string` | Precompile address (0x-prefixed).            |
| `data`     | `string` | ABI-encoded calldata (hex, 0x-prefixed).     |
| `value`    | `string` | `"0x0"` for precompile calls.                |
| `chainId`  | `string` | 0x-prefixed hex chain ID (e.g. `"0xe607"`).  |
| `gas`      | `string` | 0x-prefixed hex gas limit.                   |
| `gasPrice` | `string` | 0x-prefixed hex gas price (wei).             |

Workflow for write operations -- choose one path:

**Path A (MetaMask / browser wallet):**

1. Call an `anchor_prepare_*` tool.
2. Pass `wallet_tx_request` to `window.ethereum.request({ method: "eth_sendTransaction", params: [prepared.wallet_tx_request] })`.
3. MetaMask signs and broadcasts directly. **Do not** call `evm_send_raw_transaction` in this path.
4. Use the returned `txHash` with `evm_get_transaction_receipt` to confirm.

See `docs/METAMASK_GUIDE.md` for a full walkthrough.

**Path B (local / headless signer):**

1. Call an `anchor_prepare_*` tool.
2. Sign the `raw_tx` bytes with the sender's private key (off-server -- HSM, Vault, local keystore, etc.).
3. Submit the signed hex via `evm_send_raw_transaction`.

### NormalizedLog Fields

Used by `evm_get_logs` and in the `logs` array of `evm_get_transaction_receipt`:

| Field          | Type       | Description                              |
|----------------|------------|------------------------------------------|
| `address`      | `string`   | Emitting contract address (0x-prefixed)  |
| `topics`       | `string[]` | Indexed event topics (0x-prefixed hashes)|
| `data`         | `string`   | Non-indexed event data (hex, 0x-prefixed)|
| `block_number` | `uint64`   | Block number                             |
| `tx_hash`      | `string`   | Transaction hash (0x-prefixed)           |
| `tx_index`     | `uint`     | Transaction index in the block           |
| `log_index`    | `uint`     | Log index in the block                   |
| `removed`      | `bool`     | True if removed due to chain reorganization|

### PageRequest / PageResponse

Used by `anchor_get_registries` and `anchor_get_records` for pagination:

**PageRequest** (input):

| Field    | Type     | Description        |
|----------|----------|--------------------|
| `offset` | `uint64` | Pagination offset  |
| `limit`  | `uint64` | Pagination limit   |

**PageResponse** (output):

| Field   | Type     | Description                     |
|---------|----------|---------------------------------|
| `total` | `uint64` | Total count of matching results |
