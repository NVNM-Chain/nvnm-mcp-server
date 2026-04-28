package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/inveniam/nvnm-mcp-server/internal/anchor"
	"github.com/inveniam/nvnm-mcp-server/internal/auth"
	"github.com/inveniam/nvnm-mcp-server/internal/evm"
	"github.com/inveniam/nvnm-mcp-server/internal/version"
)

const serverName = "inveniam-evm"

// Server wraps the MCP server with its dependencies.
type Server struct {
	mcpServer *mcp.Server
	logger    *slog.Logger
}

// NewServer creates a new MCP server and registers all tools.
// When enableWriteTools is true, prepare-sign-submit tools and
// evm_send_raw_transaction are registered, gated by writeApprovalDefault.
// Middleware (if any) is registered via AddReceivingMiddleware.
func NewServer(
	evmClient evm.Client,
	anchorClient anchor.Client,
	enableWriteTools bool,
	writeApprovalDefault string,
	middleware []mcp.Middleware,
	logger *slog.Logger,
) *Server {
	mcpSrv := mcp.NewServer(
		&mcp.Implementation{
			Name:    serverName,
			Version: version.Version,
		},
		nil,
	)

	for _, mw := range middleware {
		mcpSrv.AddReceivingMiddleware(mw)
	}

	s := &Server{
		mcpServer: mcpSrv,
		logger:    logger,
	}

	registerEVMTools(mcpSrv, evmClient, logger)
	registerAnchorTools(mcpSrv, anchorClient, logger)

	if enableWriteTools {
		registerEVMWriteTools(mcpSrv, evmClient, writeApprovalDefault, logger)
		registerAnchorWriteTools(mcpSrv, anchorClient, logger)
		logger.Info("write tools enabled (anchor_prepare_*, evm_send_raw_transaction)",
			slog.String("write_approval_default", writeApprovalDefault),
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
// When limiter is non-nil, per-client rate limiting is enforced.
func (s *Server) RunHTTP(
	ctx context.Context,
	addr string,
	validator auth.TokenValidator,
	limiter *ClientRateLimiter,
) error {
	s.logger.Info("starting MCP server",
		slog.String("transport", "http"),
		slog.String("addr", addr),
		slog.Bool("auth_required", validator != nil),
		slog.Bool("rate_limiting", limiter != nil),
	)

	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return s.mcpServer
	}, nil)

	// Chain: limitRequestBody → AuthMiddleware → ClientRateLimiter → mcpHandler
	// Rate limiter sits inside auth so the client ID is set on context.
	var inner http.Handler = mcpHandler
	if limiter != nil {
		inner = limiter.Middleware(mcpHandler, s.logger)
	}
	handler := limitRequestBody(AuthMiddleware(inner, validator, s.logger))

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
