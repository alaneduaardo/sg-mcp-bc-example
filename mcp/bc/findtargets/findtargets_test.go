package findtargets

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/apperr"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/targeting"
)

// fakeSearcher is a test double for the use case's own Searcher interface — the
// proof that the interface earns its keep: the use case is exercised with no
// network. It is called concurrently (one goroutine per query), so its bookkeeping
// is guarded by a mutex and a per-query canned result keeps the fan-out
// deterministic.
type fakeSearcher struct {
	mu      sync.Mutex
	calls   []string                     // queries seen (order is nondeterministic under fan-out)
	gotMax  int                          // max_repos passed to the last call
	byQuery map[string]targeting.Targets // canned result per normalized query
	ret     targeting.Targets            // result when byQuery has no entry for the query
	err     error
}

func (f *fakeSearcher) Search(_ context.Context, q targeting.Query, maxRepos int) (targeting.Targets, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, q.String())
	f.gotMax = maxRepos
	if f.err != nil {
		return targeting.Targets{}, f.err
	}
	if f.byQuery != nil {
		return f.byQuery[q.String()], nil
	}
	return f.ret, nil
}

func (f *fakeSearcher) sawQuery(want string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, q := range f.calls {
		if q == want {
			return true
		}
	}
	return false
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
	out, err := Execute(context.Background(), f, Input{Queries: []string{"  needle   here "}, MaxRepos: 10})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !f.sawQuery("needle here") {
		t.Errorf("searcher saw %v, want normalized %q", f.calls, "needle here")
	}
	if f.gotMax != 10 {
		t.Errorf("searcher saw max %d, want 10", f.gotMax)
	}
	if len(out.NormalizedQueries) != 1 || out.NormalizedQueries[0] != "needle here" {
		t.Errorf("NormalizedQueries = %v", out.NormalizedQueries)
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

// TestExecute_MultiQueryMerge exercises the fan-out: two queries resolve
// independently and a repo matched by both appears once, with summed occurrences
// and combined, deduplicated sample paths.
func TestExecute_MultiQueryMerge(t *testing.T) {
	f := &fakeSearcher{byQuery: map[string]targeting.Targets{
		"alpha": {
			Items: []targeting.Target{
				targeting.NewTarget("github.com/a/one", 2, []string{"x.go", "y.go"}),
				targeting.NewTarget("github.com/b/two", 1, []string{"z.go"}),
			},
			TotalRepos: 2,
		},
		"beta": {
			Items: []targeting.Target{
				targeting.NewTarget("github.com/a/one", 4, []string{"y.go", "w.go"}), // overlaps a/one
				targeting.NewTarget("github.com/c/three", 5, []string{"q.go"}),
			},
			TotalRepos: 2,
		},
	}}

	out, err := Execute(context.Background(), f, Input{Queries: []string{"alpha", "beta"}, MaxRepos: 10})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(out.NormalizedQueries) != 2 {
		t.Fatalf("NormalizedQueries = %v, want both queries", out.NormalizedQueries)
	}

	// First-seen order: alpha's repos first (a/one, b/two), then beta's new c/three.
	wantRepos := []string{"github.com/a/one", "github.com/b/two", "github.com/c/three"}
	if len(out.Targets) != len(wantRepos) {
		t.Fatalf("Targets = %+v, want %d repos", out.Targets, len(wantRepos))
	}
	for i, want := range wantRepos {
		if out.Targets[i].Repo != want {
			t.Errorf("Targets[%d].Repo = %q, want %q", i, out.Targets[i].Repo, want)
		}
	}
	if out.TotalRepos != 3 {
		t.Errorf("TotalRepos = %d, want 3 distinct", out.TotalRepos)
	}

	one := out.Targets[0]
	if one.OccurrenceCount != 6 { // 2 + 4
		t.Errorf("a/one OccurrenceCount = %d, want 6", one.OccurrenceCount)
	}
	if len(one.SamplePaths) != 3 { // x.go, y.go, w.go — y.go deduped
		t.Errorf("a/one SamplePaths = %v, want 3 deduped", one.SamplePaths)
	}
}

// TestExecute_DuplicateQueriesSearchedOnce: identical queries collapse before the
// fan-out, so the upstream is hit once.
func TestExecute_DuplicateQueriesSearchedOnce(t *testing.T) {
	f := &fakeSearcher{ret: sampleTargets()}
	out, err := Execute(context.Background(), f, Input{Queries: []string{"needle here", "  needle   here "}, MaxRepos: 10})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(f.calls) != 1 {
		t.Errorf("searcher called %d times, want 1 (duplicates collapse)", len(f.calls))
	}
	if len(out.NormalizedQueries) != 1 {
		t.Errorf("NormalizedQueries = %v, want 1 after dedup", out.NormalizedQueries)
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
			if _, err := Execute(context.Background(), f, Input{Queries: []string{"q"}, MaxRepos: tc.in}); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if f.gotMax != tc.wantMax {
				t.Errorf("clamped max = %d, want %d", f.gotMax, tc.wantMax)
			}
		})
	}
}

func TestExecute_InvalidQuery(t *testing.T) {
	tests := []struct {
		name    string
		queries []string
	}{
		{name: "blank query", queries: []string{"   "}},
		{name: "one blank among valid", queries: []string{"ok", "   "}},
		{name: "no queries at all", queries: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeSearcher{ret: sampleTargets()}
			_, err := Execute(context.Background(), f, Input{Queries: tc.queries, MaxRepos: 10})
			if !errors.Is(err, ErrInvalidQuery) {
				t.Fatalf("err = %v, want ErrInvalidQuery", err)
			}
			if code, _ := apperr.Code(err); code != "INVALID_QUERY" {
				t.Errorf("contract code = %q, want INVALID_QUERY", code)
			}
			if len(f.calls) != 0 {
				t.Error("searcher should not be called when a query is invalid")
			}
		})
	}
}

func TestExecute_UpstreamError(t *testing.T) {
	sentinel := errors.New("network exploded")
	f := &fakeSearcher{err: sentinel}
	_, err := Execute(context.Background(), f, Input{Queries: []string{"q"}, MaxRepos: 10})
	if !errors.Is(err, ErrUpstream) {
		t.Errorf("err = %v, want ErrUpstream", err)
	}
	if code, _ := apperr.Code(err); code != "UPSTREAM_UNAVAILABLE" {
		t.Errorf("contract code = %q, want UPSTREAM_UNAVAILABLE", code)
	}
}
