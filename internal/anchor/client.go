package anchor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	apperrors "github.com/inveniam/nvnm-mcp-server/internal/errors"
	"github.com/inveniam/nvnm-mcp-server/internal/evm"
)

// PrecompileAddress is the fixed address of the anchoring precompile on the Inveniam chain.
const PrecompileAddress = "0x0000000000000000000000000000000000000A00"

// Client defines the interface for interacting with the anchoring precompile.
type Client interface {
	Info() PrecompileInfo
	Available() bool

	// Read methods
	GetRegistry(ctx context.Context, req GetRegistryRequest) (*Registry, error)
	GetRegistries(ctx context.Context, req GetRegistriesRequest) (*GetRegistriesResponse, error)
	GetRecords(ctx context.Context, req GetRecordsRequest) (*GetRecordsResponse, error)

	// Write preparation (prepare-sign-submit pattern)
	PrepareAddRegistry(ctx context.Context, req PrepareAddRegistryRequest) (*UnsignedTransaction, error)
	PrepareAddRecord(ctx context.Context, req PrepareAddRecordRequest) (*UnsignedTransaction, error)
	PrepareGrantRole(ctx context.Context, req PrepareGrantRoleRequest) (*UnsignedTransaction, error)
}

type client struct {
	evmClient evm.Client
	address   common.Address
	chainID   int64
	parsedABI *abi.ABI
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
	addr := common.HexToAddress(address)
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
				slog.String("address", addr.Hex()),
				slog.Int("methods", len(parsed.Methods)),
			)
		}
	} else {
		logger.Info(
			"no ANCHOR_ABI_PATH set; anchor query tools will be registered but return errors until ABI is provided",
			slog.String("address", addr.Hex()),
		)
	}

	return c
}

func (c *client) Info() PrecompileInfo {
	info := PrecompileInfo{
		Address:   c.address.Hex(),
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
		return nil, err
	}

	results, err := c.parsedABI.Unpack("registries", output)
	if err != nil {
		return nil, fmt.Errorf("unpack registries response: %w", err)
	}

	registries, err := decodeRegistries(results)
	if err != nil {
		return nil, err
	}

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
		return nil, err
	}

	results, err := c.parsedABI.Unpack("registries", output)
	if err != nil {
		return nil, fmt.Errorf("unpack registries response: %w", err)
	}

	registries, err := decodeRegistries(results)
	if err != nil {
		return nil, err
	}

	resp := &GetRegistriesResponse{
		Registries: registries,
	}
	if len(results) > 1 {
		if pg, ok := decodePagination(results[1]); ok {
			resp.Pagination = pg
		}
	}

	return resp, nil
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

	results, err := c.parsedABI.Unpack("records", output)
	if err != nil {
		return nil, fmt.Errorf("unpack records response: %w", err)
	}

	records, err := decodeRecords(results)
	if err != nil {
		return nil, err
	}

	resp := &GetRecordsResponse{
		Records: records,
	}
	if len(results) > 1 {
		if pg, ok := decodePagination(results[1]); ok {
			resp.Pagination = pg
		}
	}

	return resp, nil
}

// callPrecompile packs and executes an eth_call against the precompile.
func (c *client) callPrecompile(
	ctx context.Context,
	method string,
	args ...interface{},
) ([]byte, error) {
	input, err := c.parsedABI.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to pack %s call: %w", method, err)
	}

	msg := ethereum.CallMsg{
		To:   &c.address,
		Data: input,
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
			c.address.Hex(), apperrors.ErrAnchorABIMissing,
		)
	}
	return nil
}

// decodeRegistries extracts []Registry from the ABI-unpacked result slice.
// go-ethereum returns anonymous structs; we must use the exact anonymous type
// in the assertion (named types are distinct even if structurally identical).
func decodeRegistries(results []interface{}) ([]Registry, error) {
	if len(results) == 0 {
		return nil, nil
	}

	type row = struct { //nolint:revive // field names must match ABI component names
		Id          uint64 `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Creator     string `json:"creator"`
		CreatedAt   string `json:"createdAt"`
		Metadata    string `json:"metadata"`
	}

	rawSlice, ok := results[0].([]row)
	if !ok {
		return nil, fmt.Errorf(
			"unexpected registries result type %T: %w",
			results[0], apperrors.ErrPrecompileCall,
		)
	}

	registries := make([]Registry, len(rawSlice))
	for i := range rawSlice {
		registries[i] = Registry{
			ID:          rawSlice[i].Id,
			Name:        rawSlice[i].Name,
			Description: rawSlice[i].Description,
			Creator:     rawSlice[i].Creator,
			CreatedAt:   rawSlice[i].CreatedAt,
			Metadata:    rawSlice[i].Metadata,
		}
	}
	return registries, nil
}

// decodeRecords extracts []Record from the ABI-unpacked result slice.
func decodeRecords(results []interface{}) ([]Record, error) {
	if len(results) == 0 {
		return nil, nil
	}

	type row = struct {
		Registry     string `json:"registry"`
		Uri          string `json:"uri"` //nolint:revive // must match ABI field name
		Checksum     string `json:"checksum"`
		ChecksumAlgo string `json:"checksumAlgo"`
		Metadata     string `json:"metadata"`
		Timestamp    string `json:"timestamp"`
		Status       string `json:"status"`
		RecordId     uint64 `json:"recordId"` //nolint:revive // must match ABI field name
		Index        uint64 `json:"index"`
		IsLatest     bool   `json:"isLatest"`
	}

	rawSlice, ok := results[0].([]row)
	if !ok {
		return nil, fmt.Errorf(
			"unexpected records result type %T: %w",
			results[0], apperrors.ErrPrecompileCall,
		)
	}

	records := make([]Record, len(rawSlice))
	for i := range rawSlice {
		records[i] = Record{
			Registry:     rawSlice[i].Registry,
			RecordID:     rawSlice[i].RecordId,
			Index:        rawSlice[i].Index,
			Checksum:     rawSlice[i].Checksum,
			ChecksumAlgo: rawSlice[i].ChecksumAlgo,
			URI:          rawSlice[i].Uri,
			Status:       rawSlice[i].Status,
			IsLatest:     rawSlice[i].IsLatest,
			Timestamp:    rawSlice[i].Timestamp,
			Metadata:     rawSlice[i].Metadata,
		}
	}
	return records, nil
}

// decodePagination extracts pagination info from an ABI-unpacked result element.
func decodePagination(raw interface{}) (*PageResponse, bool) {
	type pg = struct {
		NextKey []byte `json:"nextKey"`
		Total   uint64 `json:"total"`
	}
	p, ok := raw.(pg)
	if !ok {
		return nil, false
	}
	return &PageResponse{Total: p.Total}, true
}

// loadABI reads a JSON ABI file and parses it. Accepts either a raw ABI array
// or a wrapper object with an "abi" key (as used by Truffle/Hardhat artifacts).
func loadABI(path string) (*abi.ABI, error) {
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

	parsed, err := abi.JSON(strings.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parse ABI from %s: %w", path, err)
	}

	if len(parsed.Methods) == 0 {
		return nil, fmt.Errorf("ABI at %s: %w", path, apperrors.ErrAnchorABIMissing)
	}

	return &parsed, nil
}
