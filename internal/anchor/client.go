// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package anchor

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	defiabi "github.com/defiweb/go-eth/abi"
	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

// PrecompileAddress is the fixed address of the anchoring precompile on the Inveniam chain.
const PrecompileAddress = "0x0000000000000000000000000000000000000A00"

// Client defines the interface for interacting with the anchoring precompile.
type Client interface {
	Info() PrecompileInfo
	Available() bool

	// MethodSelector returns the 0x-prefixed 4-byte ABI selector for the
	// named method (e.g. "grantRole"). ok is false when no ABI is loaded or
	// the method is unknown. Callers must derive selectors through this --
	// the precompile's grantRole takes four arguments, so its selector is
	// NOT the well-known OpenZeppelin grantRole(bytes32,address) value, and
	// a hardcoded constant would silently mis-classify every row.
	MethodSelector(name string) (selector string, ok bool)

	// Read methods
	GetRegistry(ctx context.Context, req GetRegistryRequest) (*Registry, error)
	GetRegistries(ctx context.Context, req GetRegistriesRequest) (*GetRegistriesResponse, error)
	GetRecords(ctx context.Context, req GetRecordsRequest) (*GetRecordsResponse, error)

	// Write preparation (prepare-sign-submit pattern)
	PrepareAddRegistry(ctx context.Context, req PrepareAddRegistryRequest) (*UnsignedTransaction, error)
	PrepareAddRecord(ctx context.Context, req PrepareAddRecordRequest) (*UnsignedTransaction, error)
	PrepareGrantRole(ctx context.Context, req PrepareGrantRoleRequest) (*UnsignedTransaction, error)
}

// abiRegistryRow mirrors the on-chain tuple component layout for the
// `registries` view. Field tags map to ABI component names.
type abiRegistryRow struct {
	ID          uint64 `abi:"id"`
	Name        string `abi:"name"`
	Description string `abi:"description"`
	Creator     string `abi:"creator"`
	CreatedAt   string `abi:"createdAt"`
	Metadata    string `abi:"metadata"`
}

// abiRecordRow mirrors the on-chain tuple component layout for the
// `records` view.
type abiRecordRow struct {
	Registry     string `abi:"registry"`
	URI          string `abi:"uri"`
	Checksum     string `abi:"checksum"`
	ChecksumAlgo string `abi:"checksumAlgo"`
	Metadata     string `abi:"metadata"`
	Timestamp    string `abi:"timestamp"`
	Status       string `abi:"status"`
	RecordID     uint64 `abi:"recordId"`
	Index        uint64 `abi:"index"`
	IsLatest     bool   `abi:"isLatest"`
}

// abiPaginationOutput mirrors the on-chain pagination response tuple.
type abiPaginationOutput struct {
	NextKey []byte `abi:"nextKey"`
	Total   uint64 `abi:"total"`
}

type client struct {
	evmClient evm.Client
	address   defitypes.Address
	chainID   int64
	parsedABI *defiabi.Contract
	logger    *slog.Logger
}

// NewClient creates an anchor client targeting the precompile at the given address.
// If abiPath is empty or the file cannot be loaded, the client is created in
// "unavailable" mode -- Info() and Available() work, but query methods return
// an error explaining that the ABI is needed.
func NewClient(
	evmClient evm.Client,
	address string,
	chainID int64,
	abiPath string,
	logger *slog.Logger,
) Client {
	addr := defitypes.MustAddressFromHex(address)
	c := &client{
		evmClient: evmClient,
		address:   addr,
		chainID:   chainID,
		logger:    logger,
	}

	if abiPath != "" {
		parsed, err := loadABI(abiPath)
		if err != nil {
			logger.Warn(
				"anchor ABI failed to load; anchor tools will return errors until a valid ABI is provided",
				slog.String("path", abiPath),
				slog.String("error", err.Error()),
			)
		} else {
			c.parsedABI = parsed
			logger.Info("anchor ABI loaded",
				slog.String("address", evm.AddressHex(addr)),
				slog.Int("methods", len(parsed.Methods)),
			)
		}
	} else {
		logger.Info(
			"no ANCHOR_ABI_PATH set; anchor query tools will be registered but return errors until ABI is provided",
			slog.String("address", evm.AddressHex(addr)),
		)
	}

	return c
}

func (c *client) Info() PrecompileInfo {
	info := PrecompileInfo{
		Address:   evm.AddressHex(c.address),
		ChainID:   c.chainID,
		ABILoaded: c.parsedABI != nil,
	}
	if c.parsedABI != nil {
		info.MethodCount = len(c.parsedABI.Methods)
	}
	return info
}

func (c *client) Available() bool {
	return c.parsedABI != nil
}

// MethodSelector returns the 0x-prefixed 4-byte ABI selector for name.
func (c *client) MethodSelector(name string) (string, bool) {
	if c.parsedABI == nil {
		return "", false
	}
	m, found := c.parsedABI.Methods[name]
	if !found || m == nil {
		return "", false
	}
	fb := m.FourBytes()
	return "0x" + hex.EncodeToString(fb[:]), true
}

// GetRegistry fetches a single registry by ID or name.
// It calls the "registries" view function with a filter and returns the first match.
func (c *client) GetRegistry(
	ctx context.Context,
	req GetRegistryRequest,
) (*Registry, error) {
	if err := c.requireABI(); err != nil {
		return nil, err
	}

	var registryID uint64
	var name string
	if req.ID != nil {
		registryID = *req.ID
	}
	if req.Name != nil {
		name = *req.Name
	}

	pagination := abiPaginationInput{
		Key:        []byte{},
		Offset:     0,
		Limit:      1,
		CountTotal: false,
		Reverse:    false,
	}

	output, err := c.callPrecompile(ctx, "registries", registryID, name, pagination)
	if err != nil {
		// The precompile returns a raw "collections: not found" RPC error for
		// an unknown id, which would otherwise leak the internal Cosmos proto
		// type path to the client. Map it to the clean ErrRegistryNotFound
		// sentinel. String matching is unavoidable here: the node signals
		// not-found only through the error text, not a typed or coded error.
		if strings.Contains(err.Error(), "not found") {
			return nil, fmt.Errorf("registry lookup failed: %w", apperrors.ErrRegistryNotFound)
		}
		return nil, err
	}

	var rows []abiRegistryRow
	var page abiPaginationOutput
	if err := c.parsedABI.Methods["registries"].DecodeValues(output, &rows, &page); err != nil {
		return nil, fmt.Errorf("unpack registries response: %w", err)
	}

	registries := toRegistries(rows)
	if len(registries) == 0 {
		return nil, fmt.Errorf("registry lookup returned no results: %w", apperrors.ErrRegistryNotFound)
	}
	return &registries[0], nil
}

// GetRegistries fetches a paginated list of registries.
func (c *client) GetRegistries(
	ctx context.Context,
	req GetRegistriesRequest,
) (*GetRegistriesResponse, error) {
	if err := c.requireABI(); err != nil {
		return nil, err
	}

	var registryID uint64
	var name string
	if req.RegistryID != nil {
		registryID = *req.RegistryID
	}
	if req.Name != nil {
		name = *req.Name
	}

	pagination := abiPaginationInput{
		Key:        []byte{},
		Offset:     0,
		Limit:      100,
		CountTotal: true,
		Reverse:    false,
	}
	if req.Pagination != nil {
		if req.Pagination.Offset > 0 {
			pagination.Offset = req.Pagination.Offset
		}
		if req.Pagination.Limit > 0 {
			pagination.Limit = req.Pagination.Limit
		}
	}

	output, err := c.callPrecompile(ctx, "registries", registryID, name, pagination)
	if err != nil {
		// The precompile returns a raw "collections: not found" RPC error for
		// an unknown id, which would otherwise leak the internal Cosmos proto
		// type path to the client. Map it to the clean ErrRegistryNotFound
		// sentinel. String matching is unavoidable here: the node signals
		// not-found only through the error text, not a typed or coded error.
		if strings.Contains(err.Error(), "not found") {
			return nil, fmt.Errorf("registry lookup failed: %w", apperrors.ErrRegistryNotFound)
		}
		return nil, err
	}

	var rows []abiRegistryRow
	var page abiPaginationOutput
	if err := c.parsedABI.Methods["registries"].DecodeValues(output, &rows, &page); err != nil {
		return nil, fmt.Errorf("unpack registries response: %w", err)
	}

	return &GetRegistriesResponse{
		Registries: toRegistries(rows),
		Pagination: &PageResponse{Total: page.Total},
	}, nil
}

// GetRecords queries anchor records with flexible filtering.
func (c *client) GetRecords(
	ctx context.Context,
	req GetRecordsRequest,
) (*GetRecordsResponse, error) {
	if err := c.requireABI(); err != nil {
		return nil, err
	}

	var registry, checksum string
	var recordID, index uint64
	if req.Registry != nil {
		registry = *req.Registry
	}
	if req.Checksum != nil {
		checksum = *req.Checksum
	}
	if req.RecordID != nil {
		recordID = *req.RecordID
	}
	if req.Index != nil {
		index = *req.Index
	}

	// The precompile's records query is keyed by registry NAME, not numeric
	// id. registry_id is a caller convenience, so resolve it to a name here.
	// An explicit name wins; the id is only resolved when no name was given.
	// Without this, a registry_id filter was silently ignored and the query
	// returned an empty set (the registry_id-based modes advertised by the
	// anchor_get_records tool never worked); a bad id now fails loud instead.
	if registry == "" && req.RegistryID != nil {
		reg, regErr := c.GetRegistry(ctx, GetRegistryRequest{ID: req.RegistryID})
		if regErr != nil {
			return nil, fmt.Errorf("resolve registry_id %d: %w", *req.RegistryID, regErr)
		}
		registry = reg.Name
	}

	pagination := abiPaginationInput{
		Key:        []byte{},
		Offset:     0,
		Limit:      100,
		CountTotal: true,
		Reverse:    false,
	}
	if req.Pagination != nil {
		if req.Pagination.Offset > 0 {
			pagination.Offset = req.Pagination.Offset
		}
		if req.Pagination.Limit > 0 {
			pagination.Limit = req.Pagination.Limit
		}
	}

	output, err := c.callPrecompile(
		ctx, "records", registry, checksum, recordID, index, pagination,
	)
	if err != nil {
		return nil, err
	}

	var rows []abiRecordRow
	var page abiPaginationOutput
	if err := c.parsedABI.Methods["records"].DecodeValues(output, &rows, &page); err != nil {
		return nil, fmt.Errorf("unpack records response: %w", err)
	}

	return &GetRecordsResponse{
		Records:    toRecords(rows),
		Pagination: &PageResponse{Total: page.Total},
	}, nil
}

// callPrecompile encodes and executes an eth_call against the precompile.
// args is passed directly to defiweb's Method.EncodeArgs.
func (c *client) callPrecompile(
	ctx context.Context,
	method string,
	args ...interface{},
) ([]byte, error) {
	m, ok := c.parsedABI.Methods[method]
	if !ok {
		return nil, fmt.Errorf("method %q: %w", method, apperrors.ErrAnchorABIMethodMissing)
	}
	input, err := m.EncodeArgs(args...)
	if err != nil {
		return nil, fmt.Errorf("failed to pack %s call: %w", method, err)
	}

	to := c.address
	msg := defitypes.Call{
		To:    &to,
		Input: input,
	}

	output, err := c.evmClient.CallContract(ctx, msg, nil)
	if err != nil {
		return nil, fmt.Errorf("%s call failed: %w", method, err)
	}
	return output, nil
}

func (c *client) requireABI() error {
	if c.parsedABI == nil {
		return fmt.Errorf(
			"set ANCHOR_ABI_PATH to a valid ABI JSON file (precompile address: %s): %w",
			evm.AddressHex(c.address), apperrors.ErrAnchorABIMissing,
		)
	}
	return nil
}

func toRegistries(rows []abiRegistryRow) []Registry {
	out := make([]Registry, len(rows))
	for i := range rows {
		out[i] = Registry{
			ID:          rows[i].ID,
			Name:        rows[i].Name,
			Description: rows[i].Description,
			Creator:     rows[i].Creator,
			CreatedAt:   rows[i].CreatedAt,
			Metadata:    rows[i].Metadata,
		}
	}
	return out
}

func toRecords(rows []abiRecordRow) []Record {
	out := make([]Record, len(rows))
	for i := range rows {
		out[i] = Record{
			Registry:     rows[i].Registry,
			RecordID:     rows[i].RecordID,
			Index:        rows[i].Index,
			Checksum:     rows[i].Checksum,
			ChecksumAlgo: rows[i].ChecksumAlgo,
			URI:          rows[i].URI,
			Status:       rows[i].Status,
			IsLatest:     rows[i].IsLatest,
			Timestamp:    rows[i].Timestamp,
			Metadata:     rows[i].Metadata,
		}
	}
	return out
}

// loadABI reads a JSON ABI file and parses it. Accepts either a raw ABI array
// or a wrapper object with an "abi" key (as used by Truffle/Hardhat artifacts).
func loadABI(path string) (*defiabi.Contract, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is operator-controlled config, not user input
	if err != nil {
		return nil, fmt.Errorf("read ABI file %s: %w", path, err)
	}

	raw := strings.TrimSpace(string(data))

	if strings.HasPrefix(raw, "{") {
		var wrapper struct {
			ABI json.RawMessage `json:"abi"`
		}
		if unmarshalErr := json.Unmarshal([]byte(raw), &wrapper); unmarshalErr == nil && len(wrapper.ABI) > 0 {
			raw = string(wrapper.ABI)
		}
	}

	parsed, err := defiabi.ParseJSON([]byte(raw))
	if err != nil {
		return nil, fmt.Errorf("parse ABI from %s: %w", path, err)
	}

	if len(parsed.Methods) == 0 {
		return nil, fmt.Errorf("ABI from %s: %w", path, apperrors.ErrAnchorABIEmpty)
	}

	return parsed, nil
}
