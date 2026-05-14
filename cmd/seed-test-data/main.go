package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	defitypes "github.com/defiweb/go-eth/types"
	defiwallet "github.com/defiweb/go-eth/wallet"

	"github.com/inveniam/nvnm-mcp-server/internal/anchor"
	"github.com/inveniam/nvnm-mcp-server/internal/evm"
	"github.com/inveniam/nvnm-mcp-server/internal/logging"
)

const (
	rpcURL       = "https://evm.testnet.nvnmchain.io"
	abiPath      = "abi/anchoring.json"
	chainID      = 787111
	credsPath    = ".chain_credentials.txt" //nolint:gosec // not credentials, just a file path constant
	registryName = "mcp-test-data"
	registryDesc = "Test registry seeded by cmd/seed-test-data for MCP server validation"
)

var (
	errTxReverted       = errors.New("transaction reverted")
	errMissingCredField = errors.New("file missing Address or PrivateKey fields")
	errReceiptTimeout   = errors.New("timed out waiting for transaction receipt")
	errNegativeChainID  = errors.New("negative chain ID")
)

type testRecord struct {
	URI          string
	Checksum     string
	ChecksumAlgo string
	Metadata     string
}

var records = []testRecord{
	{
		URI:          "https://docs.example.com/reports/q1-2026.pdf",
		Checksum:     "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2",
		ChecksumAlgo: "sha256",
		Metadata:     `{"type":"quarterly_report","quarter":"Q1-2026"}`,
	},
	{
		URI:          "ipfs://QmFakeHash123456789abcdef0123456789abcdef",
		Checksum:     "d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5",
		ChecksumAlgo: "sha256",
		Metadata:     `{"type":"audit_certificate","auditor":"TestAudit LLC"}`,
	},
	{
		URI:          "https://docs.example.com/contracts/nda-001.pdf",
		Checksum:     "789abcde0123456789abcdef0123456789abcdef0123456789abcdef01234567",
		ChecksumAlgo: "sha256",
		Metadata:     `{"type":"legal_contract","classification":"confidential"}`,
	},
}

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

	creds, err := loadCredentials(credsPath)
	if err != nil {
		return fmt.Errorf("load credentials: %w", err)
	}
	fmt.Printf("Sender: %s\n\n", creds.address)

	evmClient, err := evm.NewClient(ctx, rpcURL, 15*time.Second)
	if err != nil {
		return fmt.Errorf("connect to RPC: %w", err)
	}
	defer evmClient.Close()

	chainInfo, err := evmClient.GetChainInfo(ctx)
	if err != nil {
		return fmt.Errorf("chain info: %w", err)
	}
	fmt.Printf("Chain ID: %d | Latest block: %d\n\n", chainInfo.ChainID, chainInfo.LatestBlockNumber)

	anchorClient := anchor.NewClient(evmClient, anchor.PrecompileAddress, chainID, abiPath, logger)

	if err := ensureRegistry(ctx, anchorClient, evmClient, creds); err != nil {
		return err
	}

	if err := seedRecords(ctx, anchorClient, evmClient, creds); err != nil {
		return err
	}

	return verifySeededData(ctx, anchorClient)
}

func ensureRegistry(ctx context.Context, ac anchor.Client, ec evm.Client, creds *credentials) error {
	fmt.Printf("=== Creating registry %q ===\n", registryName)
	existing, lookupErr := ac.GetRegistry(ctx, anchor.GetRegistryRequest{Name: strPtr(registryName)})
	if lookupErr == nil && existing != nil {
		fmt.Printf("  already exists: id=%d creator=%s (skipping)\n\n", existing.ID, existing.Creator)
		return nil
	}

	return submitPreparedTx(ctx, ec, func() (*anchor.UnsignedTransaction, error) {
		return ac.PrepareAddRegistry(ctx, anchor.PrepareAddRegistryRequest{
			From:        creds.address,
			Name:        registryName,
			Description: registryDesc,
			Metadata:    `{"seeded_by":"cmd/seed-test-data"}`,
		})
	}, creds, "addRegistry")
}

func seedRecords(ctx context.Context, ac anchor.Client, ec evm.Client, creds *credentials) error {
	for i, rec := range records {
		fmt.Printf("=== Adding record %d/%d ===\n", i+1, len(records))
		fmt.Printf("  URI: %s\n", rec.URI)

		label := fmt.Sprintf("addRecord[%d]", i)
		err := submitPreparedTx(ctx, ec, func() (*anchor.UnsignedTransaction, error) {
			return ac.PrepareAddRecord(ctx, anchor.PrepareAddRecordRequest{
				From:         creds.address,
				Registry:     registryName,
				URI:          rec.URI,
				Checksum:     rec.Checksum,
				ChecksumAlgo: rec.ChecksumAlgo,
				Metadata:     rec.Metadata,
			})
		}, creds, label)
		if err != nil {
			return err
		}
	}
	return nil
}

func submitPreparedTx(
	ctx context.Context,
	ec evm.Client,
	prepare func() (*anchor.UnsignedTransaction, error),
	creds *credentials,
	label string,
) error {
	utx, err := prepare()
	if err != nil {
		return fmt.Errorf("prepare %s: %w", label, err)
	}
	fmt.Printf("  nonce=%d gas=%d gasPrice=%s\n", utx.Nonce, utx.Gas, utx.GasPrice)

	signed, err := signTx(ctx, utx, creds.privateKey)
	if err != nil {
		return fmt.Errorf("sign %s: %w", label, err)
	}

	txHash, err := ec.SendRawTransaction(ctx, signed)
	if err != nil {
		return fmt.Errorf("submit %s: %w", label, err)
	}
	fmt.Printf("  tx: %s\n", txHash)

	receipt, err := waitForReceipt(ctx, ec, txHash, 30*time.Second)
	if err != nil {
		return fmt.Errorf("%s receipt: %w", label, err)
	}
	if receipt.Status != "success" {
		return fmt.Errorf("%s status=%s: %w", label, receipt.Status, errTxReverted)
	}
	fmt.Printf("  mined in block %d (gas used: %d)\n\n", receipt.BlockNumber, receipt.GasUsed)
	return nil
}

func verifySeededData(ctx context.Context, ac anchor.Client) error {
	fmt.Println("=== Verifying ===")
	reg, err := ac.GetRegistry(ctx, anchor.GetRegistryRequest{Name: strPtr(registryName)})
	if err != nil {
		return fmt.Errorf("verify registry: %w", err)
	}
	fmt.Printf("  Registry: id=%d name=%q creator=%s\n", reg.ID, reg.Name, reg.Creator)

	recs, err := ac.GetRecords(ctx, anchor.GetRecordsRequest{
		Registry:   strPtr(registryName),
		Pagination: &anchor.PageRequest{Limit: 10},
	})
	if err != nil {
		return fmt.Errorf("verify records: %w", err)
	}
	fmt.Printf("  Records: %d found\n", len(recs.Records))
	for i := range recs.Records {
		r := &recs.Records[i]
		fmt.Printf("    recordID=%d checksum=%s uri=%s\n", r.RecordID, r.Checksum, r.URI)
	}

	fmt.Println("\nDone. Registry and records are on-chain.")
	return nil
}

type credentials struct {
	address    string
	privateKey *defiwallet.PrivateKey
}

func loadCredentials(path string) (*credentials, error) {
	data, err := os.ReadFile(path) //nolint:gosec // operator-controlled path
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var address, keyHex string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "Address:"); ok {
			address = strings.TrimSpace(after)
		}
		if after, ok := strings.CutPrefix(line, "PrivateKey:"); ok {
			keyHex = strings.TrimSpace(after)
		}
	}
	if address == "" || keyHex == "" {
		return nil, errMissingCredField
	}

	keyHex = strings.TrimPrefix(keyHex, "0x")
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key hex: %w", err)
	}
	priv := defiwallet.NewKeyFromBytes(keyBytes)

	return &credentials{address: address, privateKey: priv}, nil
}

func signTx(ctx context.Context, utx *anchor.UnsignedTransaction, key *defiwallet.PrivateKey) (string, error) {
	rawHex := strings.TrimPrefix(utx.RawTx, "0x")
	txBytes, err := hex.DecodeString(rawHex)
	if err != nil {
		return "", fmt.Errorf("decode raw tx hex: %w", err)
	}

	tx := defitypes.NewTransaction()
	if _, decErr := tx.DecodeRLP(txBytes); decErr != nil {
		return "", fmt.Errorf("unmarshal unsigned tx: %w", decErr)
	}
	if utx.ChainID < 0 {
		return "", fmt.Errorf("chain ID %d: %w", utx.ChainID, errNegativeChainID)
	}
	tx.SetChainID(uint64(utx.ChainID))

	if signErr := key.SignTransaction(ctx, tx); signErr != nil {
		return "", fmt.Errorf("sign tx: %w", signErr)
	}

	signedBytes, err := tx.Raw()
	if err != nil {
		return "", fmt.Errorf("marshal signed tx: %w", err)
	}

	return "0x" + hex.EncodeToString(signedBytes), nil
}

func waitForReceipt(
	ctx context.Context,
	client evm.Client,
	txHash string,
	timeout time.Duration,
) (*evm.NormalizedReceipt, error) {
	hash, err := defitypes.HashFromHex(txHash, defitypes.PadNone)
	if err != nil {
		return nil, fmt.Errorf("invalid tx hash %s: %w", txHash, err)
	}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)

		receipt, rerr := client.TransactionReceipt(ctx, hash)
		if rerr != nil {
			fmt.Printf("  waiting for receipt...\n")
			continue
		}
		return receipt, nil
	}
	return nil, fmt.Errorf("after %s for %s: %w", timeout, txHash, errReceiptTimeout)
}

func strPtr(s string) *string { return &s }
