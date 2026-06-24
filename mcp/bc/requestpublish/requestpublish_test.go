package requestpublish

import (
	"context"
	"strings"
	"testing"
)

func TestExecute_AlwaysRefusesWithGovernance(t *testing.T) {
	// Even a fully-formed, approved request is refused in v1.
	in := Input{
		SpecYAML: "name: x\n",
		Approval: Approval{Approver: "alice@example.com", Token: "tok"},
		Rollout:  Rollout{Mode: "staged", InitialBatch: 5, HaltOnFailureRate: 0.2},
	}
	out, err := Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Status != "NOT_IMPLEMENTED" {
		t.Errorf("Status = %q, want NOT_IMPLEMENTED", out.Status)
	}
	if out.SemanticsDoc == "" {
		t.Error("SemanticsDoc must be populated")
	}
	for name, field := range map[string]string{
		"scope":   out.Governance.Scope,
		"audit":   out.Governance.Audit,
		"default": out.Governance.Default,
		"v2_gate": out.Governance.V2Gate,
	} {
		if field == "" {
			t.Errorf("governance_semantics.%s is empty", name)
		}
	}
	// The thesis must be stated: human approval is an invariant.
	if !strings.Contains(out.Governance.Default, "invariant") {
		t.Errorf("default semantics should state the invariant: %q", out.Governance.Default)
	}
}

func TestExecute_RefusesEmptyRequestToo(t *testing.T) {
	out, err := Execute(context.Background(), Input{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Status != "NOT_IMPLEMENTED" {
		t.Errorf("Status = %q, want NOT_IMPLEMENTED even for an empty request", out.Status)
	}
}
