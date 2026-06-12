# Changelog

All notable changes to the NVNM Chain MCP Server are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

## [1.0.0-rc6] - 2026-06-11

> **Version naming note.** This release is tagged `v1.0.0-rc6` (no dot),
> continuing the `v1.0.0-rc4` / `v1.0.0-rc5` form rather than the dotted
> `v1.0.0-rc.N` used through rc.3. The no-dot form is deliberate: under SemVer
> pre-release precedence a dotted `1.0.0-rc.6` would sort *before* the existing
> `1.0.0-rc5` (the identifier `"rc"` is a prefix of `"rc5"`, so `"rc" < "rc5"`),
> ranking this release as older than its predecessor. `rc6` sorts correctly
> after `rc5`. The CHANGELOG header matches the tag string exactly.

### Security

- Bumped the Go toolchain to 1.26.4 (`go.mod` directive, Dockerfile base
  image `golang:1.26.4-alpine` + digest, and `GOTOOLCHAIN`) to patch two
  reachable standard-library advisories surfaced by `govulncheck`:
  `GO-2026-5039` (net/textproto, used by the SMTP and admin-HTTP paths)
  and `GO-2026-5037` (crypto/x509). No application code changes.

### Changed

- Boolean environment variables now fail loud on an unrecognized value
  instead of silently coercing it to the default. All seven boolean flags
  (`ENABLE_WRITE_TOOLS`, `ENABLE_PROMETHEUS`, `ENABLE_STDOUT_TELEMETRY`,
  `OTLP_INSECURE`, `MCP_KEYLESS_READS`, `NVNM_KEY_REQUEST_ENABLED`,
  `NVNM_TRUST_PROXY_HEADERS`) are parsed through a new `envBool` helper
  (`strconv.ParseBool` semantics: accepts `1/t/T/TRUE/true/True` and the
  false equivalents, trims whitespace). Previously a bare `== "true"`
  compare meant `ENABLE_WRITE_TOOLS=1` or `=True` silently produced a
  read-only server with no error; such values now abort startup with a
  message naming the offending key. Valid `true`/`false` configs are
  unaffected. The five `Load()`-level flags are grouped into a new
  `loadFeatureFlags` parser.

- `MCP_KEYLESS_READS` is now set explicitly in the Helm `values.yaml` and
  k8s `configmap.yaml` (self-hoster default `false`) and documented in
  `RUNBOOK.md` as a **required `true` invariant for the Inveniam-hosted
  deployment** — the published privacy policy represents that deployment as
  keyless-read, so running `false` there would falsify it. Self-hosters
  choose their own posture. No code change; the env var already existed.

### Fixed

- MCP authorization-spec compliance for HTTP transport: Claude-class clients
  (Claude Code / Desktop) no longer report "Needs authentication" when a valid
  static `Authorization: Bearer` token is configured. Two gaps are closed.
  (1) The OAuth discovery well-known paths `/.well-known/oauth-protected-resource`
  and `/.well-known/oauth-authorization-server` now return `404` — via a new
  `wellKnownGuard` ahead of `AuthMiddleware` (`internal/mcp/wellknown.go`) —
  instead of falling through to a gated `401`. A `404` signals "no OAuth
  discovery here, use your configured credentials"; the previous `401` read to
  a client as "OAuth-protected resource you cannot reach." The guard matches
  only those two exact paths, so `/.well-known/jwks.json` and any future
  well-known resource are unaffected. (2) Every `AuthMiddleware` `401` now
  carries a plain `WWW-Authenticate: Bearer` challenge (RFC 6750 / 7235) via a
  new `writeUnauthorized` helper — deliberately with **no** `resource_metadata`
  parameter, because this server authenticates opaque API keys / FusionAuth
  JWTs supplied out-of-band, not an OAuth flow. Credential validation, RBAC,
  and rate limiting are unchanged. Server-side behavior verified end-to-end
  through the full middleware chain.

- Corrected a false "non-eventful" privacy claim. The server's `initialize`
  instructions string (returned to every MCP client at session start) and
  `PRIVACY_DISCUSSION.md` stated the anchor precompile "emits no events." On-chain
  inspection (`eth_getLogs`) shows `add_record` emits an event exposing the
  anchored SHA-256 hash and `add_registry` exposes the registry name in public
  logs. The instructions string now states the true non-custody property (no
  server-side keys; prepare-sign-submit) and that anchored data is public —
  encode anything sensitive before anchoring. Supporting docs carry a dated
  retraction. No behavior change beyond the instructions-string content.

- Write-approval elicitation is now MCP-spec compliant. `evm_send_raw_transaction`
  under `write_approval: required` sends its confirmation prompt via MCP
  elicitation; the request previously carried only a `message` and **no
  `requestedSchema`**, which spec-strict clients (e.g. Claude / Fable 5) reject as
  malformed — so the broadcast could never complete from those clients (reported
  by an integrator whose script had to bypass the tool and write to chain
  directly). The request now sends `mode: "form"` and a valid `requestedSchema`
  (an object with one boolean `approve` property). The accept/decline/cancel
  action remains the decision. The gap was invisible in CI because the go-sdk
  in-process test client tolerates a nil schema; a new e2e test now asserts the
  outgoing request carries a valid form schema.

## [1.0.0-rc.5] - 2026-06-02

> **Version naming note.** Mantra tagged the previous release as
> `v1.0.0-rc4` (no dot) rather than the project's prior `v1.0.0-rc.3`
> (dotted) convention. This release returns to the dotted form for
> strict SemVer-pre-release compliance and to keep CHANGELOG headers
> consistent with the older entries. Both forms parse as valid SemVer;
> the dot is the project's continuing convention.

Phase 11 L3 self-serve API-key request endpoint complete; Phase 10 RD3
HTTP-level error-rate SLI implemented; Phase 9.14 carried-over k8s
manifest cleanups landed (**BREAKING** for existing deployments — see
`docs/RUNBOOK.md` § "K8s manifest migration (Phase 9.14 follow-up)");
miscellaneous OSS-hygiene work including the engineering-side Terms of
Service draft, Node.js 24 action bumps ahead of the 2026-09-16 runner
cutover, and the wallet-page repo migration from `inveniamcapital/`
into `NVNM-Chain/`.

### Added

- `internal/mcp/keys_pending.go`: file-backed `PendingKeyStore` for
  self-serve API-key requests with atomic-write JSON persistence and
  double-approve race guards. (PR #8 — Phase 11 L3 PR 1/3.)
- `internal/mcp/keys_request_http.go`: public `POST /api/v1/keys/request`
  endpoint with per-source-IP `KeyRequestRateLimiter`, body-size cap,
  email validation, and 202 `{request_id, status: "pending"}` response
  per Phase 11 RD3. New env vars: `NVNM_KEY_REQUEST_ENABLED` (opt-in),
  `NVNM_KEY_PENDING_FILE`, `NVNM_KEY_REQUEST_RATE_LIMIT`,
  `NVNM_KEY_REQUEST_RATE_BURST`, `NVNM_KEY_REQUEST_MAX_BODY_BYTES`.
  (PR #9 — Phase 11 L3 PR 2/3.)
- `internal/mcp/admin_keys_pending.go`: admin pending-review endpoints
  `GET /admin/keys/pending`, `POST /admin/keys/pending/{id}/approve`,
  `POST /admin/keys/pending/{id}/reject`. Approve mints the credential,
  persists the decision under a double-approve guard, and delivers the
  notification email; approve response includes the issued key so
  reviewers using the API directly (no SMTP) can deliver out-of-band.
  (PR #10 — Phase 11 L3 PR 3/3.)
- `internal/mcp/smtp.go`: provider-agnostic `EmailSender` interface
  with two implementations — `SMTPEmailSender` (plain SMTP with
  optional PlainAuth and CR/LF header-injection defense) and
  `LogOnlyEmailSender` (no-SMTP fallback that writes approval emails
  to structured logs for operators without SMTP wiring). New env vars:
  `NVNM_SMTP_HOST` / `NVNM_SMTP_PORT` / `NVNM_SMTP_USERNAME` /
  `NVNM_SMTP_PASSWORD` / `NVNM_SMTP_FROM` / `NVNM_SMTP_FROM_NAME`.
  Per Phase 11 RD2. (PR #10.)
- `internal/telemetry/http_responses.go` + new
  `internal/mcp/response_metrics.go` middleware: Phase 10 RD3 HTTP-
  level error-rate SLI with a `class` label on the new
  `mcp_http_responses_total` counter
  (`server_fault`/`customer_impact`/`client_error`/`success`).
  Wired in `server.go` as the outermost real-request layer (inside
  CORS, outside Origin guard). Adds three Prometheus alert rules to
  `deploy/prometheus/alerts.yaml`: `NvnmMCPServerFaultRate` (warn at
  1% 5xx ratio), `NvnmMCPServerFaultRateCritical` (5%), and
  `NvnmMCPCustomerImpactRate` (5% combined 5xx+429+408 ratio). (PR #5.)
- `internal/config.Config.WalletGeneratorURL` + wizard hook: the
  `nvnm_setup_wizard` `needs_wallet` response now surfaces the
  browser-hosted wallet generator page (default
  `https://wallet.nvnmchain.io`) alongside the existing snippet flow.
  New env var `NVNM_WALLET_GENERATOR_URL`. Phase 11 D-L8-2. (PR #7.)
- `docs/TERMS.md`: engineering-side Terms of Service draft for the
  hosted Service. Bakes in resolved decisions from
  `PHASE_11_DESIGN.md` § 14 — free-for-v1 + reserve-right-to-charge +
  no-grandfather (RD4); "reasonable efforts" availability (RD5);
  wallet-page out of scope (RD8); Apache 2.0 / Service bifurcation;
  acceptable-use enumerated against the real tool surface. Counsel-
  iteration items (jurisdiction, forum, effective date) appear in
  bracketed provisional form. (PR #2.)

### Changed

- **BREAKING for existing k8s deployments**: rename across
  `deploy/k8s/*` — namespace `inveniam-mcp` → `nvnm-mcp`; API-key
  mount path `/var/run/secrets/inveniam` → `/var/run/secrets/nvnm`;
  `app.kubernetes.io/part-of: inveniam` → `nvnm-chain` on every
  manifest; phantom `inveniam-keymgmt` reference in
  `networkpolicy.yaml` removed. Also fixes a Phase-9.14-era bug in
  `deploy/k8s/secret.yaml.example` (was still naming Secrets
  `inveniam-mcp-server-*` mismatching `deployment.yaml`, and was
  still setting `INVENIAM_EVM_RPC_URL` which fails loud at startup
  per Phase 8.9). Parallel-rollover migration documented in
  `docs/RUNBOOK.md` § "K8s manifest migration (Phase 9.14 follow-up)".
  (PR #6.)
- `.github/workflows/image.yml`: bumped all 5 Docker actions to
  versions on Node.js 24 ahead of the 2026-09-16 GitHub Actions
  runner cutover — `setup-qemu-action@v4`, `setup-buildx-action@v4`,
  `metadata-action@v6`, `login-action@v4`, `build-push-action@v7`.
  Per-action breaking-change analysis inline as workflow comments.
  Drop-in for this workflow's input shapes. (PR #4.)
- `nvnm_setup_wizard` `needs_wallet` response prose updated to
  introduce the wallet-generator URL alongside the language-specific
  code snippets. (PR #7.)

### Operational

- Wallet-generator-page repo migrated from
  `inveniamcapital/nvnm-wallet-page` (interim, 2026-05-27) to the
  canonical `NVNM-Chain/nvnm-wallet-page` via mirror-push of the
  scaffolding commit (`30cb60e`; same SHA preserved). Interim repo
  archived with deprecation banner. (PR #3.)
- `docs/IMPLEMENTATION_PLAN.md`: 6 backlog rows flipped to Completed
  (Phase 11 L3, k8s cleanups, Node.js 20, wallet-page migration,
  image.yml bumps). One row remains: marketing brief brand-positioning
  review (out of engineering scope). (PR #11.)
- `docs/RUNBOOK.md`: new env-var documentation rows for
  `NVNM_WALLET_GENERATOR_URL`, the five `NVNM_KEY_REQUEST_*` knobs,
  and the six `NVNM_SMTP_*` knobs.
- `docs/PRIVACY_DISCUSSION.md` § 3 (Draft B): `[TBD]` customer-
  onboarding PII schema replaced with the concrete L3 shape (email,
  optional company, free-text intended_use) now that the endpoint
  exists. The schema in code (`KeyRequestInput`) and the schema in
  the policy now match.

### Notes

- Phase 11 engineering scope is now complete. The remaining Phase 11
  exit criteria (Privacy Policy counsel sign-off, Anthropic/OpenAI
  directory submissions, launch announcement, support mailbox
  provisioning, beta-cohort onboarding) are non-engineering scope
  per the 2026-05-27 OQ walkthrough and belong with counsel /
  Inveniam Comms / Inveniam Product / Inveniam HR.
- Repo visibility flip from `private` to `public` is business-gated
  per the Phase 9.15 plan, not engineering-gated.

## [1.0.0-rc.3] - 2026-05-27

First signed release from the `NVNM-Chain/nvnm-mcp-server` home,
validating the Phase 9.14 Cosign cert-identity path end-to-end. Helm
chart bumps 0.2.1 → 0.2.2 to track the new image-tag default. Bundles
all the work in the Unreleased section below (Phase 9.4 DCO workflow,
Phase 9.7 multi-arch image + Cosign signing, Phase 9.14 repo move +
module-path rewrite, Phase 9.16 keyless-read middleware split, plus
the 2026-05-27 OQ-walkthrough resolutions baked into the design docs).

### Detail

Phase 9.14 (repo move + module-path rewrite): canonical home moved
from `inveniamcapital/NVNM_MCP_Server` (mixed-case placeholder org)
to `NVNM-Chain/nvnm-mcp-server` (lowercase-hyphen, matches the
destination org). Go module path rewritten from the vanity
placeholder `github.com/inveniam/nvnm-mcp-server` (which never
resolved to a real GitHub org) to
`github.com/NVNM-Chain/nvnm-mcp-server` (externally resolvable for
the first time). 112 occurrences across 60 files: 54 Go imports +
build/lint tooling (`.golangci.yml` local-prefixes, `Makefile`
goimports `-local`, `ci.yml` govulncheck `--ignore`, `release.yml`
ldflags `-X` target). Container image namespace moved from
`ghcr.io/inveniamcapital/nvnm-mcp-server` to
`ghcr.io/nvnm-chain/nvnm-mcp-server` (`Image` workflow `IMAGE_NAME`,
Helm `values.yaml`, k8s `deployment.yaml`, Helm chart README,
Phase 10 design doc, marketing brief, Privacy Policy publisher
identity table). Cosign cert-identity verification regex in release
notes updated to the new GitHub URL; prior releases
(`v1.0.0-rc.1`, `v1.0.0-rc.2`) retain their original release-page
links to `inveniamcapital/NVNM_MCP_Server` since the signed binary
assets and Cosign certificates were published under that identity.
Helm chart version 0.2.0 → 0.2.1 (rendered Deployment image
repository differs). Approach: fresh push to a Mantra-team-prepared
empty repo (full git history mirrored, `v1.0.0-rc.1` tag preserved),
not a GitHub repo transfer; no downstream consumers existed at the
old vanity Go path. In passing, 7 broken Prometheus `runbook_url`
entries pointing at `github.com/inveniam/NVNM_mcp_server` (which had
neither a valid org nor valid repo casing) were repaired to the new
home. Five operator-facing runtime-identifier cleanups (k8s
namespace `inveniam-mcp`, secret mount path
`/var/run/secrets/inveniam`, `app.kubernetes.io/part-of` label,
networkpolicy comment hygiene, marketing brand-positioning audit)
deferred to the Backlog Outstanding table for their own
operator-facing migration window.

Phase 9.7 (multi-arch container image + Cosign keyless signing):
added `.github/workflows/image.yml` building `linux/amd64` +
`linux/arm64` container images via Docker buildx (QEMU
cross-compile) and pushing to GHCR
(`ghcr.io/inveniamcapital/nvnm-mcp-server`). Triggers: `push` to
`main` (publishes `:main` + `:sha-<7>` tags), tag `v*` (publishes
`:<version>` + `:<major>.<minor>` + `:sha-<7>`), and `pull_request`
(builds only, no push). Manifest digest is keyless-signed via
`sigstore/cosign-installer@v3` with identity bound to
`token.actions.githubusercontent.com`; buildx SLSA provenance and
SBOM attestations are attached to the manifest. The Dockerfile was
already multi-arch-ready (`TARGETARCH` -> `GOARCH`). Dedicated
workflow file so QEMU's slow cross-compile path does not slow the
fast Go-test feedback loop in `ci.yml`. First push exercises the
path end-to-end.

Phase 9.4 (DCO sign-off CI hook): added
`.github/workflows/dco.yml` enforcing
`Signed-off-by: Name <email>` trailers on every non-merge commit in
a pull request. Failures print the offending SHAs and a fix recipe
(`git commit --amend -s` or `git rebase --signoff`). Workflow form
chosen over GitHub-App form because App installations are ephemeral
across the planned Phase 9.14 NVNM-Chain org transfer; a workflow
moves with the repo. `CONTRIBUTING.md` § 6 (DCO) updated to point at
the workflow file. The optional DCO GitHub App for per-commit
comment threading can be added later without changing the workflow.

Phase 9.16 (keyless-read auth middleware split): split the HTTP auth
chain so read tools can run anonymously while write tools keep their
existing auth requirement. New env vars `MCP_KEYLESS_READS=false`
(default), `MCP_ANON_RATE_LIMIT=5`, `MCP_ANON_RATE_BURST=5`
(HTTP-only; stdio remains all-trusted). New components:
`internal/mcp/authpolicy.go` introduces a fail-closed exempt-tool
registry (20 read/prepare tools exempt; only
`evm_send_raw_transaction` requires auth) plus an MCP receiving
middleware that rejects anonymous calls to gated tools;
`internal/mcp/anonrate.go` adds an `AnonReadRateLimiter` that throttles
anonymous traffic per source IP and bypasses authed requests.
`AuthMiddleware` now admits anonymous requests when the
`Authorization` header is fully absent (a present-but-invalid token
is still rejected). Telemetry omits `client_id` from logs and span
attributes on anonymous calls (absent, not empty-string) so anonymous
traffic carries no per-caller identifier. `apperrors.ErrAuthRequired`
added alongside `ErrPermissionDenied` and passed through
`SafeForClient` so the rejection reaches the MCP client with its
identity intact. Documented in `.env.example`, `docs/RUNBOOK.md`
env-var table, `docs/DATA_HANDLING.md` §§2/6/7.2. The Inveniam-hosted
Draft B privacy policy can now publish honestly (its "no per-customer
identifier on read traffic" commitment is enforced in code).
End-to-end coverage in `internal/mcp/server_e2e_test.go`
(`TestE2E_Keyless_*`). Backward-compatible: with the default
`MCP_KEYLESS_READS=false`, behavior is identical to pre-9.16.

Phase 9.2 (Issue + PR templates): added `.github/ISSUE_TEMPLATE/`
(`bug_report.md`, `feature_request.md` with a thin-proxy / no-custody
scope-fit checklist, `question.md` with a `docs/RUNBOOK.md` pre-flight)
plus `config.yml` (blank issues disabled; security reports routed to the
GitHub private-advisory flow per `SECURITY.md` rather than a raw email;
Discussions + docs contact links per the GitHub Discussions decision),
and `.github/PULL_REQUEST_TEMPLATE.md` (DCO sign-off, required
linked-issue/design field, docs/tests/CHANGELOG sync checklist). Honors
CONTRIBUTING.md §5's existing forward-reference to "the PR template."
Contributor-facing only; no runtime behavior change. Sequencing step 2
of Phase 9 (OSS Readiness).

Phase 9.5 (CORS middleware): added `internal/mcp/CORSMiddleware`, wired
as the outermost HTTP layer, so browser-hosted MCP clients can make
cross-origin requests. It shares the existing `NVNM_ALLOWED_ORIGINS`
allowlist but is a distinct concern from the Phase 8 Origin guard (CORS
*grants* browser permission; the Origin guard *rejects* spoofed origins —
both run). Answers `OPTIONS` preflight (`204` with `Allow-Origin`,
`Allow-Methods`, `Allow-Headers: Authorization, Content-Type,
Mcp-Session-Id`, `Max-Age`); exposes `Mcp-Session-Id` on actual
responses; `Access-Control-Allow-Credentials: false` (no cookies);
`Vary: Origin` when echoing. Server-to-server callers (no `Origin`
header) are unaffected. No new config. Built test-first. Docs:
`docs/RUNBOOK.md` "CORS (cross-origin browser access)" section.

Phase 9.6: Helm chart production polish. Chart bumped from `0.1.0`
to `0.2.0`. Hardened pod + container `securityContext`
(`runAsNonRoot`, distroless UID/GID 65532, `readOnlyRootFilesystem`,
all capabilities dropped, `seccompProfile: RuntimeDefault`); added
starter resource defaults (`100m/128Mi` request, `500m/256Mi`
limit, documented as starter values to be re-sized in Phase 10);
default `replicaCount: 2`; new optional templates for
PodDisruptionBudget (gated by `replicaCount > 1`), NetworkPolicy
(narrow egress + scoped MCP ingress), and Ingress (off by default,
cert-manager + nginx worked example in values.yaml). New chart
README at `deploy/helm/nvnm-mcp-server/README.md` covering values,
mainnet-vs-testnet, and gotchas. `helm lint` clean; `helm template`
renders 4 objects at defaults and 8 with all features enabled.
Sequencing step 6 of Phase 9 (OSS Readiness); no runtime behavior
change to the server binary.

Phase 9.9 (Makefile drift cleanup): the `run-local` target no longer
embeds chain values; it sources `.env` (failing loud if absent) and
runs `bin/nvnm-mcp-server --transport http`, so the operator's `.env`
is the single source of truth (no more stale retired-testnet `58887`
in the Makefile). Curl-probe targets (`healthz`, `readyz`, `metrics`)
now read `METRICS_ADDR` with a default of `:9190` matching
`.env.example` (was hardcoded `:9090`); they also switched from
`curl -s` to `curl -sSf` so HTTP errors fail non-zero. The four
broken per-tool probe targets (`mcp-init`, `mcp-chain-id`,
`mcp-registries`, `mcp-anchor-info`) were removed and replaced by a
single parameterized `make mcp-probe TOOL=<name> ARGS='<json>'` that
performs the full `initialize` -> `Mcp-Session-Id` capture ->
`notifications/initialized` -> `tools/call` handshake inline against
`MCP_HTTP_ADDR` (default `:8180`); pretty-prints with `jq` when
available. New `make mcp-probe-help` prints example usages. Help
text in `make help` updated accordingly. Makefile only; no behavior
change in the server.

Phase 9.12: mainnet cutover playbook landed at `docs/MAINNET_CUTOVER.md`.
Documents the testnet → mainnet config diff (`NVNM_EVM_RPC_URL`,
`NVNM_CHAIN_ID`, `NVNM_CHAIN_ENVIRONMENT`), validation sequence,
rollback path, and the open precompile-pagination question parked for
Phase 10 staging. Doc only; execution is Phase 10. Sequencing step 12
of Phase 9 (OSS Readiness); no behavior change.

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

Dependency bumps (2026-05-18): three workflow / base-image bumps
landed via Dependabot (golang 1.26.2 -> 1.26.3 alpine,
actions/download-artifact v7 -> v8, softprops/action-gh-release v2
-> v3). Plus a manual go-sdk bump 1.5.0 -> 1.6.0 (this PR) after
Dependabot's auto-rebase repeatedly failed on stale vendor/ state.

Phase 8.9: hard cut from the legacy `INVENIAM_*` env-var prefix to
`NVNM_*` and matching server-identity rename. Single coordinated
BREAKING change.

### Changed

#### Phase 9.6: Helm chart production polish

- `deploy/helm/nvnm-mcp-server/Chart.yaml`: chart `version` bumped
  from `0.1.0` to `0.2.0` (semver-meaningful chart change:
  added templates, new values keys, hardened defaults). `appVersion`
  left at `0.5.0` to match `values.image.tag`; Phase 10 will retag
  against the canonical app version in
  [`internal/version/version.go`](internal/version/version.go) at
  the next multi-arch release.
- `deploy/helm/nvnm-mcp-server/values.yaml`: structural rewrite
  with section-level comments explaining intent and override
  guidance. Notable defaults:
  - `replicaCount: 2` (minimum redundancy during voluntary
    disruptions; one pod cannot survive a node drain).
  - `resources.requests: {cpu: 100m, memory: 128Mi}` and
    `resources.limits: {cpu: 500m, memory: 256Mi}`. Documented as
    *starter* values aimed at staging; real capacity planning is
    Phase 10. Override per-environment.
  - `podDisruptionBudget.enabled: true` (with the rendering
    double-gated by `replicaCount > 1` -- a one-pod deployment
    cannot meaningfully satisfy `minAvailable=1`).
  - `networkPolicy.enabled: false` (off because not every CNI
    enforces NetworkPolicy; flip on for Calico / Cilium / etc.).
    Egress pre-populated for HTTPS (EVM RPC + FusionAuth JWKS),
    OTLP gRPC (4317), and DNS.
  - `ingress.enabled: false` with a worked cert-manager +
    nginx-ingress example commented in-file.
  - Pod- and container-level `securityContext` blocks expanded to
    cover `runAsNonRoot: true`, `runAsUser/Group: 65532`,
    `fsGroup: 65532`, `allowPrivilegeEscalation: false`,
    `readOnlyRootFilesystem: true`, `capabilities.drop: [ALL]`,
    and `seccompProfile.type: RuntimeDefault`. UID 65532 matches
    the distroless `nonroot` convention used by
    `gcr.io/distroless/static-debian12` (see [Dockerfile](Dockerfile)).
- `deploy/helm/nvnm-mcp-server/templates/deployment.yaml`: pod-
  level `securityContext` now renders the full block via `toYaml`
  so additions in `values.yaml` (notably `fsGroup` and
  `seccompProfile`) flow through without template edits.

Validation: `helm lint` clean (only the informational "icon is
recommended" hint); `helm template` smoke-tested in two modes
(default-values render produces 4 objects: ConfigMap, Service,
Deployment, PDB; full-feature render with NetworkPolicy + Ingress
+ HPA + ServiceMonitor enabled produces 8). No runtime behavior
change to the server binary. New templates (PDB, NetworkPolicy,
Ingress) are catalogued under `### Added` below.

#### Phase 9.11: README polish for OSS audience

- Added a one-paragraph elevator above the existing first lines: what
  the server is, who it is for, and why an unfamiliar reader should
  care. Existing intro paragraph preserved underneath.
- Added a status-badge row at the top: CI status (GitHub Actions on
  `main`), license (Apache 2.0), latest release (shields.io GitHub
  release tag), Cosign-signed (release pipeline).
- Added an ASCII "Request Flow" diagram of the HTTP middleware chain
  (`originGuard` -> `failGuarded` -> `limitRequestBody` ->
  `AuthMiddleware` -> `rateLimitMiddleware` -> MCP SDK -> tool handler
  -> EVM client). Sourced verbatim from
  [`internal/mcp/server.go`](internal/mcp/server.go).
- Added a "Documentation" link tree pointing at the OSS foundation
  files (`LICENSE`, `NOTICE`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`,
  `SECURITY.md`, `CHANGELOG.md`) and the deeper technical references
  (`docs/DESIGN.md`, `docs/RUNBOOK.md`, `docs/SECURITY_AUDIT.md`,
  `docs/DATA_HANDLING.md`, `docs/KEY_CUSTODY_THREAT_MODEL.md`,
  `docs/TOOL_REFERENCE.md`, `docs/IMPLEMENTATION_PLAN.md`).
- Added a "What this server is not" scope-statement section that
  mirrors `CLAUDE.md`'s private list (not a chain node, not a wallet,
  not a custodian, not an orchestrator), adapted to public-audience
  wording.
- Drive-by: flipped the License footer from the stale "Proprietary.
  All rights reserved." to Apache 2.0 with a pointer to the LICENSE
  file shipped by Phase 9.1.

No behavior change. Status paragraph, tools listing, configuration
env-var tables, and `docs/` structure listing left untouched per
Phase 9.11 scope.

#### Bump `github.com/modelcontextprotocol/go-sdk` 1.5.0 -> 1.6.0

- Direct dep `github.com/modelcontextprotocol/go-sdk` upgraded from
  1.5.0 to 1.6.0. Transitive: `github.com/google/jsonschema-go`
  0.4.2 -> 0.4.3.
- `vendor/` regenerated; 91 files changed (-1289 net lines as the
  vendor tree contracted around upstream cleanup).
- Manually applied because Dependabot's PR #19 auto-update repeatedly
  failed at the dependency-resolution level ("Dependabot failed to
  update your dependencies"). #19 is closed in favor of this PR.

Two upstream behavior changes in 1.6.0 were reviewed against this
codebase before merge:

- **Default cross-origin protection moved from on to off** in the
  SDK. Verified non-applicable: our own `originGuard` middleware sits
  at the *outermost* position in the HTTP handler chain (see
  [`internal/mcp/server.go:183`](internal/mcp/server.go#L183)),
  before the SDK ever sees a request. The SDK-level default change
  is independent of our enforcement.
- **`SetError` no longer overwrites `CallToolResult.Content`**.
  Verified non-applicable: `grep -rn "SetError\b" --include='*.go'
  cmd/ internal/` returns zero matches. We do not call `SetError`
  anywhere.

### Added

#### Phase 9.6: New Helm templates

- `deploy/helm/nvnm-mcp-server/templates/poddisruptionbudget.yaml` --
  optional PDB at `minAvailable: 1`. Gated by
  `podDisruptionBudget.enabled` AND `replicaCount > 1` so the
  template does not render an undrainable single-pod PDB by
  default.
- `deploy/helm/nvnm-mcp-server/templates/networkpolicy.yaml` --
  optional `networking.k8s.io/v1 NetworkPolicy`. Ingress opens the
  metrics port (9090) for in-cluster scrapers + kubelet probes and
  scopes the MCP port (8080) to peers listed in
  `networkPolicy.ingressFrom`. Egress is narrowed to HTTPS, OTLP
  gRPC, and DNS by default; override `networkPolicy.egressPorts`
  for tighter rules. The admin REST listener (default loopback) is
  intentionally omitted; the inline comment points operators at
  the example admin rule in
  [`deploy/k8s/networkpolicy.yaml`](deploy/k8s/networkpolicy.yaml).
- `deploy/helm/nvnm-mcp-server/templates/ingress.yaml` -- optional
  `networking.k8s.io/v1 Ingress`. Off by default; the chart does
  not presuppose an ingress controller. Worked example in the
  `values.yaml` comments uses cert-manager + nginx.

#### Mainnet cutover playbook (Phase 9.12)

- `docs/MAINNET_CUTOVER.md` -- new operator-facing playbook for moving a
  Helm/k8s deployment from testnet (`nvnm-testnet-1` / `787111`) to mainnet
  (`nvnm-1` / `1611`). Five sections: preconditions (RPC reachable,
  precompile present, FusionAuth if used, DNS, legacy `INVENIAM_*`
  hygiene), config changes (the three pinned env vars + ConfigMap and Helm
  diffs), validation sequence (pre-cutover sandbox + post-cutover
  production probes against `nvnm_overview`, `wallet_status`, `anchor_info`,
  `readyz`, and `evm.rpc.errors`), rollback (revert + redeploy; no
  on-chain unwind needed), and the open question about mainnet precompile
  pagination parked for Phase 10 staging.
- `README.md`: `docs/` listing updated to include `MAINNET_CUTOVER.md`.
- `docs/IMPLEMENTATION_PLAN.md` § 9.12: marked DONE.

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

Phase 9.16 (part 1 — FusionAuth client_id privacy): the FusionAuth JWT
`sub` is no longer logged. It is now hashed into `client_id` via a keyed
HMAC-SHA256 (`MCP_CLIENT_ID_HMAC_KEY`), so the logged identifier is stable
for audit correlation but not reversible to a real-world identity without
the server-held key; and the former DEBUG `subject` log line was removed
entirely. **Breaking for FusionAuth deployments:** `MCP_CLIENT_ID_HMAC_KEY`
is now REQUIRED when `AUTH_PROVIDER=fusionauth` — startup fails loud if
unset (at both config validation and validator construction). `client_id`
values change from the raw `sub` to its HMAC, so historical log
correlation across this upgrade is broken (acceptable). apikey deployments
are unaffected. Implements privacy-policy decisions §2.1 D4/D9. (Remaining
9.16 work — the keyless-read middleware split — lands separately.)

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

[Unreleased]: https://github.com/NVNM-Chain/nvnm-mcp-server/compare/v1.0.0-rc6...HEAD
[1.0.0-rc6]: https://github.com/NVNM-Chain/nvnm-mcp-server/releases/tag/v1.0.0-rc6
[1.0.0-rc.5]: https://github.com/NVNM-Chain/nvnm-mcp-server/releases/tag/v1.0.0-rc5
[1.0.0-rc.3]: https://github.com/NVNM-Chain/nvnm-mcp-server/releases/tag/v1.0.0-rc.3
[1.0.0-rc.2]: https://github.com/inveniamcapital/NVNM_MCP_Server/releases/tag/v1.0.0-rc.2
[1.0.0-rc.1]: https://github.com/inveniamcapital/NVNM_MCP_Server/releases/tag/v1.0.0-rc.1
