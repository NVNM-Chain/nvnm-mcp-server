# Open questions for the hosted operator and the listing owner

Questions the repository cannot answer on its own. Each one blocks a
specific item; the answer, dated, closes it. Written 2026-09-10 while
finishing rc21. Send section 1 to whoever runs the hosted MCP
deployment; send section 2 to whoever decides on the Anthropic
connector-directory submission.

Record answers inline (date, who answered, value) and then update the
document that owns the item — `docs/SECURITY_AUDIT.md` for section 1,
the rc22 P6 ticket for section 2.

## 1. Hosted deployment configuration (closes security finding F2)

Context: `docs/SECURITY_AUDIT.md`, update 2026-09-10. Findings F1/F4/F5
are closed by code in this tree and F3 is accepted; F2 (persisted admin
audit) is closed in code but only *takes effect* when the deployment
sets the Postgres DSN. A code review cannot see the running config.

Answer per environment: **testnet** (`mcp-testnet.nvnmchain.io`) and
**mainnet** (`mcp.nvnmchain.io`).

| # | Question | Why it matters | Closes |
|---|---|---|---|
| 1.1 | Is `MCP_KEYLESS_PG_DSN` set on the running server? (If `MCP_KEYLESS_WRITES=true` the binary refuses to boot without it, so "yes" is implied — please confirm rather than infer.) | Without it, `write_audit` and `admin_audit` are logs-only; the log pipeline is the audit store. | F2 |
| 1.2 | Does that Postgres contain the `write_audit` and `admin_audit` tables, with rows — i.e. has migration `internal/mcp/migrations/0004_admin_audit.sql` been applied? | Proves the store is wired, not just configured. | F2 |
| 1.3 | Is `ADMIN_API_KEYS_FILE` set (per-admin identity), or only the shared `ADMIN_API_KEY`? | Determines whether `admin_audit.actor_id` names a person or the shared `admin` identity. | F5 (attribution quality) |
| 1.4 | Is `NVNM_KEY_REQUEST_ENABLED` on? If yes, is `NVNM_SMTP_HOST` set — so `NVNM_ALLOW_KEY_IN_LOGS=true` is **not** in use and minted keys are emailed, never logged? | Confirms F3's acceptance rationale (key goes to the requested email) and F4's gate hold in production. | F3, F4 |
| 1.5 | What is the log retention for the server's structured logs (days, and where)? | If 1.1 is "no" anywhere, this retention *is* the audit retention and must be stated in the posture. | F2 fallback |
| 1.6 | Is `MCP_RELAY_ALLOW_ANY` set anywhere? | Should be unset/false. If set, the accepted F1 residual (undecodable tx → no `write_audit` row) is live, not theoretical. | F1 |

Answers:

- 1.1 — testnet: ___ · mainnet: ___
- 1.2 — testnet: ___ · mainnet: ___
- 1.3 — testnet: ___ · mainnet: ___
- 1.4 — testnet: ___ · mainnet: ___
- 1.5 — ___
- 1.6 — testnet: ___ · mainnet: ___

## 2. Anthropic connector-directory submission (rc22 ticket P6)

Context: `.scratch/rc22/spec.md` ticket P6 lists the artefacts a listing
needs and asks one gating question first. Nothing here blocks the chain
product; it blocks *being listed*.

### 2.1 Go / no-go and ownership

| # | Question |
|---|---|
| 2.1.1 | Are we submitting to the Anthropic MCP connector directory this cycle (rc22), later, or not at all? If "later", which release is the target? |
| 2.1.2 | Who owns the submission end to end (fills the form, answers reviewer questions, maintains the listing)? Engineering can produce the artefacts but cannot be the accountable contact. |
| 2.1.3 | Which deployment is listed — testnet only, mainnet only, or both as separate connectors? (The server is one-chain-per-instance; a listing is one URL.) |
| 2.1.4 | Does the listing require an Anthropic organisation/developer account we already hold, and who controls it? |

### 2.2 Artefacts the packet needs (each: exists? who provides? where does it live?)

| # | Artefact | Current state in the repo | Question |
|---|---|---|---|
| 2.2.1 | Connector manifest (`manifest.json` or the directory's equivalent: name, description, server URL, auth type, tool list) | Not present | Who writes it, and which auth mode is declared — keyless reads + API key for broadcast (the hosted default), or API key for everything? |
| 2.2.2 | Privacy policy URL (public, stable) | `docs/NVNM_MCP_Privacy_Policy_Jul_2026.pdf` exists in the repo | Is there a hosted HTML/PDF URL on a company domain, or is the GitHub raw link acceptable to the reviewers? Is the July 2026 version current? |
| 2.2.3 | Terms of service URL | `docs/TERMS.md` exists | Same as above — hosted URL, and confirmation it is the counsel-approved text. |
| 2.2.4 | README privacy / data-handling section aimed at end users (not operators) | `docs/DATA_HANDLING.md` is operator-facing | Does the directory want a short user-facing summary in the README, and who signs off on its wording? |
| 2.2.5 | Icon / logo (format and size per the directory's spec) | Not present | Who supplies the brand asset, and is NVNM or Inveniam the listed brand? |
| 2.2.6 | Example prompts (3–5 that showcase the tools) | Not present | Engineering can draft from `docs/TOOL_REFERENCE.md`; who approves the wording? |
| 2.2.7 | Reviewer test account / credentials | Not present | Reviewers will call the hosted server. Do they get an API key (which roles?), or is keyless-reads sufficient for review? Who mints and revokes it? |
| 2.2.8 | Support / security contact | `SECURITY.md` has a security contact | Is the same address acceptable as the listing's support contact, and is it monitored? |
| 2.2.9 | Release version in the binary | Hosted binary reports `version: "dev"` unless the tag is injected via `LDFLAGS` at build | Does the hosted build pipeline pass the git tag into `LDFLAGS`? If not, who owns that pipeline change? (Reviewers see `serverInfo.version` on `initialize`.) |
| 2.2.10 | `creator` field naming | rc22 ticket 24 (chain address vs EVM address) | If the listing's examples show registry output, does 24 need to land first? |

### 2.3 Process

| # | Question |
|---|---|
| 2.3.1 | What is the submission channel today — a web form, a GitHub PR to a registry repo, or an account-manager conversation? (This changes month to month; confirm at submission time.) |
| 2.3.2 | What review turnaround and what re-review policy apply when we ship a new rc? Does every tool-surface change require re-submission? |
| 2.3.3 | Are there listing requirements on tool annotations, descriptions, or rate limits beyond what the MCP spec mandates? (Every tool here already carries `ToolAnnotations`; confirm nothing else is expected.) |
| 2.3.4 | Does the directory require the server to be reachable without any credential for the reviewers' automated checks, and if so is `MCP_KEYLESS_READS=true` on the listed deployment enough? |
| 2.3.5 | Who monitors the listing after launch (reviewer follow-ups, user reports, takedown requests)? |

Answers / decisions:

- 2.1.1 — ___
- 2.1.2 — ___
- (continue per item)

## Housekeeping

When section 1 is answered, copy the dated answers into
`docs/SECURITY_AUDIT.md` (update 2026-09-10, "Question for the hosted
operator") and mark F2 closed or qualified there. When section 2 is
decided, update rc22 ticket P6 with go/no-go; if "go", the artefact table
above becomes the checklist.
