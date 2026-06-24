package createspec

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/apperr"
)

func validInput() Input {
	return Input{
		Name:        "go-fmt-errorf",
		Description: "wrap errors with %w",
		Query:       "repo:^github\\.com/org/ lang:go fmt.Errorf",
		Steps:       []Step{{Run: "gofmt -w .", Container: "golang:1.25"}},
		Template: ChangesetTemplate{
			Title:         "chore: wrap errors",
			Body:          "Automated change.",
			Branch:        "batch/wrap-errors",
			CommitMessage: "chore: wrap errors",
		},
	}
}

func TestExecute_Valid(t *testing.T) {
	out, err := Execute(context.Background(), validInput())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !out.Valid {
		t.Error("Valid = false, want true")
	}
	if !strings.Contains(out.SpecYAML, "changesetTemplate:") || !strings.Contains(out.SpecYAML, "repositoriesMatchingQuery:") {
		t.Errorf("spec_yaml not canonical:\n%s", out.SpecYAML)
	}
	if out.Warnings == nil {
		t.Error("Warnings must be a non-nil slice (so it serializes as [] not null)")
	}
	if len(out.Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", out.Warnings)
	}
}

func TestExecute_Warnings(t *testing.T) {
	in := validInput()
	in.Query = "lang:go fmt.Errorf" // no repo: filter → broad
	in.Template.Body = ""           // missing body

	out, err := Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !out.Valid {
		t.Error("a spec with only warnings is still valid")
	}
	if len(out.Warnings) < 2 {
		t.Errorf("expected broad-query + missing-body warnings, got %v", out.Warnings)
	}
}

func TestExecute_ValidationFailed(t *testing.T) {
	in := validInput()
	in.Name = ""             // hard failure
	in.Template.Branch = "x" // valid, isolate the name failure... actually keep one issue minimal

	_, err := Execute(context.Background(), in)
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("err = %v, want ErrValidationFailed", err)
	}
	if code, _ := apperr.Code(err); code != "VALIDATION_FAILED" {
		t.Errorf("code = %q, want VALIDATION_FAILED", code)
	}
	// Field-level detail must reach the client message (the agent needs it).
	if ae := apperr.As(err); ae == nil || !strings.Contains(ae.Message(), "name is required") {
		t.Errorf("client message missing field detail: %v", err)
	}
}
