// Package createspec is the composition use case: it assembles and validates the
// declarative Batch Changes spec from a conversation, and returns the canonical,
// human-reviewable YAML. It never executes — composition only. The structured
// spec is the guardrail, not the limitation.
package createspec

import (
	"context"
	"errors"
	"fmt"

	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/apperr"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/batchspec"
)

// ErrValidationFailed is the contract error (tool contract §3). Its message
// carries the field-level detail an agent needs to fix the spec — actionable and
// non-sensitive, so it is client-facing by design.
var ErrValidationFailed = apperr.New("VALIDATION_FAILED", "validation failed")

// Step is one deterministic container step (v1).
type Step struct {
	Run       string
	Container string
}

// ChangesetTemplate is the changeset (PR) each affected repo receives.
type ChangesetTemplate struct {
	Title         string
	Body          string
	Branch        string
	CommitMessage string
}

// Input is the tool input (bc_create_spec), in the use case's own terms — so the
// entrypoint depends only on this package, not on the batchspec domain.
type Input struct {
	Name        string
	Description string
	Query       string // on.repositoriesMatchingQuery, from bc_find_targets.normalized_query
	Steps       []Step
	Template    ChangesetTemplate
}

// Output is the tool output (bc_create_spec).
type Output struct {
	SpecYAML string   `json:"spec_yaml"`
	Valid    bool     `json:"valid"`
	Warnings []string `json:"warnings"`
}

// Execute composes and validates the spec, returning canonical YAML plus any
// non-fatal warnings. A spec that fails validation is rejected with
// ErrValidationFailed carrying the field-level issues.
//
// ctx is accepted for use-case signature consistency and propagation; this use
// case is pure composition and performs no I/O.
func Execute(_ context.Context, in Input) (Output, error) {
	steps := make([]batchspec.Step, len(in.Steps))
	for i, s := range in.Steps {
		steps[i] = batchspec.Step{Run: s.Run, Container: s.Container}
	}

	spec, err := batchspec.New(batchspec.Params{
		Name:        in.Name,
		Description: in.Description,
		Query:       in.Query,
		Steps:       steps,
		Template: batchspec.ChangesetTemplate{
			Title:  in.Template.Title,
			Body:   in.Template.Body,
			Branch: in.Template.Branch,
			Commit: batchspec.Commit{Message: in.Template.CommitMessage},
		},
	})
	if err != nil {
		var ve *batchspec.ValidationError
		if errors.As(err, &ve) {
			return Output{}, ErrValidationFailed.WithMessage(ve.Error())
		}
		return Output{}, fmt.Errorf("createspec: compose spec: %w", err)
	}

	yamlStr, err := spec.YAML()
	if err != nil {
		return Output{}, fmt.Errorf("createspec: render yaml: %w", err)
	}

	warnings := spec.Warnings()
	if warnings == nil {
		warnings = []string{}
	}
	return Output{
		SpecYAML: yamlStr,
		Valid:    true,
		Warnings: warnings,
	}, nil
}
