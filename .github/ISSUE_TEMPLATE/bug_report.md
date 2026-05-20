---
name: Bug report
about: Something the server does wrong, crashes on, or returns incorrectly
title: "[bug] "
labels: ["bug", "needs-triage"]
assignees: []
---

<!--
Before filing: please confirm this is a defect in the MCP server itself,
not in the chain, the upstream EVM RPC endpoint, or your MCP client.
For "how do I..." questions, use the Question template instead.
-->

## What happened

A clear, concise description of the incorrect behavior.

## Steps to reproduce

1.
2.
3.

## Expected vs. actual

- **Expected:**
- **Actual:**

## Tool / surface involved

Which MCP tool(s) or endpoint? (e.g. `evm_get_balance`, `anchor_prepare_add_record`, the admin REST API)

## Environment

- **Server version / commit:** (e.g. `v0.3.1` or `git rev-parse --short HEAD`)
- **Deployment:** stdio / Streamable HTTP; container / binary / Helm chart
- **Chain:** testnet (`787111`) / mainnet (`1611`) / other
- **MCP client:** (e.g. Claude Desktop, ChatGPT, custom Go/Python client) and version
- **Auth provider:** `apikey` / `fusionauth` / none (read-only)
- **OS / arch:**

## Logs / output

<!--
Paste relevant structured-log lines or error output. The server redacts
secrets, addresses, and payloads by design — but please double-check you
are NOT pasting raw Bearer tokens, API keys, JWTs, or private keys.
-->

```
(paste here)
```

## Additional context

Anything else that helps us reproduce — config snippets (with secrets removed), frequency, whether it is a regression.
