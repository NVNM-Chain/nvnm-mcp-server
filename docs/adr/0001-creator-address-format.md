# Registry `creator` stays bech32; a derived `creator_evm` hex field is added

The anchor precompile reports a registry's `creator` as a Cosmos bech32
string (`nvnm1…`), while every EVM tool on this server takes `0x` hex
addresses — feeding the advertised "list registries, then inspect the
creator's wallet" journey the raw value failed with `invalid Ethereum
address` (finding P1). We decided to return **both representations**:
`creator` stays exactly what the chain says (truthful, greppable against
explorers and chain state), and `creator_evm` is derived at the client
boundary from the bech32 payload — the same 20 bytes on this chain — for
use with `wallet_status` / `evm_get_balance` / `evm_get_code`.

## Considered options

1. **Convert bech32 → hex in `creator` itself** — rejected: it would
   silently change an existing field's meaning and hide the chain-native
   identity that operators see in Cosmos tooling.
2. **Document `creator` as opaque and drop the EVM journey** — rejected:
   the payload *is* the EVM account, so refusing to derive it degrades a
   working journey for no gain.
3. **Return both** — chosen: additive (no breakage), each consumer gets
   the form it needs.

## Consequences

- `creator_evm` is omitted (never invented) when the chain value cannot
  be decoded as bech32 with a 20-byte payload; see
  `internal/anchor/creator_evm.go`.
- Golden fixtures, MCP mocks, and integration assertions pin the format
  of both fields; the stale pre-rename `inveniam1` fixtures are gone.
- `github.com/btcsuite/btcd/btcutil/bech32` (already vendored,
  ISC-licensed) became a direct dependency.
