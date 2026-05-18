# NVNM Chain MCP Server — Helm chart

Deploys the [NVNM Chain MCP Server](https://github.com/inveniamcapital/NVNM_MCP_Server) on Kubernetes. Hand-curated; matches `Chart.yaml` version `0.2.0`.

## What this chart deploys

What runs in the cluster after `helm install`. Most workloads in the table are conditional — flip on what you need via `values.yaml`.

| Object | Purpose | Default |
|---|---|---|
| `Deployment` | Server pod(s); HTTP transport on `:8180`, health/metrics on `:9190`, admin REST on `:8081` | always rendered |
| `Service` | ClusterIP exposing the three ports above | always rendered |
| `ConfigMap` | Non-secret env vars (chain config, log level, etc.) | always rendered |
| `PodDisruptionBudget` | `minAvailable: 1` to keep cluster operations from draining the only pod | on, but double-gated on `replicaCount > 1` |
| `HorizontalPodAutoscaler` | Scale on CPU / memory | off |
| `ServiceMonitor` | Prometheus operator scrape config | off |
| `NetworkPolicy` | Restrict egress to RPC + JWKS + OTEL + DNS; ingress to expected ports | off (requires policy-enforcing CNI) |
| `Ingress` | nginx + cert-manager worked example | off |

## Required values

These pin the server's chain at startup. Operators **must** set them; the server fails loud at startup if any legacy `INVENIAM_*` env var is also present in the environment (Phase 8.9 hard cut). See [`docs/RUNBOOK.md`](../../../docs/RUNBOOK.md) for the env-var migration record.

| Value | Example |
|---|---|
| `env.NVNM_EVM_RPC_URL` | `https://evm.testnet.nvnmchain.io` (testnet) or `https://evm.nvnmchain.io` (mainnet) |
| `env.NVNM_CHAIN_ID` | `787111` (testnet) or `1611` (mainnet) |
| `env.NVNM_CHAIN_ENVIRONMENT` | `testnet` or `mainnet` |
| `image.repository` | `ghcr.io/inveniamcapital/nvnm-mcp-server` |
| `image.tag` | Matches a released artifact tag |

## Mainnet vs testnet

The server deploys **one instance per chain** (see [`docs/DESIGN.md`](../../../docs/DESIGN.md) § Multi-chain). Same chart, different value files; the cutover playbook is [`docs/MAINNET_CUTOVER.md`](../../../docs/MAINNET_CUTOVER.md).

| Aspect | Testnet | Mainnet |
|---|---|---|
| RPC URL | `https://evm.testnet.nvnmchain.io` | `https://evm.nvnmchain.io` |
| EVM chain ID | `787111` | `1611` |
| Environment label | `testnet` | `mainnet` |
| Cosmos chain ID | `nvnm-testnet-1` | `nvnm-1` |

## Gotchas

- **`PodDisruptionBudget` is double-gated.** Both `podDisruptionBudget.enabled` (default on) AND `replicaCount > 1` must hold for the PDB to render. Prevents a one-pod default + `minAvailable: 1` PDB from blocking node drains.
- **`NetworkPolicy` is off by default.** Many clusters don't run a policy-enforcing CNI (Calico, Cilium, etc.) and applying a `NetworkPolicy` to them has no effect. Enable explicitly with `networkPolicy.enabled=true` only when you have a CNI that enforces it.
- **`Ingress` is off by default.** Enable + override `ingress.host`, `ingress.annotations`, and `ingress.tls` for a cert-manager + nginx-ingress deploy. Don't enable unless you also have TLS termination plumbed.
- **The admin REST API binds to `:8081` inside the pod and the chart does not by default restrict it.** Limit access via `NetworkPolicy`, a separate Service, or operator-supplied middleware before exposing the pod outside the cluster.
- **`securityContext` is hardened by default** (`runAsNonRoot`, distroless UID/GID 65532, `readOnlyRootFilesystem`, all capabilities dropped, `seccompProfile: RuntimeDefault`). The published image is built distroless-compatible. If you override `image.repository` with a non-distroless build, you may need to relax some of these.
- **Resource requests / limits are starter values, not production-tuned.** `100m/128Mi` request, `500m/256Mi` limit. Right-size from real load in Phase 10; override via `resources` in `values.yaml`.

## Lint and render

```sh
helm lint deploy/helm/nvnm-mcp-server/
helm template my-release deploy/helm/nvnm-mcp-server/ --debug
```

CI does not currently lint the chart on every push; run these locally before tagging a chart release.

## Related documentation

- [`docs/DESIGN.md`](../../../docs/DESIGN.md) — architecture
- [`docs/RUNBOOK.md`](../../../docs/RUNBOOK.md) — runtime operations
- [`docs/MAINNET_CUTOVER.md`](../../../docs/MAINNET_CUTOVER.md) — testnet → mainnet cutover
- [`docs/SECURITY_AUDIT.md`](../../../docs/SECURITY_AUDIT.md) — security posture
- [`CHANGELOG.md`](../../../CHANGELOG.md) — chart and binary release notes
