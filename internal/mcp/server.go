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
	"github.com/inveniam/nvnm-mcp-server/internal/evm"
)

const (
	serverName    = "inveniam-evm"
	serverVersion = "0.3.0"
)

// Server wraps the MCP server with its dependencies.
type Server struct {
	mcpServer *mcp.Server
	logger    *slog.Logger
}

// NewServer creates a new MCP server and registers all tools.
// When enableWriteTools is true, prepare-sign-submit tools and
// evm_send_raw_transaction are registered.
func NewServer(
	evmClient evm.Client,
	anchorClient anchor.Client,
	enableWriteTools bool,
	logger *slog.Logger,
) *Server {
	mcpSrv := mcp.NewServer(
		&mcp.Implementation{
			Name:    serverName,
			Version: serverVersion,
		},
		nil,
	)

	s := &Server{
		mcpServer: mcpSrv,
		logger:    logger,
	}

	registerEVMTools(mcpSrv, evmClient, logger)
	registerAnchorTools(mcpSrv, anchorClient, logger)

	if enableWriteTools {
		registerEVMWriteTools(mcpSrv, evmClient, logger)
		registerAnchorWriteTools(mcpSrv, anchorClient, logger)
		logger.Info("write tools enabled (anchor_prepare_*, evm_send_raw_transaction)")
	}

	return s
}

// RunStdio runs the MCP server over stdin/stdout.
func (s *Server) RunStdio(ctx context.Context) error {
	s.logger.Info("starting MCP server", slog.String("transport", "stdio"))
	return s.mcpServer.Run(ctx, &mcp.StdioTransport{})
}

// RunHTTP runs the MCP server over Streamable HTTP on the given address.
func (s *Server) RunHTTP(ctx context.Context, addr string) error {
	s.logger.Info("starting MCP server",
		slog.String("transport", "http"),
		slog.String("addr", addr),
	)

	handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return s.mcpServer
	}, nil)

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
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
		return srv.Close()
	case err := <-errCh:
		return err
	}
}
