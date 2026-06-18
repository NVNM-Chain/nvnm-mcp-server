# Contributing to NVNM Chain MCP Server

Thanks for your interest in contributing. This document is for external Go
engineers who want to file an issue or send a pull request. It assumes
baseline Go competence and does not duplicate content from the
[README](README.md) or [`docs/RUNBOOK.md`](docs/RUNBOOK.md).

## 1. Before you contribute

This repository is the canonical NVNM Chain MCP Server: a Go-based
[Model Context Protocol](https://modelcontextprotocol.io/) server that
exposes the NVNM Chain (an Inveniam L2 on MANTRA) through a curated set of
typed tools. It is licensed under Apache 2.0; see [`LICENSE`](LICENSE) and
[`NOTICE`](NOTICE).

**Architectural invariants — please read these before proposing changes
that touch the core flows.** They are deliberate, load-bearing, and won't
change:

- **Zero key custody.** The server holds no signing keys, ever. Write
  tools construct unsigned transactions; signing happens caller-side. See
  [`docs/KEY_CUSTODY_THREAT_MODEL.md`](docs/KEY_CUSTODY_THREAT_MODEL.md)
  for the rationale. Proposals that put a signing key in this server will
  be closed with a pointer to that document.
- **Privacy-by-design.** No personal data about end-users is collected or
  stored; the only persisted identity material is hashed API-key entries.
  See [`docs/DATA_HANDLING.md`](docs/DATA_HANDLING.md).
- **No internal orchestration.** Tools return `next_actions` hints so
  callers chain calls themselves; the server does not call other tools
  internally.
- **Multi-instance, not multi-chain.** Chain choice is encoded in which
  URL a client connects to (`NVNM_CHAIN_ID` + `NVNM_CHAIN_ENVIRONMENT`
  pinned at startup). Same-session chain switching is not supported; see
  [`docs/DESIGN.md`](docs/DESIGN.md) for the reasoning.

If your proposed change conflicts with any of these, please open an issue
to discuss before sending a PR.

## 2. Dev setup

You'll need:

- **Go 1.26 or newer.**
- **`golangci-lint` v2.11.4.** CI pins this exact version; newer versions
  may surface lint findings that CI does not, leading to local-vs-CI
  disagreement. Please match the pinned version locally.
- **`govulncheck`** for the vulnerability check CI runs.
- **`pre-commit`** for the project's git hooks (formatting, secret
  scanning, etc.).
- **`detect-secrets`**, invoked by `pre-commit`.

Install everything via:

```sh
make setup-dev
```

Then set up your environment:

```sh
cp .env.example .env
# Edit .env with your NVNM_* chain config and (for integration tests)
# testnet credentials. See docs/RUNBOOK.md for the env-var reference.
```

The `NVNM_*` env-var family is the only one the server reads for chain
configuration. The legacy `INVENIAM_*` prefix is hard-rejected at startup;
do not re-introduce it. See
[`docs/RUNBOOK.md`](docs/RUNBOOK.md) `#env-var-migration`.

## 3. Build & test

```sh
make build               # Builds bin/nvnm-mcp-server
make test                # Unit + MCP E2E tests; fast.
make test-integration    # Integration tests against a live testnet.
                         #   Requires testnet credentials in .env.
make lint                # golangci-lint (v2.11.4 expected)
make check-all           # format + vet + lint, the same gates CI runs
```

Run `pre-commit run --all-files` (or `pre-commit run` against staged
changes) **before** `git commit`, not as a discovery step inside it. The
commit-time hook will reject otherwise, costing you a re-stage and a
retry.

## 4. Test layers

Three test layers, each with its own placement and execution rule:

- **Unit tests** live alongside the source
  (`internal/foo/foo.go` → `internal/foo/foo_test.go`). No build tag. Run
  by `make test` / `go test ./...`. Prefer table-driven tests where cases
  share a shape.
- **Integration tests** carry the `//go:build integration` constraint and
  require live testnet credentials. Run via `make test-integration`. They
  hit the real EVM JSON-RPC and verify end-to-end transaction submission;
  do not mock the testnet inside these.
- **MCP end-to-end tests** live in
  [`internal/mcp/server_e2e_test.go`](internal/mcp/server_e2e_test.go).
  They spin the server in-process, exercising it as an MCP client would.
  Part of the default `make test` run.

When adding new behavior: extend the layer that owns the new surface.
Adding a new tool means updating both the MCP E2E test (registration +
invocation) and the unit tests for the handler. Changing a request /
response shape always needs unit-test coverage; integration tests are
re-run when the change crosses the boundary they exercise.

## 5. Commit & PR norms

- **Commit format:** [Conventional Commits](https://www.conventionalcommits.org/).
  The existing log shows the shape: `docs(security):`, `feat(mcp):`,
  `chore(graphify):`. Type prefixes are not optional.
- **One logical unit per commit.** A "logical unit" is usually one commit;
  sometimes 2–3 commits that only make sense together (interface change +
  caller updates + tests).
- **Commit messages reference design docs, not implementation details.**
  Explain *why* in the body. The diff already shows *what* changed.
- **`git commit --no-verify` is forbidden.** If a pre-commit hook fails,
  diagnose and fix the cause rather than skipping the hook.
- **Docs and tests stay synced with code.** A code change that adds,
  removes, or changes observable behavior updates the docs that describe
  that surface and the tests that cover it, in the same commit. Drift is
  the next commit's job to fix when noticed.
- **PRs:** link an issue or design doc; include a "Test plan" describing
  how you verified the change; check the DCO sign-off and "docs and tests
  updated" boxes; mark BREAKING if applicable. The PR template prompts
  for these.
- **`git blame` and bulk rewrites:** mechanical rewrites that touch many
  files (e.g., the Phase 9.3 SPDX-header addition) are recorded in
  [`.git-blame-ignore-revs`](.git-blame-ignore-revs). GitHub's blame
  view honors this file automatically; to make local `git blame`
  honor it, run once per clone:

  ```sh
  git config blame.ignoreRevsFile .git-blame-ignore-revs
  ```

## 6. Developer Certificate of Origin (DCO)

This project requires a Developer Certificate of Origin (DCO) sign-off on
every commit. The DCO is a per-commit attestation that you have the right
to submit the code under the project's license. It's one line in the
commit; there is no separate Contributor License Agreement (CLA).

To sign off a commit, append `-s` to `git commit`:

```sh
git commit -s -m "feat(mcp): add new tool"
```

This appends a line to your commit message:

```
Signed-off-by: Your Name <your.email@example.com>
```

CI enforces DCO via [`.github/workflows/dco.yml`](.github/workflows/dco.yml).
Every non-merge commit in a pull request must carry the trailer; the
workflow walks `base..head` and prints the specific commit SHA on
failure. Merge commits are exempt. Operators may additionally install
the standard DCO GitHub App for per-commit status comments, but the
workflow alone is sufficient to gate merges.

If you forgot to sign off an earlier commit, fix it with
`git commit --amend -s` (latest commit) or `git rebase --signoff`
(multiple commits), then force-push the corrected branch to your fork.

For non-issue, non-security questions you'd rather not file as a public
issue, email <EMAIL_TBD:maintainers@nvnmchain.io>.

## 7. Security disclosures

**Do not file security issues as public GitHub issues, in PR
descriptions, in commit messages, or in community forums.** Coordinated
disclosure protects users who haven't yet patched.

The private reporting channel and the response SLO are in
[`SECURITY.md`](SECURITY.md). GitHub Security Advisories is the primary
channel; a fallback email is also available.

## 8. Vendor directory

This repository commits its Go dependencies under `vendor/` and builds
with `-mod=vendor` for supply-chain hardening. If your PR touches
`go.mod` or `go.sum`, you must also regenerate the vendor tree in the
same commit:

```sh
go mod tidy
go mod vendor
```

CI verifies the vendor tree is consistent with `go.mod`
(`go mod verify`) and that running `go mod vendor` produces no diff
(`git diff --exit-code vendor/`). PRs that update deps but forget the
vendor regen will fail CI.

We don't accept blind dependency bumps. Each new (or upgraded)
dependency must:

- Be license-compatible with Apache 2.0 — see
  [`docs/LICENSE_EXCEPTIONS.md`](docs/LICENSE_EXCEPTIONS.md) for the
  project's allowed/forbidden license matrix.
- Pass `govulncheck` against the proposed version.
- Survive the existing test suite (CI gates this automatically).

### Triaging Dependabot security alerts

Dependabot and `govulncheck` answer different questions, and on a
vendored repo they often disagree. Dependabot reasons over the
**dependency graph** — if a vulnerable module *version* is present in
`go.mod` it alerts, even when nothing imports the affected package.
`govulncheck` reasons over the **call graph** — it reports a finding
only when a vulnerable symbol is reachable from code this module
actually executes. A HIGH Dependabot alert with a clean `govulncheck`
is the common case for transitive, unimported modules.

When an alert fires:

1. Confirm reachability — run `govulncheck ./...` (pinned version, as
   CI does). "Your code is affected by 0 vulnerabilities" means the
   advisory is not on any execution path.
2. Check whether the module is even imported —
   `go mod why <module>`. "main module does not need package …" means
   it's a require-graph passenger (a sub-module of the same repo is
   usually what's actually used; e.g. `btcd/btcec/v2` for secp256k1,
   not the `btcd` root).
3. **Prefer bumping to a patched version over dismissing the alert**,
   even when unreachable — a bump silences the recurring graph-level
   nag permanently, whereas a dismissal lurks and re-surfaces. Reserve
   dismissal for cases where no patched version exists yet. Follow the
   `go mod tidy` + `go mod vendor` steps above; the PR diff will
   include the regenerated `vendor/` tree.

Note the reachability verdict and the import-path reasoning in the PR
body so a reviewer can see *why* the bump is hygiene vs. a fix.

## Code of conduct

Participation in this project is governed by the
[Code of Conduct](CODE_OF_CONDUCT.md). The enforcement contact is in
that file.

## License

By contributing, you agree that your contributions are licensed under
the Apache License, Version 2.0 — see [`LICENSE`](LICENSE) and
[`NOTICE`](NOTICE).
