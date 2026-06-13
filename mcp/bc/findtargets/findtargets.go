// Package findtargets is the discovery use case: it turns a search query into
// batch-change targeting (the on: clause factory). Output is shaped for spec
// composition, not browsing — counts and a few sample paths, never full
// content. It owns its narrow Searcher interface and is tested against a fake of
// it.
package findtargets

import (
	"context"
	"errors"
	"fmt"

	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/targeting"
)

// max_repos bounds (tool contract §1).
const (
	DefaultMaxRepos = 25
	MaxAllowedRepos = 100
)

// Sentinel errors callers (the MCP handler) branch on to pick a protocol code.
var (
	ErrInvalidQuery = errors.New("findtargets: invalid query")
	ErrUpstream     = errors.New("findtargets: upstream unavailable")
)

// Searcher is what this use case needs from code intelligence, declared here on
// the consumer side (ISP). *sgclient.Client satisfies it.
type Searcher interface {
	Search(ctx context.Context, q targeting.Query, maxRepos int) (targeting.Targets, error)
}

// Input is the tool input (bc_find_targets).
type Input struct {
	Query    string
	MaxRepos int
}

// Target is one resolved repository, summarized for composition.
type Target struct {
	Repo            string   `json:"repo"`
	OccurrenceCount int      `json:"occurrence_count"`
	SamplePaths     []string `json:"sample_paths"`
}

// Output is the tool output (bc_find_targets).
type Output struct {
	Targets         []Target `json:"targets"`
	NormalizedQuery string   `json:"normalized_query"`
	TotalRepos      int      `json:"total_repos"`
	Truncated       bool     `json:"truncated"`
}

// Execute normalizes the query, clamps max_repos to the contract bounds, runs
// the search and shapes the result for spec composition.
func Execute(ctx context.Context, searcher Searcher, in Input) (Output, error) {
	q, err := targeting.NewQuery(in.Query)
	if err != nil {
		return Output{}, fmt.Errorf("%w: %v", ErrInvalidQuery, err)
	}

	maxRepos := in.MaxRepos
	if maxRepos <= 0 {
		maxRepos = DefaultMaxRepos
	}
	if maxRepos > MaxAllowedRepos {
		maxRepos = MaxAllowedRepos
	}

	targets, err := searcher.Search(ctx, q, maxRepos)
	if err != nil {
		return Output{}, fmt.Errorf("%w: %v", ErrUpstream, err)
	}

	out := Output{
		NormalizedQuery: q.String(),
		TotalRepos:      targets.TotalRepos,
		Truncated:       targets.Truncated,
	}
	for _, t := range targets.Items {
		out.Targets = append(out.Targets, Target{
			Repo:            t.Repo,
			OccurrenceCount: t.OccurrenceCount,
			SamplePaths:     t.SamplePaths,
		})
	}
	return out, nil
}
