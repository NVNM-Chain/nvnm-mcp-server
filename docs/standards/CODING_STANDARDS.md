# NVNM Chain MCP Server — Coding Standards & Development Guide (Go)

**Last Updated:** April 28, 2026

## Overview

This guide provides coding standards and best practices for the NVNM Chain MCP Server. All contributors should follow these standards to ensure consistency, maintainability, and quality across the codebase.

## Table of Contents

1. [Core Principles](#core-principles)
2. [Error Handling Standards](#error-handling-standards)
3. [Configuration Management](#configuration-management)
4. [Go Style Guidelines](#go-style-guidelines)
5. [Architecture Principles](#architecture-principles)
6. [Type Safety](#type-safety)
7. [Logging Standards](#logging-standards)
8. [Testing Requirements](#testing-requirements)
9. [Security Guidelines](#security-guidelines)
10. [Resilience Patterns](#resilience-patterns)
11. [Pre-commit Hooks](#pre-commit-hooks)
12. [Pre-Development Checklist](#pre-development-checklist)
13. [Pre-Commit Checklist](#pre-commit-checklist)
14. [Common Commands](#common-commands)

---

## Core Principles

> **CRITICAL: READ THIS FIRST**
>
> These principles must be followed in ALL development on this project.
> Violation leads to code that doesn't fit the architecture.

### CRITICAL (Must Follow)

#### 1. No Defensive Programming

- **Fail Fast**: Do not mask errors with defaults and fallbacks
- **No Silent Fallbacks**: Errors must surface immediately, not be hidden
- **Explicit Permission**: Only use defaults/fallbacks with explicit human approval
- **Quality Over False Resilience**: Fail clearly rather than produce wrong results

```go
// CORRECT - Fail fast
func NewClient(rpcURL string) (*Client, error) {
    if rpcURL == "" {
        return nil, fmt.Errorf("RPC URL required - cannot proceed")
    }
    return &Client{rpcURL: rpcURL}, nil
}

// WRONG - Silent fallback that masks misconfiguration
func NewClientBad(rpcURL string) *Client {
    if rpcURL == "" {
        rpcURL = "http://localhost:8545" // hides configuration issues
    }
    return &Client{rpcURL: rpcURL}
}
```

#### 2. Generic Implementation

- Do not code to specific test data; write generic, reusable code
- Parameterize functions; avoid hardcoded addresses, block numbers, or hash values in business logic
- Avoid magic constants in tool handlers; use configuration

#### 3. Verification Before Coding

- Check method names and signatures before writing code — do not guess
- Consult `docs/DESIGN.md` and `docs/IMPLEMENTATION_PLAN.md`; do not assume interfaces
- Read the ABI (`abi/anchoring.json`) before writing contract call code
- Slow down; verify first

### IMPORTANT (Should Follow)

#### 4. Incremental Development

- Work in small, verifiable steps; test after each change
- Get human approval before moving to the next step
- Do not accumulate uncommitted changes across unrelated concerns

---

## Error Handling Standards

### Always Check Errors

**REQUIRED**: Check every error; never ignore.

```go
// REQUIRED
data, err := os.ReadFile(path)
if err != nil {
    return nil, fmt.Errorf("read ABI file: %w", err)
}

// FORBIDDEN
data, _ := os.ReadFile(path)
```

### Error Wrapping

Use `fmt.Errorf` with `%w` to preserve the error chain:

```go
if err != nil {
    return fmt.Errorf("anchor_store: call precompile: %w", err)
}
```

### Sentinel Errors

Define package-level sentinel errors for domain conditions; use `errors.Is` / `errors.As` at call sites:

```go
var ErrAnchorNotFound = errors.New("anchor not found")
var ErrInvalidAddress = errors.New("invalid Ethereum address")
```

### MCP Layer

- Map internal errors to typed MCP error responses; do not leak stack traces or internal hostnames
- Never expose raw RPC error text directly to MCP callers

### No Generic Recovery

- Avoid broad `recover()` that hides failures
- Fail fast; no silent fallbacks without explicit approval

---

## Configuration Management

### Source of Truth

Configuration is loaded from environment variables; implementation in `internal/config`.
Full reference: `README.md` (Configuration section).

### Validation

- Validate after loading; fail fast on missing or invalid values
- Required fields (RPC URL, chain ID) must be non-empty
- Timeouts must be positive durations
- If write tools are enabled (`ENABLE_WRITE_TOOLS=true`), signing config must be present

```go
func (c *Config) Validate() error {
    if c.RPCURL == "" {
        return fmt.Errorf("NVNM_EVM_RPC_URL is required")
    }
    if c.RequestTimeout <= 0 {
        return fmt.Errorf("request timeout must be positive")
    }
    if c.WriteToolsEnabled && c.SigningKey == "" {
        return fmt.Errorf("signing key required when write tools are enabled")
    }
    return nil
}
```

### Secrets

- Do not embed RPC credentials, private keys, or API keys in source
- Use `.env` (gitignored) or deployment secrets locally
- `.chain_credentials.txt` is gitignored (`chmod 600`) for local testnet work

### Naming

- Chain-specific settings: `NVNM_` prefix (the former `INVENIAM_` prefix was hard-cut in Phase 8.9, see `docs/RUNBOOK.md#env-var-migration`)
- Anchor-specific settings: `ANCHOR_` prefix
- Document all new variables in `README.md`

---

## Go Style Guidelines

### Formatting

- Use `gofmt` and `goimports`
- Import order: standard library, third-party, then local (automated by `goimports`)
- Tabs for indentation; follow `.golangci.yml`

### Import Organization

```go
import (
    // Standard library
    "context"
    "fmt"
    "math/big"

    // Third-party
    "github.com/ethereum/go-ethereum/common"
    "github.com/modelcontextprotocol/go-sdk/mcp"

    // Local
    "github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
    "github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)
```

### Naming Conventions

- **Exported types/functions**: PascalCase (e.g. `NormalizedBlock`, `CallContract`, `AnchorRecord`)
- **Unexported types/functions**: camelCase (e.g. `parseBlockNumber`, `evmClient`)
- **Constants**: PascalCase for exported, camelCase for unexported
- **Interfaces**: End with `-er` where natural (e.g. `Caller`, `Storer`)

### Documentation

All exported functions, types, and methods must have godoc comments:

```go
// AnchorRecord returns the anchoring data stored for the given document hash.
// Returns ErrAnchorNotFound if no record exists on-chain for that hash.
func (c *Client) AnchorRecord(ctx context.Context, hash common.Hash) (*AnchorData, error) {
```

### Linting

- `golangci-lint` with project `.golangci.yml` (17+ linters including `err113`, `errcheck`, `lll`, `nolintlint`)
- Resolve all reported issues; do not add `//nolint` without a comment explaining why
- `make check-all` before every commit — IDE diagnostics alone are insufficient

---

## Architecture Principles

### Package Responsibilities

| Package | Responsibility |
|---|---|
| `cmd/nvnm-mcp-server` | Entrypoint; wire dependencies; configure transports |
| `internal/config` | Load and validate environment configuration |
| `internal/logging` | Structured `slog`-based logger; redaction utilities |
| `internal/errors` | Shared sentinel errors |
| `internal/evm` | Generic EVM RPC client; chain reads; raw tx submission |
| `internal/anchor` | Inveniam-specific anchor precompile adapter |
| `internal/mcp` | MCP tool registration and handler wiring |
| `internal/version` | Build-time version constant |

### Separation of Concerns

- The EVM layer (`internal/evm`) must remain generic and anchor-agnostic
- The anchor layer (`internal/anchor`) handles Inveniam-specific precompile encoding/decoding
- MCP handlers (`internal/mcp`) are thin: validate input, call the right layer, format output

### Write Flow (Prepare-Sign-Submit)

The server **never holds private keys**. Write tools follow the pattern:

1. **Prepare** — construct the full unsigned transaction (to, data, gas estimate, nonce)
2. **Sign** — caller (MetaMask / CLI / hardware wallet) signs offline
3. **Submit** — `evm_send_raw_transaction` submits the signed raw bytes

---

## Type Safety

### Use Strong Types

Prefer specific types over `interface{}`:

```go
// GOOD
type AnchorData struct {
    DocumentHash common.Hash    `json:"document_hash"`
    AnchoredAt   time.Time      `json:"anchored_at"`
    Anchorer     common.Address `json:"anchorer"`
}

// BAD
type AnchorDataBad struct {
    DocumentHash interface{} // too generic; loses type guarantees
}
```

### Avoid `interface{}`

- No new `interface{}` or `map[string]interface{}` in production code without a comment justifying it
- For contract call results that must be decoded dynamically, decode immediately into a typed struct and add a comment explaining why

---

## Logging Standards

### Use Project Logger

- Use `internal/logging` which wraps `log/slog`
- Pass logger via dependency injection; do not use global logger in package-level logic
- Do not add `log.Printf` or `fmt.Println` in production paths

### Levels

| Level | Use |
|---|---|
| DEBUG | RPC call details, decoded values, tool argument tracing |
| INFO | Server start, tool calls, successful operations, chain confirmations |
| WARN | Retries, slow RPC, fallback conditions |
| ERROR | RPC failures, decode errors, configuration problems requiring investigation |

### Structure

Use `slog` typed fields; do not concatenate strings:

```go
slog.Info("anchor stored",
    slog.String("tx_hash", tx.Hex()),
    slog.String("document_hash", docHash.Hex()),
    slog.Duration("elapsed", elapsed),
)
```

### Safety

- Never log private keys, signing material, full RPC credentials, or raw bearer tokens
- Redact sensitive fields before logging; use the `internal/logging` redact helpers

---

## Testing Requirements

### Workflow

- Test after each change before proceeding
- `make test` — all tests; `make test-unit` — short/unit; `make test-coverage` — with coverage report; `make test-verbose` — verbose output

### Structure

- Tests live next to code (`foo_test.go` beside `foo.go`)
- Table-driven tests for multiple cases; keep cases readable
- Do not code tests to specific fixture data; keep test helpers generic

### Practices

- `t.Helper()` in all test helpers
- `t.Setenv()` for environment variables in tests
- `t.TempDir()` for temporary filesystem needs
- `t.Cleanup()` for teardown
- Standard library `testing` + `net/http/httptest` only — no third-party test frameworks
- Do not fake or mock data without explicit instruction; prefer documented fixtures or real endpoints with build tags

### Integration Tests

- Tag with `//go:build integration`
- Run with `make test-integration`
- For RPC-dependent tests: use recorded responses or test against testnet endpoint

### Coverage

- Meaningful coverage on new logic; run `make test-coverage` when touching critical paths
- Aim for coverage on all error paths, not just happy paths

### Pre-commit / CI

- `make pre-commit` runs all hooks (fmt, imports, vet, lint, secrets)
- Tests are run via Makefile/CI, not pre-commit hooks
- CI runs `go test -race`; run locally with `go test -race ./...` when touching concurrency code

---

## Security Guidelines

See `.cursor/rules/security.mdc` for the full security rule set. Key points:

- Never commit secrets, private keys, or RPC credentials
- `detect-secrets` runs in pre-commit; fix findings before committing
- `govulncheck ./...` runs in CI; treat vulnerabilities as blocking
- Validate all Ethereum addresses, tx hashes, and block references at the MCP boundary
- Map raw RPC errors to safe typed responses before returning to callers
- Production transports must use TLS

---

## Resilience Patterns

These patterns are relevant for Phase 4 (Hardening) and any code touching RPC or concurrency.

### Retry with Exponential Backoff

For transient RPC errors, retry with bounded backoff:

```go
// Classify before retrying: not all RPC errors are transient
func isTransientRPCError(err error) bool {
    // connection refused, timeout, 429 rate-limit, 503 unavailable
    // NOT: invalid params, not found, revert
}
```

- Start: 500ms; cap: 30s; max attempts: configurable via `INVENIAM_RPC_MAX_RETRIES`
- Log each retry at WARN level with attempt count and delay

### Rate Limiting

- All outbound RPC calls pass through a rate limiter
- Configurable via `INVENIAM_RPC_RATE_LIMIT_RPS`
- Return a typed `ErrRateLimitExceeded` — do not silently drop or delay without logging

### Request Timeouts

- Each tool call enforces a per-request timeout via context
- Default: `INVENIAM_REQUEST_TIMEOUT=15s`; never allow unbounded RPC waits

### Circuit Breaker

- Open circuit after N consecutive failures; configurable threshold
- Half-open after a cooldown period; allow a probe request
- Return `ErrCircuitOpen` immediately when open — fail fast rather than queue

### Graceful Shutdown

- All long-running operations respect context cancellation
- Server listens for `SIGINT`/`SIGTERM`; drains in-flight requests before exit
- Log shutdown initiation and completion at INFO level

---

## Pre-commit Hooks

Pre-commit runs automatically on `git commit`. To run manually:

```bash
make pre-commit        # run all hooks on staged files
make install-hooks     # install hooks (run once after cloning)
```

Hooks include: `gofmt`, `goimports`, `go vet`, `golangci-lint`, `detect-secrets`.

**Never use `git commit --no-verify`** without explicit user approval.

---

## Pre-Development Checklist

**CRITICAL:**
- [ ] No Defensive Programming: using explicit errors, not silent fallbacks?
- [ ] Generic Code: not hardcoded to specific test addresses or hashes?
- [ ] Method Verified: checked that method/ABI function actually exists?

**IMPORTANT:**
- [ ] Incremental: working in small, testable steps?
- [ ] Design Reviewed: read `docs/DESIGN.md` for the affected package?
- [ ] Phase Alignment: change is within the current phase in `docs/IMPLEMENTATION_PLAN.md`?

---

## Pre-Commit Checklist

- [ ] All exported functions have godoc comments
- [ ] All errors are checked and returned (no `_` ignoring errors)
- [ ] No hardcoded addresses, private keys, or RPC credentials
- [ ] Tests written for new behavior; existing tests pass
- [ ] `make check-all` passes with zero issues
- [ ] `make test` passes
- [ ] No new `interface{}` without justifying comment
- [ ] Meaningful variable and function names; no single-letter vars outside loops
- [ ] Follows package structure in `docs/DESIGN.md`

---

## Common Commands

```bash
# Build and run
make run              # Build and run (stdio transport)
make run-http         # Build and run (HTTP transport)

# Quality checks
make check-all        # format + vet + lint
make format           # gofmt + goimports
make vet              # go vet
make lint             # golangci-lint

# Tests
make test             # all tests
make test-unit        # unit tests only
make test-coverage    # with coverage report
make test-verbose     # verbose output
make test-race        # with race detector (use when touching concurrency)

# Pre-commit
make pre-commit       # run all hooks
make install-hooks    # install hooks (once per clone)
make setup-dev        # install dev deps + hooks

# CI simulation
make ci               # install-dev + check-all + test-coverage
make release-check    # clean + ci + build

# Security
govulncheck ./...     # check for known vulnerabilities

# Project info
make info
```
