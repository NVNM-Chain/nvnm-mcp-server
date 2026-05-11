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

// defaultPriorityFeeWei is the fallback miner tip when SuggestGasTipCap
// returns an error or zero. Set to 1 gwei -- low enough to not overpay,
// high enough to be non-trivially above zero on most EVM chains. The
// chain's actual minimum is enforced by the broadcast step, not here.
var defaultPriorityFeeWei = big.NewInt(1_000_000_000)

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

	return c.buildUnsignedTx(ctx, req.From, calldata, req.PreferLegacy)
}

// PrepareAddRecord constructs an unsigned addRecord transaction.
//
//nolint:gocritic // hugeParam: value-pass kept for signature symmetry across all Prepare* methods
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

	status := req.Status
	if status == "" {
		status = "Active"
	}

	record := recordTuple{
		Registry:     req.Registry,
		Uri:          req.URI,
		Checksum:     req.Checksum,
		ChecksumAlgo: req.ChecksumAlgo,
		Metadata:     req.Metadata,
		Timestamp:    "",
		Status:       status,
		RecordId:     0,
		Index:        0,
		IsLatest:     false,
	}

	calldata, err := c.parsedABI.Pack("addRecord", record)
	if err != nil {
		return nil, fmt.Errorf("pack addRecord: %w", err)
	}

	return c.buildUnsignedTx(ctx, req.From, calldata, req.PreferLegacy)
}

// PrepareGrantRole constructs an unsigned grantRole transaction.
//
//nolint:gocritic // hugeParam: value-pass kept for signature symmetry across all Prepare* methods
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

	if !common.IsHexAddress(req.Account) {
		return nil, fmt.Errorf("account %q: %w", req.Account, apperrors.ErrInvalidAddress)
	}
	account := common.HexToAddress(req.Account)
	calldata, err := c.parsedABI.Pack(
		"grantRole", req.RegistryID, req.Checksum, account, req.Role,
	)
	if err != nil {
		return nil, fmt.Errorf("pack grantRole: %w", err)
	}

	return c.buildUnsignedTx(ctx, req.From, calldata, req.PreferLegacy)
}

// buildUnsignedTx fetches nonce, gas estimate, and fee data, then
// constructs a complete unsigned transaction ready for signing. The
// preferLegacy flag selects between type-0 (LegacyTx) and type-2
// (DynamicFeeTx / EIP-1559). Type 2 is the default; preferLegacy=true
// opts back into type 0 for signers that cannot produce type-2
// signatures.
func (c *client) buildUnsignedTx(
	ctx context.Context,
	fromHex string,
	calldata []byte,
	preferLegacy bool,
) (*UnsignedTransaction, error) {
	if !common.IsHexAddress(fromHex) {
		return nil, fmt.Errorf("from %q: %w", fromHex, apperrors.ErrInvalidAddress)
	}
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

	dataHex := "0x" + hex.EncodeToString(calldata)
	toHex := c.address.Hex()
	fromChecksummed := common.HexToAddress(fromHex).Hex()

	if preferLegacy {
		return c.buildLegacyUnsignedTx(
			nonce, gasLimit, gasPrice, calldata, dataHex, toHex, fromChecksummed,
		)
	}
	return c.buildDynamicFeeUnsignedTx(
		ctx, nonce, gasLimit, gasPrice, calldata, dataHex, toHex, fromChecksummed,
	)
}

// buildLegacyUnsignedTx constructs a type-0 LegacyTx. Used when the
// caller opts in via PreferLegacy, typically because their signer
// cannot produce type-2 signatures.
func (c *client) buildLegacyUnsignedTx(
	nonce, gasLimit uint64,
	gasPrice *big.Int,
	calldata []byte,
	dataHex, toHex, fromChecksummed string,
) (*UnsignedTransaction, error) {
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		To:       &c.address,
		Value:    big.NewInt(0),
		Gas:      gasLimit,
		GasPrice: gasPrice,
		Data:     calldata,
	})
	txBytes, err := tx.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("serialize unsigned transaction: %w", err)
	}
	return &UnsignedTransaction{
		RawTx:    "0x" + hex.EncodeToString(txBytes),
		Type:     0,
		To:       toHex,
		Data:     dataHex,
		Nonce:    nonce,
		Gas:      gasLimit,
		GasPrice: gasPrice.String(),
		Value:    "0",
		ChainID:  c.chainID,
		WalletTxRequest: &WalletTransactionRequest{
			From:     fromChecksummed,
			To:       toHex,
			Data:     dataHex,
			Value:    "0x0",
			ChainID:  "0x" + big.NewInt(c.chainID).Text(16),
			Gas:      "0x" + new(big.Int).SetUint64(gasLimit).Text(16),
			GasPrice: "0x" + gasPrice.Text(16),
		},
	}, nil
}

// buildDynamicFeeUnsignedTx constructs a type-2 DynamicFeeTx (EIP-1559).
// Default since Phase 8.4. Fetches the miner tip via SuggestGasTipCap;
// falls back to defaultPriorityFeeWei when the chain returns zero or
// errors. MaxFeePerGas is computed as 2x SuggestGasPrice to give
// headroom against base-fee inflation.
//
// GasPrice in the response equals MaxFeePerGas so a legacy signer that
// only knows about GasPrice still has a usable value to sign with.
func (c *client) buildDynamicFeeUnsignedTx(
	ctx context.Context,
	nonce, gasLimit uint64,
	gasPrice *big.Int,
	calldata []byte,
	dataHex, toHex, fromChecksummed string,
) (*UnsignedTransaction, error) {
	tipCap, err := c.evmClient.SuggestGasTipCap(ctx)
	if err != nil || tipCap == nil || tipCap.Sign() <= 0 {
		// Fall back rather than fail: a chain that doesn't implement
		// eth_maxPriorityFeePerGas can still sign + broadcast type-2
		// txs as long as the values we pick are above its minimum.
		tipCap = new(big.Int).Set(defaultPriorityFeeWei)
	}
	maxFee := new(big.Int).Mul(gasPrice, big.NewInt(2))
	if maxFee.Cmp(tipCap) < 0 {
		// Pathological: SuggestGasPrice < tip. Ensure maxFee >= tipCap
		// so the signed tx is well-formed.
		maxFee = new(big.Int).Set(tipCap)
	}

	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(c.chainID),
		Nonce:     nonce,
		To:        &c.address,
		Value:     big.NewInt(0),
		Gas:       gasLimit,
		GasTipCap: tipCap,
		GasFeeCap: maxFee,
		Data:      calldata,
	})
	txBytes, err := tx.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("serialize unsigned transaction: %w", err)
	}

	return &UnsignedTransaction{
		RawTx:                "0x" + hex.EncodeToString(txBytes),
		Type:                 2,
		To:                   toHex,
		Data:                 dataHex,
		Nonce:                nonce,
		Gas:                  gasLimit,
		GasPrice:             maxFee.String(), // dual-populate for legacy signers
		MaxFeePerGas:         maxFee.String(),
		MaxPriorityFeePerGas: tipCap.String(),
		Value:                "0",
		ChainID:              c.chainID,
		WalletTxRequest: &WalletTransactionRequest{
			From:                 fromChecksummed,
			To:                   toHex,
			Data:                 dataHex,
			Value:                "0x0",
			ChainID:              "0x" + big.NewInt(c.chainID).Text(16),
			Gas:                  "0x" + new(big.Int).SetUint64(gasLimit).Text(16),
			MaxFeePerGas:         "0x" + maxFee.Text(16),
			MaxPriorityFeePerGas: "0x" + tipCap.Text(16),
		},
	}, nil
}

// applyGasBuffer adds a safety margin to the gas estimate.
func applyGasBuffer(estimate uint64) uint64 {
	buffer := estimate * gasEstimateBufferPercent / 100
	return estimate + buffer
}
