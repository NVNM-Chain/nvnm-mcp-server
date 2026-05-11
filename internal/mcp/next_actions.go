package mcp

// Per-tool next_actions builders. Built on each call rather than declared
// as package-level vars so call sites can inject data-dependent hints
// (e.g., the tx hash to pass into the receipt-poll hint).
//
// Keep hints short and concrete -- agents read them in their reasoning
// loop; every wasted token costs them context budget.
//
// All Tool: literals here must match a registered tool name. The
// reachability tests in annotations_test.go enforce that.

func evmChainIDNext() []NextAction {
	return []NextAction{
		{Tool: "evm_get_block", Hint: "Inspect the latest block (or any other) by number or hash."},
	}
}

func evmGetBlockNext() []NextAction {
	return []NextAction{
		{Tool: "evm_get_transaction", Hint: "Look up any transaction listed in this block by its hash."},
		{Tool: "evm_get_logs", Hint: "Filter logs in a block range to surface specific events."},
	}
}

func evmGetTransactionNext() []NextAction {
	return []NextAction{
		{Tool: "evm_get_transaction_receipt", Hint: "Fetch the receipt to confirm status, gas used, and logs."},
	}
}

// evmGetReceiptNext branches on receipt status. status == "reverted"
// points the agent at the original transaction's calldata so they can
// diagnose what failed; status == "success" suggests verifying any
// downstream anchor write actually landed.
func evmGetReceiptNext(status string) []NextAction {
	if status == "reverted" {
		return []NextAction{
			{
				Tool: "evm_get_transaction",
				Hint: "Re-inspect the original transaction's calldata to diagnose the revert.",
			},
		}
	}
	return []NextAction{
		{
			Tool: "anchor_get_records",
			Hint: "If this receipt is for an anchor write, query records to verify it landed on chain.",
		},
	}
}

// evmGetBalanceNext currently returns no hint. In Phase 8.8 this will
// point at wallet_status, which returns the same balance plus a status
// classification and onboarding-aware next steps.
func evmGetBalanceNext() []NextAction {
	return nil
}

func evmGetCodeNext(isContract bool) []NextAction {
	if !isContract {
		return []NextAction{
			{Tool: "evm_get_balance", Hint: "Address has no contract bytecode -- inspect its balance instead."},
		}
	}
	return []NextAction{
		{Tool: "evm_call_contract", Hint: "Use the contract's ABI to make a read-only call against this bytecode."},
	}
}

func evmGetLogsNext() []NextAction {
	return []NextAction{
		{
			Tool: "evm_get_transaction_receipt",
			Hint: "Fetch the receipt for a specific log's tx_hash to see all events from that tx.",
		},
	}
}

// evmCallContractNext returns nil. Pure-read tool with no obvious
// universal next step; the agent's reasoning context dictates what to
// do with the decoded output.
func evmCallContractNext() []NextAction {
	return nil
}

func anchorInfoNext() []NextAction {
	return []NextAction{
		{Tool: "anchor_get_registries", Hint: "List the registries on chain (logical containers for records)."},
	}
}

func anchorGetRegistryNext() []NextAction {
	return []NextAction{
		{Tool: "anchor_get_records", Hint: "Browse records inside this registry."},
		{
			Tool: "anchor_prepare_add_record",
			Hint: "Anchor a new record into this registry (caller must hold editor role).",
		},
	}
}

// anchorGetRegistriesNext branches on whether the query returned any
// results.
func anchorGetRegistriesNext(emptyResult bool) []NextAction {
	if emptyResult {
		return []NextAction{
			{
				Tool: "anchor_prepare_add_registry",
				Hint: "No registries match. Create your own; you become its admin automatically.",
			},
		}
	}
	return []NextAction{
		{Tool: "anchor_get_registry", Hint: "Drill into a specific registry by id or name."},
		{
			Tool: "anchor_prepare_add_record",
			Hint: "Anchor a new record (requires the editor role on the target registry).",
		},
	}
}

func anchorGetRecordsNext() []NextAction {
	return []NextAction{
		{
			Tool: "anchor_prepare_add_record",
			Hint: "Anchor an updated version of a record (same registry + record_id), or a brand-new record.",
		},
	}
}

// anchorPrepareWriteNext is shared by all three anchor_prepare_* tools.
// The unsigned tx must be signed externally and broadcast via
// evm_send_raw_transaction.
func anchorPrepareWriteNext() []NextAction {
	return []NextAction{
		{
			Tool: "evm_send_raw_transaction",
			Hint: "Sign the raw_tx with your private key locally, then broadcast the signed bytes here.",
		},
	}
}

// evmSendRawTxNext echoes the tx_hash into the receipt-poll hint so the
// agent has a copy-pasteable next call.
func evmSendRawTxNext(txHash string) []NextAction {
	return []NextAction{
		{
			Tool: "evm_get_transaction_receipt",
			Hint: "Wait ~one block, then call this tool with tx_hash=" + txHash +
				" to confirm inclusion and inspect the decoded events.",
		},
	}
}
