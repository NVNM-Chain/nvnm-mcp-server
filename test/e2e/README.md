# End-to-End Tests

One ordered run that drives **every MCP tool the server advertises**
against a **real deployed server** over real HTTP, backed by the **real
testnet chain**. Nothing is mocked and nothing is started in-process.

Writes are real transactions: each run creates one registry and one
record, updates that record's status, and grants + revokes a role. That
costs testnet gas.

## Run

```bash
make test-e2e
# or
go test -tags e2e -v ./test/e2e/...
```

Requires `.chain_credentials.txt` at the repo root (git-ignored):

```
Address: 0x...
PrivateKey: 0x...
```

The wallet must be funded. Without the file, or against an unfunded
wallet, the suite **skips** rather than fails.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `NVNM_MCP_TEST_SERVER_URL` | `https://mcp-testnet.nvnmchain.io` | MCP server under test |
| `NVNM_MCP_TEST_API_KEY` | *(unset)* | Bearer token; omit against a keyless deployment |
| `NVNM_MCP_TEST_CREDENTIALS` | `../../.chain_credentials.txt` | Signing credentials file |

Against a locally running server:

```bash
NVNM_MCP_TEST_SERVER_URL=http://localhost:8080 go test -tags e2e -v ./test/e2e/...
```

If the deployment enforces auth, the key needs the `writer` (or `admin`)
role — `anchor_prepare_grant_role` and `anchor_prepare_revoke_role`
require `admin`.

## Layout

| File | Contents |
|---|---|
| `flow_test.go` | `TestE2E_AllTools` — the ordered journey, plus the coverage assertion |
| `harness_test.go` | Connection, credentials, sign → broadcast → confirm, call helpers |
| `onboarding_test.go` | `nvnm_overview`, `nvnm_setup_wizard`, `nvnm_setup_verify_*`, `wallet_status` |
| `anchor_test.go` | `anchor_info`, `anchor_get_*`, `anchor_prepare_*` |
| `evm_test.go` | `evm_get_*`, `evm_call_contract` |

It is deliberately a single test with ordered subtests: the tools are
causally linked (you cannot read a record you have not written), and one
run costs one registry plus one record instead of one per test.

## Adding a tool

The final `coverage` phase compares what the run called against the
server's `tools/list`. **A new tool with no phase here fails the suite** —
add a `phase*` function and wire it into `TestE2E_AllTools`.

## Notes for maintainers

**Status: not yet run against a live server.** The suite is
compile-verified only. Expect the first real run to need tuning — most
likely the `evm_get_logs` block-range assertion, and grant/revoke role if
the endpoint enforces the `admin` role.

**`index` is omitted from `anchor_prepare_update_record_status`.** A
missing index means "the latest version", which is what updating a
record's current status means. Pinning an explicit index targets one
historical version instead and is rejected on chain. Do not "fix" this by
passing `rec.Index`.

**Responses are decoded from the wire, not from the server's Go types.**
Those envelope types are unexported, but reusing them would be wrong here
anyway: decoding into local structs asserts the *published JSON contract*.
That is what it is for — it caught five field-name mismatches on the first
draft (`timestamp_unix` not `timestamp`, `bytecode` not `code`, `tx_hash`
not `transaction_hash`, `data` not `input`, and `to` / `block_number`
being pointers because they are absent while a transaction is pending).

**Preconditions are checked on the parent test, not via `t.Skip` in a
subtest.** `t.Run` reports a *skipped* subtest as success, so a skip
inside `phaseWalletStatus` would not stop the run — it would sail on into
writes that cannot work and produce a wall of unrelated failures. The
phases record `writeToolsAvailable` / `walletFunded` and
`TestE2E_AllTools` acts on them. Skipping there also suppresses the
`coverage` phase, which would otherwise report a false gap.

**`evm_call_contract` targets the signing EOA with empty calldata.** That
is the one `eth_call` whose result is deterministic on any chain: no code
to run, so empty return data and no revert. Pointing it at the precompile
with real calldata would instead simulate a state-changing method, whose
outcome depends on chain state.
