// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/config"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
	mcpserver "github.com/NVNM-Chain/nvnm-mcp-server/internal/mcp"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/telemetry"
)

func ConfiguredServerURL() string {
	return os.Getenv(envServerURL)
}

// StartTarget returns the MCP URL the suite should talk to. A set
// NVNM_MCP_TEST_SERVER_URL is a deployment (or a local binary). Absence
// selects an in-process server with real chain clients.
func StartTarget(t *testing.T) string {
	t.Helper()
	if url := ConfiguredServerURL(); url != "" {
		t.Logf("MCP server under test: %s (URL override)", url)
		return url
	}
	url := startInProcess(t)
	t.Logf("MCP server under test: %s (in-process)", url)
	return url
}

func startInProcess(t *testing.T) string {
	t.Helper()

	rpcURL := os.Getenv("NVNM_EVM_RPC_URL")
	if rpcURL == "" {
		SkipOrFail(t, "in-process target requires NVNM_EVM_RPC_URL")
	}

	chainID := int64(787111)
	if raw := os.Getenv("NVNM_CHAIN_ID"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			t.Fatalf("NVNM_CHAIN_ID: %v", err)
		}
		chainID = id
	}

	env := config.InferEnvironmentFromChainID(chainID)
	if env == "" {
		env = config.EnvTestnet
		if raw := os.Getenv("NVNM_CHAIN_ENVIRONMENT"); raw != "" {
			env = config.ChainEnvironment(raw)
		}
	}

	abiPath := os.Getenv("ANCHOR_ABI_PATH")
	if abiPath == "" {
		if root := RepoRoot(); root != "" {
			abiPath = filepath.Join(root, "abi", "anchoring.json")
		} else {
			abiPath = filepath.Join("..", "..", "abi", "anchoring.json")
		}
	}
	anchorAddr := os.Getenv("ANCHOR_ADDRESS")
	if anchorAddr == "" {
		anchorAddr = "0x0000000000000000000000000000000000000A00"
	}

	t.Logf("dialing EVM RPC %s", rpcURL)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	raw, err := evm.NewClient(ctx, rpcURL, 15*time.Second)
	if err != nil {
		SkipOrFail(t, "cannot reach EVM RPC "+rpcURL+": "+err.Error())
	}
	t.Cleanup(raw.Close)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mp := sdkmetric.NewMeterProvider()
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	mets, mErr := telemetry.NewMetrics(mp)
	if mErr != nil {
		t.Fatalf("NewMetrics: %v", mErr)
	}
	evmClient := evm.NewResilientClient(raw, evm.ResilientConfig{
		MaxRetries:       5,
		InitialBackoff:   500 * time.Millisecond,
		MaxBackoff:       5 * time.Second,
		RateLimit:        100,
		RateBurst:        20,
		BreakerThreshold: 10,
		BreakerTimeout:   30 * time.Second,
	}, mets, logger)

	anchorClient := anchor.NewClient(evmClient, anchorAddr, chainID, abiPath, logger)

	cfg := &config.Config{
		EVMRPCURL:        rpcURL,
		ChainID:          chainID,
		ChainEnvironment: env,
		AnchorAddress:    anchorAddr,
		AnchorABIPath:    abiPath,
		EnableWriteTools: true,
		Transport:        "http",
		RequestTimeout:   30 * time.Second,
	}

	srv := mcpserver.NewServer(evmClient, anchorClient, cfg, nil, nil, nil, nil, nil, logger)
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(httpServer.Close)
	return httpServer.URL
}

func ConnectSession(t *testing.T, serverURL, token string) *mcp.ClientSession {
	t.Helper()

	httpClient := &http.Client{
		Transport: &bearerTransport{token: token},
		Timeout:   RegistriesTimeout,
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "nvnm-mcp-e2e", Version: "1.0.0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   serverURL,
		HTTPClient: httpClient,
	}, nil)
	if err != nil {
		t.Fatalf("connect to MCP server %s: %v", serverURL, err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}
