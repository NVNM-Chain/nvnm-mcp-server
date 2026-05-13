package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	deficrypto "github.com/defiweb/go-eth/crypto"
	defitypes "github.com/defiweb/go-eth/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	apperrors "github.com/inveniam/nvnm-mcp-server/internal/errors"
)

// nvnm_setup_verify_hash and nvnm_setup_verify_signature are
// stateless onboarding-flow helpers. They issue a deterministic
// per-address challenge so the server can recompute the expected
// value without retaining any per-call state across HTTP requests,
// and verify that the caller can correctly hash (verify_hash) or
// sign (verify_signature) that challenge.

// challengeVersionTag is the per-server constant mixed into the
// deterministic challenge. It is NOT a secret in the cryptographic
// sense -- the challenge protocol only needs determinism, not
// confidentiality -- but pinning a version-tagged constant lets the
// challenge derivation evolve later (e.g. v2 challenges with an
// added bit) without breaking existing wizard runs.
//
//nolint:gosec,nolintlint // G101 false positive: this is a protocol version tag, not a credential
const challengeVersionTag = "nvnm-setup-challenge-v1"

// challengeForAddress returns the canonical challenge string for an
// address. The same address always yields the same challenge, on any
// server instance, with no time dependence. Lowercased hex address so
// EIP-55 capitalization differences don't fork the challenge.
func challengeForAddress(addr defitypes.Address) string {
	lower := strings.ToLower(addr.String())
	sum := sha256.Sum256([]byte(lower + ":" + challengeVersionTag))
	return "0x" + hex.EncodeToString(sum[:])
}

// expectedHashForChallenge returns the canonical SHA-256 hash of the
// challenge string. The caller is expected to hash the bytes of the
// challenge (the literal `0x...` string as UTF-8) and submit the
// hex digest; the server recomputes here and compares.
func expectedHashForChallenge(challenge string) string {
	sum := sha256.Sum256([]byte(challenge))
	return "0x" + hex.EncodeToString(sum[:])
}

// --- verify_hash ---

type verifyHashInput struct {
	Address string `json:"address" jsonschema:"EVM address (0x...) the challenge was issued for"`
	Hash    string `json:"hash" jsonschema:"Caller's SHA-256 of the challenge bytes, 0x-prefixed hex"`
}

type verifyHashOutput struct {
	OK          bool         `json:"ok"`
	Address     string       `json:"address"`
	Challenge   string       `json:"challenge"`
	Expected    string       `json:"expected"`
	Got         string       `json:"got"`
	NextActions []NextAction `json:"next_actions,omitempty"`
}

func registerVerifyHashTool(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:  "nvnm_setup_verify_hash",
		Title: "Verify Hash (setup challenge)",
		Description: "Issues a deterministic per-address challenge and " +
			"verifies that the caller can produce its SHA-256 digest. " +
			"Use during onboarding to prove an off-chain hashing path " +
			"works before broadcasting an anchor transaction. Pure " +
			"compute, no chain calls.",
		Annotations: newClosedWorldReadOnly(),
	}, makeVerifyHashHandler())
}

func makeVerifyHashHandler() mcp.ToolHandlerFor[verifyHashInput, verifyHashOutput] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input verifyHashInput,
	) (*mcp.CallToolResult, verifyHashOutput, error) {
		if err := requireRole(ctx, readRoleSet...); err != nil {
			return nil, verifyHashOutput{}, err
		}
		addr, err := parseAddress(input.Address)
		if err != nil {
			return nil, verifyHashOutput{}, err
		}
		if input.Hash == "" {
			return nil, verifyHashOutput{},
				fmt.Errorf("hash is required: %w", apperrors.ErrMissingRequired)
		}
		challenge := challengeForAddress(addr)
		expected := expectedHashForChallenge(challenge)
		got := strings.ToLower(input.Hash)
		if !strings.HasPrefix(got, "0x") {
			got = "0x" + got
		}
		ok := got == expected

		out := verifyHashOutput{
			OK:        ok,
			Address:   addr.String(),
			Challenge: challenge,
			Expected:  expected,
			Got:       got,
		}
		if !ok {
			out.NextActions = []NextAction{
				{
					Tool: "nvnm_setup_verify_hash",
					Hint: "Recompute SHA-256 over the challenge string verbatim (UTF-8 bytes, " +
						"including the 0x prefix). The expected digest is in this response's " +
						"`expected` field.",
				},
			}
			return nil, out, fmt.Errorf("hash mismatch: %w", apperrors.ErrInvalidHash)
		}
		out.NextActions = []NextAction{
			{Tool: "nvnm_setup_verify_signature", Hint: "Hash path proven. Next: prove your signing path works."},
		}
		return nil, out, nil
	}
}

// --- verify_signature ---

type verifySignatureInput struct {
	//nolint:lll // schema docstring; line length unavoidable on tag
	Address string `json:"address" jsonschema:"EVM address (0x...) the challenge was issued for; also the address that should be recovered from the signature"`
	//nolint:lll // schema docstring
	Signature string `json:"signature" jsonschema:"EIP-191 personal_sign output over the challenge, 0x-prefixed hex (65 bytes)"`
}

type verifySignatureOutput struct {
	OK          bool         `json:"ok"`
	Address     string       `json:"address"`
	Challenge   string       `json:"challenge"`
	RecoveredAt string       `json:"recovered_address,omitempty"`
	NextActions []NextAction `json:"next_actions,omitempty"`
}

func registerVerifySignatureTool(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:  "nvnm_setup_verify_signature",
		Title: "Verify Signature (setup challenge)",
		Description: "Issues a deterministic per-address challenge and " +
			"verifies an EIP-191 `personal_sign` signature over it. " +
			"Use during onboarding to prove the signing path works " +
			"before broadcasting an anchor transaction. Pure compute, " +
			"no chain calls.",
		Annotations: newClosedWorldReadOnly(),
	}, makeVerifySignatureHandler())
}

func makeVerifySignatureHandler() mcp.ToolHandlerFor[verifySignatureInput, verifySignatureOutput] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input verifySignatureInput,
	) (*mcp.CallToolResult, verifySignatureOutput, error) {
		if err := requireRole(ctx, readRoleSet...); err != nil {
			return nil, verifySignatureOutput{}, err
		}
		addr, err := parseAddress(input.Address)
		if err != nil {
			return nil, verifySignatureOutput{}, err
		}
		if input.Signature == "" {
			return nil, verifySignatureOutput{},
				fmt.Errorf("signature is required: %w", apperrors.ErrMissingRequired)
		}
		sig, sigErr := defitypes.SignatureFromHex(input.Signature)
		if sigErr != nil {
			return nil, verifySignatureOutput{},
				fmt.Errorf("signature must be 65-byte 0x-prefixed hex: %w", apperrors.ErrInvalidSignature)
		}

		challenge := challengeForAddress(addr)

		// EIP-191 personal_sign prepends a fixed prefix; defiweb's
		// crypto.ECRecoverer.RecoverMessage handles the prefixing
		// and Keccak hashing internally so the caller passes the
		// raw message and the 65-byte signature.
		recovered, recErr := deficrypto.ECRecoverer.RecoverMessage([]byte(challenge), sig)
		if recErr != nil || recovered == nil {
			return nil, verifySignatureOutput{
					Address:   addr.String(),
					Challenge: challenge,
				},
				fmt.Errorf("recover signer: %w", apperrors.ErrInvalidSignature)
		}

		ok := *recovered == addr
		out := verifySignatureOutput{
			OK:          ok,
			Address:     addr.String(),
			Challenge:   challenge,
			RecoveredAt: recovered.String(),
		}
		if !ok {
			out.NextActions = []NextAction{
				{
					Tool: "nvnm_setup_verify_signature",
					Hint: "Recovered signer does not match the input address. Sign the " +
						"challenge with the private key for the input address; do not pre-hash.",
				},
			}
			return nil, out, fmt.Errorf("signer mismatch: %w", apperrors.ErrInvalidSignature)
		}
		out.NextActions = []NextAction{
			{Tool: "wallet_status", Hint: "Signing path proven. Check the wallet's on-chain state and move on to anchor tools."},
		}
		return nil, out, nil
	}
}
