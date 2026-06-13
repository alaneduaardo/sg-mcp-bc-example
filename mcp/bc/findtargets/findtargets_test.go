package findtargets

import (
	"context"
	"errors"
	"testing"

	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/apperr"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/targeting"
)

// fakeSearcher is a test double for the use case's own Searcher interface — the
// proof that the interface earns its keep: the use case is exercised with no
// network.
type fakeSearcher struct {
	gotQuery string
	gotMax   int
	ret      targeting.Targets
	err      error
}

func (f *fakeSearcher) Search(_ context.Context, q targeting.Query, maxRepos int) (targeting.Targets, error) {
	f.gotQuery = q.String()
	f.gotMax = maxRepos
	return f.ret, f.err
}

func sampleTargets() targeting.Targets {
	return targeting.Targets{
		Items: []targeting.Target{
			targeting.NewTarget("github.com/a/one", 3, []string{"x.go", "y.go"}),
			targeting.NewTarget("github.com/b/two", 1, []string{"z.go"}),
		},
		TotalRepos: 2,
		Truncated:  false,
	}
}

func TestExecute_HappyPath(t *testing.T) {
	f := &fakeSearcher{ret: sampleTargets()}
	out, err := Execute(context.Background(), f, Input{Query: "  needle   here ", MaxRepos: 10})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if f.gotQuery != "needle here" {
		t.Errorf("searcher saw query %q, want normalized %q", f.gotQuery, "needle here")
	}
	if f.gotMax != 10 {
		t.Errorf("searcher saw max %d, want 10", f.gotMax)
	}
	if out.NormalizedQuery != "needle here" {
		t.Errorf("NormalizedQuery = %q", out.NormalizedQuery)
	}
	if out.TotalRepos != 2 || out.Truncated {
		t.Errorf("TotalRepos/Truncated = %d/%t", out.TotalRepos, out.Truncated)
	}
	if len(out.Targets) != 2 {
		t.Fatalf("Targets len = %d, want 2", len(out.Targets))
	}
	if out.Targets[0].Repo != "github.com/a/one" || out.Targets[0].OccurrenceCount != 3 {
		t.Errorf("Targets[0] = %+v", out.Targets[0])
	}
	if len(out.Targets[0].SamplePaths) != 2 {
		t.Errorf("Targets[0].SamplePaths = %v", out.Targets[0].SamplePaths)
	}
}

func TestExecute_MaxReposClamping(t *testing.T) {
	tests := []struct {
		name    string
		in      int
		wantMax int
	}{
		{name: "zero uses default", in: 0, wantMax: DefaultMaxRepos},
		{name: "negative uses default", in: -5, wantMax: DefaultMaxRepos},
		{name: "within range passes through", in: 42, wantMax: 42},
		{name: "over cap clamps", in: 1000, wantMax: MaxAllowedRepos},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeSearcher{ret: sampleTargets()}
			if _, err := Execute(context.Background(), f, Input{Query: "q", MaxRepos: tc.in}); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if f.gotMax != tc.wantMax {
				t.Errorf("clamped max = %d, want %d", f.gotMax, tc.wantMax)
			}
		})
	}
}

func TestExecute_InvalidQuery(t *testing.T) {
	f := &fakeSearcher{ret: sampleTargets()}
	_, err := Execute(context.Background(), f, Input{Query: "   ", MaxRepos: 10})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("err = %v, want ErrInvalidQuery", err)
	}
	if code, _ := apperr.Code(err); code != "INVALID_QUERY" {
		t.Errorf("contract code = %q, want INVALID_QUERY", code)
	}
	if f.gotMax != 0 {
		t.Error("searcher should not be called when the query is invalid")
	}
}

func TestExecute_UpstreamError(t *testing.T) {
	sentinel := errors.New("network exploded")
	f := &fakeSearcher{err: sentinel}
	_, err := Execute(context.Background(), f, Input{Query: "q", MaxRepos: 10})
	if !errors.Is(err, ErrUpstream) {
		t.Errorf("err = %v, want ErrUpstream", err)
	}
	if code, _ := apperr.Code(err); code != "UPSTREAM_UNAVAILABLE" {
		t.Errorf("contract code = %q, want UPSTREAM_UNAVAILABLE", code)
	}
}
