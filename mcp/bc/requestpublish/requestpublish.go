// Package requestpublish is the governance statement: in v1 it returns
// NOT_IMPLEMENTED plus the semantics that governed publication would require.
// The contract is the deliverable — the refusal demonstrates the thesis that
// human approval is an invariant, not a feature flag.
//
// It imports nothing internal and never errors: it does not execute, validate,
// or publish. The Input type and the tool schema document the interface that a
// future, measurement-gated v2 will implement.
package requestpublish

import "context"

const (
	statusNotImplemented = "NOT_IMPLEMENTED"
	semanticsDoc         = "v1 composes, validates and previews — it never publishes. " +
		"Governed publication ships as a documented contract, not an implementation: " +
		"governance_semantics states what it will require. See docs/03-tool-contracts.md §5."
)

// governanceSemantics is the documented contract for governed publication
// (tool contract §5).
var governanceSemantics = GovernanceSemantics{
	Scope:   "dedicated write scope, separate from the MCP read scope — 'propose but not publish' must be expressible",
	Audit:   "every action attributable: which agent, authorized by which human, when, and what",
	Default: "dry-run; publication requires explicit human approval — an invariant, not a feature flag",
	V2Gate: "graduates only when the measurement layer exists (blast-radius scoring, CI-signal tiering, canary " +
		"stop rule) — without risk-tiering, human approval of bespoke diffs at scale is theater",
}

// Approval is the human authorization a governed publish would require — no
// agent self-approval.
type Approval struct {
	Approver string // human identity, required
	Token    string // out-of-band approval token
}

// Rollout is the staged-rollout configuration a governed publish would accept.
type Rollout struct {
	Mode              string  // enum: "staged"
	InitialBatch      int     // default 5
	HaltOnFailureRate float64 // default 0.2
}

// Input is the tool input (bc_request_publish) — the documented shape of a
// governed publish request.
type Input struct {
	SpecYAML string
	Approval Approval
	Rollout  Rollout
}

// GovernanceSemantics is the contract for what governed publication requires.
type GovernanceSemantics struct {
	Scope   string `json:"scope"`
	Audit   string `json:"audit"`
	Default string `json:"default"`
	V2Gate  string `json:"v2_gate"`
}

// Output is the tool output (bc_request_publish).
type Output struct {
	Status       string              `json:"status"`
	SemanticsDoc string              `json:"semantics_doc"`
	Governance   GovernanceSemantics `json:"governance_semantics"`
}

// Execute returns the governed refusal: NOT_IMPLEMENTED plus the semantics that
// publication would require. It is fixed regardless of input — the contract is
// the deliverable.
func Execute(_ context.Context, _ Input) (Output, error) {
	return Output{
		Status:       statusNotImplemented,
		SemanticsDoc: semanticsDoc,
		Governance:   governanceSemantics,
	}, nil
}
