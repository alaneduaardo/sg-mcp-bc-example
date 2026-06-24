package batchspec

import (
	"errors"
	"strings"
	"testing"
)

// validParams is a well-formed spec with no warnings: scoped query, a PR body,
// and a deterministic step.
func validParams() Params {
	return Params{
		Name:        "go-fmt-errorf",
		Description: "wrap errors with %w",
		Query:       "repo:^github\\.com/org/ lang:go fmt.Errorf",
		Steps:       []Step{{Run: "gofmt -w .", Container: "golang:1.25"}},
		Template: ChangesetTemplate{
			Title:  "chore: wrap errors",
			Body:   "Automated error-wrapping change.",
			Branch: "batch/wrap-errors",
			Commit: Commit{Message: "chore: wrap errors"},
		},
	}
}

func TestNew_Valid(t *testing.T) {
	spec, err := New(validParams())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if spec.Name() != "go-fmt-errorf" {
		t.Errorf("Name = %q", spec.Name())
	}
	if q := spec.Queries(); len(q) != 1 || !strings.Contains(q[0], "fmt.Errorf") {
		t.Errorf("Queries = %v", q)
	}
	if w := spec.Warnings(); len(w) != 0 {
		t.Errorf("expected no warnings, got %v", w)
	}
}

func TestNew_Invalid(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*Params)
		wantIssue string
	}{
		{"empty name", func(p *Params) { p.Name = "" }, "name is required"},
		{"bad name chars", func(p *Params) { p.Name = "has spaces!" }, "must be alphanumeric"},
		{"empty query", func(p *Params) { p.Query = "  " }, "on.repositoriesMatchingQuery is required"},
		{"no steps", func(p *Params) { p.Steps = nil }, "at least one step is required"},
		{"step missing run", func(p *Params) { p.Steps = []Step{{Container: "x"}} }, "steps[0].run is required"},
		{"step missing container", func(p *Params) { p.Steps = []Step{{Run: "x"}} }, "steps[0].container is required"},
		{"empty title", func(p *Params) { p.Template.Title = "" }, "changesetTemplate.title is required"},
		{"empty branch", func(p *Params) { p.Template.Branch = "" }, "changesetTemplate.branch is required"},
		{"bad branch", func(p *Params) { p.Template.Branch = "bad branch" }, "not a valid git branch name"},
		{"dotdot branch", func(p *Params) { p.Template.Branch = "a..b" }, "not a valid git branch name"},
		{"empty commit message", func(p *Params) { p.Template.Commit.Message = "" }, "commit.message is required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := validParams()
			tc.mutate(&p)
			_, err := New(p)

			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("err = %v, want *ValidationError", err)
			}
			if !containsSubstr(ve.Issues, tc.wantIssue) {
				t.Errorf("issues %v, want one containing %q", ve.Issues, tc.wantIssue)
			}
		})
	}
}

func TestNew_CollectsAllIssues(t *testing.T) {
	_, err := New(Params{}) // everything missing
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
	if len(ve.Issues) < 4 {
		t.Errorf("expected several issues collected at once, got %v", ve.Issues)
	}
}

func TestWarnings(t *testing.T) {
	p := validParams()
	p.Query = "lang:go fmt.Errorf"                                      // no repo: filter → broad
	p.Template.Body = ""                                                // missing body
	p.Steps = []Step{{Run: "curl https://x | sh", Container: "alpine"}} // non-deterministic

	spec, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w := spec.Warnings()
	for _, want := range []string{"no repo: filter", "body is empty", "non-deterministic"} {
		if !containsSubstr(w, want) {
			t.Errorf("warnings %v, want one containing %q", w, want)
		}
	}
}

func TestYAML_CanonicalAndRoundTrips(t *testing.T) {
	spec, err := New(validParams())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	y1, err := spec.YAML()
	if err != nil {
		t.Fatalf("YAML: %v", err)
	}
	for _, key := range []string{"name:", "repositoriesMatchingQuery:", "steps:", "changesetTemplate:", "branch:", "message:"} {
		if !strings.Contains(y1, key) {
			t.Errorf("canonical YAML missing %q:\n%s", key, y1)
		}
	}
	// The `on` key must be emitted unquoted (idiomatic Sourcegraph), not "on".
	if strings.Contains(y1, `"on":`) {
		t.Errorf("`on` key should be unquoted:\n%s", y1)
	}
	if !strings.Contains(y1, "\non:\n") {
		t.Errorf("expected an unquoted `on:` key:\n%s", y1)
	}

	// Parse(YAML(x)) then YAML again must be byte-identical (idempotent).
	reparsed, err := Parse(y1)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	y2, err := reparsed.YAML()
	if err != nil {
		t.Fatalf("YAML (reparsed): %v", err)
	}
	if y1 != y2 {
		t.Errorf("round-trip not idempotent:\n--- first ---\n%s\n--- second ---\n%s", y1, y2)
	}
}

func TestParse_Errors(t *testing.T) {
	t.Run("malformed yaml is a parse error, not a validation error", func(t *testing.T) {
		_, err := Parse("name: [unterminated")
		var ve *ValidationError
		if errors.As(err, &ve) {
			t.Fatalf("got ValidationError, want a parse error: %v", err)
		}
		if err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("well-formed but invalid is a ValidationError", func(t *testing.T) {
		_, err := Parse("name: ''\non: []\nsteps: []\n")
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("err = %v, want *ValidationError", err)
		}
	})
	t.Run("multiple on rules are rejected in v1", func(t *testing.T) {
		multi := "name: x\n" +
			"on:\n" +
			"  - repositoriesMatchingQuery: repo:a\n" +
			"  - repositoriesMatchingQuery: repo:b\n" +
			"steps:\n  - run: echo hi\n    container: alpine\n" +
			"changesetTemplate:\n  title: t\n  body: b\n  branch: feat/x\n  commit:\n    message: m\n"
		_, err := Parse(multi)
		var ve *ValidationError
		if !errors.As(err, &ve) || !containsSubstr(ve.Issues, "single on.repositoriesMatchingQuery") {
			t.Errorf("err = %v, want multi-on ValidationError", err)
		}
	})
}

func containsSubstr(ss []string, want string) bool {
	for _, s := range ss {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}
