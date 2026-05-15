# Key custody threat model — why the wizard does not mediate private keys

This document explains a deliberate non-goal of the onboarding wizard
([`internal/mcp/tools_setup_wizard.go`](../internal/mcp/tools_setup_wizard.go)):

> **The wizard will never cause private-key bytes to transit the MCP
> server, the LLM agent, or the chat transcript. Key generation happens
> in a process the user controls, and only the resulting public address
> crosses any logged channel.**

It exists because this design choice has been re-proposed more than once
under variants of "but the agent could do it" or "but the bytes are only
in the chat briefly." Both proposals share a single misconception about
where agent-side surfaces actually log content. This document writes
that misconception down so it doesn't have to be re-litigated.

This is a companion to
[SECURITY_CONSUMER_GUIDANCE.md](SECURITY_CONSUMER_GUIDANCE.md), which
covers threats that fall on consuming agents in general. Key custody is
a sufficiently load-bearing decision to deserve its own document.

---

## 1. What the wizard does today

`nvnm_setup_wizard` in the `needs_wallet` state
([tools_setup_wizard.go:156-174](../internal/mcp/tools_setup_wizard.go#L156-L174))
returns three pieces of content:

1. Prose: "Generate a wallet, then call this wizard again with the
   address."
2. Three language-specific snippets (Python, JavaScript, Go) at
   [tools_setup_wizard.go:235-300](../internal/mcp/tools_setup_wizard.go#L235-L300).
   Each snippet generates a key and hands it directly to a secrets
   backend (keyring / `.env` with mode `0600` / file with mode `0600`).
   **No snippet prints the private key.**
3. A `next_actions` hint telling the agent to re-call the wizard once
   it has an address.

The user runs the snippet in *their own* Python / Node / Go runtime.
The key bytes exist only in that local process. The agent receives
nothing about the key; the MCP server receives only the address on the
next wizard call. The wizard's four-state machine derives onboarding
state entirely from on-chain reads (balance + nonce) at
[tools_setup_wizard.go:108-124](../internal/mcp/tools_setup_wizard.go#L108-L124),
so the server is structurally key-free by construction.

This is consistent with the project's stated identity in
[`CLAUDE.md`](../CLAUDE.md): *"Not a wallet. Holds zero private keys,
ever."*

---

## 2. The proposal under consideration (and why we reject it)

Periodically, someone proposes a variant of:

> The MCP server tells the agent to derive a private key (e.g., using
> ECDSA on secp256k1). The agent does so, displays the key to the user,
> and tells the user to save it in a secrets store and delete the chat.
> The MCP server never sees the key.

The proposal's framing is that **only the agent** sees the key, not the
server. The framing is technically accurate and operationally
misleading. "The agent" in any realistic MCP deployment is not a single
trusted local actor — it is a composition of remote LLM inference, a
client app, transport-layer surfaces, and other MCP servers connected
to the same session. Section 4 enumerates the specific failure modes.

---

## 3. Adversaries and channels

A useful threat model names who you are defending against and what
channels you are defending. This one is short.

### Adversaries

| Adversary | Capability assumed |
|---|---|
| **Casual snooper with retroactive access** | Recovers chat transcripts from cloud-synced clients, browser caches, screenshot folders, or terminal scrollback after the fact |
| **Compromised provider account** | Has read access to the user's account at the LLM provider or agent host (Anthropic, OpenAI, Cursor, etc.) — common after credential theft, SIM swap, or shared family account |
| **Prompt-injection attacker** | Has write access to *any* content the agent later reads — registry strings from another tenant ([SECURITY_CONSUMER_GUIDANCE § indirect prompt injection](SECURITY_CONSUMER_GUIDANCE.md)), other MCP servers' tool results, web fetches, files the user uploads |
| **Adversarial MCP server** | A peer MCP server connected to the same agent session, which can return content designed to exfiltrate the agent's context |
| **Lawful disclosure** | A subpoena or warrant served on the LLM provider's content-retention store |

### Channels with retention or replay

| Channel | Who controls retention | Typical retention |
|---|---|---|
| LLM provider inference logs | Provider (Anthropic / OpenAI / Google / Bedrock / Azure) | Days to weeks for abuse review; longer if flagged |
| Agent host transcript sync | Host vendor (Cursor, Cline, Claude Desktop, ChatGPT clients) | Vendor-defined; commonly cloud-synced before user-side delete |
| Agent context window | The LLM, for the duration of the session | Every subsequent turn — every later tool call sees prior context |
| Adversarial MCP server visibility | The peer server's operator | Whatever they choose to log |
| OS-level capture | The user's OS | Terminal scrollback, clipboard history, screenshot service, swap file |

The user controls *zero* of those channels' retention policies. They
control one thing: whether to put content into the channel in the
first place.

---

## 4. Flow comparison

### Flow A — agent-mediated derivation (proposed, rejected)

```
MCP server  --(prompt to derive)-->  Agent (LLM)
                                       |
                                       |--(key bytes)--> User's chat UI
                                       |--(key bytes)--> Provider inference log
                                       |--(key bytes)--> Host transcript sync
                                       |--(key bytes)--> Context window for rest of session
                                       |--(key bytes)--> Any later prompt-injection vector
                                       |
                                     User  --("delete the chat")--> Local UI only
                                                                    (provider+host copies survive)
```

**Trust set for the key to remain confidential:** LLM vendor + agent
host + every peer MCP server in the session + every later tool result
(including chain-anchored strings, web fetches, file uploads) + the
user's OS + the user's promptness to delete + the user's understanding
that delete is local-only.

### Flow B — user-local derivation (shipped)

```
MCP server  --(snippet text)-->  Agent  --(snippet text, displayed verbatim)-->  User
                                                                                  |
                                                                                  v
                                                                          User's local
                                                                          Python/Node/Go
                                                                                  |
                                                                                  v
                                                                          OS keychain /
                                                                          .env (mode 0600)
                                                                          on user's disk
                                                                                  |
User  --(public address only)--> Agent --(public address)--> MCP server
```

**Trust set for the key to remain confidential:** the crypto library
(`eth_account` / `ethers` / `defiweb/go-eth`) + the language runtime +
the OS keychain.

The trust sets are not comparable in size. Flow A's set is open-ended
and grows with every MCP integration the user adds. Flow B's set is
bounded and the user already implicitly audited it when they installed
the language runtime.

---

## 5. Specific failure modes of Flow A

### 5.1 Provider inference logs retain the key

Every turn the agent takes is processed by the LLM provider's
infrastructure. Provider content retention runs days to weeks
regardless of any user-side deletion gesture. The user cannot redact
upstream. This is published policy for every commercial LLM API, not a
speculative attack.

### 5.2 Client transcript sync ships the key to vendor cloud before delete

Claude Desktop, ChatGPT clients, Cursor's Composer, and similar agent
hosts sync transcripts to vendor cloud storage. "Delete the chat"
typically deletes the local copy with a soft-delete window upstream.
The window is long enough that bytes are in two places by the time the
user thinks about deletion.

### 5.3 Context-window contagion

The most subtle failure mode. The agent's "memory" of the key is not a
variable it can forget — it is part of the context window fed into
every subsequent turn. Consequences:

- Any later prompt-injection attack via a registry string
  ([SECURITY_CONSUMER_GUIDANCE.md § indirect prompt injection](SECURITY_CONSUMER_GUIDANCE.md))
  can exfiltrate the key by instructing the agent to repeat its
  context.
- Any peer MCP server's tool result that contains adversarial content
  is similarly capable.
- Any "summarize what we've discussed" instruction the user later
  issues will surface the key.
- KV-cache files on disk during the session may hold the bytes outside
  the LLM's working memory.

The LLM is structurally a single bag of tokens with no
compartmentalization. There is no API to mark bytes as
"high-sensitivity, do not log, do not include in summaries." Anything
the agent has seen is one well-crafted instruction away from being
emitted in its next response.

### 5.4 Adversarial MCP servers in the same session

MCP is designed to be polyglot — users connect multiple servers to one
agent. The ecosystem assumes adversarial servers exist (which is why
the spec carries `ToolAnnotations`; see
[`internal/mcp/annotations.go`](../internal/mcp/annotations.go) for our
usage). If the key is in the agent's context, any peer server's tool
response can include content designed to leak it. The wizard cannot
gate which other servers the user connects.

### 5.5 The "delete the chat" instruction is unenforceable

The wizard cannot verify the user complied. The provider's copy
survives regardless. The instruction is hygiene theater for a control
the user does not have authority over.

### 5.6 No key-hygiene primitives exist at the agent layer

The agent cannot:

- Allocate memory excluded from the context window,
- Refuse to include the key in a future summary,
- Detect a later tool result instructing it to reveal the key,
- Mark bytes as "do not log" in any layer below it.

These primitives exist at the OS process layer (mlock, secure-erase),
which is where Flow B operates, and do not exist at the LLM layer.

### 5.7 Training the wrong mental model

Even if a specific deployment happened to avoid all of the above, the
wizard cannot tell whether it is running in that deployment.
Shipping Flow A trains every user to expect "the agent gives me my
key" — a mental model they will carry to deployments where the threat
surface is dramatically larger. The wizard is a teaching surface; it
should teach a custody discipline the user can carry forward, not one
that happens to work in a specific narrow setup.

---

## 6. The narrow case where Flow A almost works

In fairness, there is a deployment topology where Flow A's threat
model is closer to defensible:

- A fully local LLM (llama.cpp, Ollama, etc.) with disabled telemetry,
- Running on the user's own machine,
- Talking to a local MCP server,
- With **no** peer MCP servers connected,
- In a fresh chat with no prior turns,
- With ephemeral transcript storage configured,
- With the user immediately deleting the conversation file before any
  backup runs.

Even there, three problems remain:

1. The KV cache hits disk unless explicitly configured otherwise — and
   it usually is not.
2. The MCP server cannot distinguish that deployment from the typical
   Anthropic-API-backed deployment. The same code path serves both.
3. It still teaches the wrong mental model (see § 5.7).

The narrow case is not worth designing for, because the same product
code cannot reliably distinguish it from the unsafe case, and because
the same user will carry the learned flow into a riskier deployment
later.

---

## 7. Why Flow B's trust set is small

Flow B's trust set looks like:

- **The crypto library** — `eth_account.Account.create()` (Python),
  `ethers.Wallet.createRandom()` (JavaScript),
  `defiwallet.NewRandomKey()` (Go). Each library uses the OS CSPRNG
  (`/dev/urandom` or equivalent). All three are widely deployed,
  open-source, and externally audited.
- **The language runtime** — CPython / Node / the Go runtime. The user
  already implicitly trusted these when they installed them.
- **The OS keychain (or `.env` file)** — macOS Keychain, Linux
  Secret Service, Windows Credential Manager, or a file at mode
  `0600`. The user already trusts these for every other credential on
  their machine.

The bytes are generated in a process the user owns, written through
APIs the user already trusted, and stored in a vault the user already
uses for other secrets. The MCP server's only contribution is the
snippet text — a published instruction, not a logged credential.

This is the same security pattern that OAuth's authorization-code flow
adopted after the password-grant disasters of 2008–2014: relying
parties do not see credentials. PKCE is the gold standard now. The
wizard's snippet flow is the auth-code-with-PKCE of key generation.

---

## 8. Alternative onboarding surfaces (future)

Flow B is one valid implementation; it is not the only one. Other
surfaces that satisfy the same constraint ("key bytes never traverse
the MCP-agent-transcript chain") are viable and may be added as
parallel options without weakening the threat model:

| Surface | Status | Trust delta |
|---|---|---|
| Snippet executed in user's local runtime | Shipped (Flow B) | OS keychain + crypto library + language runtime |
| Static browser page, key generated in-browser via a vendored secp256k1 library | **Selected 2026-05-15.** Targeting Phase 10 design / Phase 11 launch. Pre-design notes: [WALLET_GENERATOR_PAGE_NOTES.md](WALLET_GENERATOR_PAGE_NOTES.md). | Browser + page integrity (SRI-pinned, open-source, strict CSP) + the hosting origin's DNS/TLS posture — structurally the same trust set MetaMask's "Create Wallet" page operates under |
| Existing external wallet (MetaMask, Rabby, Phantom, Ledger, Trezor) | Already supported (wizard accepts any address) | The wallet vendor's threat model |
| Signed standalone binary `nvnm-keygen` published by Inveniam | Not pursued (superseded by webpage path 2026-05-15) | Same as snippet, packaged |

All four surfaces share the structural property that the MCP server
returns a *pointer* to a generation surface the user controls, and
the agent acts as a router rather than a participant. None of them
require the LLM or the server to handle key bytes.

A user segment that genuinely cannot use any of these four surfaces is
a user segment that should not be self-custodying private keys. The
right product for that segment is a custodial offering with explicit
disclosure ("Inveniam holds your keys, here are the implications"),
which is a different product, not a different wizard state.

---

## 9. Implications for future feature requests

Proposals that imply key bytes traversing the agent, the MCP server,
or the chat transcript should be rejected at intake unless they
explicitly address every channel in § 3 with a concrete control. In
practice, no such proposal has produced a viable set of controls,
because the channels are not controllable from the wizard side.

Re-proposals tend to take one of these forms. Each has a short answer:

| Re-proposal | Short answer |
|---|---|
| "But we tell the user to delete the chat." | § 5.5 — unenforceable, and provider/host copies survive. |
| "But it's only testnet." | § 5.7 — testnet keys get reused on mainnet, and the wizard is a teaching surface. |
| "But only the agent sees it, not our server." | § 4 — "the agent" implicates 5+ parties' logs. |
| "But the user can use a local LLM." | § 6 — the wizard cannot distinguish that deployment from the unsafe one. |
| "But our users can't run a Python snippet." | § 8 — the right answer is a browser page or external wallet, not weaker custody. |
| "But the key is shown only inside a code fence." | Markdown formatting does not change what hits the wire. Every byte of every assistant turn is logged. |

---

## 10. Decision history

- **Phase 8.8 (2026-05-13).** `nvnm_setup_wizard` shipped with the
  snippet-based `needs_wallet` response. The decision to keep private
  keys out of the agent surface was made at design time and recorded
  in [`PHASE_8_DESIGN.md § 3.7`](PHASE_8_DESIGN.md). No explicit
  threat-model document existed at that point; the design comment in
  [tools_setup_wizard.go:14-39](../internal/mcp/tools_setup_wizard.go#L14-L39)
  records the rationale inline.
- **2026-05-15.** This document written after a product-side proposal
  to have the agent derive and display keys in-chat. The proposal was
  declined. The document exists so the next re-proposal has a written
  answer to argue against rather than re-litigating from scratch.
- **2026-05-15 (later same day).** Selected the browser-page surface
  from § 8 as the second supported onboarding path (additive to the
  snippet flow — the snippet flow is *not* being deprecated). The
  page targets users who cannot or will not paste a snippet into a
  local runtime. Pre-design notes captured in
  [WALLET_GENERATOR_PAGE_NOTES.md](WALLET_GENERATOR_PAGE_NOTES.md);
  full design will live in the Phase 10 design doc when it is
  written. The threat-model statement updates to: *key bytes are
  generated in a process the user controls — either a local language
  runtime (snippet flow) or the user's browser at an Inveniam-
  controlled origin (page flow). Neither path lets bytes traverse the
  MCP server, the LLM agent, or the chat transcript.* The phishing
  surface introduced by the page flow is the one residual threat
  worth tracking explicitly; see [WALLET_GENERATOR_PAGE_NOTES.md § 5
  "Phishing — the residual threat"](WALLET_GENERATOR_PAGE_NOTES.md)
  for the mitigation plan.

---

## 11. Cross-references

- [`internal/mcp/tools_setup_wizard.go`](../internal/mcp/tools_setup_wizard.go)
  — the wizard implementation and snippet content.
- [`docs/SECURITY_CONSUMER_GUIDANCE.md`](SECURITY_CONSUMER_GUIDANCE.md)
  — companion document for threats that fall on consuming agents,
  including indirect prompt injection (which is one of the exfiltration
  vectors discussed in § 5.3).
- [`docs/PHASE_8_DESIGN.md § 3.7`](PHASE_8_DESIGN.md) — the
  onboarding-tools design that established the snippet flow.
- [`CLAUDE.md`](../CLAUDE.md) — project identity statement: *"Not a
  wallet. Holds zero private keys, ever."*
- [`docs/WALLET_GENERATOR_PAGE_NOTES.md`](WALLET_GENERATOR_PAGE_NOTES.md)
  — pre-design notes for the browser-page surface selected
  2026-05-15. Captures design points, the MANTRA-piggyback
  investigation, and the phishing-mitigation plan that this document
  references but does not duplicate.
