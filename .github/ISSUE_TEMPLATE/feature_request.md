---
name: Feature request
about: Propose a new tool, capability, or behavior change
title: "[feature] "
labels: ["enhancement", "needs-triage"]
assignees: []
---

<!--
This server is deliberately narrow: it is a non-custodial proxy to the
Inveniam Chain. It is NOT a wallet, a chain node, a custodian, or an
orchestrator. Proposals that add key custody, signing, or per-user
server-side state are very likely out of scope — see CONTRIBUTING.md
and docs/DESIGN.md before filing.
-->

## Problem statement

What can't you do today, and why does it matter? Describe the need, not a
pre-chosen solution.

## Proposed surface

How might the server expose this? If it's a new MCP tool, sketch its
inputs and outputs. Keep in mind the server returns server-authored
guidance, never user data it wasn't given.

## Is this already on the roadmap?

- [ ] I checked `docs/IMPLEMENTATION_PLAN.md` and `docs/ROADMAP.md` (if present) and this isn't already planned
- [ ] I checked open issues and Discussions for an existing request

## Alternatives considered

What workarounds exist today? Why are they insufficient?

## Scope check

- [ ] This does **not** require the server to hold private keys or sign on a user's behalf
- [ ] This does **not** add per-user server-side state beyond the existing credential store
- [ ] This fits the "thin proxy" model described in `docs/DESIGN.md`
