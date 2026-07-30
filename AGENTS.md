# AGENTS.md — Development Guide for AI Agents and Contributors

This file is the entry point for AI coding agents (and a quick orientation
for humans) working on this repository. It defines the scope of the
project, the non-negotiable practices, and the testing requirements every
change must meet. It complements — and defers to —
[CONTRIBUTING.md](CONTRIBUTING.md),
[docs/standards/CODING_STANDARDS.md](docs/standards/CODING_STANDARDS.md),
and [docs/TESTING.md](docs/TESTING.md); read those before large changes.

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
- **Update the docs that own the surface you changed:** `docs/TESTING.md`
  for new test layers/helpers, `docs/TOOL_REFERENCE.md` for tool changes,
  `README.md` for operator-facing config, `CHANGELOG.md` under
  Unreleased.
- **Dependency updates (e.g. Dependabot):** treat the full CI suite as
  the safety net it is — run `make coverage-check` and `govulncheck`
  locally against the bump; check the dep's changelog for breaking
  changes in APIs this repo actually calls before approving.
