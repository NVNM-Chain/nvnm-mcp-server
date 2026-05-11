package mcp

// NextAction is a hint embedded in tool responses that points an agent at
// the next logical tool call. Agents chain a journey through the tool
// surface by reading these hints; there is no server-side orchestration.
//
// Per-tool hint builders live alongside the tool handlers (added in a
// later commit); the envelope wrappers that attach NextActions to existing
// response types live alongside them.
type NextAction struct {
	// Tool is the name of the MCP tool the agent should consider calling
	// next. Must match a registered tool name; a reachability test
	// enforces this.
	Tool string `json:"tool"`
	// Hint is a one-sentence reason the agent might want to call Tool.
	// Kept tight -- agents pay context tokens for every word.
	Hint string `json:"hint"`
	// When is an optional precondition phrased for an agent's reasoning
	// loop, e.g. "after funding lands in your wallet". Empty when the
	// next call is unconditional.
	When string `json:"when,omitempty"`
}
