// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/config"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/telemetry"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/version"
)

const serverName = "nvnm-chain"

// initializeInstructions is the per-server "instructions" string the
// MCP SDK surfaces in the initialize response (see the MCP spec field
// InitializeResult.instructions). Clients are expected to treat it
// like a system-prompt hint -- the model sees it before any tool
// description. This is defense in depth for the lobby-tool pattern:
// agents whose client compresses tool descriptions, or who skip
// browsing tools/list, still receive the privacy-by-design caveat
// and a pointer to nvnm_overview at session start.
//
// Keep this string terse (a few sentences). The richer chain
// summary, prereqs, and canonical journey live in the nvnm_overview
// tool response, which this string deliberately points at rather
// than duplicates.
const initializeInstructions = "NVNM Chain MCP server -- typed " +
	"access to NVNM Chain, an EVM L2 on MANTRA Chain used as a " +
	"neutral notary for document anchoring. The anchoring " +
	"precompile deliberately emits no events: this server can " +
	"report whether a wallet has been funded or has sent " +
	"transactions, but never what those transactions did. If you " +
	"have never used this server before, call nvnm_overview first " +
	"for chain identity, prereqs, and a recommended 6-step agent " +
	"journey."

// Server wraps the MCP server with its dependencies.
type Server struct {
	mcpServer    *mcp.Server
	logger       *slog.Logger
	keylessReads bool
}

// NewServer creates a new MCP server and registers all tools.
// cfg is the full server configuration. The Phase 8.8 onboarding tools
// (nvnm_overview, wallet_status, nvnm_setup_wizard, the two verify
// helpers) read several fields off cfg (chain ID, environment label,
// anchor address, explorer/docs/bridge URLs) so passing the typed
// struct beats threading individual scalars.
//
// When enableWriteTools is true, prepare-sign-submit tools and
// evm_send_raw_transaction are registered, gated by
// cfg.WriteApprovalDefault. The Phase 8.8 onboarding tools and the
// read tools register regardless of write-tool gating.
//
// Middleware (if any) is registered via AddReceivingMiddleware.
//
// Tool-registration order below reflects the conceptual grouping
// onboarding → reads → writes. tools/list responses are sorted
// alphabetically by the SDK, so this order is not user-visible today;
// it is preserved here so a future SDK that respects registration
// order, or a consumer that reads the source, sees the intended
// hierarchy.
func NewServer(
	evmClient evm.Client,
	anchorClient anchor.Client,
	cfg *config.Config,
	middleware []mcp.Middleware,
	logger *slog.Logger,
) *Server {
	mcpSrv := mcp.NewServer(
		&mcp.Implementation{
			Name:    serverName,
			Version: version.Version,
		},
		&mcp.ServerOptions{Instructions: initializeInstructions},
	)

	// Keyless reads are HTTP-only; stdio is local/trusted and has no
	// AuthMiddleware. Gate on transport so a stdio+keyless combo cannot
	// reject local write-tool calls.
	keyless := cfg.KeylessReads && cfg.Transport == "http"
	if cfg.KeylessReads && cfg.Transport != "http" {
		logger.Warn("MCP_KEYLESS_READS set but transport is not HTTP; ignored (stdio is local-trusted)")
	}

	// Per-tool auth enforcement is registered BEFORE caller-supplied
	// middleware. The SDK composes successive AddReceivingMiddleware
	// calls later-added=outer, so telemetry added in the loop below
	// wraps this layer and still observes anonymous-write rejections.
	// No-op when keyless is off (AuthMiddleware then guarantees claims
	// are present for every tools/call).
	mcpSrv.AddReceivingMiddleware(NewAuthEnforcementMiddleware(keyless, logger))

	for _, mw := range middleware {
		mcpSrv.AddReceivingMiddleware(mw)
	}

	s := &Server{
		mcpServer:    mcpSrv,
		logger:       logger,
		keylessReads: keyless,
	}

	// 1. Onboarding tools first so they appear at the top of tools/list.
	registerOverviewTool(mcpSrv, cfg)
	registerWalletTool(mcpSrv, evmClient, cfg)
	registerSetupWizardTool(mcpSrv, evmClient, cfg)
	registerVerifyHashTool(mcpSrv)
	registerVerifySignatureTool(mcpSrv)

	// 2-3. Existing read tools.
	registerEVMTools(mcpSrv, evmClient, logger)
	registerAnchorTools(mcpSrv, anchorClient, logger)

	// 4. Write tools, gated.
	if cfg.EnableWriteTools {
		chainEnvironment := string(cfg.ChainEnvironment)
		registerEVMWriteTools(mcpSrv, evmClient, cfg.WriteApprovalDefault, chainEnvironment, logger)
		registerAnchorWriteTools(mcpSrv, anchorClient, logger)
		logger.Info("write tools enabled (anchor_prepare_*, evm_send_raw_transaction)",
			slog.String("write_approval_default", cfg.WriteApprovalDefault),
			slog.String("chain_environment", chainEnvironment),
		)
	}

	return s
}

// RunStdio runs the MCP server over stdin/stdout.
func (s *Server) RunStdio(ctx context.Context) error {
	s.logger.Info("starting MCP server", slog.String("transport", "stdio"))
	return s.mcpServer.Run(ctx, &mcp.StdioTransport{})
}

// MaxRequestBodyBytes limits the size of incoming HTTP request bodies (10 MB).
const MaxRequestBodyBytes = 10 * 1024 * 1024

// RunHTTP runs the MCP server over Streamable HTTP on the given address.
// When validator is non-nil, requests must include a valid
// "Authorization: Bearer <token>" header.
// Keyless read mode (cfg.KeylessReads, fixed at construction time) relaxes
// this: requests with no Authorization header are admitted anonymously and
// per-tool enforcement gates write tools. A present-but-invalid credential
// is still rejected.
// When limiter is non-nil, per-client rate limiting is enforced.
// When failLimiter is non-nil, pre-auth failure-rate limiting per
// source IP is enforced (defeats credential stuffing).
// When allowedOrigins is nil, DefaultOriginAllowlist() is used
// (localhost variants only); production deployments must supply a
// non-nil allowlist that includes the origins of trusted MCP clients.
func (s *Server) RunHTTP(
	ctx context.Context,
	addr string,
	validator auth.TokenValidator,
	limiter *ClientRateLimiter,
	anonLimiter *AnonReadRateLimiter,
	failLimiter *IPFailRateLimiter,
	allowedOrigins *OriginAllowlist,
	metrics *telemetry.Metrics,
	keyRequestHandler http.Handler,
) error {
	if allowedOrigins == nil {
		allowedOrigins = DefaultOriginAllowlist()
	}

	s.logger.Info("starting MCP server",
		slog.String("transport", "http"),
		slog.String("addr", addr),
		slog.Bool("auth_required", validator != nil),
		slog.Bool("keyless_reads", s.keylessReads),
		slog.Bool("rate_limiting", limiter != nil),
		slog.Bool("anon_rate_limiting", anonLimiter != nil),
		slog.Bool("fail_rate_limiting", failLimiter != nil),
		slog.Bool("key_request_endpoint", keyRequestHandler != nil),
		slog.Any("allowed_origins", allowedOrigins.Resolved()),
	)

	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return s.mcpServer
	}, nil)

	// Chain (outermost first):
	//   CORSMiddleware       → browser preflight + cross-origin permission
	//   responseMetrics      → Phase 10 RD3 SLI counter (class label)
	//   originGuard          → cheap string lookup, DNS-rebinding defense
	//   IPFailRateLimiter    → pre-auth: blocks credential-stuffing per source IP
	//   limitRequestBody     → cap body before any parser sees it
	//   path mux             → branches the chain by URL path:
	//     /api/v1/keys/request → public key-request handler (Phase 11 L3)
	//                            (NO AuthMiddleware; bring-your-own rate limit
	//                            inside the handler)
	//     default              → AuthMiddleware → AnonReadRateLimiter →
	//                            ClientRateLimiter → mcpHandler
	//   AuthMiddleware       → validates bearer (penalizes failLimiter on miss);
	//                          under keyless mode admits anonymous when the
	//                          Authorization header is absent
	//   AnonReadRateLimiter  → per-IP throttle for anonymous traffic; bypasses
	//                          authed requests (they pay ClientRateLimiter)
	//   ClientRateLimiter    → per-client bucket; requires identity from Auth,
	//                          passes anonymous through
	//   mcpHandler           → MCP SDK
	var mcpChain http.Handler = mcpHandler
	if limiter != nil {
		mcpChain = limiter.Middleware(mcpChain, s.logger)
	}
	if anonLimiter != nil {
		mcpChain = anonLimiter.Middleware(mcpChain, s.logger)
	}
	mcpChain = AuthMiddleware(mcpChain, validator, failLimiter, s.keylessReads, s.logger)

	// Path mux: if the public key-request handler is wired, route its
	// exact path to it; everything else falls through to the MCP auth
	// chain. When the endpoint is disabled the mux is collapsed to the
	// MCP chain so there is no extra hop on the hot path.
	routed := mcpChain
	if keyRequestHandler != nil {
		mux := http.NewServeMux()
		mux.Handle(KeyRequestPath, keyRequestHandler)
		mux.Handle("/", mcpChain)
		routed = mux
	}

	bodyLimited := limitRequestBody(routed)
	failGuarded := bodyLimited
	if failLimiter != nil {
		failGuarded = failLimiter.Wrap(bodyLimited, s.logger)
	}
	guarded := originGuard(failGuarded, allowedOrigins, s.logger)
	// Response metrics sit just inside CORS so the Phase 10 SLI counter
	// (mcp_http_responses_total{class=...}) sees every real-request
	// response with its final status — including Origin-guard rejections
	// (intentionally; a spike of 403s is a misconfiguration signal) —
	// but does not record OPTIONS preflight noise that CORS handles
	// before delegating downward. nil metrics is a passthrough; tests
	// and stdio-only callers pass nil.
	metered := responseMetricsMiddleware(guarded, metrics)
	// CORS sits outermost so it answers browser OPTIONS preflight before
	// the Origin guard or any parser. It shares the same allowlist but
	// grants cross-origin permission rather than rejecting (see cors.go).
	handler := CORSMiddleware(metered, allowedOrigins, s.logger)

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("HTTP server error: %w", err)
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("shutting down HTTP server")
		drainCtx, drainCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer drainCancel()
		return srv.Shutdown(drainCtx)
	case err := <-errCh:
		return err
	}
}

func limitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodyBytes)
		next.ServeHTTP(w, r)
	})
}
