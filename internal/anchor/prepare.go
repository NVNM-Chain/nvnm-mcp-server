package anchor

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	apperrors "github.com/inveniam/nvnm-mcp-server/internal/errors"
)

const gasEstimateBufferPercent = 20

// PrepareAddRegistry constructs an unsigned addRegistry transaction.
func (c *client) PrepareAddRegistry(
	ctx context.Context,
	req PrepareAddRegistryRequest,
) (*UnsignedTransaction, error) {
	if err := c.requireABI(); err != nil {
		return nil, err
	}
	if req.From == "" {
		return nil, fmt.Errorf("from address is required: %w", apperrors.ErrMissingRequired)
	}
	if req.Name == "" {
		return nil, fmt.Errorf("name is required: %w", apperrors.ErrMissingRequired)
	}

	calldata, err := c.parsedABI.Pack("addRegistry", req.Name, req.Description, req.Metadata)
	if err != nil {
		return nil, fmt.Errorf("pack addRegistry: %w", err)
	}

	return c.buildUnsignedTx(ctx, req.From, calldata)
}

// PrepareAddRecord constructs an unsigned addRecord transaction.
//
//nolint:gocritic // hugeParam: consistent value signature across all Prepare* methods
func (c *client) PrepareAddRecord(
	ctx context.Context,
	req PrepareAddRecordRequest,
) (*UnsignedTransaction, error) {
	if err := c.requireABI(); err != nil {
		return nil, err
	}
	if req.From == "" {
		return nil, fmt.Errorf("from address is required: %w", apperrors.ErrMissingRequired)
	}
	if req.Registry == "" {
		return nil, fmt.Errorf("registry is required: %w", apperrors.ErrMissingRequired)
	}
	if req.Checksum == "" {
		return nil, fmt.Errorf("checksum is required: %w", apperrors.ErrMissingRequired)
	}

	// The ABI expects a single tuple struct for addRecord
	type recordTuple struct {
		Registry     string
		Uri          string //nolint:revive // must match ABI field name
		Checksum     string
		ChecksumAlgo string
		Metadata     string
		Timestamp    string
		Status       string
		RecordId     uint64 //nolint:revive // must match ABI field name
		Index        uint64
		IsLatest     bool
	}

	record := recordTuple{
		Registry:     req.Registry,
		Uri:          req.URI,
		Checksum:     req.Checksum,
		ChecksumAlgo: req.ChecksumAlgo,
		Metadata:     req.Metadata,
		Timestamp:    "",
		Status:       "",
		RecordId:     0,
		Index:        0,
		IsLatest:     false,
	}

	calldata, err := c.parsedABI.Pack("addRecord", record)
	if err != nil {
		return nil, fmt.Errorf("pack addRecord: %w", err)
	}

	return c.buildUnsignedTx(ctx, req.From, calldata)
}

// PrepareGrantRole constructs an unsigned grantRole transaction.
func (c *client) PrepareGrantRole(
	ctx context.Context,
	req PrepareGrantRoleRequest,
) (*UnsignedTransaction, error) {
	if err := c.requireABI(); err != nil {
		return nil, err
	}
	if req.From == "" {
		return nil, fmt.Errorf("from address is required: %w", apperrors.ErrMissingRequired)
	}
	if req.Account == "" {
		return nil, fmt.Errorf("account address is required: %w", apperrors.ErrMissingRequired)
	}
	if req.Role == "" {
		return nil, fmt.Errorf("role is required: %w", apperrors.ErrMissingRequired)
	}

	account := common.HexToAddress(req.Account)
	calldata, err := c.parsedABI.Pack(
		"grantRole", req.RegistryID, req.Checksum, account, req.Role,
	)
	if err != nil {
		return nil, fmt.Errorf("pack grantRole: %w", err)
	}

	return c.buildUnsignedTx(ctx, req.From, calldata)
}

// buildUnsignedTx fetches nonce, gas estimate, and gas price, then constructs
// a complete unsigned EIP-155 transaction ready for signing.
func (c *client) buildUnsignedTx(
	ctx context.Context,
	fromHex string,
	calldata []byte,
) (*UnsignedTransaction, error) {
	from := common.HexToAddress(fromHex)

	nonce, err := c.evmClient.PendingNonceAt(ctx, from)
	if err != nil {
		return nil, fmt.Errorf("get pending nonce: %w", err)
	}

	gasPrice, err := c.evmClient.SuggestGasPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("get gas price: %w", err)
	}

	msg := ethereum.CallMsg{
		From: from,
		To:   &c.address,
		Data: calldata,
	}
	gasEstimate, err := c.evmClient.EstimateGas(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("estimate gas: %w", err)
	}

	gasLimit := applyGasBuffer(gasEstimate)

	chainID := big.NewInt(c.chainID)
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		To:       &c.address,
		Value:    big.NewInt(0),
		Gas:      gasLimit,
		GasPrice: gasPrice,
		Data:     calldata,
	})

	signer := types.NewEIP155Signer(chainID)
	rawBytes := signer.Hash(tx).Bytes()
	_ = rawBytes // signer hash is for signing; we serialize the full tx for the caller

	txBytes, err := tx.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("serialize unsigned transaction: %w", err)
	}

	return &UnsignedTransaction{
		RawTx:    "0x" + hex.EncodeToString(txBytes),
		To:       c.address.Hex(),
		Data:     "0x" + hex.EncodeToString(calldata),
		Nonce:    nonce,
		Gas:      gasLimit,
		GasPrice: gasPrice.String(),
		Value:    "0",
		ChainID:  c.chainID,
	}, nil
}

// applyGasBuffer adds a safety margin to the gas estimate.
func applyGasBuffer(estimate uint64) uint64 {
	buffer := estimate * gasEstimateBufferPercent / 100
	return estimate + buffer
}
