// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	defitypes "github.com/defiweb/go-eth/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/telemetry"
)

// signerGates bundles the Phase-5 per-signer enforcement dependencies +
// policy for the keyless write path (blacklist + quota). Zero value (nil
// stores) disables the gates -- used by non-keyless/self-host call sites and
// tests, where the handler never consults them.
type signerGates struct {
	blacklist         SignerBlacklistStore
	quota             SignerQuotaStore
	rate              int
	window            time.Duration
	quotaFailOpen     bool
	blacklistFailOpen bool
	now               func() time.Time // nil -> time.Now
}

func registerEVMWriteTools(
	srv *mcp.Server,
	evmClient evm.Client,
	anchorAddr string,
	keylessWrites bool,
	audit WriteAuditStore,
	metrics WriteMetrics,
	gates signerGates,
	logger *slog.Logger,
) {
	addTool(srv, &mcp.Tool{
		Name:  "evm_send_raw_transaction",
		Title: "Send Raw Transaction",
		Description: "Broadcast a signed transaction to the network. " +
			"Use this for the local/headless signer path only: " +
			"sign the raw_tx bytes from anchor_prepare_* externally, " +
			"then pass the signed hex here. " +
			"Do NOT use this after signing with MetaMask or a browser wallet -- " +
			"those wallets broadcast directly and return a tx_hash themselves. " +
			"Input is a 0x-prefixed signed transaction hex string. " +
			"Returns the transaction hash. " +
			"Confirm the result with evm_get_transaction_receipt.",
		Annotations: newDestructiveWriteTool(),
	}, makeSendRawTxHandler(evmClient, anchorAddr, keylessWrites, audit, metrics, gates, logger))
}

// --- Input/output types ---

type sendRawTxInput struct {
	SignedTxHex string `json:"signed_tx" jsonschema:"Signed transaction hex (0x-prefixed)"`
}

type sendRawTxOutput struct {
	TxHash      string       `json:"tx_hash"`
	NextActions []NextAction `json:"next_actions,omitempty"`
}

// --- Handler ---

func makeSendRawTxHandler(
	c evm.Client, anchorAddr string, keylessWrites bool, audit WriteAuditStore, metrics WriteMetrics,
	gates signerGates, logger *slog.Logger,
) mcp.ToolHandlerFor[sendRawTxInput, sendRawTxOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input sendRawTxInput,
	) (*mcp.CallToolResult, sendRawTxOutput, error) {
		if err := requireRole(ctx, "writer", "admin", "automation"); err != nil {
			return nil, sendRawTxOutput{}, err
		}
		if input.SignedTxHex == "" {
			return nil, sendRawTxOutput{},
				fmt.Errorf(
					"signed_tx is required: %w",
					apperrors.ErrMissingRequired,
				)
		}

		recordBroadcast := func(outcome string) {
			if metrics != nil {
				metrics.RecordBroadcast(ctx, outcome)
			}
		}
		recordReject := func(cause telemetry.RelayRejectCause) {
			if metrics != nil {
				metrics.RecordRelayReject(ctx, cause)
			}
		}

		// decoded drives the signer-keyed audit; broadcastHex is the raw
		// passthrough (authed/self-host) or the scoped canonical re-encode
		// (keyless writes, D9 / §5).
		decoded, quotaWindowStart, broadcastHex, perr := resolveBroadcast(
			ctx, input.SignedTxHex, anchorAddr, keylessWrites, gates, logger, recordReject,
		)
		if perr != nil {
			return nil, sendRawTxOutput{}, perr
		}

		clientID := auth.ClientIDFromContext(ctx)

		// identityAttrs records the recovered signer (when the tx decoded --
		// always under keyless writes, best-effort under authed writes, F1)
		// and the authenticated caller's client_id (empty under keyless /
		// anonymous). Authed-mode lines carry BOTH the API caller and the
		// on-chain signer. Addresses/tx hashes only, no keys -- §4.D.
		identityAttrs := func() []slog.Attr {
			var attrs []slog.Attr
			if decoded != nil {
				attrs = append(attrs,
					slog.String("signer", decoded.Signer.String()),
					slog.String("to", addrString(decoded.To)),
					slog.String("value_wei", decoded.Value.String()),
					slog.Int("calldata_len", len(decoded.Input)),
				)
			}
			if clientID != "" {
				attrs = append(attrs, slog.String("client_id", clientID))
			}
			return attrs
		}

		// recordAudit persists one broadcast attempt best-effort. A row is
		// written whenever a signer was recovered (decoded != nil -- keyless
		// writes, or an authed broadcast whose tx decoded, F1) AND a store is
		// configured. The persisted row is signer-keyed; the authed caller's
		// client_id stays in the slog line (the table has no client_id column).
		recordAudit := func(outcome, txHash, errMsg string) {
			if audit == nil || decoded == nil {
				return
			}
			rerr := audit.Record(ctx, WriteAuditEntry{
				Signer:      decoded.Signer.String(),
				ToAddr:      addrString(decoded.To),
				ValueWei:    decoded.Value.String(),
				CalldataLen: len(decoded.Input),
				TxHash:      txHash,
				Outcome:     outcome,
				Error:       errMsg,
			})
			if rerr != nil {
				logger.LogAttrs(ctx, slog.LevelWarn, "audit", auditGroup([]slog.Attr{
					slog.String("tool", "evm_send_raw_transaction"),
					slog.String("phase", "write_audit_persist_failed"),
					slog.String("signer", decoded.Signer.String()),
					slog.String("error", rerr.Error()),
				}))
			}
		}

		txHash, err := c.SendRawTransaction(ctx, broadcastHex)
		if err != nil {
			failAttrs := append([]slog.Attr{
				slog.String("tool", "evm_send_raw_transaction"),
				slog.String("phase", "broadcast_failed"),
				slog.Int("signed_tx_len", len(input.SignedTxHex)),
				slog.String("error", err.Error()),
			}, identityAttrs()...)
			logger.LogAttrs(ctx, slog.LevelWarn, "audit", auditGroup(failAttrs))
			recordAudit("broadcast_failed", "", err.Error())
			recordBroadcast("failed")
			return nil, sendRawTxOutput{}, err
		}

		okAttrs := append([]slog.Attr{
			slog.String("tool", "evm_send_raw_transaction"),
			slog.String("phase", "broadcast_ok"),
			slog.String("tx_hash", txHash),
		}, identityAttrs()...)
		logger.LogAttrs(ctx, slog.LevelInfo, "audit", auditGroup(okAttrs))
		recordAudit("broadcast_ok", txHash, "")
		recordBroadcast("ok")

		// Increment the per-signer quota only on a successful broadcast; a
		// failed/errored broadcast (handled above, which returns early)
		// must never consume quota.
		if decoded != nil && gates.quota != nil {
			if ierr := gates.quota.Increment(ctx, decoded.Signer.String(), quotaWindowStart); ierr != nil {
				logger.LogAttrs(ctx, slog.LevelWarn, "audit", auditGroup([]slog.Attr{
					slog.String("tool", "evm_send_raw_transaction"),
					slog.String("phase", "quota_increment_failed"),
					slog.String("signer", decoded.Signer.String()),
					slog.String("error", ierr.Error()),
				}))
			}
		}

		return nil, sendRawTxOutput{TxHash: txHash, NextActions: evmSendRawTxNext(txHash)}, nil
	}
}

// resolveBroadcast produces the decoded tx (for the signer-keyed audit),
// the quota window start, and the bytes to broadcast. Under keyless writes
// it runs the full pre-broadcast pipeline (decode + relay-scope gate +
// blacklist/quota + canonical re-encode). In authed/self-host mode it does
// a best-effort decode ONLY -- to audit the broadcast (F1) -- with no
// relay-scope enforcement and raw passthrough of the caller's bytes; a
// decode failure is non-fatal (decoded stays nil, the tx still broadcasts,
// and the audit falls back to the client_id slog line).
func resolveBroadcast(
	ctx context.Context,
	signedTxHex, anchorAddr string,
	keylessWrites bool,
	gates signerGates,
	logger *slog.Logger,
	recordReject func(telemetry.RelayRejectCause),
) (*evm.DecodedTx, time.Time, string, error) {
	if keylessWrites {
		return prepareKeylessBroadcast(ctx, signedTxHex, anchorAddr, gates, logger, recordReject)
	}
	if dtx, derr := evm.DecodeSignedTx(signedTxHex); derr == nil {
		return dtx, time.Time{}, signedTxHex, nil
	}
	return nil, time.Time{}, signedTxHex, nil
}

// prepareKeylessBroadcast runs the keyless-write pre-broadcast pipeline:
// decode, anchor-scope check, and the Phase-5 blacklist/quota gates. It
// returns the decoded tx, the quota window start (for the caller's later
// increment-on-success call), and the canonical broadcast hex, or a non-nil
// error if any check rejects the request. Split out of makeSendRawTxHandler
// to keep that function's cyclomatic complexity within the package's lint
// budget.
func prepareKeylessBroadcast(
	ctx context.Context,
	signedTxHex, anchorAddr string,
	gates signerGates,
	logger *slog.Logger,
	recordReject func(telemetry.RelayRejectCause),
) (*evm.DecodedTx, time.Time, string, error) {
	dtx, derr := evm.DecodeSignedTx(signedTxHex)
	if derr != nil {
		recordReject(telemetry.CauseDecode)
		return nil, time.Time{}, "", derr // ErrTxDecode (input class)
	}
	anchor, aerr := defitypes.AddressFromHex(anchorAddr)
	if aerr != nil {
		recordReject(telemetry.CauseAnchorMisconfig)
		return nil, time.Time{}, "",
			fmt.Errorf("anchor address misconfigured: %w", apperrors.ErrInvalidAddress)
	}
	if serr := checkRelayScope(dtx.To, anchor); serr != nil {
		logger.LogAttrs(ctx, slog.LevelWarn, "audit", auditGroup([]slog.Attr{
			slog.String("tool", "evm_send_raw_transaction"),
			slog.String("phase", "relay_scope_rejected"),
			slog.String("signer", dtx.Signer.String()),
			slog.String("to", addrString(dtx.To)),
		}))
		recordReject(telemetry.CauseRelayScope)
		return nil, time.Time{}, "", serr // ErrRelayScopeRejected (input class)
	}

	// Blacklist + quota gates (Phase 5, keyless writes only). Blacklist is
	// consulted first: a banned signer is rejected outright and never
	// consults/consumes quota.
	ws, gerr := checkSignerGates(ctx, gates, dtx.Signer.String(), logger, recordReject)
	if gerr != nil {
		return nil, time.Time{}, "", gerr
	}

	return dtx, ws, "0x" + hex.EncodeToString(dtx.CanonicalRaw), nil
}

// checkSignerGates enforces the Phase-5 per-signer blacklist and quota gates
// for signer, blacklist first. It returns the quota window start (valid even
// when gates.quota is nil, so the caller can reuse it for the post-broadcast
// increment) and a non-nil error when the request must be rejected.
// recordReject is invoked for every metrics-visible rejection; it must never
// be called with caller-derived data (the signer belongs only in the audit
// log lines here, never in a metric label).
func checkSignerGates(
	ctx context.Context,
	gates signerGates,
	signer string,
	logger *slog.Logger,
	recordReject func(telemetry.RelayRejectCause),
) (time.Time, error) {
	nowFn := gates.now
	if nowFn == nil {
		nowFn = time.Now
	}
	ws := WindowStart(nowFn(), gates.window)

	if gates.blacklist != nil {
		banned, berr := gates.blacklist.IsBlacklisted(ctx, signer)
		switch {
		case berr != nil && !gates.blacklistFailOpen:
			recordReject(telemetry.CauseBlacklistStoreErr)
			return ws, fmt.Errorf("blacklist store unavailable: %w", berr)
		case berr != nil: // fail-open: allow, log loudly
			logger.LogAttrs(ctx, slog.LevelWarn, "audit", auditGroup([]slog.Attr{
				slog.String("tool", "evm_send_raw_transaction"),
				slog.String("phase", "blacklist_fail_open"),
				slog.String("signer", signer),
				slog.String("error", berr.Error()),
			}))
		case banned:
			logger.LogAttrs(ctx, slog.LevelWarn, "audit", auditGroup([]slog.Attr{
				slog.String("tool", "evm_send_raw_transaction"),
				slog.String("phase", "signer_blacklisted"),
				slog.String("signer", signer),
			}))
			recordReject(telemetry.CauseSignerBlacklist)
			return ws, ErrSignerBlacklisted
		}
	}

	if gates.quota != nil {
		n, qerr := gates.quota.Count(ctx, signer, ws)
		switch {
		case qerr != nil && !gates.quotaFailOpen:
			recordReject(telemetry.CauseQuotaStoreErr)
			return ws, fmt.Errorf("quota store unavailable: %w", qerr)
		case qerr != nil: // fail-open: allow, log loudly
			logger.LogAttrs(ctx, slog.LevelWarn, "audit", auditGroup([]slog.Attr{
				slog.String("tool", "evm_send_raw_transaction"),
				slog.String("phase", "quota_fail_open"),
				slog.String("signer", signer),
				slog.String("error", qerr.Error()),
			}))
		case n >= gates.rate:
			recordReject(telemetry.CauseSignerQuota)
			return ws, ErrSignerQuotaExceeded
		}
	}

	return ws, nil
}

// auditGroup builds the "audit" slog group from a dynamic attribute set.
// It uses slog.Group rather than a slog.Attr{Key: ...} composite literal so
// the package's raw-key-literal guard (keys_constraint_test.go, which forbids
// `Key:` struct literals) is not tripped by audit logging. slog.Group accepts
// slog.Attr values among its variadic args.
func auditGroup(attrs []slog.Attr) slog.Attr {
	args := make([]any, len(attrs))
	for i, a := range attrs {
		args[i] = a
	}
	return slog.Group("audit", args...)
}
