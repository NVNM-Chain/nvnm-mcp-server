# Onboarding & Orientation

**Purpose:** get a new contributor productive fast — a reading order through the existing docs, the design decisions that look like bugs but aren't, and the local workflow that keeps CI green. This is a map, not a re-explanation: each section points at the canonical document rather than restating it.

**Who this is for:** Go developers picking up feature work, security remediation, or releases on this server. If you operate a deployment rather than change the code, start with [`RUNBOOK.md`](RUNBOOK.md) instead.

---

## 1. What this server is (the 60-second version)

A typed [Model Context Protocol](https://modelcontextprotocol.io/) bridge between AI agents and NVNM Chain (Inveniam's L2 on MANTRA). It exposes **21 curated, typed tools** across four surfaces — EVM reads, anchor reads, prepare-sign-submit **writes**, and a guided onboarding wizard — with normalized responses and **zero private-key custody**. It is deliberately **not** a JSON-RPC passthrough.

Read next, in order:

1. [`../README.md`](../README.md) — the technical entry point; request-flow middleware diagram and env-var table.
2. [`DESIGN.md`](DESIGN.md) — architecture decisions, package dependency graph, tool design, write-transaction flows, security considerations. **The canonical architecture reference.**
3. [`TOOL_REFERENCE.md`](TOOL_REFERENCE.md) — the per-tool contract for all 21 tools.
4. [`KEY_CUSTODY_THREAT_MODEL.md`](KEY_CUSTODY_THREAT_MODEL.md) — why the server never holds a key.

Everything below assumes you've skimmed those.

---

## 2. Design decisions that look like bugs but aren't

The single most useful thing to internalize before changing code. Each of these is intentional; "fixing" it breaks the security or compliance model. Where a decision has a canonical write-up, follow the link.

1. **Zero key custody / authless writes.** The server never holds a private key. Clients sign locally (prepare → sign → submit). The hosted deployment accepts *anonymous* signed writes. Rationale: [`KEY_CUSTODY_THREAT_MODEL.md`](KEY_CUSTODY_THREAT_MODEL.md), [`DESIGN.md`](DESIGN.md) §4.
2. **Precompile-only relay scope, enforced on every path.** `evm_send_raw_transaction` only relays transactions to the anchor precompile. Both the authed and keyless paths share one decode-and-scope helper (`internal/mcp/tools_evm_write.go`) so the scope holds *by construction*, not by two code paths kept in sync. Self-hosters can opt out with `MCP_RELAY_ALLOW_ANY` (default `false`), which **fails to boot if combined with keyless writes** — anonymous writes stay pinned to the precompile. See [`TOOL_REFERENCE.md`](TOOL_REFERENCE.md) §16.
3. **The EVM library is `defiweb/go-eth`, not `go-ethereum`.** This is a licensing constraint, not a preference: `go-ethereum` carries LGPL/GPL terms incompatible with this project. Use `DecodeRLP`/`EncodeRLP`/`ECRecoverer.RecoverTransaction`; do not reach for go-ethereum idioms. See [`LICENSE_EXCEPTIONS.md`](LICENSE_EXCEPTIONS.md).
4. **The version constant is deliberately `dev`.** `internal/version/version.go` stays `dev`; the real version is injected at build time from the git tag via `-ldflags -X`. Hardcoding it produces a restated version that drifts silently; `dev` makes a broken injection loud. Do not hardcode a release string here.
5. **Data retention defaults to "retain indefinitely."** Every retention window is unset by default. An operator must set the retention environment variables for any bounded-retention claim to hold. See [`DATA_HANDLING.md`](DATA_HANDLING.md).
6. **The anonymous-write quota is a soft ceiling.** It is a coarse throttle read-then-incremented without a lock, so brief over-admission under concurrency is possible and *accepted* — see [`DATA_HANDLING.md`](DATA_HANDLING.md) §8.2. Don't harden it into a strict lock without revisiting that tradeoff.
7. **Untrusted node responses are decode-guarded.** Responses from the upstream JSON-RPC node are hostile-input surfaces: ABI/RLP decoders can panic on crafted blobs. The node-decode paths recover from those panics, and fuzz corpora guard against regression. Do not remove those guards.

---

## 3. Codebase map

Full package responsibilities and the dependency graph live in [`DESIGN.md`](DESIGN.md) §2. Quick index of `internal/`:

| Package | Responsibility |
|---|---|
| `mcp/` | The 21 tool handlers, server, and HTTP middleware chain |
| `evm/` | EVM client, RLP decode, signer recovery (via `defiweb/go-eth`) |
| `anchor/` | The chain's anchoring precompile interface — the flagship surface |
| `auth/` | API-key + FusionAuth auth, admin auth, key store |
| `config/` | All environment variables and boot-time validation (fail-fast lives here) |
| `logging/` | Structured logging and redaction |
| `telemetry/` | OpenTelemetry metrics/traces (OTLP gRPC exporter) |
| `errors/`, `version/` | Error taxonomy; injected build version |

Entrypoints in `cmd/`: `nvnm-mcp-server` (server + admin), `key-mgmt`, `query-anchor`, `seed-test-data`.

The request flow — how a call traverses the middleware chain before reaching a handler — is diagrammed in [`../README.md`](../README.md) ("Request Flow").

---

## 4. Local workflow & the quality gate

Setup and contribution conventions (branching, PRs, DCO sign-off) are in [`../CONTRIBUTING.md`](../CONTRIBUTING.md). Testing strategy is in [`TESTING.md`](TESTING.md). This section only adds the CI-parity gate.

- **Discover commands with `make help`.** Common ones: `make build`, `make test`, `make lint`, `make vet`, `make check-all`, `make format`.
- **CI checks the whole tree; local hooks only touch changed files.** Run the equivalent of CI before your first push, not as a discovery step inside `git commit`:
  ```
  go build ./... && go vet ./... && scripts/check_license_headers.sh && make lint && go test -mod=vendor -race ./...
  ```
- **Match CI's pinned linter version.** golangci-lint is version-pinned in `.github/workflows/ci.yml`; a newer local build can report false positives the pinned version does not. Use the version CI pins, not whatever your package manager installs.
- **Three CI-only gates that a plain `go build` won't catch:**
  - **License headers** — every source file carries an SPDX line; `scripts/check_license_headers.sh` enforces it.
  - **Secret scanning** — a baseline gate runs as a separate job; keep `.secrets.baseline` in sync.
  - **DCO sign-off** — every commit needs `Signed-off-by`; commit with `git commit -s`.
- **The repo vendors dependencies.** Use `GOFLAGS=-mod=mod` for `go get`/`go install`, and run `go mod vendor` after any dependency change.
- **Docs and tests are part of the same change as the code.** Before committing, audit which docs reference the surface you changed and which tests cover it, and update them in the same PR.

---

## 5. Releases

The full deployment topology is in [`DESIGN.md`](DESIGN.md) §8 and operational procedure in [`RUNBOOK.md`](RUNBOOK.md). Release mechanics for contributors:

- **What warrants a release candidate:** only a change that alters the compiled **image**. CI-workflow-only and docs-only changes ride the next code RC.
- **Cutting an RC:** promote `CHANGELOG.md`'s `## [Unreleased]` section to `## [<version>] - <date>` (release notes are extracted from it, so an un-promoted changelog yields empty notes), then create an annotated, signed tag on the verified tip. The built binary must self-report the tag, not `dev`.
- **Go version bumps touch three pins in lockstep:** the `go.mod` directive, the Dockerfile base-image digest, and the Dockerfile `GOTOOLCHAIN`. Miss the toolchain pin and the image build fails at dependency download; verify with `docker build --target builder`.
- Images publish to the container registry, are Cosign-signed, and ship an SBOM.

---

## 6. Security posture

- **Public threat-model and audit docs:** [`KEY_CUSTODY_THREAT_MODEL.md`](KEY_CUSTODY_THREAT_MODEL.md), [`SECURITY_AUDIT.md`](SECURITY_AUDIT.md), [`OWASP_AUDIT.md`](OWASP_AUDIT.md), [`SECURITY_CONSUMER_GUIDANCE.md`](SECURITY_CONSUMER_GUIDANCE.md), and [`../SECURITY.md`](../SECURITY.md) for disclosure.
- **The architecture invariants** (relay-scope-every-path, no caller-supplied key, no caller-controlled metric labels, no credentials in logs, keyless mode fully gated, untrusted-decode panic-guarded, admin loopback-by-default) are the security review contract. They are applied at review time, not enforced by CI — respect them when changing the affected surfaces.
- **Detailed internal security assessments and design records are kept out of the public tree** by policy; if you need them for a security-sensitive change, ask a maintainer.

---

## 7. Where to go next

- Operating a deployment → [`RUNBOOK.md`](RUNBOOK.md), [`INCIDENT_RUNBOOK.md`](INCIDENT_RUNBOOK.md)
- Wallet / signing integration → [`METAMASK_GUIDE.md`](METAMASK_GUIDE.md), [`DESIGN.md`](DESIGN.md) §4
- Privacy / data questions → [`DATA_HANDLING.md`](DATA_HANDLING.md)
- Adding or changing a tool → [`TOOL_REFERENCE.md`](TOOL_REFERENCE.md), [`DESIGN.md`](DESIGN.md) §3
