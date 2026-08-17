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
2026-08-17.** 23/23 tools covered. Four phases fail, all of them reporting
real defects rather than suite bugs:

| Failing phase | What the server does |
|---|---|
| `anchor_prepare_update_record_status` (and `..._versioned/index_omitted`) | Omitting `index` is rejected, though the schema documents it as optional and defaulting to the latest version. See below. |
| `anchor_get_registry` | `creator` comes back bech32 (`nvnm1d3s…`), not the `0x` address the field is documented as — and the same docs tell callers to disambiguate duplicate registry names on it. |
| `evm_get_transaction` | `block_number` and `block_hash` are both absent for a transaction that already has a receipt, with `is_pending` false. A caller cannot locate a mined transaction from the response. |
| `evm_get_transaction_receipt` | A lookup for a hash that was never broadcast does not answer within the 30s client timeout. The harness polls this tool on every write expecting a prompt not-found, so this is on the path every write already takes. |

**Omitting `index` from `anchor_prepare_update_record_status` does not
work, and there is no way to say "latest".** The tool schema and
`docs/TOOL_REFERENCE.md` both call `index` optional, "default: latest".
Nothing implements that default: `index` is a plain `uint64` from
`prepareUpdateRecordStatusInput` through
`anchor.PrepareUpdateRecordStatusRequest` to `EncodeArgs`, and
`abi/anchoring.json` declares it a required `uint64`, so an omitted index
goes on the wire as a literal `0` — indistinguishable from a caller who
typed `"index": 0`. The chain confirms both halves: the two forms fail
identically (gas estimation reverts, so the tool rejects the call before
anything is signed), and an explicit `index` matching a real version
succeeds. Version indexes are 1-based, so `0` is no version at all.

Note the asymmetry with the read path, which is where the "default:
latest" wording presumably came from: `anchor_get_records` *does* treat
`index=0` as "latest" — it takes `*uint64` and checks for nil
(`internal/anchor/client.go`) — while the write path has no such
optionality. Reading works the way the docs describe; writing does not.

`phaseUpdateRecordStatus` covers the single-version record. The
distinction cannot be drawn there — one version means "latest", "first"
and "whatever `0` selects" would all be the same row — so that phase tries
the documented form, reports its rejection, and then does the update the
way a caller actually has to, so the read-back assertion still runs.
`phaseUpdateRecordStatusVersioned` is where the readings come apart: it
anchors three versions of one document and updates the latest and a
historical one, reading every version back by
`(registry_id, record_id, index)` after each write — the only lookup mode
that can see a superseded version, and so the only way to know *which* row
moved.

Its four cases assert different things on purpose. `index_omitted` must
work, because the schema promises it (this is the failing one).
`index_zero` asserts only that it behaves *identically* to the omitted
form, which is what makes "an omitted index is encoded as `0`" a fact
about the chain rather than a reading of the code. `explicit_latest_index`
must work, because changing a record's current status is the tool's whole
job. `explicit_historical_index` presumes no answer: the precompile
accepts it today (statuses are per-version and independent), but if a
future rule forbids it, the case records the rejection and asserts nothing
moved. What every case asserts either way is that the write agreed with
the read — landed, and only the named version changed; rejected, and
nothing changed at all. A call that reports success while moving a
different version is the failure they exist to catch.

An earlier revision of this file claimed a missing index "means the latest
version", claimed an explicit index "is rejected on chain", and told
maintainers not to pass `rec.Index`. All three are false; none had been
verified.

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
