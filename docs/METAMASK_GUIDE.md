# MetaMask Quick Start Guide

How to sign and submit anchor write operations using MetaMask and the NVNM Chain MCP Server.

---

## Prerequisites

Before you can anchor documents on the NVNM chain:

1. **MetaMask installed** in your browser (Chrome, Firefox, Brave, or Edge extension)
2. **NVNM Chain added** to MetaMask (see [Add the NVNM Chain](#step-1-add-the-nvnm-chain))
3. **wmantraUSD balance** -- the gas token on NVNM testnet
4. **MCP server running** with write tools enabled:
   ```bash
   ENABLE_WRITE_TOOLS=true make run-http
   ```
5. **An MCP client or agent** that can call tools and handle the response

---

## Step 1: Add the NVNM Chain to MetaMask

Open MetaMask → Settings → Networks → Add a network → Add a network manually.

| Field | Value |
|---|---|
| Network name | NVNM Chain (Inveniam L2) |
| New RPC URL | `https://evm.testnet.nvnmchain.io` |
| Chain ID | `787111` |
| Currency symbol | `wmantraUSD` |
| Block explorer | `https://explorer.evm.testnet.nvnmchain.io` |

Click **Save**. Switch to the NVNM Chain network.

---

## Step 2: Get Your Wallet Address

In MetaMask, copy your account address. It looks like `0xAbc123...`.

This is your `from` address -- you pass it to every `anchor_prepare_*` tool call.

---

## Step 3: Prepare a Transaction

Call any anchor write tool from your MCP client. Example for adding a record:

```json
{
  "tool": "anchor_prepare_add_record",
  "params": {
    "from": "0xYourWalletAddress",
    "registry": "my-documents",
    "uri": "https://example.com/contract.pdf",
    "checksum": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    "checksum_algo": "sha256",
    "metadata": "{\"title\":\"contract.pdf\"}"
  }
}
```

The response includes two signing paths. You want `wallet_tx_request`:

```json
{
  "raw_tx": "0x...",
  "to": "0x0000000000000000000000000000000000000A00",
  "data": "0x...",
  "nonce": 5,
  "gas": 120000,
  "gas_price": "5000000000",
  "value": "0",
  "chain_id": 787111,
  "wallet_tx_request": {
    "from": "0xYourWalletAddress",
    "to": "0x0000000000000000000000000000000000000A00",
    "data": "0x...",
    "value": "0x0",
    "chainId": "0xc02a7",
    "gas": "0x1d4c0",
    "gasPrice": "0x12a05f200"
  }
}
```

---

## Step 4: Send via MetaMask

Pass `wallet_tx_request` directly to MetaMask using the EIP-1193 API.

**In the browser console or a JavaScript client:**

```js
const prepared = /* response from anchor_prepare_add_record */;

const txHash = await window.ethereum.request({
  method: "eth_sendTransaction",
  params: [prepared.wallet_tx_request],
});

console.log("Transaction hash:", txHash);
```

MetaMask will pop up a confirmation window showing:

- **From:** your wallet address
- **To:** the anchor precompile (`0x0000...0A00`)
- **Gas fee:** estimated in wmantraUSD
- **Data:** the ABI-encoded anchor call (shown as hex)

Review and click **Confirm**.

---

## Step 5: Wait for the Transaction

MetaMask returns a `txHash` once the transaction is submitted. Use it to check the result:

```json
{
  "tool": "evm_get_transaction_receipt",
  "params": {
    "tx_hash": "0xabc123..."
  }
}
```

A successful receipt has `"status": "success"` and a non-zero `block_number`.

---

## Step 6: Verify the Anchor

Read back the anchored record to confirm it is on-chain:

```json
{
  "tool": "anchor_get_records",
  "params": {
    "registry": "my-documents"
  }
}
```

Your record should appear with `is_latest: true`.

---

## Signing All Three Write Operations

All three write tools follow exactly the same pattern:

| Tool | What it does |
|---|---|
| `anchor_prepare_add_registry` | Creates a new registry container |
| `anchor_prepare_add_record` | Anchors a document in a registry |
| `anchor_prepare_grant_role` | Grants `admin` or `editor` access to an address |

Each returns a `wallet_tx_request`. Pass it to MetaMask the same way.

---

## Troubleshooting

**MetaMask shows "Wrong network"**
Switch to NVNM Chain in MetaMask. The `chainId` in `wallet_tx_request` is `0xc02a7` (787111 decimal).

**MetaMask shows "Transaction underpriced"**
The gas price suggested at prepare time may have changed. Call `anchor_prepare_*` again to get a fresh estimate.

**MetaMask shows "Nonce too low"**
You may have a pending transaction. Wait for it to confirm, then call `anchor_prepare_*` again to get the current nonce.

**`anchor_prepare_*` returns an error**
Make sure:
- The MCP server was started with `ENABLE_WRITE_TOOLS=true`
- The `from` address is a valid `0x`-prefixed Ethereum address
- You are connected to the MCP server's HTTP transport (not stdio)

**Transaction confirmed but record not visible**
Call `anchor_get_records` with the registry name or checksum. The chain may take a few seconds to index the event.

---

## At a Glance

```
1. Add NVNM chain to MetaMask  (chainId 787111 / 0xc02a7)
2. Call anchor_prepare_*       (from = your wallet address)
3. Take wallet_tx_request      (ready-made MetaMask payload)
4. window.ethereum.request     (eth_sendTransaction)
5. MetaMask signs and sends    (you approve in the popup)
6. evm_get_transaction_receipt (confirm success)
7. anchor_get_records          (verify on-chain)
```

The MCP server never sees your private key. MetaMask handles signing locally.
