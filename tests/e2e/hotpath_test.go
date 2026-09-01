// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/NVNM-Chain/nvnm-mcp-server/tests/e2e"
)

// TestE2E_HotPath_AnchorDocument is the human-facing operator journey:
// onboard, create a registry, anchor a document, retire it, and observe
// the write through EVM tools. Subtest names are the language of the
// flow so a QA failure report says which stage gave way. Decode uses
// published JSON field names, so a read/prepare contract change fails
// here. Grant/revoke are not in this path: they need the admin MCP role
// and there is no role-read tool to observe the effect.
//
//	go test -tags e2e -v -run TestE2E_HotPath_AnchorDocument ./tests/e2e
func TestE2E_HotPath_AnchorDocument(t *testing.T) {
	f := e2e.NewFlow(t)
	e2e.SetupDiscovery(t, f)
	e2e.PreflightChainLive(t, f)

	if !f.WriteToolsAvailable {
		if f.MainnetReadOnly {
			t.Skip("mainnet: hot path skipped (read-only smoke)")
		}
		e2e.SkipOrFail(t, "hot path requires write tools")
	}
	if !f.WalletFunded {
		e2e.SkipOrFail(t, "hot path requires a funded signing wallet "+f.Address)
	}

	t.Run("registry", func(t *testing.T) {
		var utx *e2e.UnsignedTx
		t.Run("prepare", func(t *testing.T) {
			f.RegistryName = "mcp-hotpath-" + e2e.UniqueSuffix()
			utx = f.Prepare(t, "anchor_prepare_add_registry", map[string]any{
				"from":        f.Address,
				"name":        f.RegistryName,
				"description": "NVNM MCP hot-path journey",
				"metadata":    `{"suite":"tests/e2e","journey":"hot-path"}`,
			})
			e2e.AssertUnsignedTxShape(t, f, utx)
		})
		if utx == nil {
			t.Fatal("prepare stage did not produce an unsigned transaction")
		}

		var rcpt *e2e.Receipt
		t.Run("sign_broadcast_confirm", func(t *testing.T) {
			rcpt = f.SignBroadcastConfirm(t, utx)
			e2e.AssertReceiptContract(t, rcpt)
		})
		if rcpt == nil {
			t.Fatal("sign/broadcast/confirm stage did not produce a receipt")
		}

		t.Run("read_back", func(t *testing.T) {
			var listing e2e.RegistriesResponse
			started := time.Now()
			f.CallOK(t, "anchor_get_registries", map[string]any{
				"name":  f.RegistryName,
				"match": "exact",
				"limit": 10,
			}, &listing)
			elapsed := time.Since(started)
			t.Logf("anchor_get_registries by-name took %s", elapsed.Truncate(time.Millisecond))
			if elapsed > e2e.RegistriesLatencyBudget {
				t.Errorf("by-name listing took %s, budget is %s (HTTP wait remains %s)",
					elapsed.Truncate(time.Millisecond), e2e.RegistriesLatencyBudget, e2e.RegistriesTimeout)
			}
			if listing.ContentTrust == "" {
				t.Fatal("anchor_get_registries content_trust empty")
			}
			var found *e2e.Registry
			for i := range listing.Registries {
				if listing.Registries[i].Name == f.RegistryName {
					found = &listing.Registries[i]
					break
				}
			}
			if found == nil {
				// Name listing can miss if the scan hit its page cap.
				got := f.ResolveCreatedRegistry(t, f.RegistryName, rcpt.BlockNumber)
				f.RegistryID = got.ID
			} else {
				e2e.AssertRegistryContract(t, *found, f.Address)
				f.RegistryID = found.ID
			}

			var byID e2e.RegistryResponse
			f.CallOK(t, "anchor_get_registry", map[string]any{"id": f.RegistryID}, &byID)
			if byID.ContentTrust == "" {
				t.Fatal("anchor_get_registry content_trust empty")
			}
			e2e.AssertRegistryContract(t, byID.Registry, f.Address)
			t.Logf("read back registry %q id=%d after tx %s", byID.Name, byID.ID, rcpt.TxHash)
		})
	})
	if f.RegistryID == 0 {
		t.Fatal("registry stage did not resolve a registry id")
	}

	t.Run("record", func(t *testing.T) {
		sum := sha256.Sum256([]byte("nvnm mcp hot-path document " + e2e.UniqueSuffix()))
		f.Checksum = hex.EncodeToString(sum[:])
		f.URI = "https://example.invalid/nvnm-hotpath/" + e2e.UniqueSuffix()

		var utx *e2e.UnsignedTx
		t.Run("prepare", func(t *testing.T) {
			utx = f.Prepare(t, "anchor_prepare_add_record", map[string]any{
				"from":          f.Address,
				"registry_id":   f.RegistryID,
				"uri":           f.URI,
				"checksum":      f.Checksum,
				"checksum_algo": "sha256",
				"metadata":      `{"suite":"tests/e2e","journey":"hot-path"}`,
			})
			e2e.AssertUnsignedTxShape(t, f, utx)
		})
		if utx == nil {
			t.Fatal("prepare stage did not produce an unsigned transaction")
		}

		var rcpt *e2e.Receipt
		t.Run("sign_broadcast_confirm", func(t *testing.T) {
			rcpt = f.SignBroadcastConfirm(t, utx)
			e2e.AssertReceiptContract(t, rcpt)
			f.RecordTxHash = rcpt.TxHash
			f.RecordBlock = rcpt.BlockNumber
		})
		if rcpt == nil {
			t.Fatal("sign/broadcast/confirm stage did not produce a receipt")
		}

		t.Run("read_back", func(t *testing.T) {
			var out e2e.RecordsResponse
			f.CallOK(t, "anchor_get_records", map[string]any{
				"registry_id": f.RegistryID,
				"checksum":    f.Checksum,
			}, &out)
			if out.ContentTrust == "" {
				t.Fatal("anchor_get_records content_trust empty")
			}
			var found *e2e.Record
			for i := range out.Records {
				if out.Records[i].Checksum == f.Checksum ||
					strings.EqualFold(
						strings.TrimPrefix(strings.ToLower(out.Records[i].Checksum), "0x"),
						strings.ToLower(f.Checksum),
					) {
					found = &out.Records[i]
					break
				}
			}
			if found == nil {
				t.Fatalf("read-back did not find checksum %s in registry %d",
					f.Checksum, f.RegistryID)
			}
			e2e.AssertRecordReadBack(t, *found, f.RegistryID, f.Checksum, f.URI)
			f.RecordID = found.RecordID
			f.RecordIndex = found.Index
			t.Logf("read back record_id=%d index=%d status=%s after tx %s",
				found.RecordID, found.Index, found.Status, rcpt.TxHash)
		})
	})
	if f.RecordID == 0 {
		t.Fatal("record stage did not resolve a record id")
	}

	t.Run("lifecycle", func(t *testing.T) {
		if !f.LifecycleToolsAvailable {
			t.Skip("anchor_prepare_update_record_status is not advertised")
		}
		var utx *e2e.UnsignedTx
		t.Run("prepare", func(t *testing.T) {
			utx = f.Prepare(t, "anchor_prepare_update_record_status", map[string]any{
				"from":        f.Address,
				"registry_id": f.RegistryID,
				"record_id":   f.RecordID,
				"index":       f.RecordIndex,
				"status":      "Superseded",
			})
			e2e.AssertUnsignedTxShape(t, f, utx)
		})
		if utx == nil {
			t.Fatal("prepare stage did not produce an unsigned transaction")
		}
		t.Run("sign_broadcast_confirm", func(t *testing.T) {
			rcpt := f.SignBroadcastConfirm(t, utx)
			e2e.AssertReceiptContract(t, rcpt)
		})
		t.Run("read_back", func(t *testing.T) {
			var out e2e.RecordsResponse
			f.CallOK(t, "anchor_get_records", map[string]any{
				"registry_id": f.RegistryID,
				"checksum":    f.Checksum,
			}, &out)
			var found *e2e.Record
			for i := range out.Records {
				if out.Records[i].RecordID == f.RecordID {
					found = &out.Records[i]
					break
				}
			}
			if found == nil {
				t.Fatalf("lifecycle read-back did not find record_id %d", f.RecordID)
			}
			if found.Status != "Superseded" {
				t.Errorf("status = %q, want Superseded", found.Status)
			}
			if found.URI != f.URI {
				t.Errorf("uri = %q, want %q", found.URI, f.URI)
			}
			t.Logf("record %d status is %s after update", found.RecordID, found.Status)
		})
	})

	t.Run("observe", func(t *testing.T) {
		if f.RecordTxHash == "" || f.RecordBlock == 0 {
			t.Fatal("record stage did not capture the add_record receipt")
		}

		var tx e2e.TransactionResponse
		f.CallOK(t, "evm_get_transaction", map[string]any{"tx_hash": f.RecordTxHash}, &tx)
		if !strings.EqualFold(tx.Hash, f.RecordTxHash) {
			t.Errorf("hash = %s, want %s", tx.Hash, f.RecordTxHash)
		}
		if !strings.EqualFold(tx.From, f.Address) {
			t.Errorf("from = %s, want %s", tx.From, f.Address)
		}
		if tx.To == nil || !strings.EqualFold(*tx.To, f.AnchorAddress) {
			t.Errorf("to = %v, want precompile %s", tx.To, f.AnchorAddress)
		}
		if tx.BlockNumber == nil || *tx.BlockNumber != f.RecordBlock {
			t.Errorf("block_number = %v, want %d", tx.BlockNumber, f.RecordBlock)
		}

		var logs e2e.LogsResponse
		f.CallOK(t, "evm_get_logs", map[string]any{
			"address":    f.AnchorAddress,
			"from_block": float64(f.RecordBlock),
			"to_block":   float64(f.RecordBlock),
		}, &logs)
		if logs.Count == 0 || len(logs.Logs) == 0 {
			t.Error("evm_get_logs returned no logs for the add_record block")
		} else {
			foundTx := false
			for i := range logs.Logs {
				if strings.EqualFold(logs.Logs[i].TxHash, f.RecordTxHash) {
					foundTx = true
					break
				}
			}
			if !foundTx {
				t.Errorf("evm_get_logs in block %d did not include tx %s",
					f.RecordBlock, f.RecordTxHash)
			}
		}

		var code e2e.CodeResponse
		f.CallOK(t, "evm_get_code", map[string]any{"address": f.AnchorAddress}, &code)
		if !strings.EqualFold(code.Address, f.AnchorAddress) {
			t.Errorf("evm_get_code.address = %s, want precompile %s", code.Address, f.AnchorAddress)
		}

		latestID, peekedName := f.PeekLatestRegistryID(t)
		if latestID == 0 {
			t.Fatal("evm_call_contract reverse peek returned registry id 0")
		}
		t.Logf("observed add_record tx %s in block %d; latest registry id=%d name=%q",
			f.RecordTxHash, f.RecordBlock, latestID, peekedName)
	})
}
