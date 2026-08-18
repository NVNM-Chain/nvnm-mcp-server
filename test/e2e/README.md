# End-to-End Tests

One ordered run that drives **every MCP tool the server advertises**
against a **real deployed server** over real HTTP, backed by the **real
testnet chain**. Nothing is mocked and nothing is started in-process.

Writes are real transactions: each run creates one registry, one record,
and three more records anchored at a single URI (the versioning probe),
then updates record statuses and grants + revokes a role — a dozen or so
transactions. That costs testnet gas.

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

**One file per MCP tool, named after the tool.** `anchor_get_records_test.go`
asserts `anchor_get_records`, and nothing else does. To find a tool's
coverage, guess the filename.

Four files are not tools:

| File | Contents |
|---|---|
| `flow_test.go` | `TestE2E_AllTools` — the running order, and the coverage assertion |
| `fixture_test.go` | The shared on-chain state every phase asserts against |
| `harness_test.go` | Connection, credentials, call helpers, sign → broadcast → confirm |
| `wire_test.go` | Response shapes used by more than one tool's file |

It is deliberately one test with ordered subtests rather than a set of
independent `Test` functions. The tools are causally linked — you cannot
read a record you have not written, or fetch a receipt for a transaction
you have not broadcast — and one shared registry costs a fraction of what
a fixture per test would.

### Running one tool's tests

Subtest names match tool names exactly:

```bash
go test -tags e2e -v -run 'TestE2E_AllTools/anchor_get_records' ./test/e2e/...
go test -tags e2e -v -run 'TestE2E_AllTools/anchor_prepare_(add_record|grant_role)' ./test/e2e/...
```

This works because the fixture is built in `TestE2E_AllTools`' own body,
not in a subtest: `-run` filters subtests, so a prerequisite living in one
would vanish under a filter and take every later phase with it. The
fixture still costs its registry and record whatever you select — that is
the price of state you cannot fake.

The `coverage` phase is itself a subtest, so a filtered run skips it
rather than reporting 22 false gaps.

### Setup extracts, phases assert

The division of labour between `fixture_test.go` and the per-tool files
is worth keeping to. Setup functions *extract* the state later phases
need and fail loudly if they cannot; per-tool phases *assert* the tool's
contract. So `setupDiscovery` reads `chain_id` out of `nvnm_overview`
without judging the rest of that response — `phaseOverview` does that —
and `setupRecord` anchors a record without asserting
`anchor_prepare_add_record`'s output contract, which
`phasePrepareAddRecord` does.

The payoff is that a tool's assertions live in exactly one place. The
cost is that the fixture calls some tools before their own phase runs,
so those phases assert against an artifact rather than creating one.

## Adding a tool

The `coverage` phase compares what the run actually called against the
server's `tools/list`. **A new tool with no phase fails the suite.** Add
`<tool_name>_test.go` with a `phase*` function and wire it into
`TestE2E_AllTools`.

Coverage is a runtime check, not a directory listing: it counts what
`f.call` sent. A file that exists but never reaches its tool fails just
as loudly as a missing one.

## Notes for maintainers

**Status: run against `mcp-testnet.nvnmchain.io` (nvnm-testnet-1) on
2026-08-17.** 23/23 tools covered. Three phases fail, all of them reporting
real defects rather than suite bugs:

| Failing phase | What the server does |
|---|---|
| `anchor_get_registry` | `creator` comes back bech32 (`nvnm1d3s…`), not the `0x` address the field is documented as — and the same docs tell callers to disambiguate duplicate registry names on it. |
| `evm_get_transaction` | `block_number` and `block_hash` are both absent for a transaction that already has a receipt, with `is_pending` false. A caller cannot locate a mined transaction from the response. |
| `evm_get_transaction_receipt` | A lookup for a hash that was never broadcast does not answer within the 30s client timeout. The harness polls this tool on every write expecting a prompt not-found, so this is on the path every write already takes. |

A fourth phase used to fail here — `anchor_prepare_update_record_status`
rejected the documented index-omitted call. That is fixed in this
repository, and the two status phases pass against it; they were verified
by running the built server locally against the same testnet chain
(`NVNM_MCP_TEST_SERVER_URL=http://127.0.0.1:8181`), because a deployment
still carrying the old build will keep failing them until it is updated.
What those phases now pin is described next.

**`index` is optional, and both `0` and omitting it mean "latest".** The
tool schema and `docs/TOOL_REFERENCE.md` call `index` optional,
"default: latest", and the server implements that default rather than
passing the caller's silence through to the ABI. It cannot pass it
through: `abi/anchoring.json` declares a plain `uint64`, version indexes
are 1-based, and the `0` an omitted index would encode as names no
version at all — gas estimation reverts on it. So
`anchor.PrepareUpdateRecordStatus` takes `Index *uint64` and resolves nil
and `0` alike with one `records` read before packing the call
(`internal/anchor/prepare.go`). The transaction a caller signs therefore
always carries a concrete index, and a record with no version to update
is rejected up front with `record not found` instead of an opaque
gas-estimation revert.

That makes the write path agree with the read path, which is where the
"default: latest" wording came from: `anchor_get_records` also treats
`index=0` as "latest" (`internal/anchor/client.go`), pinned by
`phaseGetRecords`. Both tools now spell "latest" the same two ways.

`phaseUpdateRecordStatus` covers the single-version record: it makes the
documented call, requires it to land, and reads the status back. The
version distinction cannot be drawn there — with one version, "latest",
"first" and "whatever `0` selects" are all the same row — which is why
`phaseUpdateRecordStatusVersioned` exists. It anchors three versions of
one document and updates the latest and a historical one, reading every
version back by `(registry_id, record_id, index)` after each write — the
only lookup mode that can see a superseded version, and so the only way
to know *which* row moved.

Its four cases assert different things on purpose. `index_omitted` must
work, because the schema promises it, and must land on the latest
version — with three rows, that is finally an attributable claim.
`index_zero` asserts that `0` behaves *identically* to the omitted form,
which is the chain-level statement that the two spellings resolve to one
version rather than two. `explicit_latest_index` must work, because
changing a record's current status is the tool's whole job.
`explicit_historical_index` presumes no answer: the precompile accepts it
today (statuses are per-version and independent), but if a future rule
forbids it, the case records the rejection and asserts nothing moved.
What every case asserts either way is that the write agreed with the
read — landed, and only the named version changed; rejected, and nothing
changed at all. A call that reports success while moving a different
version is the failure they exist to catch.

An earlier revision of this file claimed a missing index "means the latest
version", claimed an explicit index "is rejected on chain", and told
maintainers not to pass `rec.Index`. All three were false; none had been
verified. The first is true now only because the server was changed to
make it true.

**Versions are keyed by checksum, not by URI.** `anchor_prepare_add_record`
hardcodes `recordId=0` and `index=0` in the record tuple and exposes no
"new version of record N" input, so the precompile decides, and it decides
on content: re-anchoring the *same* checksum bumps the same `record_id` to
the next index, while the same URI under a different checksum creates an
unrelated record. That is why `anchorVersions` varies only the metadata
between calls. If the chain ever stops versioning that way, the phase
fails with the observed record IDs rather than quietly testing three
unrelated records.

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
