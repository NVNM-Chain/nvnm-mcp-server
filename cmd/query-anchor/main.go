// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/inveniam/nvnm-mcp-server/internal/anchor"
	"github.com/inveniam/nvnm-mcp-server/internal/evm"
	"github.com/inveniam/nvnm-mcp-server/internal/logging"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	logger := logging.NewText("warn")

	rpcURL := "https://evm.testnet.nvnmchain.io"
	abiPath := "abi/anchoring.json"

	evmClient, err := evm.NewClient(ctx, rpcURL, 15*time.Second)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer evmClient.Close()

	chainInfo, err := evmClient.GetChainInfo(ctx)
	if err != nil {
		return fmt.Errorf("chain info: %w", err)
	}
	fmt.Printf("Chain ID: %d | Latest Block: %d\n", chainInfo.ChainID, chainInfo.LatestBlockNumber)

	anchorClient := anchor.NewClient(evmClient, anchor.PrecompileAddress, chainInfo.ChainID, abiPath, logger)

	allRegs, getRegsErr := anchorClient.GetRegistries(ctx, anchor.GetRegistriesRequest{
		Pagination: &anchor.PageRequest{Limit: 200},
	})
	if getRegsErr != nil {
		return fmt.Errorf("get registries: %w", getRegsErr)
	}
	fmt.Printf("\nTotal registries: %d (fetched %d)\n", allRegs.Pagination.Total, len(allRegs.Registries))

	creators := map[string]int{}
	for i := range allRegs.Registries {
		creators[allRegs.Registries[i].Creator]++
	}
	fmt.Printf("\nCreators:\n")
	for c, count := range creators {
		fmt.Printf("  %s: %d registries\n", c, count)
	}

	if len(allRegs.Registries) > 0 {
		fmt.Printf("\nEarliest: %s (ID=%d)\n", allRegs.Registries[0].CreatedAt, allRegs.Registries[0].ID)
		last := allRegs.Registries[len(allRegs.Registries)-1]
		fmt.Printf("Latest:   %s (ID=%d)\n", last.CreatedAt, last.ID)
	}

	fmt.Printf("\n=== Records Survey ===\n")
	totalRecords := uint64(0)
	registriesWithRecords := 0
	limit := 20
	if len(allRegs.Registries) < limit {
		limit = len(allRegs.Registries)
	}

	for i := range allRegs.Registries[:limit] {
		reg := &allRegs.Registries[i]
		records, recErr := anchorClient.GetRecords(ctx, anchor.GetRecordsRequest{
			Registry:   &reg.Name,
			Pagination: &anchor.PageRequest{Limit: 5},
		})
		if recErr != nil {
			fmt.Printf("  Registry %d (%s): error: %v\n", reg.ID, reg.Name, recErr)
			continue
		}
		var count uint64
		if records.Pagination != nil {
			count = records.Pagination.Total
		}
		totalRecords += count
		if count > 0 {
			registriesWithRecords++
			fmt.Printf("\n  Registry %d (%s): %d records\n", reg.ID, reg.Name, count)
			for j := range records.Records {
				r := &records.Records[j]
				fmt.Printf("    Record %d (v%d) status=%s checksum=%s algo=%s latest=%v\n",
					r.RecordID, r.Index, r.Status, r.Checksum, r.ChecksumAlgo, r.IsLatest)
				fmt.Printf("      URI: %s\n", r.URI)
				fmt.Printf("      Timestamp: %s\n", r.Timestamp)
				if r.Metadata != "" {
					fmt.Printf("      Metadata: %s\n", r.Metadata)
				}
			}
		}
	}

	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("Registries surveyed: %d / %d\n", limit, allRegs.Pagination.Total)
	fmt.Printf("With records: %d\n", registriesWithRecords)
	fmt.Printf("Total records found: %d\n", totalRecords)

	fmt.Printf("\n=== All Records (no filter, limit 20) ===\n")
	allRecords, allRecErr := anchorClient.GetRecords(ctx, anchor.GetRecordsRequest{
		Pagination: &anchor.PageRequest{Limit: 20},
	})
	if allRecErr != nil {
		fmt.Printf("error: %v\n", allRecErr)
	} else {
		fmt.Printf("Total records on chain: %d\n", allRecords.Pagination.Total)
		fmt.Printf("Returned: %d\n", len(allRecords.Records))
		printJSON(allRecords)
	}

	return nil
}

func printJSON(v interface{}) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Printf("  (marshal error: %v)\n", err)
		return
	}
	fmt.Println(string(data))
}
