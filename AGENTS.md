# AGENTS.md — Development Guide for AI Agents and Contributors

This file is the entry point for AI coding agents (and a quick orientation
for humans) working on this repository. It defines the scope of the
project, the non-negotiable practices, and the testing requirements every
change must meet. It complements — and defers to —
[CONTRIBUTING.md](CONTRIBUTING.md),
[docs/standards/CODING_STANDARDS.md](docs/standards/CODING_STANDARDS.md),
and [docs/TESTING_UNIT.md](docs/TESTING_UNIT.md) (automated suite) plus
[docs/TESTING.md](docs/TESTING.md) (manual local e2e); read those before large
changes.

## What this server is

A Go-based [Model Context Protocol](https://modelcontextprotocol.io/)
server exposing the NVNM Chain (an Inveniam L2 on MANTRA) through a
curated set of typed MCP tools: EVM reads, anchor-precompile reads, and
prepare/broadcast write flows. Entrypoint: `cmd/nvnm-mcp-server`; core
logic under `internal/` (see "Project Structure" in the README).

## Architectural invariants (never violate)

These are deliberate and load-bearing. PRs that break them get closed.

1. **Zero key custody.** The server never holds signing keys. Write tools
   return unsigned transactions; signing is caller-side. See
   `docs/KEY_CUSTODY_THREAT_MODEL.md`.
2. **Privacy-by-design.** No end-user personal data is collected or
   stored; the only persisted identity material is hashed API-key
   entries. See `docs/DATA_HANDLING.md`.
3. **No internal orchestration.** Tools return `next_actions` hints;
   the server never calls other tools internally.
4. **Multi-instance, not multi-chain.** Chain identity is pinned at
   startup (`NVNM_CHAIN_ID` + `NVNM_CHAIN_ENVIRONMENT`); no same-session
   chain switching. See `docs/DESIGN.md`.
5. **Fail fast, no silent fallbacks.** Do not mask misconfiguration with
   defaults. Errors surface immediately. See CODING_STANDARDS "Core
   Principles".

## Hard rules for every change

- **Go 1.26+, vendored dependencies.** Build and test with
  `-mod=vendor` (CI does). Adding a dependency requires `go mod tidy &&
  go mod vendor`, a license compatible with the CI allowlist (no GPL/
  LGPL/AGPL), and a clean `govulncheck ./...`.
- **SPDX license headers.** Every `.go` file under `cmd/` and
  `internal/` must start with
  `// SPDX-License-Identifier: Apache-2.0` (line 1) followed by the
  copyright line. CI enforces via `scripts/check_license_headers.sh`;
  fix with `scripts/add_license_headers.sh`.
- **Lint clean under golangci-lint v2.11.4** (the CI-pinned version) with
  the repo's `.golangci.yml`. Run `make lint`. Keep lines ≤120 chars.
- **No secrets in the tree.** `detect-secrets` runs in pre-commit and CI
  against `.secrets.baseline`. Never commit keys, DSNs with real
  credentials, or `.env`.
- **Conventional Commits + DCO sign-off** (`git commit -s`). CI rejects
  commits without the `Signed-off-by:` trailer. `--no-verify` is
  forbidden.
- **Env vars use the `NVNM_*` prefix.** The legacy `INVENIAM_*` prefix is
  hard-rejected at startup; do not re-introduce it.

## Testing requirements — every new feature

CI gates every PR on the full suite passing with the race detector AND on
**total statement coverage ≥ 80%** (`scripts/check_coverage.sh`, mirrored
locally by `make coverage-check`). A feature is not done until:

1. **Unit tests** cover the new code's success *and* error paths,
   alongside the source (`internal/foo/foo.go` → `internal/foo/foo_test.go`,
   no build tag). Table-driven where cases share a shape. Hermetic: no
   network, no live chain — use the existing mocks/fakes
   (`mockEVM`/`mockAnchor` in `internal/mcp/tools_test.go`, the fake
   `defiRPCClient` pattern in `internal/evm`, `httptest` servers for
   HTTP surfaces).
2. **API/E2E tests** exist for anything that changes the MCP surface.
   New MCP tool → register it in the E2E expectations
   (`internal/mcp/server_test.go`) and add invocation tests through the
   real HTTP transport + MCP SDK client
   (`internal/mcp/server_e2e_test.go`). New admin/HTTP endpoint → E2E
   tests in `internal/mcp/admin_test.go` style. Auth/RBAC changes →
   default-deny cases included.
3. **Golden tests** protect any new/changed JSON response shape
   (`testdata/*.golden.json`; delete + re-run to regenerate, review the
   diff).
4. **Integration tests** (build tag `integration`, run via
   `make test-integration`) when the change crosses the live-testnet
   boundary. These must skip cleanly without credentials.
5. **Coverage holds ≥ 80% total.** Run `make coverage-check` before
   pushing. Don't chase the number with vacuous assertions — cover the
   branches that matter (error paths, boundary conditions, auth
   denials).

Postgres-backed tests in `internal/mcp` are gated on `NVNM_TEST_PG_DSN`
and skip when unset; set it locally (any disposable Postgres 16) to
exercise that surface — CI always does.

## Local verification loop

```sh
make setup-dev        # once: dev tools + pre-commit hooks
make test             # fast: unit + MCP E2E, no network
make coverage-check   # -race + coverage report + 80% gate (what CI enforces)
make check-all        # format + vet + lint
pre-commit run --all-files
```

Before declaring any task complete: `make check-all && make coverage-check`
must both pass, and `go build -mod=vendor ./...` must succeed.

## Full tool sweep (live MCP tool exercise)

`make test` and `make coverage-check` do not answer "does every tool
actually work when a Claude-class client calls it against a live chain?"
Three failure classes escape the automated suite entirely:

- **Output-schema violations.** Tool output is validated against the
  schema the MCP SDK infers from the Go envelope type. A struct field
  whose Go type marshals differently than the inferred schema expects
  (classically `[]byte` → base64 *string* vs. an inferred `array`) fails
  validation only when a response actually populates that field — which
  hermetic mocks usually don't.
- **Error-message quality.** Whether a failure reaches the caller as an
  actionable message or as a bare `upstream operation failed` is only
  visible against a real chain that actually rejects things.
- **Description quality.** Whether the tool descriptions lead an agent to
  the right tool with the right arguments is only observable by having an
  agent do it.

Run this sweep after changing any tool's schema, envelope, description, or
error handling, and before any release.

### Target environment

The sweep runs against the **local server on the chain `.env` currently
points at** — normally `make run-http` on `:8080`. Do not substitute a
hosted or demo endpoint: the point is to exercise the build in the working
tree.

Two things that bite:

- **Check which chain you are actually on.** `.env` carries both testnet
  and mainnet blocks. `evm_get_chain_id` (787111 testnet / 1611 mainnet)
  is the cheapest confirmation, and every tool reporting
  `chain_environment` or a token symbol (`wmantraUSD` / `wmmUSD`)
  corroborates it. Record it in the run header — the same tool passes on
  one chain and fails on another.
- **Claude Code caches `tools/list` at connection time.** After any change
  to a tool's name, schema, or description: rebuild, restart the server,
  then `/mcp` to reconnect. Behavior-only changes need just the restart.

### Phase coverage

Phases A–F are read-only and run autonomously. Phase G broadcasts real
transactions and is **testnet-only** (see below).

**Phase A — identity reads (5 tools).** `nvnm_overview`,
`evm_get_chain_id`, `anchor_info`, `wallet_status`, and
`nvnm_setup_wizard` twice (no address → `needs_wallet`; with address →
whichever of `unfunded` / `funded_unused` / `funded_active` applies).
Confirms chain identity, precompile address, `abi_loaded:true`, and
`method_count`.

**Phase B — anchor reads (4 tools).** Chain real IDs through the calls
rather than inventing them:

| # | Tool | Case |
|---|---|---|
| B1 | `anchor_get_registries` | `limit=5` — first page, captures a cursor |
| B2 | `anchor_get_registries` | `offset=5, limit=5` — pagination continuity |
| B3 | `anchor_get_registries` | `name=<from B1>` — by-name scan (walks the whole table via cursors) |
| B4 | `anchor_get_registry` | single lookup by an `id` from B1 |
| B5 | `anchor_get_records` | by `registry_id` |
| B6 | `anchor_get_records` | by `checksum` alone — cross-registry mode |
| B7 | `anchor_get_records` | unfiltered, `limit=5` |

Checks: no field shift (`registry_id` / `record_id` / `checksum` mutually
coherent), page N's last id + 1 equals page N+1's first id,
`content_trust` warning present, and `pagination.next_key` is a **string**
(the regression guarded by `internal/mcp/output_schema_test.go`).

`pagination.total` is mode-dependent and must be read that way -- it is
not a single quirk to confirm. A chain-paged listing reports `0` even
while returning rows, because this chain never populates the count, so
the walk has to terminate on an empty `next_key` and a caller cannot use
`total` to size the table. A name-filtered listing reports the **real**
match count, because the scan is client-side and has counted every match
before returning a window of it (`name=us-ca, match=prefix` →
`total: 37`, no cursor). Treating either number as the other's semantics
is the mistake to watch for: a sweep that asserts "total is always 0"
will now fail on the name path, and code that trusts `total` on the
listing path will conclude a populated table is empty.

**Phase C — EVM reads (8 tools).** `evm_get_block` latest and by hash from
that result; `evm_get_balance`; `evm_get_code` on the precompile;
`evm_get_transaction` + `evm_get_transaction_receipt`; `evm_get_logs` over
a narrow range; `evm_call_contract` with raw calldata against the
precompile. On an idle chain the tx/receipt/log happy paths are
unreachable — that is `partial`, not `pass` (see § *Report the results*).

**Phase D — setup verification (2 tools, 4 cases).** Both tools on both
sides: `nvnm_setup_verify_hash` pass and mismatch, and
`nvnm_setup_verify_signature` pass and wrong-signer. The mismatch cases
matter most — they must return `ok:false` **with** `expected`/`got` or
`recovered_address` and a remediation hint, not an error.

No tooling is needed for the hash: the challenge is
`sha256(lowercase(address) + ":nvnm-setup-challenge-v1")` and the
submitted hash is `sha256` of that challenge string including its `0x`
prefix, both reproducible with `shasum -a 256`. The signature needs a real
EIP-191 signature; the vendored `github.com/defiweb/go-eth/wallet` signs
one from a throwaway key in a few lines of `go run -mod=vendor`. Delete
any such scratch program before committing.

**Phase E — prepare tools, happy path (5 tools).** All five
`anchor_prepare_*` tools. No signing, no broadcast — assert `raw_tx` and
`wallet_tx_request` are both present and structurally valid, and that the
method selector is unchanged (`anchor_prepare_add_registry` →
`0x318b38b1`, `anchor_prepare_add_record` → `0x64d25295`).

These tools are **not registered unless `ENABLE_WRITE_TOOLS=true`**, which
`.env` deliberately leaves off. Rather than flipping it, run Phase E
against an ephemeral second instance on a spare port and tear it down
afterwards — the main server and `.env` stay untouched:

```sh
set -a && . ./.env && set +a && \
  MCP_HTTP_ADDR=:8280 METRICS_ADDR=:9290 ENABLE_WRITE_TOOLS=true \
  ./bin/nvnm-mcp-server --transport http > /tmp/e.log 2>&1 &
EPHEMERAL_PID=$!
# ... run Phase E/F against :8280 ...
kill "$EPHEMERAL_PID"
```

Kill the captured PID, never the port (`lsof -ti:8280 | xargs kill` would
take out whatever happens to own 8280 if startup failed or the port was
reused — see *Kill by PID, never by port* below).

Never call `evm_send_raw_transaction` from that instance. It is registered
alongside the prepare tools and it broadcasts.

**Phase F — error cases.** Regression checks on curated messages:

| # | Call | Expected |
|---|---|---|
| F1 | `anchor_prepare_add_record` with `metadata: "{}"` | curated empty-JSON-object rejection |
| F2 | `anchor_prepare_add_record` with a 100-char checksum | curated "exceeds the maximum length" |
| F3 | `evm_call_contract` with `from: "not-an-address"` | input-validation error naming the field |
| F4 | `evm_get_transaction` with a nonexistent hash | `transaction not found` (not `is_pending:true`) |
| F5 | `evm_get_logs` with `from_block=1, to_block=latest` | range-too-wide error with a retry hint |
| F6 | `anchor_prepare_add_record` on a registry the `from` address does not own | role denial — probe whether it is curated or bare |

**Phase G — write cycle. Testnet only; never run on mainnet.** Requires a
funded wallet and a local signer, and broadcasts ~5 real transactions:
prepare → sign → `evm_send_raw_transaction` →
`evm_get_transaction_receipt` → read back, closing the loop on all five
write-path tools. Skip it
automatically when the signing prerequisites are absent, and skip it
unconditionally when `NVNM_CHAIN_ENVIRONMENT=mainnet`. Signing is
caller-side: pass the tool's `wallet_tx_request` to a local signer that
reads its key from a gitignored file or the OS keychain. Never write a
private key — not even a truncated one — into this repo.

### State the sweep changes, and restore it

A full sweep is not read-only. It mutates the working tree, the key store,
the server, and -- in Phase G -- the chain. Track every change as you make
it and restore it at the end; the report must state anything that cannot be
undone.

**Restore afterwards:**

- `.env` -- the chain block and `ENABLE_WRITE_TOOLS`. Phase G needs the flag
  on; leaving it on is how a later `make run-http` ends up serving broadcast
  tools against whatever chain `.env` next points at.
- **API keys.** Phase E/G's role tools require an `admin` **API key**, which
  is separate from the signing wallet's on-chain role. If you create one
  (`make key-create NAME=sweep-admin ROLES=admin`), disable it when done
  (`make key-disable`). The key store is read at boot, so both operations
  need a server restart to take effect.
- **Throwaway signers and credentials.** A local signer written to sign
  Phase G transactions is scratch: delete it before committing, and never
  write a private key -- not even a truncated one -- into a tracked file.
  `.chain_credentials.txt` is gitignored; keep it mode `0600`.

**Cannot be undone -- report it:** Phase G writes are permanent. Record the
registry and record IDs created, the transaction hashes, the nonce range,
and the gas spent, so a later reader can tell sweep artifacts apart from
real data.

**Operating the server:**

- **Kill by PID, never by port.** Ports move: sourcing `.env` changes the
  metrics port, and an ephemeral instance can end up on the port you assumed
  was yours. `pgrep -f "bin/nvnm-mcp-server --transport http"` first.
- **Restart after:** a rebuild, a key-store change, or an `.env` edit.
- **Reconnect (`/mcp`) after:** any change to a tool's name, input schema, or
  description. The client caches `tools/list` at connection time, so calls
  keep working with stale schemas and the description-quality dimension of
  the sweep silently goes unexercised. Note it in the report when you could
  not reconnect.

**Driving the server over HTTP:** responses are SSE-framed and a large
payload arrives as multiple `data:` frames, so `sed -n 's/^data: //p'` alone
yields concatenated JSON that fails to parse. Join the frame payloads and
decode a single value (`json.JSONDecoder().raw_decode`). A parse failure
here looks exactly like a tool failure in the results table -- do not record
one as the other.

### Diagnosing opaque failures

`apperrors.SafeForClient` collapses upstream errors to
`upstream operation failed`, and `NewMCPMiddleware` records the real
error **on the OTel span only** — it never reaches the logs, which show
just `"status":"error"`. To see the cause, start a second instance on a
spare port (leaving the one you were using untouched) with the stdout
trace exporter on:

```sh
set -a && . ./.env && set +a && \
  MCP_HTTP_ADDR=:8280 METRICS_ADDR=:9290 \
  ENABLE_STDOUT_TELEMETRY=true LOG_LEVEL=debug \
  ./bin/nvnm-mcp-server --transport http > /tmp/srv.log 2>&1 &

MCP_API_KEY=<raw key> MCP_HTTP_ADDR=:8280 \
  make mcp-probe TOOL=<failing_tool> ARGS='{...}'

grep -A4 exception.message /tmp/srv.log   # spans batch; allow ~5s
```

Always do this before concluding *why* something failed. Two different
root causes flatten to the identical client-facing string, and one can
mask the other — a call that fails validation will keep failing for that
reason even after the permission problem behind it is fixed.

`make mcp-probe` reproduces a failure without a client in the loop, which
separates "the server rejects this" from "the client sent something
unexpected". To inspect a generated output schema directly, `POST` a
`tools/list` and read `result.tools[].outputSchema`.

### Report the results in this exact format

Every sweep — whether run by a person, by Claude, or by a subagent —
reports as the run header plus the table below. Do not summarize in prose
instead; a reader has to be able to see which tools were touched, with
what, and what came back.

Rules that make the table trustworthy:

- **List every tool, always**, including the ones that passed, in phase
  order. A tool silently missing from the table is indistinguishable from
  a tool that was never registered.
- **Never report a tool as `pass` on an error-path call alone.** If the
  chain had no data to exercise the happy path, that is `partial`, and
  the gap goes in the "Not covered" list.
- **Evidence, not adjectives.** Quote the field or value that proves the
  result — `chain_id: 1611`, `selector: 0x318b38b1`, the actual error
  string.
- **Root-cause every failure from the span**, not from the sanitized
  message.

**Run header** (all five lines, every time):

```
Chain:    1611 (mainnet) via https://evm.nvnmchain.io
Build:    <git rev> + working tree, binary built <time>
Server:   make run-http on :8080, ENABLE_WRITE_TOOLS=false
Surface:  17 of 23 tools registered (+ ephemeral :8280 for Phase E/F)
Date:     <UTC>
```

**Results table** (example rows — keep the columns and the legend):

| # | Tool | Case | Result | Evidence / notes |
|---|---|---|---|---|
| A2 | `evm_get_chain_id` | `{}` | pass | `chain_id: 1611`, block 2,386,211 |
| B1 | `anchor_get_registries` | `limit=5` | pass | ids 1–5, `next_key: "AAAAAAAAAAY="` (string) |
| C5 | `evm_get_transaction` | nonexistent hash | partial | `transaction not found` — chain idle, no happy path |
| E3 | `anchor_prepare_update_record_status` | happy path | **FAIL** | bare `upstream operation failed`; span: `index cannot be zero` |

Legend — `pass`: happy path exercised and correct. `partial`: tool
responded correctly but only the error/empty path was reachable.
**`FAIL`**: wrong result, or an error that is not the correct answer.

Close with a tally and an explicit gap list:

```
36/36 calls — 30 pass, 4 partial, 2 FAIL
Not covered: evm_get_transaction_receipt happy path (idle chain);
             Phase G (mainnet — testnet only, no credentials)
```

(36 is the full A–F call count — A: 6, B: 7, C: 8, D: 4, E: 5, F: 6 — and
the pass/partial/FAIL categories must sum to the total.)

### Findings ledger

Carry a ledger across sweeps so fixed items stay fixed and open items get
re-probed. Update the status column every run.

| Finding | Description | Probe | Status |
|---|---|---|---|
| C1 | `update_record_status` / `revoke_role` unreachable (auth-exempt gap) | E3, E5 | Resolved — both callable; failures are chain-level permission denials |
| C6 | `next_actions` says "by id **or name**" when `anchor_get_registry` is ID-only | B1 | Resolved — split into two hints, name pointing at `anchor_get_registries` |
| C7 | Role denial surfaces as bare `upstream operation failed` | E2, E3, F6 | Resolved — `unauthorized` maps to `ErrPermissionDenied`; message distinguishes chain-side from API-key authorization |
| C8 | `evm_get_code` precompile hint says "inspect its balance instead" | C4 | Resolved — precompiles point at `anchor_info`; ordinary accounts keep the balance hint |
| C9 | `evm_get_balance` labels the amount `ether` | C3 | Resolved — `balance_human` + `token_wrapped` added; `ether` kept as a documented legacy alias |
| C10 | `revoke_role` description says "granting" | E5 | Resolved — now "remove admin or editor permissions" |
| C12 | `balance_human` float64 precision artifact | post-write | Not reproduced — testnet Phase G showed full 18-dp precision (`29.899510860000000000`) |
| C13 | `anchor_prepare_update_record_status` declares `index` optional; omitting it sends `0`, which the precompile rejects | E3 | Resolved — `index` required and 1-based; rejected by schema before any chain call |
| C14 | RBAC denial did not say whether the chain or the API key refused | E4, E5 | Resolved — message names the caller's credential (API key or JWT) as the server-side principal |

Then act on it: every failure gets a root cause from the span, a
regression test in the layer that should have caught it, and a
`CHANGELOG.md` entry. A tool that fails only on non-empty pagination, or
only when a caller omits an "optional" field, is still a broken tool.

## CI pipeline (what a PR must survive)

`.github/workflows/ci.yml`, on every PR: `go vet` → license headers →
golangci-lint (pinned) → license scan (allowlist) → `govulncheck` →
`go test -mod=vendor -race -coverprofile` (with a Postgres 16 service) →
**coverage gate ≥ 80%** → build. A separate job runs `detect-secrets`
against the baseline, and `dco.yml` enforces per-commit sign-off.

## Practices for agents specifically

- **Read before writing.** Check method signatures, the ABI
  (`abi/anchoring.json`), and neighboring tests before coding — do not
  guess interfaces. Match the style of the package you're editing.
- **Don't weaken the gates.** Never lower the coverage threshold, add
  `//nolint` without a reason, extend the secrets baseline to sneak a
  value through, or delete failing tests to get green.
- **Keep tests deterministic.** `-race -count=1` must pass; no sleeps as
  synchronization, no dependence on wall-clock, network, or test order.
- **Update the docs that own the surface you changed:**
  `docs/TESTING_UNIT.md` for new test layers/helpers, `docs/TESTING.md` for
  changes to the local e2e / MCP-client setup, `docs/TOOL_REFERENCE.md` for tool changes,
  `README.md` for operator-facing config, `CHANGELOG.md` under
  Unreleased.
- **Dependency updates (e.g. Dependabot):** treat the full CI suite as
  the safety net it is — run `make coverage-check` and `govulncheck`
  locally against the bump; check the dep's changelog for breaking
  changes in APIs this repo actually calls before approving.
