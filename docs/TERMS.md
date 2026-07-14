# Terms of Service — NVNM Chain MCP Server (hosted Service)

These Terms of Service ("Terms") govern access to and use of the hosted
NVNM Chain Model Context Protocol (MCP) Server operated by Inveniam
Capital Partners ("Inveniam," "we," "us," "our"). The hosted Service is
distinct from the open-source software it runs — the software is
governed by the Apache License 2.0 in [LICENSE](../LICENSE); these Terms
govern your use of the *Service we host*.

**Audience:** any party that accesses the hosted Service. The hosted
Service is **anonymous**: it issues no accounts and no credentials, and
requires none. Operators who deploy the open-source binary themselves are
governed by Apache 2.0, not these Terms — they are the operator of their
own deployment, and the credentialed features described in the Software's
documentation belong to that deployment, not to this Service.

**Currency:** reflects the code as of commit `ac4bacb` (2026-07-07), the
anonymous-write bundle that removed caller authentication from the hosted
Service. The published
[Privacy Policy](NVNM_MCP_Privacy_Policy_Jul_2026.pdf) is the parallel
counsel-finalized artifact and is being revised against the same code.

**Effective Date:** [TBD at v1 launch — set at the moment of public
repository flip and Service availability].

**Working-draft notice.** This document is the engineering-side draft
that will be revised by counsel before public launch. Acceptable-use
enumeration (§ 6), liability language (§§ 11–13), and governing-law
clause (§ 16) are the sections most likely to change in counsel review.

> **Open question for counsel — acceptance (§ 3).** These Terms were
> drafted when Service access required a credential, which supplied a
> clean acceptance event: you requested a key, you accepted the Terms.
> **That event no longer exists.** The hosted Service is anonymous — a
> caller presents no credential, creates no account, provides no email,
> and performs no affirmative act by which acceptance could be recorded.
> The only remaining theory in § 3 is constructive acceptance by use
> against posted Terms. Engineering has no view on whether that is
> sufficient; we flag it because it is now the *only* mechanism, and
> because the same fact (no caller accepts terms, no contract is formed)
> is the factual predicate on which the revised Privacy Policy relies for
> its GDPR Art 6(1)(f) legitimate-interests basis. The two documents must
> not contradict each other on this point.

---

## 1. Scope

These Terms apply to:

- The hosted NVNM Chain MCP Server endpoints operated by Inveniam,
  including but not limited to the production mainnet instance and the
  public testnet instance;
- All traffic to those endpoints, which is **anonymous** — both reads and
  writes. Inveniam issues no credential for the hosted Service and
  requires none. See [docs/DATA_HANDLING.md](DATA_HANDLING.md).

These Terms do **not** apply to:

- Use of the open-source software at
  <https://github.com/NVNM-Chain/nvnm-mcp-server> — that use is
  governed by the Apache License 2.0 in [LICENSE](../LICENSE) (see
  § 7);
- The NVNM Chain blockchain itself or the underlying MANTRA Chain L1 —
  these are public, decentralized networks governed by their own
  operators and on-chain protocol rules;
- The wallet-generator page at the wallet project's hosting URL — that
  surface is governed by its own Terms and Privacy Policy;
- Any third-party tool, agent, or framework you use to call the
  Service.

## 2. Definitions

| Term | Meaning |
|---|---|
| **Service** | The hosted NVNM Chain MCP Server endpoints operated by Inveniam. All traffic to them is anonymous. |
| **Software** | The open-source source code at <https://github.com/NVNM-Chain/nvnm-mcp-server>, licensed under Apache 2.0. |
| **Operator** | Inveniam, with respect to the hosted Service. A self-hosting third party is the Operator of their own deployment, which these Terms do not govern. |
| **You / User** | The individual or legal entity accessing the Service. Because the Service is anonymous, Inveniam does not know who You are and holds no identifying information about You. |
| **Signer** | The blockchain wallet address recovered from the signature on a transaction You submit. It is the only identifier the Service observes, and the identifier on which the abuse controls in § 6 operate. Inveniam has no means of resolving a Signer to a person. |
| **Credential** | *Not applicable to the hosted Service.* The Software supports API keys and federated identity for self-hosted deployments; the hosted Service issues none and accepts none. The term is retained only so that references from the Software's documentation resolve. |
| **Tools** | The MCP tools exposed by the Service, including but not limited to the EVM read tools, anchor tools, onboarding tools, and `evm_send_raw_transaction`. |
| **Chain** | The NVNM Chain (mainnet `nvnm-1` / testnet `nvnm-testnet-1`) and the underlying public blockchain infrastructure. |

## 3. Acceptance

> **Counsel review required — see the open question in the header.** The
> mechanisms below are the *only* ones available, because the hosted
> Service has no credential request, no account creation, no sign-in, and
> no affirmative acceptance gesture of any kind. Engineering states the
> facts; the sufficiency of constructive acceptance is a legal judgment.

You accept these Terms by sending any request to the Service while these
Terms are posted at a publicly reachable URL.

If you do not agree to these Terms, do not use the Service. The Service
may be used as Software under Apache 2.0 instead — see § 7.

The version of these Terms in force is the version posted at the time of
your request. **The Service records no acceptance event.** It holds no
account, no credential record, and no `tos_version` / `tos_accepted_at`
field — there is no artifact in which such a record could be kept, and no
identifier for a caller to which it could be attached.

## 4. Eligibility

You represent and warrant that:

- You are at least the age of majority in your jurisdiction (typically
  18) and have the legal capacity to enter into a binding agreement;
- If you are accessing the Service on behalf of an organization, you
  have authority to bind that organization to these Terms;
- You are not located in, ordinarily resident in, or organized under
  the laws of any jurisdiction subject to a comprehensive embargo by
  the United States;
- You are not identified on any U.S. Treasury Department Office of
  Foreign Assets Control (OFAC) list of restricted parties or any
  comparable list under applicable law;
- Your access to and use of the Service complies with all laws
  applicable to you, including export-control, sanctions, anti-money
  laundering, and tax laws.

## 5. No credentials; your keys are yours

### 5.1 The Service issues no credentials

**Inveniam issues no API key, no account, and no federated identity for
the hosted Service, and requires none to use it.** There is no sign-up,
no sign-in, and no credential to safeguard. Access is anonymous.

The Software supports credentialed operation, and a self-hosting operator
may enable it. That is a property of *their* deployment and is outside
these Terms.

### 5.2 Your blockchain private keys

Inveniam holds **zero blockchain private keys** and performs **zero
signing** — see [docs/KEY_CUSTODY_THREAT_MODEL.md](KEY_CUSTODY_THREAT_MODEL.md).
Every write you make must arrive at the Service already signed by a key
You control; the Service cannot originate a transaction on your behalf.

You are solely responsible for the custody and security of any blockchain
private keys you use with the Service, and for every transaction signed
with them. The Service broadcasts the signed transaction it receives; it
does not prompt for, obtain, or verify human approval of any write. If
approval by a human is required in your context, it must be enforced by
your own wallet or agent software, not by the Service.

### 5.3 Multiple wallets

The abuse controls in § 6 (per-wallet quotas and the ban list) key on the
Signer address recovered from your transaction. Generating additional
wallet addresses in order to circumvent a rate limit or quota, or to evade
an active ban, is a breach of these Terms — notwithstanding that the
Service is technically unable to prevent it.

## 6. Acceptable use

You must not use the Service to:

### 6.1 Network and protocol abuse

- Submit traffic intended to degrade Service availability for other
  users (denial-of-service attacks, request floods, sustained spike
  traffic clearly designed to exhaust rate-limit budget rather than
  perform productive work);
- Bypass, defeat, or circumvent any rate limit, per-wallet quota, ban
  list, relay-scope restriction, Origin guard, or other access control;
- Probe the Service for vulnerabilities except under an authorized
  security-research arrangement (see § 6.5);
- Reverse engineer, decompile, or attempt to extract operational
  details of the hosted deployment beyond what is publicly disclosed
  in the open-source Software repository.

### 6.2 Transaction abuse (`evm_send_raw_transaction`)

**The hosted Service is not a general-purpose transaction relay.** It
will broadcast a signed transaction **only** if that transaction is
addressed to the NVNM anchoring contract. It **refuses** value transfers,
contract deployments, and calls to any other contract, and it broadcasts
its own canonical re-encoding of the transaction it decoded rather than
the raw bytes it was given. A transaction outside that scope is rejected
and never reaches the Chain.

Within that scope, You retain full responsibility for any transaction you
sign and submit, **and** you must not use this tool to:

- Anchor or reference data prohibited by applicable law (see § 6.3);
- Submit transactions from on-chain addresses identified on applicable
  sanctions lists;
- Submit transactions whose primary purpose is to defeat the
  irreversibility properties of the Chain (e.g., orchestrated
  chain-reorg solicitation, knowing collusion with validators to
  reorder finalized state).

Nothing in this Section enlarges Inveniam's role beyond that of a
scope-limited forwarder. Inveniam does not pre-screen the *content* of an
anchoring transaction, validate its economic intent, or assume custody
over it. Once submitted to the EVM RPC endpoint, transactions enter the
Chain's public mempool and are subject to the Chain's protocol rules —
Inveniam cannot recall, reverse, or modify them.

**Abuse controls.** The Service enforces a per-wallet write quota (500
writes per 24 hours) and a wallet ban list, both keyed on the Signer
address. Inveniam may add any Signer to the ban list at its discretion,
including for suspected abuse or breach of these Terms.

### 6.3 Anchor tools

The `anchor_*` tools surface anchored data hashes and the smart
contract that records them. You must not use these tools to:

- Solicit or attempt to anchor data prohibited by applicable law,
  including content that would constitute child sexual abuse material,
  unlawful threats, or material that infringes intellectual-property
  rights you do not own or have permission to anchor;
- Misrepresent the meaning of an anchored hash to a third party
  (anchoring a hash does not certify the underlying data is true,
  accurate, lawful, or owned by the anchor party).

### 6.4 Generally prohibited use

You must not use the Service to:

- Violate any law applicable to you or any third party;
- Infringe the intellectual-property, privacy, or other rights of any
  third party;
- Send unsolicited bulk communications using any contact information
  obtained from the Service or from interactions with Inveniam
  personnel;
- Train, fine-tune, or evaluate a machine-learning model in a manner
  that constitutes a derivative work of any Inveniam-proprietary
  artifact you obtain through the Service (this restriction does not
  apply to the Software, which is governed by Apache 2.0);
- Resell, sublicense, or expose the Service to your own end users
  under a wrapper service without Inveniam's prior written agreement
  for that arrangement.

### 6.5 Security research

Good-faith security research targeting the Service is permitted
*only* under the disclosure terms documented at
[SECURITY.md](../SECURITY.md). In particular, you must not exfiltrate
data belonging to other users, degrade Service availability, or
publicly disclose a vulnerability before the timelines in SECURITY.md
elapse. Researchers acting in compliance with SECURITY.md are not
considered to be in breach of § 6.1.

## 7. Open-source software (Apache 2.0)

The Software (source code, build artifacts, container images) at
<https://github.com/NVNM-Chain/nvnm-mcp-server> is licensed under the
Apache License 2.0 in [LICENSE](../LICENSE). These Terms do not modify,
limit, or extend that license in any way. In particular:

- You may run the Software yourself (self-host) under Apache 2.0 with
  no obligation under these Terms. A self-hosted deployment is your
  own Service; you are its Operator.
- You may study, modify, redistribute, and sublicense the Software
  under Apache 2.0's terms.
- Apache 2.0 includes a patent grant and disclaimers of warranty
  applicable to the Software. Those provisions apply to the Software
  on their own terms; this § 7 does not restate them.

The hosted Service is a separate offering: it is the running
instance(s) of the Software that Inveniam operates, plus the data
those instances hold and process, plus any operational artifacts
(rate-limit state, per-wallet quota counters, the ban list, the
broadcast audit, telemetry) that exist only in Inveniam's deployment.
These Terms govern that hosted offering.

If you contribute to the open-source repository, that contribution is
also governed by the Developer Certificate of Origin sign-off required
by repository branch protection ([CONTRIBUTING.md](../CONTRIBUTING.md)),
not by these Terms.

## 8. Privacy

Your use of the Service is also governed by the NVNM Chain MCP Server
Privacy Policy (link to be added at v1 publication; canonical
engineering reference at [docs/DATA_HANDLING.md](DATA_HANDLING.md)).
The Privacy Policy describes what data the Service processes and how
long it retains it; § 6 of these Terms describes what use of that
processing capacity is permitted.

Where these Terms and the Privacy Policy address the same subject, the
Privacy Policy governs Inveniam's data-handling obligations and these
Terms govern your use obligations; they are intended to be
complementary, not conflicting.

## 9. Fees and future charging

### 9.1 Free at v1

The Service is provided to You free of charge for v1. Inveniam does not
charge for request volume within published rate limits, or for any other
access category, at v1. (There is nothing to charge *for* at the account
level: the Service issues no credentials and holds no accounts.)

### 9.2 Reservation of right to charge

Inveniam expressly reserves the right to introduce paid tiers, usage
fees, or other charges in the future. Reasonable advance notice
(minimum 30 days) will be provided through the same channels these
Terms are published on (the canonical repository and any successor
hosted-policy URL). Continued use of the Service after a fee
introduction's effective date constitutes acceptance of the fee terms.

### 9.3 No grandfathering

**Use of the Service while it is free does not entitle You to
permanent free use of the Service.** Use during the free period carries
no grandfathered, perpetual, or special-rate status. If Inveniam
introduces fees, those fees apply to all then-active use of the Service,
regardless of when that use began.

This clause is intentional: provisioning permanent free access by
silence creates compounding obligations that cannot be undone without
breaking customer trust. By making this term explicit, both sides know
the rules in advance.

## 10. Service availability

The Service is provided on a **reasonable-efforts basis**. Inveniam
does not commit to any availability level, response-time SLA, or
uptime guarantee at v1. Inveniam may, at its discretion and without
prior notice:

- Modify the Service, including adding, changing, or removing Tools,
  endpoints, rate limits, or response shapes;
- Suspend the Service for maintenance, upgrades, security incidents,
  or operational reasons;
- Discontinue the Service in whole or in part.

Inveniam will use reasonable efforts to communicate planned
discontinuation through the canonical repository and the Service's
public documentation. **Because the Service is anonymous, Inveniam
cannot notify users individually** — it holds no email address, no
account, and no contact information for any caller. Public posting is
the only notice channel available. The Service runs against external
infrastructure (the EVM RPC endpoint, the underlying Chain, telemetry
sinks) whose availability is itself not under Inveniam's sole control;
Service availability is
necessarily bounded by the availability of those dependencies.

The use of "reasonable efforts" here is deliberate. The term "best
efforts" carries a heightened legal standard in some jurisdictions and
is intentionally avoided.

## 11. Disclaimers

THE SERVICE IS PROVIDED "AS IS" AND "AS AVAILABLE." TO THE MAXIMUM
EXTENT PERMITTED BY APPLICABLE LAW, INVENIAM DISCLAIMS ALL WARRANTIES,
EXPRESS OR IMPLIED, INCLUDING WITHOUT LIMITATION WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE, NON-INFRINGEMENT,
AND ACCURACY OF INFORMATION OBTAINED THROUGH THE SERVICE.

Without limiting the generality of the foregoing:

- **Chain irreversibility.** Transactions submitted via the Service to
  the Chain are recorded on a public, decentralized ledger and are
  cryptographically irreversible. Inveniam cannot reverse, refund, or
  modify a transaction once it has been included in a finalized block,
  even if the transaction was submitted in error.
- **No financial, investment, legal, or tax advice.** The Service
  surfaces on-chain data and forwards signed transactions. It does not
  constitute financial, investment, legal, accounting, or tax advice.
  You are solely responsible for the financial and legal consequences
  of your use of the Service.
- **No custody.** Inveniam holds zero blockchain private keys and
  performs zero signing of blockchain transactions. The Service is
  not a custodial wallet, exchange, broker, or escrow.
- **No representation as to RPC endpoint behavior.** The EVM RPC
  endpoint that the Service forwards traffic to is operated separately
  (by Inveniam or by a third party depending on the instance);
  Inveniam makes no warranty as to the completeness, freshness, or
  accuracy of data returned by that endpoint.
- **No representation as to Chain state.** Block heights, balances,
  receipts, and contract-call results returned by the Service reflect
  the state of the Chain as observed by the RPC endpoint at the time
  of the request. Chain re-organizations, while rare on finalized
  blocks, can in principle invalidate a result; Inveniam does not
  warrant against this.

## 12. Limitation of liability

TO THE MAXIMUM EXTENT PERMITTED BY APPLICABLE LAW, IN NO EVENT WILL
INVENIAM, ITS AFFILIATES, OR ITS OR THEIR RESPECTIVE OFFICERS,
DIRECTORS, EMPLOYEES, OR AGENTS BE LIABLE FOR ANY INDIRECT,
INCIDENTAL, SPECIAL, CONSEQUENTIAL, EXEMPLARY, OR PUNITIVE DAMAGES,
INCLUDING DAMAGES FOR LOST PROFITS, LOST DATA, BUSINESS INTERRUPTION,
LOSS OF GOODWILL, OR FAILURE OF SECURITY MECHANISMS, ARISING OUT OF
OR RELATING TO YOUR USE OF OR INABILITY TO USE THE SERVICE, EVEN IF
INVENIAM HAS BEEN ADVISED OF THE POSSIBILITY OF SUCH DAMAGES.

INVENIAM'S TOTAL AGGREGATE LIABILITY ARISING OUT OF OR RELATING TO
THESE TERMS OR YOUR USE OF THE SERVICE WILL NOT EXCEED THE GREATER OF
(A) ONE HUNDRED U.S. DOLLARS ($100), OR (B) THE TOTAL FEES PAID BY YOU
TO INVENIAM FOR THE SERVICE IN THE TWELVE MONTHS PRECEDING THE EVENT
GIVING RISE TO THE LIABILITY.

For so long as the Service is provided free of charge under § 9.1, no
fees will have been paid, and limb (A) will be the applicable cap.

Some jurisdictions do not allow the exclusion or limitation of certain
damages. In those jurisdictions, the foregoing limitations apply only
to the extent permitted by applicable law.

## 13. Indemnification

You will defend, indemnify, and hold harmless Inveniam, its
affiliates, and its and their respective officers, directors,
employees, and agents from and against any claim, demand, loss,
liability, damage, fine, penalty, or expense (including reasonable
attorneys' fees) arising out of or relating to:

- Your breach of these Terms, including breach of any representation
  or warranty in § 4 or any acceptable-use obligation in § 6;
- Your use of the Service in violation of applicable law;
- Any content, transaction, or instruction You submit to the Service,
  including signed transactions you submit via
  `evm_send_raw_transaction`;
- Any claim by a third party that arises from Your use of the
  Service.

Inveniam will give You prompt notice of any claim subject to this
Section and reasonable cooperation in the defense. You will not settle
any claim that imposes any obligation on Inveniam, requires Inveniam
to admit fault, or restricts Inveniam's future conduct without
Inveniam's prior written consent.

## 14. Suspension and termination

### 14.1 By You

You may stop using the Service at any time. Nothing needs to be
cancelled: You hold no account and no credential, and the Service retains
no relationship with You to terminate.

### 14.2 By Inveniam

Inveniam may block or ban any Signer address, or terminate Your access to
the Service, at any time, with or without notice, for any reason or no
reason. Reasons include:

- Suspected breach of these Terms (including any acceptable-use
  obligation in § 6);
- Operational necessity (capacity exhaustion, incident response,
  forced maintenance);
- Legal obligation (a binding order, sanctions designation,
  regulatory direction).

**Notice is generally not possible.** Because the Service is anonymous,
Inveniam holds no contact information for any caller and cannot give
individual notice of a ban. A banned Signer's writes are rejected at the
point of use.

### 14.3 Survival

Sections 6 (Acceptable use, for past acts), 8 (Privacy), 11
(Disclaimers), 12 (Limitation of liability), 13 (Indemnification),
this § 14.3, and § 16 (Governing law and dispute resolution) survive
termination.

### 14.4 Effect of termination

On termination, Your right to access the Service ends. The Software
remains available to You under Apache 2.0 — see § 7. Data Inveniam
holds about You that is operationally required for security, audit,
legal, or regulatory purposes may be retained after termination per
the Privacy Policy.

## 15. Modifications to these Terms

Inveniam may revise these Terms from time to time. Revisions are
effective when published at the URL these Terms are then served from
and identified by an updated `Effective Date` and version identifier.
Material changes will be communicated through reasonable means, which may
include in-MCP `instructions` payload notices and repository release
notes. **Individual notice is not possible**: the Service is anonymous
and Inveniam holds no contact information for any caller.

Continued use of the Service after a revision's Effective Date
constitutes acceptance of the revised Terms. If You do not agree to the
revision, You must stop using the Service.

## 16. Governing law and dispute resolution

[**Provisional:** these Terms are governed by the laws of [the United
States and the State of [State TBD by counsel — typical defaults are
Delaware (for entity convenience) or California (for technology-sector
default)]], excluding its conflict-of-laws principles. Counsel review
will finalize the jurisdiction.]

[**Provisional:** any dispute arising out of or relating to these
Terms or the Service will be brought exclusively in the state and
federal courts located in [forum to be specified by counsel], and the
parties consent to the personal jurisdiction of those courts. Counsel
review will determine whether mandatory arbitration is preferable for
the v1 customer profile.]

Nothing in this Section prevents either party from seeking injunctive
or equitable relief in any court of competent jurisdiction to enforce
intellectual-property rights or confidentiality obligations.

## 17. Miscellaneous

- **Entire agreement.** These Terms, together with the Privacy
  Policy and (if You contribute to the open-source repository) the
  Developer Certificate of Origin, constitute the entire agreement
  between You and Inveniam concerning the Service. No prior or
  contemporaneous statement, communication, or agreement supplements
  or modifies these Terms.
- **Severability.** If any provision of these Terms is held
  unenforceable, the remaining provisions remain in force, and the
  unenforceable provision will be replaced by an enforceable
  provision closest in intent.
- **No waiver.** Inveniam's failure to enforce any provision is not
  a waiver of that provision or any other provision.
- **Assignment.** You may not assign or transfer these Terms (in
  whole or in part) without Inveniam's prior written consent.
  Inveniam may assign these Terms to an affiliate or to a successor
  in connection with a merger, acquisition, or sale of substantially
  all of its assets relating to the Service.
- **No agency.** Nothing in these Terms creates an agency,
  partnership, joint venture, fiduciary, or employment relationship
  between You and Inveniam.
- **Force majeure.** Neither party is liable for failure to perform
  obligations under these Terms due to circumstances beyond its
  reasonable control, including acts of God, war, terrorism, civil
  unrest, government action, labor disputes, internet or
  telecommunications outages, or infrastructure failures upstream of
  the Service.
- **Notices to Inveniam.** Notices to Inveniam under these Terms must
  be sent to `privacy@nvnmchain.io` (general legal notices) or
  `security@nvnmchain.io` (security-related notices). Notices to You may
  be posted at the URL these Terms are served from, or included in an
  in-MCP `instructions` payload. Inveniam has no means of contacting You
  directly: the Service is anonymous and holds no address for You.
- **Headings.** Section headings are for convenience only and do not
  affect interpretation.

## 18. Contact

| Topic | Contact |
|---|---|
| General privacy + legal | `privacy@nvnmchain.io` |
| Security disclosure | `security@nvnmchain.io` — see [SECURITY.md](../SECURITY.md) for the full disclosure timeline |
| Support (post-launch) | `support@nvnmchain.io` and GitHub Discussions at <https://github.com/NVNM-Chain/nvnm-mcp-server/discussions> |
| Open-source software | Repository at <https://github.com/NVNM-Chain/nvnm-mcp-server>; license: Apache 2.0 in [LICENSE](../LICENSE) |

## 19. Cross-references

- [docs/DATA_HANDLING.md](DATA_HANDLING.md) — engineering reference
  for what data the Service processes; canonical source for the
  Privacy Policy's technical detail.
- [Privacy Policy](NVNM_MCP_Privacy_Policy_Jul_2026.pdf) — the
  counsel-finalized Privacy Policy that consumes DATA_HANDLING.md.
- [docs/KEY_CUSTODY_THREAT_MODEL.md](KEY_CUSTODY_THREAT_MODEL.md) —
  rationale for the zero-key-custody posture referenced in §§ 5.2
  and 11.
- [docs/SECURITY.md](../SECURITY.md) — security disclosure terms
  referenced in §§ 6.5 and 18.
- [LICENSE](../LICENSE) — Apache 2.0 license governing the Software.
- [CONTRIBUTING.md](../CONTRIBUTING.md) — Developer Certificate of
  Origin requirements for contributions to the open-source
  repository.
