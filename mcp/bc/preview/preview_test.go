package preview

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/apperr"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/targeting"
)

const validSpecYAML = `name: wrap-errors
on:
  - repositoriesMatchingQuery: repo:foo lang:go fmt.Errorf
steps:
  - run: gofmt -w .
    container: golang:1.25
changesetTemplate:
  title: chore
  body: automated
  branch: batch/x
  commit:
    message: chore
`

type fakeResolver struct {
	gotQuery  string
	repos     []string
	truncated bool
	err       error
}

func (f *fakeResolver) ResolveRepos(_ context.Context, query targeting.Query) (Resolution, error) {
	f.gotQuery = query.String()
	return Resolution{Repos: f.repos, Truncated: f.truncated}, f.err
}

func TestExecute_ResolvesAndDedup(t *testing.T) {
	f := &fakeResolver{repos: []string{"github.com/b/two", "github.com/a/one", "github.com/b/two"}}
	out, err := Execute(context.Background(), f, Input{SpecYAML: validSpecYAML})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if f.gotQuery != "repo:foo lang:go fmt.Errorf" {
		t.Errorf("resolver got query %q", f.gotQuery)
	}
	want := []string{"github.com/a/one", "github.com/b/two"} // deduped + sorted
	if strings.Join(out.ResolvedRepos, ",") != strings.Join(want, ",") {
		t.Errorf("ResolvedRepos = %v, want %v", out.ResolvedRepos, want)
	}
	if out.EstimatedChangesets != 2 {
		t.Errorf("EstimatedChangesets = %d, want 2", out.EstimatedChangesets)
	}
	if !out.Validation.Valid {
		t.Error("Validation.Valid = false, want true")
	}
	if out.Truncated {
		t.Error("Truncated = true, want false for an un-capped resolution")
	}
	if out.BoundaryNote == "" {
		t.Error("BoundaryNote must be set on every preview")
	}
}

func TestExecute_TruncatedIsSurfaced(t *testing.T) {
	f := &fakeResolver{repos: []string{"github.com/a/one"}, truncated: true}
	out, err := Execute(context.Background(), f, Input{SpecYAML: validSpecYAML})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !out.Truncated {
		t.Error("Truncated = false, want true when the resolver caps results")
	}
	found := false
	for _, iss := range out.Validation.Issues {
		if strings.Contains(iss, "truncated") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a truncation issue, got %v", out.Validation.Issues)
	}
}

func TestExecute_NoMatchesIsAnIssue(t *testing.T) {
	f := &fakeResolver{repos: nil}
	out, err := Execute(context.Background(), f, Input{SpecYAML: validSpecYAML})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.EstimatedChangesets != 0 {
		t.Errorf("EstimatedChangesets = %d, want 0", out.EstimatedChangesets)
	}
	found := false
	for _, iss := range out.Validation.Issues {
		if strings.Contains(iss, "matched no repositories") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a 'matched no repositories' issue, got %v", out.Validation.Issues)
	}
}

func TestExecute_InvalidSpec(t *testing.T) {
	t.Run("validation error surfaces field detail", func(t *testing.T) {
		bad := strings.Replace(validSpecYAML, "name: wrap-errors", "name: ''", 1)
		_, err := Execute(context.Background(), &fakeResolver{}, Input{SpecYAML: bad})
		if !errors.Is(err, ErrInvalidSpec) {
			t.Fatalf("err = %v, want ErrInvalidSpec", err)
		}
		if ae := apperr.As(err); ae == nil || !strings.Contains(ae.Message(), "name is required") {
			t.Errorf("client message missing field detail: %v", err)
		}
	})
	t.Run("malformed yaml does not leak internals to the client", func(t *testing.T) {
		_, err := Execute(context.Background(), &fakeResolver{}, Input{SpecYAML: "name: [oops"})
		if code, _ := apperr.Code(err); code != "INVALID_SPEC" {
			t.Fatalf("code = %q, want INVALID_SPEC", code)
		}
		if ae := apperr.As(err); ae == nil || ae.Message() != "invalid spec" {
			t.Errorf("client message = %q, want generic 'invalid spec'", apperr.As(err).Message())
		}
	})
}

func TestExecute_UpstreamError(t *testing.T) {
	f := &fakeResolver{err: errors.New("connection refused")}
	_, err := Execute(context.Background(), f, Input{SpecYAML: validSpecYAML})
	if !errors.Is(err, ErrUpstream) {
		t.Errorf("err = %v, want ErrUpstream", err)
	}
}
