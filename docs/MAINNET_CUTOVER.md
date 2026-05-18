# Mainnet Cutover Playbook

> Reflects code as of commit `c8f10e4`, 2026-05-18.

This playbook documents the sequence for moving a running deployment from the
NVNM Chain testnet (`nvnm-testnet-1`, EVM chain ID `787111`) to mainnet
(`nvnm-1`, EVM chain ID `1611`). Execution is **Phase 10**; this doc exists so
Phase 10 has a checklist and a rollback path, not a design discussion mid-flight.

Audience: the operator (Inveniam DevOps) who will run the cutover in a staging
cluster first and then in production.

Cross-references:

- [`docs/PHASE_9_DESIGN.md` § 3.11](PHASE_9_DESIGN.md) — the spec this doc satisfies.
- [`docs/RUNBOOK.md` § Env var migration](RUNBOOK.md#env-var-migration) — the
  Phase 8.9 hard cut that this cutover assumes is already in place.
- [`docs/DESIGN.md` § Target Chain](DESIGN.md) — canonical testnet/mainnet
  property table; the source of truth for the identifiers below.
- [`deploy/helm/nvnm-mcp-server/values.yaml`](../deploy/helm/nvnm-mcp-server/values.yaml)
  and [`deploy/k8s/configmap.yaml`](../deploy/k8s/configmap.yaml) — the two
  manifests an operator edits.

The chain swap is a **config change, not a code change**. The same compiled
binary serves both networks; chain choice is pinned at startup via three env
vars. There is no per-session switching and no orchestration cleanup needed on
rollback. See [`docs/DESIGN.md` § 8 Multi-chain](DESIGN.md) for the reasoning.

---

## 1. Preconditions

This section is the gate that must be green before any ConfigMap is touched.
The operator should be able to tick every box from upstream evidence (chain
explorer, DNS, ops tickets), not from the MCP server itself — the server is
the thing being moved, not the source of truth for whether mainnet is ready.

- [ ] **Mainnet EVM RPC endpoint live and reachable.** Per
  [`docs/DESIGN.md` § Target Chain](DESIGN.md), the canonical URL is
  `https://evm.nvnmchain.io`. Validate with a plain JSON-RPC probe before
  proceeding:

  ```sh
  curl -sS -X POST https://evm.nvnmchain.io \
    -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}'
  # Expect: {"jsonrpc":"2.0","id":1,"result":"0x64b"}   # 0x64b = 1611
  ```

- [ ] **Mainnet anchor precompile present at the canonical address.** Per
  `docs/DESIGN.md`, both networks expose the precompile at
  `0x0000000000000000000000000000000000000A00`. Confirm via
  `eth_getCode` against the mainnet RPC — a non-`0x` response indicates the
  precompile is active.

- [ ] **FusionAuth tenant configured for mainnet** *if* `AUTH_PROVIDER=fusionauth`
  is in use. JWKS endpoint reachable from the target cluster; role mappings
  (`automation` → `auto`, others → `required`) replicated from the testnet
  tenant. Skip this gate if `AUTH_PROVIDER=apikey` (the default).

- [ ] **DNS for `mcp.nvnmchain.io` cut over** per issue #18 Track F. The
  mainnet instance is reachable at the production hostname before traffic is
  flipped to it; testnet stays at `mcp.testnet.nvnmchain.io`.

- [ ] **Operator confirmed as the data controller** for both production
  traffic and operational logs at the mainnet endpoint. The server emits
  structured request logs and Prometheus metrics; the operator owns
  retention, access, and any required notices.

- [ ] **Sandbox cluster available.** Section 3 starts here; never flip
  production directly.

- [ ] **Legacy env-var hygiene.** Per [Phase 8.9 / RUNBOOK § Env var migration](RUNBOOK.md#env-var-migration),
  the server **hard-rejects** any `INVENIAM_EVM_RPC_URL`,
  `INVENIAM_EVM_ARCHIVE_RPC_URL`, or `INVENIAM_CHAIN_ID` set in the
  environment, even alongside the `NVNM_*` equivalent. Grep every layer of
  the mainnet ConfigMap / Helm overlay / secrets manager for the
  `INVENIAM_` prefix before deploy; a stale key is a startup failure, not a
  silent drift.

---

## 2. Config changes

This section is the edit list. Three env vars change; everything else
(precompile address, transport settings, auth wiring, observability) stays
identical. The operator applies these edits to the ConfigMap and Helm
overlay, then deploys.

### Pinned-at-startup env vars

| Variable | Testnet (current) | Mainnet (target) |
|---|---|---|
| `NVNM_EVM_RPC_URL` | `https://evm.testnet.nvnmchain.io` | `https://evm.nvnmchain.io` |
| `NVNM_CHAIN_ID` | `787111` | `1611` |
| `NVNM_CHAIN_ENVIRONMENT` | `testnet` | `mainnet` |

`NVNM_CHAIN_ENVIRONMENT` is the disambiguating label that lets the
per-instance chain pin survive a chain-ID typo (the server cross-validates
the three at startup). Do not omit it.

### ConfigMap diff — [`deploy/k8s/configmap.yaml`](../deploy/k8s/configmap.yaml)

```diff
 data:
-  NVNM_CHAIN_ID: "787111" # current testnet (nvnm-testnet-1)
+  NVNM_CHAIN_ID: "1611"   # mainnet (nvnm-1)
+  NVNM_CHAIN_ENVIRONMENT: "mainnet"
```

`NVNM_EVM_RPC_URL` is conventionally injected via the companion Secret (see
`deploy/k8s/secret.yaml.example`), not the ConfigMap, because it may carry an
embedded provider API key. Update it in the secrets manager / sealed-secret
of choice, not in this file.

### Helm values diff — [`deploy/helm/nvnm-mcp-server/values.yaml`](../deploy/helm/nvnm-mcp-server/values.yaml)

```diff
 env:
-  NVNM_EVM_RPC_URL: ""
-  NVNM_CHAIN_ID: "787111"
+  NVNM_EVM_RPC_URL: "https://evm.nvnmchain.io"
+  NVNM_CHAIN_ID: "1611"
+  NVNM_CHAIN_ENVIRONMENT: "mainnet"
```

If the deployment uses a values overlay (`-f values.mainnet.yaml`) rather
than editing the chart default, mirror the same three keys there and leave
the chart default at testnet — that pattern is easier to roll back.

### Legacy prefix — hard rejected

The `INVENIAM_EVM_RPC_URL` / `INVENIAM_EVM_ARCHIVE_RPC_URL` /
`INVENIAM_CHAIN_ID` prefix was hard-cut in Phase 8.9. The server fails loud
at startup with `ErrLegacyEnvVars` and a pointer back to
[`docs/RUNBOOK.md#env-var-migration`](RUNBOOK.md#env-var-migration) if any
of those keys is set — even when the matching `NVNM_*` is also set. Search
the mainnet overlay for the `INVENIAM_` substring before the first deploy:

```sh
git grep -n 'INVENIAM_' deploy/  # expect zero hits
```

A non-empty result means the next deploy will not start.

---

## 3. Validation sequence

This section is the dry run that the operator runs against a sandbox cluster
first and then repeats post-cutover in production. The server holds no keys
and no per-user state, so every check below is a read against the mainnet
endpoint via the MCP tool surface or the HTTP health ports. None of these
checks mutate chain state.

### 3.1 Pre-cutover (sandbox cluster)

- [ ] **Start a fresh deployment in a sandbox cluster** with the Section 2
  edits applied. Do not flip production traffic yet.

- [ ] **Run integration tests against mainnet.** Export an operator-controlled
  mainnet test address + private key (do **not** commit key bytes; pass via
  the same shell env as testnet):

  ```sh
  export NVNM_EVM_RPC_URL=https://evm.nvnmchain.io
  export NVNM_CHAIN_ID=1611
  export NVNM_CHAIN_ENVIRONMENT=mainnet
  export NVNM_TEST_PRIVATE_KEY=<mainnet test key — operator-controlled>
  export NVNM_TEST_ADDRESS=<address derived from above>
  make test-integration
  ```

  All testnet integration tests must pass against mainnet. A failure here
  almost always means an RPC-shape regression or a missing precompile, not
  a server bug.

- [ ] **`nvnm_overview` returns mainnet identifiers.** Call the tool through
  an MCP client; verify the response contains `chain_id=1611` and
  `environment=mainnet`. Anything else means a startup-time chain-pin
  mismatch (the server should have already refused to start in that case,
  so a discrepancy here is a serious bug).

- [ ] **`wallet_status` against a known mainnet address.** Pick an address
  with a nonzero balance and a known transaction history; verify the tool
  returns the expected balance and nonce. This proves the mainnet RPC is
  routing correctly and the EVM client is decoding responses against the
  right chain.

- [ ] **`anchor_info` returns the expected precompile config.** Response
  must include the precompile address (`0x0000000000000000000000000000000000000A00`),
  the ABI loaded, and a positive `method_count`. Zero ABI methods means
  `ANCHOR_ABI_PATH` is unset or the file is missing from the mainnet image
  — fix and redeploy.

- [ ] **`readyz` returns 200.** The health endpoint also performs an upstream
  reachability check; a 503 means the configured RPC URL is wrong or the
  endpoint is degraded.

  ```sh
  curl -sS -o /dev/null -w '%{http_code}\n' \
    http://<sandbox-mcp-host>:9090/readyz
  # Expect: 200
  ```

### 3.2 Post-cutover (production)

Repeat 3.1 against the production endpoint. Then:

- [ ] **Watch `evm.rpc.errors` for the first hour.** A spike above the
  testnet baseline points at either an RPC-provider issue or a hot config
  knob (timeouts, retries) tuned for testnet that doesn't suit mainnet's
  latency profile. The relevant Prometheus expression:

  ```promql
  rate(evm_rpc_errors_total[5m])
  ```

  Compare against the testnet rate for the same window before declaring the
  cutover stable.

- [ ] **Eyeball `mcp.server.tool.errors` by tool label** during the same
  window. A regression isolated to a single tool (most likely `wallet_status`
  or one of the `anchor_get_*` reads) points at a precompile or
  RPC-response-shape difference between networks — see the open question in
  Section 5.

---

## 4. Rollback

This section is the unwind path. The cutover is reversible because the
server is stateless with respect to chain identity: it holds no keys, no
per-user state beyond the API-key store (which is chain-agnostic), and no
in-flight transactions (write txs are caller-signed and submitted by the
caller). The only thing that needs to change to roll back is which RPC the
server points at.

1. **Revert the Section 2 edits.** ConfigMap and Helm overlay keys go back
   to their testnet values:

   ```diff
   -  NVNM_EVM_RPC_URL: "https://evm.nvnmchain.io"
   -  NVNM_CHAIN_ID: "1611"
   -  NVNM_CHAIN_ENVIRONMENT: "mainnet"
   +  NVNM_EVM_RPC_URL: "https://evm.testnet.nvnmchain.io"
   +  NVNM_CHAIN_ID: "787111"
   +  NVNM_CHAIN_ENVIRONMENT: "testnet"
   ```

2. **Redeploy.** A pod restart picks up the new ConfigMap; Helm rollback
   (`helm rollback nvnm-mcp-server <PREV_REV>`) is the cleaner option if
   the cutover was a `helm upgrade`.

3. **Verify with Section 3.1's checks** against the testnet endpoint.
   `nvnm_overview` should again report `chain_id=787111`, `environment=testnet`.

**No on-chain unwind is required.** The server holds no keys and signs no
mainnet transactions; nothing it did on mainnet needs to be reversed.
Anything a *caller* submitted to mainnet during the cutover window is the
caller's transaction on the caller's wallet — outside this server's scope
and outside the rollback's effect.

If DNS for `mcp.nvnmchain.io` was flipped as part of the cutover, that
flip is the slowest piece to reverse; budget TTL accordingly when planning
the cutover window.

---

## 5. Open question

This section flags one verification item that cannot be answered from
testnet alone and must be checked during Phase 10 staging.

**Does the mainnet anchor precompile return `pagination.total > 0` for
`anchor_get_registries` and `anchor_get_records`?**

The testnet precompile returns `pagination.total = 0` for both tools,
regardless of how many records actually exist — the precompile reports an
opaque cursor and the page payload, but the total counter is hardcoded to
zero. Clients have adapted by treating `total` as "unknown" rather than
"empty," and the MCP tool docs say so.

If mainnet also returns `total = 0`, no client-side adaptation is needed
beyond the existing convention. If mainnet returns the actual count, the
behavioral difference between networks should be documented in
`docs/TOOL_REFERENCE.md` and an issue opened to align caller expectations —
not a server-side bug, but a chain-side property worth surfacing.

**To be verified during Phase 10 staging.** Parked in
`project_phase10_devops_doc` memory.
