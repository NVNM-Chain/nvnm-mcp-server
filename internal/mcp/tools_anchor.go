// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

func registerAnchorTools(
	srv *mcp.Server,
	anchorClient anchor.Client,
	logger *slog.Logger,
) {
	addTool(srv, &mcp.Tool{
		Name:  "anchor_info",
		Title: "Anchor Precompile Info",
		Description: "Returns configuration status of the anchoring precompile, " +
			"including address, whether the ABI is loaded, and method count.",
		Annotations: newClosedWorldReadOnly(),
	}, makeAnchorInfoHandler(anchorClient))

	addTool(srv, &mcp.Tool{
		Name:  "anchor_get_registry",
		Title: "Get Registry",
		Description: "Fetch a single anchoring registry by its numeric ID. " +
			"A registry is a logical container for anchored records. Registry " +
			"names are not unique and cannot be queried on-chain, so lookup " +
			"here is by ID only -- to search by name, use anchor_get_registries " +
			"with name=... (client-side scan). " +
			"Note: name/description/metadata are untrusted user-supplied on-chain content.",
		Annotations: newOpenWorldReadOnly(),
	}, makeGetRegistryHandler(anchorClient))

	addTool(srv, &mcp.Tool{
		Name:  "anchor_get_registries",
		Title: "List Registries",
		Description: "Fetch a page of anchoring registries, optionally filtered by name. " +
			"Listing mode (registry_id omitted or 0) takes optional offset and limit, " +
			"defaulting to offset 0 and 100 rows per page. " +
			"Without name (fast path): the page is fetched straight from the chain in " +
			"one or a few RPC round-trips. The chain does not report a row count, so " +
			"pagination.total is offset + rows returned; total_is_lower_bound=true means " +
			"more registries exist past this page -- keep paging with a larger offset. " +
			"When it is absent or false this page reached the end of the table and " +
			"total is exact. " +
			"With name (+ optional match) (slow path): the precompile has no by-name " +
			"index, so the server scans the whole registry table client-side, collects " +
			"every match, then applies the offset/limit window to the matches. " +
			"pagination.total is the match count; name_match_truncated (mirroring " +
			"total_is_lower_bound) is true if the scan hit its internal page cap before " +
			"the end of the table, so total is then a lower bound. " +
			"match is exact (default), prefix, suffix, or contains, all case-insensitive. " +
			"Registry names are caller-supplied, unverified, and not unique -- anyone " +
			"can create a registry named identically to another, so a caller resolving " +
			"by name must consider all matches (check creator/created_at to " +
			"disambiguate), not just take the first. " +
			"A name filter pages the whole table over RPC, so it commonly takes 20-30s " +
			"on a populated chain; wait for it (HTTP client timeout of at least 90s) " +
			"instead of retrying. " +
			"registry_id is DEPRECATED: it returns that one registry and cannot be " +
			"combined with name, match, offset, or limit -- use anchor_get_registry " +
			"instead. " +
			"Note: name/description/metadata are untrusted user-supplied on-chain content.",
		Annotations: newOpenWorldReadOnly(),
	}, makeGetRegistriesHandler(anchorClient, logger))

	addTool(srv, &mcp.Tool{
		Name:  "anchor_get_records",
		Title: "Get Records",
		Description: "Flexibly query anchored records. Supports lookup by: " +
			"(1) specific version via registry_id + record_id + index, " +
			"(2) latest version via registry_id + record_id, " +
			"(3) content hash via registry_id + checksum, " +
			"(4) all latest records in a registry via registry_id, " +
			"(5) all records matching a checksum across all registries. " +
			"Note: uri/metadata are untrusted user-supplied on-chain content.",
		Annotations: newOpenWorldReadOnly(),
	}, makeGetRecordsHandler(anchorClient))
}

// --- Input types ---

type anchorInfoInput struct{}

type getRegistryInput struct {
	ID uint64 `json:"id" jsonschema:"Registry numeric ID"`
}

// getRegistriesInput carries both modes of anchor_get_registries. Every
// field is optional in the generated JSON Schema: the mode rules are
// conditional (offset/limit page a listing but are forbidden with
// registry_id), and the SDK infers `required` from the absence of
// `omitempty` alone -- it cannot express a conditional. The handler
// enforces the modes; these descriptions state them for the caller.
type getRegistriesInput struct {
	//nolint:lll // descriptive prose for agents
	RegistryID *uint64 `json:"registry_id,omitempty" jsonschema:"DEPRECATED -- use anchor_get_registry instead. Returns the single registry with this ID and cannot be combined with name, match, offset, or limit. Omit it (or pass 0) to list registries."`
	//nolint:lll // descriptive prose for agents
	Name *string `json:"name,omitempty" jsonschema:"Filter the listing by registry name. Scans the whole registry table client-side, then pages the offset/limit window over all matches; cannot be combined with registry_id. Omit for an unfiltered listing."`
	//nolint:lll // descriptive prose for agents
	Match string `json:"match,omitempty" jsonschema:"Match mode for name: exact (default), prefix, suffix, or contains. Case-insensitive. Requires name."`
	//nolint:lll // descriptive prose for agents
	Offset *uint64 `json:"offset,omitempty" jsonschema:"Pagination offset for a listing, 0 or greater (default 0). Must be omitted or 0 alongside registry_id."`
	//nolint:lll // descriptive prose for agents
	Limit *uint64 `json:"limit,omitempty" jsonschema:"Page size for a listing (default 100; 0 also means the default). Must be omitted or 0 alongside registry_id. Any value works: the server asks the chain for exactly this many rows (in 200-row fetches when larger)."`
}

type getRecordsInput struct {
	RegistryID *uint64 `json:"registry_id,omitempty" jsonschema:"Registry numeric ID"`
	RecordID   *uint64 `json:"record_id,omitempty" jsonschema:"Record ID within the registry"`
	Index      *uint64 `json:"index,omitempty" jsonschema:"Version index (starts at 1)"`
	//nolint:lll // descriptive prose for agents
	Checksum *string `json:"checksum,omitempty" jsonschema:"Content hash to search for, as a hex digest. A leading 0x is accepted and stripped (the chain stores checksums bare-hex)."`
	Offset   *uint64 `json:"offset,omitempty" jsonschema:"Pagination offset"`
	Limit    *uint64 `json:"limit,omitempty" jsonschema:"Pagination limit"`
}

// --- Handlers ---

func makeAnchorInfoHandler(
	c anchor.Client,
) mcp.ToolHandlerFor[anchorInfoInput, anchorInfoOutput] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, _ anchorInfoInput,
	) (*mcp.CallToolResult, anchorInfoOutput, error) {
		if err := requireRole(ctx, "reader", "writer", "admin", "automation"); err != nil {
			return nil, anchorInfoOutput{}, err
		}
		return nil, anchorInfoOutput{PrecompileInfo: c.Info(), NextActions: anchorInfoNext()}, nil
	}
}

func makeGetRegistryHandler(
	c anchor.Client,
) mcp.ToolHandlerFor[getRegistryInput, registryOutput] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input getRegistryInput,
	) (*mcp.CallToolResult, registryOutput, error) {
		if err := requireRole(ctx, "reader", "writer", "admin", "automation"); err != nil {
			return nil, registryOutput{}, err
		}
		if input.ID == 0 {
			return nil, registryOutput{},
				fmt.Errorf("provide id: %w", apperrors.ErrMissingRequired)
		}

		registry, err := c.GetRegistry(ctx, anchor.GetRegistryRequest{
			ID: input.ID,
		})
		if err != nil {
			return nil, registryOutput{}, err
		}
		capRegistryFields(registry)
		return nil, registryOutput{
			Registry:     *registry,
			ContentTrust: contentTrustNotice,
			NextActions:  anchorGetRegistryNext(),
		}, nil
	}
}

func makeGetRegistriesHandler(
	c anchor.Client,
	logger *slog.Logger,
) mcp.ToolHandlerFor[getRegistriesInput, registriesOutput] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input getRegistriesInput,
	) (*mcp.CallToolResult, registriesOutput, error) {
		if err := requireRole(ctx, "reader", "writer", "admin", "automation"); err != nil {
			return nil, registriesOutput{}, err
		}

		// registry_id > 0 selects the deprecated single-registry lookup;
		// registry_id 0 or omitted selects a listing. The two modes accept
		// disjoint parameter sets, so which one a call means is never
		// ambiguous.
		if input.RegistryID != nil && *input.RegistryID > 0 {
			out, err := handleRegistriesByID(ctx, c, input)
			return nil, out, err
		}

		offset, limit := resolveRegistriesPage(input.Offset, input.Limit)

		// Two listing paths behind one tool surface. A name filter has no
		// chain-side index yet, so it must walk the whole table client-side
		// (handleRegistriesNameLookup); when the chain ships a by-name query
		// only that branch changes. An unfiltered listing needs no walk: the
		// precompile pages the table itself (handleRegistriesListing).
		if input.Name != nil && *input.Name != "" {
			out, err := handleRegistriesNameLookup(
				ctx, c, logger, *input.Name, input.Match, offset, limit,
			)
			return nil, out, err
		}

		if input.Match != "" {
			// match without a name to match against is a caller error, not
			// a silently ignorable parameter -- fail fast, same as the
			// mode combinations above.
			return nil, registriesOutput{}, apperrors.ErrMatchWithoutName
		}

		out, err := handleRegistriesListing(ctx, c, offset, limit)
		return nil, out, err
	}
}

// handleRegistriesListing services the unfiltered branch of
// anchor_get_registries: it asks the precompile for the caller's window
// directly -- Offset from the caller, Limit = whatever is still missing,
// capped at nameScanPageSize (the most rows the precompile returns per
// call). One round-trip for any limit up to that cap; a larger limit
// chains pages until it is filled or the table ends. Verified live
// (2026-09-10, testnet, 4400 registries): Offset is honored, Limit below
// the cap is served exactly, each call is ~0.5s, an offset past the end
// yields zero rows and no cursor. No full-table walk, so the chain's true
// row count is not known here (its countTotal is always 0):
// pagination.total is offset + rows returned, and TotalIsLowerBound flags
// when the chain reported more rows past this page.
func handleRegistriesListing(
	ctx context.Context, c anchor.Client, offset, limit uint64,
) (registriesOutput, error) {
	noIDFilter := uint64(0)
	var rows []anchor.Registry
	hasMore := true
	for page := 0; page < maxNameScanPages && hasMore && uint64(len(rows)) < limit; page++ {
		resp, err := c.GetRegistries(ctx, anchor.GetRegistriesRequest{
			RegistryID: &noIDFilter,
			Pagination: &anchor.PageRequest{
				Offset: offset + uint64(len(rows)),
				Limit:  min(limit-uint64(len(rows)), nameScanPageSize),
			},
		})
		if err != nil {
			return registriesOutput{}, err
		}
		rows = append(rows, resp.Registries...)
		nextKey, err := resp.Pagination.CursorBytes()
		if err != nil {
			return registriesOutput{}, err
		}
		hasMore = len(nextKey) > 0
	}
	for i := range rows {
		capRegistryFields(&rows[i])
	}
	total := offset + uint64(len(rows))
	if len(rows) == 0 && offset > 0 {
		// An offset past the end says nothing about the table's size beyond
		// "fewer than offset rows": report the only bound actually known.
		total, hasMore = 0, true
	}
	return registriesOutput{
		GetRegistriesResponse: anchor.GetRegistriesResponse{
			Registries: rows,
			Pagination: &anchor.PageResponse{Total: total},
		},
		ContentTrust:      contentTrustNotice,
		TotalIsLowerBound: hasMore,
		NextActions:       anchorGetRegistriesNext(len(rows) == 0),
	}, nil
}

// handleRegistriesByID services the deprecated registry_id branch of
// anchor_get_registries -- a single-registry lookup that predates
// anchor_get_registry and is kept only for backward compatibility. It
// accepts no listing parameter: offset and limit may be present only as 0,
// since there is nothing to page.
func handleRegistriesByID(
	ctx context.Context, c anchor.Client, input getRegistriesInput,
) (registriesOutput, error) {
	nameSet := input.Name != nil && *input.Name != ""
	offsetNonZero := input.Offset != nil && *input.Offset != 0
	limitNonZero := input.Limit != nil && *input.Limit != 0
	if nameSet || input.Match != "" || offsetNonZero || limitNonZero {
		return registriesOutput{}, apperrors.ErrInvalidFilterCombination
	}

	resp, err := c.GetRegistries(ctx, anchor.GetRegistriesRequest{
		RegistryID: input.RegistryID,
	})
	if err != nil {
		return registriesOutput{}, err
	}
	for i := range resp.Registries {
		capRegistryFields(&resp.Registries[i])
	}
	return registriesOutput{
		GetRegistriesResponse: *resp,
		ContentTrust:          contentTrustNotice,
		NextActions:           anchorGetRegistriesNext(len(resp.Registries) == 0),
	}, nil
}

// handleRegistriesNameLookup services the name-filtered branch of
// anchor_get_registries: runs the client-side scan, logs its cost, then
// returns the offset/limit window of the match set.
func handleRegistriesNameLookup(
	ctx context.Context,
	c anchor.Client,
	logger *slog.Logger,
	name, match string,
	offset, limit uint64,
) (registriesOutput, error) {
	start := time.Now()
	matches, truncated, err := scanRegistriesByName(ctx, c, name, match)
	if err != nil {
		return registriesOutput{}, err
	}
	// Operational visibility for the client-side scan: each by-name call is
	// a multi-page upstream walk whose cost grows with the registry table
	// (see nameScanPageSize/maxNameScanPages below), so operators need its
	// frequency and duration in logs. The scan is a stopgap -- if the chain
	// gains a by-name index as expected, or if indexing is solved off-chain,
	// it can be retired (#79). The scanned name itself is caller-supplied
	// input and deliberately not logged.
	logger.InfoContext(ctx, "anchor_get_registries by-name scan",
		slog.Duration("duration", time.Since(start)),
		slog.Int("matches", len(matches)),
		slog.Uint64("offset", offset),
		slog.Uint64("limit", limit),
		slog.Bool("truncated", truncated))

	page := pageRegistries(matches, offset, limit)
	for i := range page {
		capRegistryFields(&page[i])
	}
	return registriesOutput{
		GetRegistriesResponse: anchor.GetRegistriesResponse{
			Registries: page,
			// total is the match count from the scan. When truncated=false the
			// scan reached the natural end of the table and this is exact;
			// when truncated=true the scan hit its page cap before finishing,
			// so total is a lower bound on the true match count (signaled by
			// TotalIsLowerBound below). Either way it exceeds the chain's own
			// pagination.total, which is always 0 on this precompile.
			Pagination: &anchor.PageResponse{Total: uint64(len(matches))},
		},
		ContentTrust:       contentTrustNotice,
		NameMatchTruncated: truncated,
		TotalIsLowerBound:  truncated,
		// Branch on the match set, not the page: a page emptied only by an
		// offset past the end would otherwise be told "no registries match"
		// when matches do exist earlier in the set.
		NextActions: anchorGetRegistriesNext(len(matches) == 0),
	}, nil
}

// defaultRegistriesPageSize is the page size applied when a listing call
// omits limit (or passes 0). Both listing branches -- name-filtered and
// unfiltered -- resolve pagination through resolveRegistriesPage and apply
// the window client-side, so this constant is the single source of truth
// for what "no pagination given" means.
const defaultRegistriesPageSize = 100

// resolveRegistriesPage fills in the listing defaults for the optional
// offset/limit inputs: a missing offset starts at the beginning, and a
// missing (or zero) limit takes defaultRegistriesPageSize. Both branches of
// a listing resolve the bounds through here so a name-filtered page and a
// chain-side page never disagree on what "no pagination given" means.
func resolveRegistriesPage(offset, limit *uint64) (resolvedOffset, resolvedLimit uint64) {
	if offset != nil {
		resolvedOffset = *offset
	}
	resolvedLimit = defaultRegistriesPageSize
	if limit != nil && *limit > 0 {
		resolvedLimit = *limit
	}
	return resolvedOffset, resolvedLimit
}

// pageRegistries returns the offset/limit window of an already-complete
// match set. An offset past the end yields an empty page rather than an
// error, matching how the chain treats an out-of-range offset.
func pageRegistries(matches []anchor.Registry, offset, limit uint64) []anchor.Registry {
	total := uint64(len(matches))
	if offset >= total {
		return nil
	}
	end := offset + limit
	// end < offset catches the uint64 wrap a caller-supplied limit near
	// math.MaxUint64 would cause, which would otherwise truncate the page.
	if end < offset || end > total {
		end = total
	}
	return matches[offset:end]
}

// nameScanPageSize and maxNameScanPages bound the client-side by-name
// registry walk that scanRegistriesByName performs. The anchoring
// precompile has no by-name index, so a name-filtered anchor_get_registries
// call must page through the registry table itself. pagination.total is
// unreliable on this chain (it reports 0 even when rows are returned), so
// the walk terminates when a page comes back with an empty NextKey, never
// on a reported count (and deliberately not on a short row count -- see
// the termination comment in scanRegistriesByName).
//
// nameScanPageSize=200 is not just a client-side choice: verified live
// against evm.testnet.nvnmchain.io, the precompile itself hard-caps the
// actual page size at 200 regardless of the limit requested (limit=500
// returned exactly 200 rows, silently, no error). Requesting more than 200
// buys nothing; 200 is the real ceiling. handleRegistriesListing requests
// the same size for its direct page fetch.
//
// maxNameScanPages is a safety backstop against an unbounded or misbehaving
// chain, not an expected limit: at nameScanPageSize per page that is up to
// 10,000 registries scanned, comfortably above anything this testnet
// precompile has held (highest assigned registry ID observed live: 2612).
// Hitting it is surfaced to the caller via NameMatchTruncated rather than
// silently returning a partial match set indistinguishable from a complete
// one.
const (
	nameScanPageSize = 200
	maxNameScanPages = 50
)

// registryNameMatcher returns a case-insensitive matcher for the given mode,
// or an error if mode is not one of the supported values. Empty mode means
// "exact" (the default).
func registryNameMatcher(target, mode string) (func(name string) bool, error) {
	target = strings.ToLower(target)
	switch mode {
	case "", "exact":
		return func(name string) bool { return strings.EqualFold(name, target) }, nil
	case "prefix":
		return func(name string) bool { return strings.HasPrefix(strings.ToLower(name), target) }, nil
	case "suffix":
		return func(name string) bool { return strings.HasSuffix(strings.ToLower(name), target) }, nil
	case "contains":
		return func(name string) bool { return strings.Contains(strings.ToLower(name), target) }, nil
	default:
		return nil, apperrors.ErrInvalidMatchMode
	}
}

// latestRegistryID returns the highest currently-assigned registry ID via
// reverse=true, limit=1 on the same "registries" precompile query
// scanRegistriesByName pages -- the cheapest substitute this chain has for
// pagination.total, which it always reports as 0 (docs/TESTING.md). found
// is false when no registries exist yet. This is a best-effort optimization,
// not a correctness dependency: scanRegistriesByName's own termination (a
// short page) never relies on it, so callers should treat a non-nil err as
// non-fatal and fall back to the plain walk.
func latestRegistryID(ctx context.Context, c anchor.Client) (id uint64, found bool, err error) {
	noIDFilter := uint64(0)
	resp, err := c.GetRegistries(ctx, anchor.GetRegistriesRequest{
		RegistryID: &noIDFilter,
		Pagination: &anchor.PageRequest{Limit: 1, Reverse: true},
	})
	if err != nil {
		return 0, false, err
	}
	if len(resp.Registries) == 0 {
		return 0, false, nil
	}
	return resp.Registries[0].ID, true, nil
}

// scanRegistriesByName pages through the entire registry table via
// c.GetRegistries, client-side, returning every registry whose name matches
// target under the given mode.
//
// It first peeks at the highest assigned registry ID (latestRegistryID) to
// possibly skip the walk entirely (an empty table) and to cross-check
// completeness afterward. The walk itself never depends on that peek --
// it terminates the same way regardless: on a short page (fewer rows than
// requested), never on a reported count, since pagination.total is
// unreliable on this chain. truncated is true if the walk hit
// maxNameScanPages before a short page, OR if the peek succeeded but the
// total rows scanned came in lower than the highest known ID (e.g. a
// registry was added concurrently with this scan, or IDs are not
// contiguous from 1) -- either way, the caller should not treat the result
// as a proven-complete match set.
func scanRegistriesByName(
	ctx context.Context, c anchor.Client, target, mode string,
) (matches []anchor.Registry, truncated bool, err error) {
	matchFn, err := registryNameMatcher(target, mode)
	if err != nil {
		return nil, false, err
	}

	highestID, haveHighestID, peekErr := latestRegistryID(ctx, c)
	if peekErr == nil && !haveHighestID {
		// The peek call succeeded and found zero registries -- unlike a
		// timed-out or errored peek, this is a definitive "table is empty,"
		// not merely "we don't know," so it's safe to skip the walk.
		return nil, false, nil
	}

	// Same underlying precompile call as anchor_get_registry: the
	// "registries" method takes a registry ID and pagination. id > 0
	// (GetRegistry) returns that one registry; id == 0 (here) means
	// "no ID filter" and returns a page of the full table, which the loop
	// below walks to completion and filters client-side by name.
	noIDFilter := uint64(0)

	// Cursor via NextKey, not Offset: the underlying Cosmos SDK pagination
	// seeks directly to a key (O(1) store lookup) but walks the collection
	// one entry at a time to satisfy an Offset (see the PageRequest.Key
	// doc comment in types.go). Offset-walking from 0,200,400,... would
	// make the full scan cost grow with how far into the table each page
	// is, not just with the page size; cursoring avoids that entirely.
	var cursorKey []byte
	var totalScanned uint64
	for page := 0; page < maxNameScanPages; page++ {
		resp, err := c.GetRegistries(ctx, anchor.GetRegistriesRequest{
			RegistryID: &noIDFilter,
			Pagination: &anchor.PageRequest{Key: cursorKey, Limit: nameScanPageSize},
		})
		if err != nil {
			return nil, false, err
		}
		totalScanned += uint64(len(resp.Registries))
		for _, r := range resp.Registries {
			if matchFn(r.Name) {
				matches = append(matches, r)
			}
		}
		nextKey, err := resp.Pagination.CursorBytes()
		if err != nil {
			return nil, false, err
		}
		// NextKey emptiness, not the row count, is the authoritative "done"
		// signal: the SDK only sets NextKey when it can see at least one
		// more entry past this page. Deliberately NOT also stopping on a
		// short page (len < nameScanPageSize): if the chain's own page cap
		// ever drops below nameScanPageSize, every page would come back
		// "short" with NextKey still set, and a short-page check would
		// silently end the walk after one page. Trusting NextKey alone
		// costs nothing today and survives that cap drift.
		if len(nextKey) == 0 {
			if peekErr == nil && haveHighestID && totalScanned < highestID {
				return matches, true, nil
			}
			return matches, false, nil
		}
		cursorKey = nextKey
	}
	return matches, true, nil
}

func makeGetRecordsHandler(
	c anchor.Client,
) mcp.ToolHandlerFor[getRecordsInput, recordsOutput] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input getRecordsInput,
	) (*mcp.CallToolResult, recordsOutput, error) {
		if err := requireRole(ctx, "reader", "writer", "admin", "automation"); err != nil {
			return nil, recordsOutput{}, err
		}
		r := anchor.GetRecordsRequest{
			RegistryID: input.RegistryID,
			RecordID:   input.RecordID,
			Index:      input.Index,
			Checksum:   input.Checksum,
		}
		if input.Offset != nil || input.Limit != nil {
			r.Pagination = &anchor.PageRequest{}
			if input.Offset != nil {
				r.Pagination.Offset = *input.Offset
			}
			if input.Limit != nil {
				r.Pagination.Limit = *input.Limit
			}
		}

		resp, err := c.GetRecords(ctx, r)
		if err != nil {
			return nil, recordsOutput{}, err
		}
		for i := range resp.Records {
			capRecordFields(&resp.Records[i])
		}
		return nil, recordsOutput{
			GetRecordsResponse: *resp,
			ContentTrust:       contentTrustNotice,
			NextActions:        anchorGetRecordsNext(),
		}, nil
	}
}
