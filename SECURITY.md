# Security Policy — NVNM Chain MCP Server

We take security reports seriously and respond on a published timeline. This
document tells external researchers how to reach us privately, what is in
scope, the deliberate hardening invariants we don't consider bugs, and the
response expectations you can hold us to.

## Reporting a vulnerability

**Primary channel: GitHub Security Advisories.** Go to this repository's
"Security" tab and select "Report a vulnerability". This opens a private
channel with the maintainers; reports are encrypted in flight and never
become public until coordinated disclosure.

**Fallback channel:** email <EMAIL_TBD:security@nvnmchain.io>. Use this only
if GitHub Security Advisories is unavailable to you.

Please **do not** file vulnerabilities as public GitHub issues, in
pull-request descriptions, in commit messages, or in any community forum.
Coordinated disclosure protects users who haven't yet patched.

## Scope

**In scope:**

- Any code in this repository's `main` branch.
- The published Docker image (once published; see release notes for the
  digest).
- The Helm chart at [`deploy/helm/nvnm-mcp-server/`](deploy/helm/nvnm-mcp-server/).

**Out of scope:**

- Issues in third-party dependencies. Route those upstream; mention them in
  your report only if their exploitation against this project's configuration
  is non-obvious.
- Issues in deployed instances run by third parties (hosted services, forks).
  Route those to the operator of that instance.
- Social engineering of project maintainers.
- Physical attacks on contributor hardware or infrastructure.

## Hardening invariants (deliberate, not bugs)

The following are intentional design properties of the server. Reports that
treat them as bugs are misunderstandings rather than vulnerabilities. We list
them so researchers don't burn cycles on them.

- **Zero key custody.** The server holds no signing keys, ever. Write tools
  follow a prepare-sign-submit pattern; signing happens caller-side. See
  [`docs/KEY_CUSTODY_THREAT_MODEL.md`](docs/KEY_CUSTODY_THREAT_MODEL.md) for
  the full rationale.
- **API keys hashed at rest, indexed by hash in memory.** SHA-256 hashes only;
  raw keys are never written to disk
  ([`internal/mcp/keys.go`](internal/mcp/keys.go)).
- **Constant-time auth-path comparison** with a placeholder compare on the
  miss path so unknown-key and known-key request timings match
  ([`internal/auth/`](internal/auth/)).
- **HTTP transport requires `Origin` header validation** as the outermost
  middleware, with allowlist via `NVNM_ALLOWED_ORIGINS`
  ([`internal/mcp/origin.go`](internal/mcp/origin.go)).
- **Write tools default to `WRITE_APPROVAL_DEFAULT=required`** — human in the
  loop via MCP elicitation before broadcasting.
- **Rate limiting** is enforced per API key (token bucket) and per source IP
  (failure-rate limiter for credential-stuffing).
- **Legacy `INVENIAM_*` env vars are deliberately rejected at startup** with
  a pointer to the migration runbook. The fail-loud policy is intentional
  hygiene against silent-drift config; it is not a configuration bug.
- **Test fixtures pin EVM chain ID `58887`** (a retired testnet) on purpose.
  This is documented test scaffolding, not stale code — see the project
  notes for the reasoning.

For the full audit history, see
[`docs/SECURITY_AUDIT.md`](docs/SECURITY_AUDIT.md). That file is a frozen
point-in-time snapshot — do not amend it; new findings get their own
appendix sections.

## Response SLO

- **Acknowledgement:** 3 business days from receipt.
- **Initial severity assessment:** 7 business days from receipt.
- **Coordinated disclosure window:** standard 90 days from initial report.
  Negotiable downward for critical issues that are actively exploited; let
  us know your timeline in the initial report if it differs from the
  default.

## What you'll get from us

- An acknowledgement on the SLO above.
- A severity assessment with reasoning we can both inspect.
- Credit in the release notes for the fix (or anonymity, if you prefer).

## What we don't offer

- **No monetary bug bounty.** We say this plainly so reports don't open with
  an expectation we won't meet. Acknowledgement, severity assessment, fix,
  and named credit are committed; cash is not.

## Related documents

- [`docs/SECURITY_AUDIT.md`](docs/SECURITY_AUDIT.md) — point-in-time security snapshot.
- [`docs/OWASP_AUDIT.md`](docs/OWASP_AUDIT.md) — OWASP Top 10 coverage matrix.
- [`docs/DATA_HANDLING.md`](docs/DATA_HANDLING.md) — privacy-by-design technical reference.
- [`docs/KEY_CUSTODY_THREAT_MODEL.md`](docs/KEY_CUSTODY_THREAT_MODEL.md) — zero-key-custody rationale.
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — contributor norms, including security-disclosure conduct.
- [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md) — community standards.
