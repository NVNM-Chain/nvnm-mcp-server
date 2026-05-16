# Changelog

All notable changes to the NVNM Chain MCP Server are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

Phase 9.3: per-file SPDX license headers added to every .go file
under `cmd/` and `internal/` (100 files). Mechanical bulk rewrite
recorded in `.git-blame-ignore-revs`; CI lint enforces the header on
future additions. Sequencing step 3 of Phase 9 (OSS Readiness); no
behavior change.

Phase 9.1: OSS foundation documents shipped (LICENSE, NOTICE,
CODE_OF_CONDUCT, CONTRIBUTING, SECURITY). Sequencing step 1 of Phase 9
(OSS Readiness); no behavior change.

Phase 9 prep (PR #23): surface a server-level `instructions` string
in the MCP initialize response so first-contact agents receive the
lobby pointer and the privacy-by-design caveat at session start, even
if their client compresses or omits tool descriptions.

Phase 8.9: hard cut from the legacy `INVENIAM_*` env-var prefix to
`NVNM_*` and matching server-identity rename. Single coordinated
BREAKING change.

### Added

#### Per-file SPDX license headers (Phase 9.3)

- Every `.go` file under `cmd/` and `internal/` now carries a
  two-line SPDX header (`SPDX-License-Identifier: Apache-2.0`
  + copyright). 100 files; `vendor/` excluded.
- `scripts/add_license_headers.sh` -- idempotent prepend; safe to
  re-run.
- `scripts/check_license_headers.sh` -- CI guard invoked from
  `.github/workflows/ci.yml`'s new "License headers" step; fails
  the build if any `.go` file under `cmd/` or `internal/` is
  missing the header.
- `.git-blame-ignore-revs` -- created at repo root with the
  rewrite commit's full hash. GitHub's blame view honors this
  automatically; local users opt in via `git config
  blame.ignoreRevsFile .git-blame-ignore-revs` (documented in
  `CONTRIBUTING.md` § 5).

#### OSS foundation documents (Phase 9.1)

- `LICENSE` -- standard Apache 2.0 text (canonical SHA-256
  `cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30`,
  no trailing whitespace).
- `NOTICE` -- minimal Apache attribution declaring Inveniam Capital
  Partners as the original copyright holder.
- `CODE_OF_CONDUCT.md` -- Contributor Covenant 2.1, verbatim.
  Enforcement contact reserved with placeholder
  `<EMAIL_TBD:conduct@nvnmchain.io>` until the alias is provisioned.
- `CONTRIBUTING.md` -- dev setup, build & test, test layers,
  commit/PR norms, DCO sign-off requirement, security-disclosure
  policy, and vendor-directory rules. 8 sections per
  `docs/PHASE_9_DESIGN.md` § 3.2. Maintainer contact reserved with
  placeholder `<EMAIL_TBD:maintainers@nvnmchain.io>`.
- `SECURITY.md` -- private-disclosure path (GitHub Security Advisories
  primary; `<EMAIL_TBD:security@nvnmchain.io>` fallback). Enumerates
  deliberate hardening invariants so researchers don't report design
  properties as bugs. Response SLO: 3-day acknowledgement, 7-day
  initial severity assessment, 90-day coordinated disclosure window.
  Explicit "no monetary bug bounty" per Phase 9 decision D4.
- `.env.example` -- starter template covering required chain config,
  HTTP transport, both auth providers, write-tool gating, integration-
  test credentials, and observability. All sensitive fields are
  `PLACEHOLDER`; `.env` itself is gitignored.

All mail-alias references use the disambiguating placeholder form
`<EMAIL_TBD:foo@nvnmchain.io>` so eventual substitution is mechanical
(`sed -i 's/<EMAIL_TBD:foo@nvnmchain.io>/foo@nvnmchain.io/g'`).
Pre-merge grep for the literal substring `EMAIL_TBD` still catches
every occurrence and is a Phase 9 exit criterion before the public
repo flip (Phase 9.15).

#### MCP initialize-response `instructions` string (PR #23)

- `internal/mcp/server.go`: `NewServer` now passes a populated
  `mcp.ServerOptions{Instructions: ...}` to the SDK constructor
  (previously `nil`). The SDK propagates the string into the
  `initialize` response per the MCP spec field
  `InitializeResult.instructions`; clients are expected to treat it
  like a system-prompt hint the model sees before any tool
  description is processed.
- Content: a three-sentence orientation that names the server, states
  the no-events privacy property, and points first-time agents at
  `nvnm_overview` for the canonical six-step journey. Kept terse on
  purpose -- the richer chain summary, prereqs, and journey still
  live in the `nvnm_overview` tool response; the instructions string
  points at that tool rather than duplicating it. Future changes to
  the chain summary or journey only need to update one place
  (`tools_overview.go`).
- Why: defense in depth for the lobby-tool pattern. The existing
  design relies on the agent noticing "Call this first if you have
  never used this server before" inside the `nvnm_overview` tool
  description; that works in clients that surface full descriptions
  to the model but fails silently in clients that compress them or
  feed tool names only. The instructions field lands at the protocol
  level and is much more likely to be read before the agent picks a
  tool. Same reasoning that motivated repeating the privacy caveat
  in `funded_active` wizard messages -- instructions is just the
  highest-leverage place to repeat it.
- Test: `TestE2E_Initialize_IncludesInstructions` asserts the field
  is non-empty and contains both `nvnm_overview` and `emits no
  events`. Wording-stable (substring check, not full-string match)
  so future copy edits do not break the test.

### Changed (BREAKING)

#### Env var prefix: `INVENIAM_*` -> `NVNM_*` (hard cut, no alias)

- All chain/RPC config keys renamed. The three legacy keys
  (`INVENIAM_EVM_RPC_URL`, `INVENIAM_EVM_ARCHIVE_RPC_URL`,
  `INVENIAM_CHAIN_ID`) are no longer read. New names per
  `docs/RUNBOOK.md#env-var-migration`:
  `NVNM_EVM_RPC_URL`, `NVNM_EVM_ARCHIVE_RPC_URL`, `NVNM_CHAIN_ID`.
- `Config.Load` runs a pre-validation pass that scans `os.Environ()`
  for any of the three legacy keys. If **any** are present the server
  exits immediately with `ErrLegacyEnvVars` and a pointer to the
  migration runbook. **The check fires even when the matching
  `NVNM_*` is also set** -- dual-populated config is the silent-drift
  trap fail-loud exists to catch. An operator who thinks they
  migrated but left a stale `INVENIAM_*` in a ConfigMap hears about
  it on the next deploy, not when someone later unsets `NVNM_*` and
  the legacy value silently takes over. The strict policy was
  chosen 2026-05-13 over the original "only fail if `NVNM_*` is
  unset" design wording. See `CLAUDE.md` "Migration hygiene principle"
  for the broader rule this case follows.
- Test: `TestLoad_RejectsLegacyEnvVars` (table-driven across all
  three keys, with valid `NVNM_*` also set) asserts the strict
  policy.

#### Server identity rename

- MCP `serverName` constant: `inveniam-evm` -> `nvnm-chain`. Visible
  in the `initialize` response and any client-side server-tab label.
- `OTEL_SERVICE_NAME` default: `inveniam-mcp-server` -> `nvnm-mcp-server`.
- Internal OTel `Tracer` and `Meter` names: `inveniam-mcp-server` ->
  `nvnm-mcp-server`. Dashboards that filter by `service.name`,
  `tracer`, or `meter` need their queries updated; metrics keep
  flowing, just under different labels.
- Helm chart directory: `deploy/helm/inveniam-mcp-server/` ->
  `deploy/helm/nvnm-mcp-server/`. `Chart.yaml` `name` field +
  every template-helper include updated to match. (The image
  repository in `values.yaml` was carried in this Phase 8.9 commit
  as a deferred item and renamed in 8.13 below as part of the
  atomic binary + Docker artifact rename.)
- Kubernetes ConfigMap (`deploy/k8s/configmap.yaml`) and Helm
  `values.yaml` migrated to `NVNM_*` keys; ConfigMap chain ID stays
  at `787111` (current testnet, `nvnm-testnet-1`).
- `Makefile` `run-local`, `docker-run`, and `docker-smoke` targets
  updated to set `NVNM_*` env vars.

#### Binary + Docker artifact identity rename (Phase 8.13)

- `cmd/inveniam-mcp-server/` -> `cmd/nvnm-mcp-server/`. The Go
  module path (`github.com/inveniam/nvnm-mcp-server`) already
  matched the new name, so no import-path edits were needed.
- Binary name: `Makefile` `BINARY_NAME` / `CMD_DIR` / `DOCKER_IMAGE`,
  `Dockerfile` build output + `ENTRYPOINT`, and CI build paths now
  produce `nvnm-mcp-server`. Release artifacts published from
  `.github/workflows/release.yml` use the new name template
  (`nvnm-mcp-server-${TAG}-<os>-<arch>`), with matching cosign
  verification instructions in the release-notes body.
- Published Docker image path: Helm `values.yaml` `image.repository`
  is now `ghcr.io/inveniamcapital/nvnm-mcp-server`. The first
  release tag pushed after this commit publishes to the new path;
  the old `inveniamcapital/inveniam-mcp-server` image stays in
  ghcr but receives no further updates. Operators pinning to the
  old image must either flip `image.repository` (Helm) or update
  the `image:` field in their K8s `Deployment` manifest.
- K8s manifests (`deploy/k8s/*.yaml`): every
  `app.kubernetes.io/name` label, container name, and resource
  `metadata.name` (Deployment / Service / HPA / ServiceMonitor /
  NetworkPolicy / Namespace label) renamed; image reference flipped
  to the new ghcr path; ConfigMap and Secret references renamed
  (`inveniam-mcp-server-config` -> `nvnm-mcp-server-config`,
  `inveniam-mcp-server-keys` -> `nvnm-mcp-server-keys`,
  `inveniam-mcp-server-secret` -> `nvnm-mcp-server-secret`).
- Prometheus alerts (`deploy/prometheus/alerts.yaml`): alert group
  name, every `app:` label, the scrape job filter
  (`up{job="..."} == 0`), and the descriptive prose all renamed.
- Grafana dashboard (`deploy/grafana/dashboard.json`): `title` and
  `uid` updated. **Operator action:** the dashboard `uid` change
  breaks existing direct URLs (`/d/inveniam-mcp-server/...`) --
  re-import the dashboard (or update the panel `uid` in your
  provisioning files) to pick up the new identifier.
- Internal: `internal/evm/tracing.go` `evmTracerName` constant;
  startup log line in `cmd/nvnm-mcp-server/main.go`.

### Migration

**Env vars (8.9).** Operators must rename `INVENIAM_*` to `NVNM_*`
in every layer that sets chain config -- ConfigMaps, Helm overlays,
`.env` files, systemd units, Compose files, Terraform, shell
wrappers. **Don't keep the old key alongside the new one** -- the
server refuses to start when it sees both. Migration table and
worked example in `docs/RUNBOOK.md#env-var-migration`.

**Binary + Docker image (8.13).** No automatic detection, just
path changes:

- Binary on PATH: `inveniam-mcp-server` -> `nvnm-mcp-server`
  (wrapper scripts, systemd `ExecStart`, MCP client launchers,
  CI invocations).
- Docker image: `ghcr.io/inveniamcapital/inveniam-mcp-server:*` ->
  `ghcr.io/inveniamcapital/nvnm-mcp-server:*`. The old path stops
  receiving updates after this release; pin by digest if you need
  the pre-rename image for rollback testing.
- K8s resources: if you applied the previous `deploy/k8s/*.yaml`
  manifests, the renamed `metadata.name` fields mean a
  `kubectl apply -k deploy/k8s/` will create new resources rather
  than mutating the old ones. Plan a deliberate cutover (parallel
  deploy + traffic shift, or scheduled downtime + delete + apply).
- Grafana: re-import or update provisioning to pick up the new
  dashboard `uid` (`nvnm-mcp-server`); direct `/d/inveniam-mcp-server/`
  URLs will 404.

---

## [1.0.0-rc.2] -- 2026-05-13

Phase 8.1-8.8 ship: tool annotations + `next_actions` envelope across
all tools, EIP-1559 default, Origin-header validation, API-key hashing
migration, five new onboarding tools, plus the defiweb/go-eth swap
and K8s Secret pattern cleanup.

### Added

#### MCP tool surface (16 -> 21 tools)

- **Five onboarding tools (Phase 8.8):**
  - `nvnm_overview` (closed-world read) -- static lobby tool. Returns
    chain identity (name, env, ID, precompile, explorer/docs/bridge
    URLs, env-aware native/wrapped token naming), a 2-3 sentence
    "what is NVNM Chain" blurb that includes the privacy-by-design
    property, a 6-step canonical agent journey, and `next_actions`
    pointing at `nvnm_setup_wizard`. No chain calls.
  - `wallet_status` (open-world read) -- one-shot snapshot of an EVM
    address. Returns balance (wei + env-aware human form in
    `wmmUSD`/`wmantraUSD`), pending nonce, `has_sent_tx`, and one of
    three honest status values: `unfunded`, `funded_unused`,
    `funded_active`. `funded_active` means "has sent any tx," NOT
    "has anchored" -- the chain emits no events by design.
  - `nvnm_setup_wizard` (open-world read) -- four-state prose-guided
    flow: `needs_wallet` (samples for Python/JS/Go that store keys
    via `keyring` / mode-0o600 `.env` / `os.WriteFile` mode 0o600,
    never print them); `unfunded` (bridge instructions);
    `funded_unused` (optional verify_hash + verify_signature
    challenges); `funded_active` (usage patterns + anchor-prepare
    pointers, with explicit "any tx, not anchored" caveat).
  - `nvnm_setup_verify_hash` + `nvnm_setup_verify_signature` (both
    closed-world read, pure compute) -- stateless verification
    helpers. Challenge is
    `sha256(lower(address) + ":" + protocol-version-tag)` so the
    server can recompute the expected value from the address alone
    (no per-call state, no time dependence). Signature path uses
    EIP-191 personal_sign via defiweb's `ECRecoverer.RecoverMessage`.
- A unit test asserts wizard sample code uses an allowlisted
  safe-storage primitive per language; if a future contributor
  changes a sample to print the key the test fails.

#### MCP `ToolAnnotations` on every tool (Phase 8.2)

- Three annotation constructors in `internal/mcp/annotations.go`
  (`newOpenWorldReadOnly`, `newClosedWorldReadOnly`,
  `newDestructiveWriteTool`) return a fresh `*mcp.ToolAnnotations`
  per call -- no shared singleton pointers (PR #20 Q4).
- All 21 tools carry explicit `Title`, `Description`, and
  `Annotations` with an explicit `OpenWorldHint`. `evm_send_raw_-`
  `transaction` locked in as `DestructiveHint=true,`
  `OpenWorldHint=true`. Regression tests prevent silent relaxation.

#### `next_actions` envelope on every tool (Phase 8.3)

- Per-tool hint builders in `internal/mcp/next_actions.go`. Most
  return static hints; data-dependent ones branch on receipt status,
  bytecode presence, empty registries, and (for
  `evm_send_raw_transaction`) echo the tx hash into the receipt-poll
  hint so the agent has a copy-pasteable next call.
- Envelope structs in `internal/mcp/envelopes.go` embed the
  underlying response types and add `NextActions []NextAction`.
  JSON shape stays backwards-compatible via field promotion.
- AST reachability test parses every non-test `.go` file in the
  package, collects every `Tool:` literal that appears inside a
  `NextAction` composite literal, and asserts each name is in the
  registered tool set. Catches typos and stale references in every
  branch of every builder.

#### EIP-1559 prepare-tools (Phase 8.4)

- `anchor_prepare_add_registry`, `anchor_prepare_add_record`, and
  `anchor_prepare_grant_role` now build type-2 transactions by
  default. `MaxFeePerGas = 2 * SuggestGasPrice` (2x headroom against
  baseFee inflation), `MaxPriorityFeePerGas = SuggestGasTipCap` with
  a 1-gwei fallback when the RPC returns zero or errors. `GasPrice`
  dual-populated (`= MaxFeePerGas`) so legacy-only signers still
  have a usable value.
- `UnsignedTransaction` gains `Type`, `MaxFeePerGas`, and
  `MaxPriorityFeePerGas` (all `omitempty` -- type-0 responses
  preserve the legacy JSON shape exactly). `WalletTransactionRequest`
  gains the same fee fields; `GasPrice` becomes `omitempty` so
  MetaMask et al. prefer EIP-1559 fields when present.
- `prefer_legacy_tx` opt-out parameter on each prepare tool flips
  back to the type-0 builder.
- `Client.SuggestGasTipCap(ctx)` added to the EVM client interface
  (base, resilient, and tracing wrappers).
- New golden fixture `unsigned_transaction_eip1559.golden.json`;
  testnet integration round-trips for both type-2 and type-0
  signed-and-broadcast paths.

#### Origin-header validation (Phase 8.5)

- `internal/mcp/origin.go` -- `OriginAllowlist`, `originGuard`
  middleware. DNS-rebinding defense per the MCP specification.
  Requests with an `Origin` header must match the allowlist or get
  403; requests without an `Origin` header (server-to-server, CLI,
  curl) pass through unchanged.
- Origin guard installed at the outermost middleware position
  (before auth, body limit, rate limiter) so rejection
  short-circuits before any expensive work.
- Default allowlist covers both `http://` and `https://` for
  `localhost`, `127.0.0.1`, and `[::1]`. Loopback hosts accept any
  port; non-loopback entries require exact-match including port to
  defeat patterns like `http://localhost.attacker.tld`.
- `NVNM_ALLOWED_ORIGINS` env var (comma-separated) overrides the
  default. Startup log line surfaces the resolved allowlist.

#### Foundation types (Phase 8.1)

- `internal/mcp/types.go` -- shared `NextAction` type embedded in
  tool responses.
- `internal/mcp/runtime.go` -- `RuntimeInfo` + `RuntimeInfoFromConfig`
  bundle (chain env, anchor address, explorer/docs/bridge URLs)
  consumed by the onboarding tools.
- `internal/config/environment.go` -- `ChainEnvironment` enum
  (`testnet`/`mainnet`), `TokenNaming`, `NamingFor(env)`, and
  `InferEnvironmentFromChainID`.

#### Pre-red-team security hardening

- Pre-auth IP failure-rate limiter
  (`internal/mcp/failrate.go`). Outermost middleware after the
  origin check; `AuthMiddleware` calls `Penalize` on every 401, so
  credential stuffing now hits a 429 instead of unlimited attempts.
  Trust-X-Forwarded-For is opt-in via `NVNM_TRUST_PROXY_HEADERS`.
- Per-client rate-limiter map is bounded via LRU eviction and a TTL
  janitor; same pattern applied to the new IP failure limiter.
- API-key store writes are atomic (`tmp + fsync + rename`); the
  previous file remains intact if any step fails.
- Admin server hashes both sides of the bearer compare with SHA-256
  before `subtle.ConstantTimeCompare`, so the length-mismatch
  shortcut cannot probe the admin key's length. All bearer failures
  now return 401 per RFC 7235 (previously 403).
- Approval prompt decodes the recovered signer address and the
  first 4 bytes of calldata (method selector), shows wei with
  thousand separators and a 6-decimal ETH approximation, and renders
  the chain ID with a human label ("testnet"/"mainnet").
- All five write-tool audit lines emit a structured `slog.Group`
  with stable `tool` / `phase` / `client_id` keys so SIEM rules
  don't string-match on the message body.
- `parsehex` seed corpus added for fuzz testing.

#### Documentation

- `docs/DESIGN.md` § 8 Deployment Topology gains a "Multi-chain
  (testnet + mainnet)" subsection: two instances, one per chain, NOT
  per-session selection within a single instance. Records the six
  reasons (blast-radius isolation, audit-trail clarity, per-chain
  RBAC, per-tier ops, existing startup-env config model, small
  agent-UX cost) plus a "revisit triggers" list.
- `docs/SECURITY_AUDIT.md` gains the "Update 2026-05-12: Fresh
  pre-red-team review and remediation" and "Update 2026-05-13:
  Phase 8.6 and 8.7 (hashed-at-rest, constant-time auth)"
  sections covering the pre-red-team remediation log and the
  8.6/8.7 storage + validator design.
- `docs/SECURITY_CONSUMER_GUIDANCE.md` (new) describes the two
  threats the server deliberately does NOT mitigate at its boundary
  -- indirect prompt injection via on-chain string fields and
  approval-substitution via swapped signed-tx bytes -- and what
  consuming agents should do.
- `docs/RUNBOOK.md` § 9 documents the Phase 8.6 keys-file migration
  upgrade procedure (before/during/after signals, flock multi-process
  safety, rollback via `.pre-migration` restore).
- `docs/LICENSE_EXCEPTIONS.md` (new) tracks documented license
  exceptions.
- `CLAUDE.md` (new) project-specific session context: chain-ID
  history, "Inveniam Chain != MANTRA dukong" disambiguation table,
  multi-chain deployment model, tool surface, test layout.

### Changed

#### Tool-surface internals

- **Breaking (Go API):** `NewServer` signature refactored to take
  `*config.Config` instead of individual scalars (`evmClient,`
  `anchorClient, enableWrite, writeApproval, chainEnvironment,`
  `middleware, logger` -> `evmClient, anchorClient, cfg, middleware,`
  `logger`). The onboarding tools need `ChainID`, `AnchorAddress`,
  `ExplorerURL`, `DocsURL`, and `BridgeURL` from cfg; callers
  outside `cmd/inveniam-mcp-server/main.go` must update.

#### EVM client

- **Replaced go-ethereum with `github.com/defiweb/go-eth` v0.7.0
  (MIT).** Removes a GPL-3.0 / LGPL-3.0 dependency under the
  project's proprietary commercial license policy and shrinks the
  dep surface significantly. A build-tagged differential test
  imported both libraries and asserted byte-for-byte ABI calldata
  equality across 13 cases (addRegistry / addRecord / grantRole)
  before the go-ethereum import was removed.
- Surface changes: `common.Address`/`Hash` -> defitypes equivalents
  (new `internal/evm/addrhex.go` preserves EIP-55 output);
  `types.Transaction` -> `defitypes.Transaction` fluent builder +
  `defiwallet.PrivateKey.SignTransaction` +
  `deficrypto.ECRecoverer.RecoverTransaction`; `ethclient.Client`
  -> `rpc.Client` + `transport.NewHTTP`; `accounts/abi.ABI` ->
  `defiabi.Contract` with `abi:"..."` field tags;
  `ethereum.CallMsg`/`FilterQuery` -> `defitypes.Call` /
  `defitypes.FilterLogsQuery`.
- Vendored (~32 MB under `vendor/`); CI uses `-mod=vendor`.
  pre-commit excludes `vendor/` from Go fmt/vet/imports/lint hooks.
- License allowlist tightened to permissive-only (`MIT`, `BSD-2`,
  `BSD-3`, `ISC`, `Apache-2.0`, `MPL-2.0`, etc.). GPL-3.0 and
  LGPL-3.0 removed.

#### Resilient client

- Recognizes the Cosmos-EVM `eth_gasPrice` -> "failed to get receipts
  from comet block" race as transient and retries it. Matched via a
  named constant so a future upstream wording change is caught by a
  unit test rather than by flaky CI.
- Integration test helpers now wire `evm.NewResilientClient` over
  the bare `evm.NewClient` (production parity); receipt-poll budget
  bumped from 30s to 60s to cover observed worst-case latency.

#### K8s manifests

- New `deploy/k8s/secret.yaml.example` demonstrates the Secret
  pattern: one Secret for `INVENIAM_EVM_RPC_URL` / `MCP_API_KEY` /
  `ADMIN_API_KEY` / FusionAuth IDs (`envFrom secretRef`), a second
  Secret for the `MCP_API_KEYS_FILE` JSON payload (mounted as a
  read-only volume, `defaultMode=256`). The ConfigMap stops carrying
  secret-shaped fields.
- `deployment.yaml` pulls both `configMapRef` and `secretRef`;
  optional flags let FusionAuth-only deploys skip the keys Secret.
- `networkpolicy.yaml` documents why `:8081` is intentionally NOT in
  the ingress list (admin server binds to loopback by default;
  operators who flip it must add a narrow podSelector + namespace-
  Selector rule).
- `configmap.yaml`: chain ID corrected from `58887` to `787111`
  (current Inveniam testnet); sensitive fields removed;
  `NVNM_ALLOWED_ORIGINS` and `NVNM_TRUST_PROXY_HEADERS` surfaced as
  commented examples.
- `.gitignore` excludes `deploy/k8s/secret.yaml`.

#### Build / toolchain

- Go toolchain bumped to 1.26.3 (govulncheck). `GOTOOLCHAIN` pinned
  in the Dockerfile build stage to match `go.mod` -- reproducible
  builds.
- `golang.org/x/sys` promoted from indirect to direct (used for
  `unix.Flock` in the keys-file writer).

### Security

#### API-key hashing migration (Phase 8.6, IRREVERSIBLE)

- **Storage migrated from raw bearer tokens to sha256 at rest.**
  `KeyEntry` gains `KeyHash` (sha256 hex) and `KeyPrefix`; raw `Key`
  retained as a load-only legacy field with `omitempty`, cleared
  after migration. The pre-8.6 `SECURITY_AUDIT.md` claimed "hashed
  at rest" but the code did not match; this release makes the
  claim accurate.
- `NewKeyEntry(id, rawKey, writeApproval, roles)` is the sole
  production constructor (hashes once, captures prefix, never
  retains raw key). Direct `KeyEntry` literals with `Key:` set are
  confined to migration helpers and the migration regression test.
- `KeyStore.byHash` map replaces `byKey`. `Lookup(rawKey)` hashes
  before probe.
- `SaveKeysFile` adds `flock(LOCK_EX|LOCK_NB)` on top of the atomic
  `tmp + fsync + rename`. `LoadKeysFile` falls back to `<path>.tmp`
  on parse failure for interrupted-write recovery.
- **`NewManagedKeyStore` writes a one-shot `<path>.pre-migration`
  backup BEFORE any mutation** (never overwritten on subsequent
  migrations), normalizes in-memory entries, opportunistically
  re-saves (INFO on success, WARN-and-continue on save failure).
  See `docs/RUNBOOK.md` § 9 for the operator upgrade procedure and
  rollback path.
- `internal/auth.HashKey(rawKey)` shared between storage migration
  and validator compare so both sides cannot drift.

#### Constant-time validator on hash bytes (Phase 8.7)

- `Validate` hashes the input, looks up by hash, then verifies with
  `subtle.ConstantTimeCompare` on fixed-length sha256 hex digests.
  The previous compare against raw `entry.Key` was a placebo (the
  map probe used the same raw bytes); the new compare is genuine
  defense-in-depth.
- **Miss path burns a placeholder `ConstantTimeCompare` to flatten
  hit/miss timing.**
- `KeyResult.Key` removed; `KeyResult.KeyHash` added. Raw key never
  exposed across the package boundary.

#### Other security changes

- **Breaking:** HTTP transport fails closed when no auth validator
  can be built. Previously logged WARN and ran unauthenticated; now
  returns `config.ErrHTTPAuthRequired` and refuses to start.
- **Breaking:** Admin REST API now binds to `127.0.0.1:8081` by
  default. The admin key is the master key; cluster-wide exposure
  was a privilege-escalation foot-gun. Operators who need
  cross-pod access must flip the bind explicitly AND add a narrow
  NetworkPolicy rule.
- **Breaking:** `OTLP_INSECURE` default flipped. Operators
  exporting OTLP over plaintext must now opt in explicitly. See
  `docs/SECURITY_AUDIT.md` "Update 2026-05-12: Fresh pre-red-team
  review and remediation".
- **Breaking:** `NVNM_CHAIN_ENVIRONMENT` is required when the
  configured chain ID is not one of the recognized testnet/mainnet
  IDs. Private forks must set it explicitly; recognized IDs still
  infer the environment.

### Fixed

- Approval prompt previously rendered an opaque hex chain ID; now
  threads a human label ("testnet"/"mainnet") through `NewServer`.
- Telemetry middleware comment corrected: errors ARE recorded in
  OpenTelemetry span events; only the response to the client is
  sanitized via `apperrors.SafeForClient`.
- Pre-existing `govet` shadow declarations of the package-level
  `ctx` in several test files (`approval_test.go`,
  `ratelimit_test.go`, `rbac_test.go`) renamed to `tCtx` / `authCtx`.
- `gosec` G115 on the `int(f.Fd())` syscall cast in
  `internal/mcp/keys.go` -- suppressed for the CI lint version
  (golangci-lint v2.11.4) while local v2.12 does not flag it.
  Documented divergence; matches the prior pattern.

### Removed

- `github.com/ethereum/go-ethereum` and all its transitive
  dependencies. See the **Changed** section above for the
  defiweb/go-eth swap.
- `KeyResult.Key` (raw bearer token) -- replaced by
  `KeyResult.KeyHash`.
- Raw-key fallback in `summarize()` -- `KeyPrefix` is read directly.


---

## [1.0.0-rc.1] -- 2026-04-28

First release candidate. Phases 0-7 complete; full pre-red-team security audit
performed and all High/Critical findings remediated.

### Added

#### MCP tool surface (16 tools)

- **EVM reads (8):** `evm_get_chain_id`, `evm_get_block`, `evm_get_transaction`,
  `evm_get_transaction_receipt`, `evm_get_balance`, `evm_get_code`,
  `evm_get_logs`, `evm_call_contract`
- **Anchor reads (4):** `anchor_info`, `anchor_get_registry`,
  `anchor_get_registries`, `anchor_get_records`
- **Anchor writes (3):** `anchor_prepare_add_registry`,
  `anchor_prepare_add_record`, `anchor_prepare_grant_role`
- **Broadcast (1):** `evm_send_raw_transaction`

All tool inputs validated at the MCP boundary. All outputs normalized into
typed JSON with `snake_case` field names.

#### Write architecture (prepare-sign-submit)

- Server constructs complete unsigned transactions but **never holds private
  keys**.
- Each `anchor_prepare_*` tool returns both:
  - `raw_tx` (RLP-encoded) for local/headless signers (HSM, Vault, CLI)
  - `wallet_tx_request` (EIP-1193 hex-quantity payload) for MetaMask /
    browser wallets
- Human-in-the-loop write approval via MCP elicitation, configurable
  per-client (`required` or `auto`) and globally (`WRITE_APPROVAL_DEFAULT`).
- See `docs/METAMASK_GUIDE.md` for the browser-wallet walkthrough.

#### Authentication and authorization

- **Two auth providers,** selected by `AUTH_PROVIDER`:
  - `apikey` (default) -- self-managed Bearer keys with per-client identity,
    backed by a JSON key store with hot-reload.
  - `fusionauth` -- OAuth/JWT validation via JWKS; `automation` role maps
    to auto-approval.
- **Per-tool RBAC.** Roles (`reader` / `writer` / `admin` / `automation`)
  gate every tool handler. Backward compatible: when no roles are present,
  no enforcement.
- **Per-client rate limiting.** Token-bucket via `MCP_RATE_LIMIT` (default
  60 req/s) and `MCP_RATE_BURST` (default 10). Returns HTTP `429` when
  exceeded.
- **Admin REST API** on a separate port (`:8081`, requires `ADMIN_API_KEY`)
  for runtime key CRUD without server restarts. Constant-time token
  comparison, audit-logged mutations, raw key shown once on creation.

#### Resilience and operations

- Retry with exponential backoff for transient RPC errors (`RPC_MAX_RETRIES`,
  `RPC_INITIAL_BACKOFF`, `RPC_MAX_BACKOFF`). `eth_sendRawTransaction`
  excluded for idempotency.
- Token-bucket rate limit on upstream RPC (`RPC_RATE_LIMIT`,
  `RPC_RATE_BURST`).
- Circuit breaker on upstream RPC (`CIRCUIT_BREAKER_THRESHOLD`,
  `CIRCUIT_BREAKER_TIMEOUT`).
- Per-tool request timeouts via context.
- Graceful shutdown on `SIGINT` / `SIGTERM` with telemetry flush.

#### Observability

- OpenTelemetry traces and metrics on every MCP tool call and upstream
  RPC call.
- Configurable trace sampling (`OTEL_TRACE_SAMPLE_RATIO`) using
  `ParentBased(TraceIDRatioBased(...))`.
- Prometheus `/metrics` endpoint on the dedicated metrics port.
- Health check endpoints: `/healthz` (liveness), `/readyz` (readiness with
  EVM RPC + ABI checks).
- Structured `slog` logging with redaction (addresses, URLs, tx data,
  private keys).
- Per-client identity (`client_id`) on every span and audit log entry.

#### Deployment artifacts

- Dockerfile with digest-pinned base images (`golang:1.26.2-alpine` and
  `gcr.io/distroless/static-debian12`), `ARG TARGETARCH` for cross-platform
  builds, runs as UID 65532 nonroot, read-only filesystem, all caps dropped.
- Kubernetes manifests in `deploy/k8s/`: Namespace, Deployment, Service,
  ServiceMonitor, HPA, ConfigMap, NetworkPolicy, Kustomization.
- Helm chart in `deploy/helm/inveniam-mcp-server/` with security context
  parity.
- Grafana dashboard JSON (`deploy/grafana/dashboard.json`).
- Prometheus alerting rules (`deploy/prometheus/alerts.yaml`) with
  `runbook_url` annotations pointing to `docs/RUNBOOK.md`.

#### Security and supply chain

- **Pre-red-team security audit** performed (`docs/SECURITY_AUDIT.md`):
  18 findings; 17 remediated (all High/Critical), 1 backlog item (CORS,
  Low priority).
- `gosec` and 17+ `golangci-lint` linters in CI.
- `govulncheck` runs in CI; no known vulnerabilities.
- `go-licenses` check on every push/PR with explicit allowed-licenses list.
- SBOM (CycloneDX JSON) generated by `anchore/sbom-action` on every push to
  `main`.
- Cosign keyless signing of compiled binary on every push to `main` via
  Sigstore OIDC.
- `detect-secrets` pre-commit hook with project baseline.
- Dependabot configured for `gomod`, `docker`, and `github-actions`.

#### Testing

- 271 automated tests across 30 test files in 8 packages.
- Layers: unit tests with mocks, golden tests for response-shape stability,
  integration tests against the live Inveniam testnet
  (`//go:build integration`), MCP HTTP end-to-end tests through the
  official MCP SDK, k6 load tests, Docker smoke test.
- Standard library `testing` only -- no third-party test frameworks.
- E2E coverage of approval flows (auto / required / declined / canceled /
  no-elicitation), API key auth, FusionAuth JWT validation, per-client
  approval overrides, admin API, RBAC enforcement, rate limiting.

#### Documentation

- `README.md` -- overview, configuration, tool catalog, deployment.
- `docs/DESIGN.md` -- architecture, package responsibilities, write flow,
  observability, security.
- `docs/IMPLEMENTATION_PLAN.md` -- phased plan with completion status.
- `docs/SECURITY_AUDIT.md` -- threat model and remediation log.
- `docs/RUNBOOK.md` -- operational runbook with alert response procedures.
- `docs/TOOL_REFERENCE.md` -- complete schema reference for all 16 tools.
- `docs/METAMASK_GUIDE.md` -- browser-wallet quick start.
- `docs/TESTING.md` -- testing strategy and latest results.
- `docs/OVERVIEW.md` -- capabilities overview.
- `docs/standards/CODING_STANDARDS.md` -- contributor coding standards.
- `.cursor/rules/` -- IDE rules for AI-assisted development.

### Tech stack

- Go 1.26.2 (`CGO_ENABLED=0`)
- MCP Go SDK v1.5.0 (`github.com/modelcontextprotocol/go-sdk`)
- go-ethereum v1.17.2 (`github.com/ethereum/go-ethereum`)
- OpenTelemetry SDK v1.43.0
- Resilience: `cenkalti/backoff/v5`, `sony/gobreaker/v2`,
  `golang.org/x/time/rate`
- Auth: `MicahParks/keyfunc/v3`, `golang-jwt/jwt/v5`

### Target chain

- NVNM Chain (Inveniam L2), Chain ID `58887` (`0xe607`)
- MANTRA-secured consumer chain via Interchain Security
- Native currency: mUSD
- Anchor precompile: `0x0000000000000000000000000000000000000A00`

### Known limitations

- Multi-arch Docker image is **not** published to any registry yet
  (Dockerfile is buildx-ready; needs registry decision -- see
  `docs/IMPLEMENTATION_PLAN.md` backlog).
- CORS middleware not implemented (Low priority; only relevant for
  browser-based MCP clients without a reverse proxy in front).
- k6 load test script does not currently send `Authorization` headers; the
  server must be run with no keys configured for load testing, or the
  script must be extended.
- Self-serve API key request workflow is on the backlog (Medium priority).

[Unreleased]: https://github.com/inveniamcapital/NVNM_MCP_Server/compare/v1.0.0-rc.2...HEAD
[1.0.0-rc.2]: https://github.com/inveniamcapital/NVNM_MCP_Server/releases/tag/v1.0.0-rc.2
[1.0.0-rc.1]: https://github.com/inveniamcapital/NVNM_MCP_Server/releases/tag/v1.0.0-rc.1
