//go:build integration

package evm_test

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

func TestIntegration_CallContract_Precompile(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	precompile := common.HexToAddress(testPrecompileAddr)

	// The precompile exposes a "registries" view function.
	// Even with empty calldata, the precompile should return something
	// (or error gracefully) rather than panic.
	t.Log("calling precompile with empty calldata...")
	result, err := c.CallContract(ctx, ethereum.CallMsg{
		To:   &precompile,
		Data: []byte{},
	}, nil)
	if err != nil {
		t.Logf("  CallContract with empty data returned error (expected): %v", err)
	} else {
		t.Logf("  result length: %d bytes", len(result))
		if len(result) > 0 {
			t.Logf("  first 32 bytes (hex): %x", result[:min(32, len(result))])
		}
	}
}

func TestIntegration_CallContract_NonExistentAddress(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	addr := common.HexToAddress("0x0000000000000000000000000000000000000001")

	result, err := c.CallContract(ctx, ethereum.CallMsg{
		To:   &addr,
		Data: []byte{0x00},
	}, nil)
	if err != nil {
		t.Logf("  error (may be expected): %v", err)
		return
	}

	t.Logf("  result from non-existent contract: %d bytes", len(result))
}
