// Package findtargets is the discovery use case: it turns search queries into
// batch-change targeting (the on: clause factory). A batch spec's on: clause is
// a list of rules, so this tool takes a list of queries, resolves them
// concurrently against code intelligence, and folds the matches into one union
// shaped for spec composition — counts and a few sample paths, never full
// content. It owns its narrow Searcher interface and is tested against a fake of
// it.
package findtargets

import (
	"context"

	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/apperr"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/fanout"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/targeting"
)

// max_repos bounds (tool contract §1).
const (
	DefaultMaxRepos = 25
	MaxAllowedRepos = 100
)

// Coded sentinel errors (tool contract §1). The code is the domain's promise;
// the handler reads it back, it does not assign it. errors.Is still matches.
var (
	ErrInvalidQuery = apperr.New("INVALID_QUERY", "invalid query")
	ErrUpstream     = apperr.New("UPSTREAM_UNAVAILABLE", "upstream unavailable")
)

// Searcher is what this use case needs from code intelligence, declared here on
// the consumer side (ISP). *sgclient.Client satisfies it. It is called
// concurrently — once per query — so an implementation must be safe for
// concurrent use.
type Searcher interface {
	Search(ctx context.Context, q targeting.Query, maxRepos int) (targeting.Targets, error)
}

// Input is the tool input (bc_find_targets). Queries is the list of on-rules to
// resolve; each is normalized and they are searched in parallel.
type Input struct {
	Queries  []string
	MaxRepos int
}

// Target is one resolved repository, summarized for composition.
type Target struct {
	Repo            string   `json:"repo"`
	OccurrenceCount int      `json:"occurrence_count"`
	SamplePaths     []string `json:"sample_paths"`
}

// Output is the tool output (bc_find_targets). NormalizedQueries are the
// deduplicated, normalized queries (each ready to drop into an
// on.repositoriesMatchingQuery rule); Targets is their merged union.
type Output struct {
	Targets           []Target `json:"targets"`
	NormalizedQueries []string `json:"normalized_queries"`
	TotalRepos        int      `json:"total_repos"`
	Truncated         bool     `json:"truncated"`
}

// Execute normalizes and deduplicates the queries, clamps max_repos to the
// contract bounds, runs the searches concurrently and folds the matches into one
// union shaped for spec composition.
func Execute(ctx context.Context, searcher Searcher, in Input) (Output, error) {
	queries, err := normalizeQueries(in.Queries)
	if err != nil {
		return Output{}, ErrInvalidQuery.Wrap(err)
	}

	maxRepos := in.MaxRepos
	if maxRepos <= 0 {
		maxRepos = DefaultMaxRepos
	}
	if maxRepos > MaxAllowedRepos {
		maxRepos = MaxAllowedRepos
	}

	// Each query is an independent network search, so they fan out and join
	// before the merge. fanout preserves input order, so the union below is
	// deterministic despite the concurrency.
	results, err := fanout.Run(ctx, queries, func(ctx context.Context, q targeting.Query) (targeting.Targets, error) {
		return searcher.Search(ctx, q, maxRepos)
	})
	if err != nil {
		return Output{}, ErrUpstream.Wrap(err)
	}

	return merge(queries, results, maxRepos), nil
}

// normalizeQueries validates each query and drops duplicates, preserving
// first-seen order. At least one valid query is required; an empty list, or a
// query that normalizes to nothing, is an invalid input — caught here, before
// any network call, so a bad request never reaches the upstream.
func normalizeQueries(raw []string) ([]targeting.Query, error) {
	if len(raw) == 0 {
		return nil, targeting.ErrEmptyQuery
	}

	seen := make(map[string]struct{}, len(raw))
	queries := make([]targeting.Query, 0, len(raw))
	for _, r := range raw {
		q, err := targeting.NewQuery(r)
		if err != nil {
			return nil, err
		}

		_, isDup := seen[q.String()]
		if isDup {
			continue // identical queries would only duplicate the search and the matches
		}

		seen[q.String()] = struct{}{}
		queries = append(queries, q)
	}
	return queries, nil
}

// merge folds the per-query results into one union, summarized for spec
// composition. A repository matched by several queries appears once: its
// occurrence counts add up and its sample paths combine (deduplicated, re-capped
// at targeting.MaxSamplePaths). First-seen order is preserved across queries
// (query order, then each query's ranking). The union is capped at maxRepos;
// anything dropped — or any truncated underlying search — marks the result
// Truncated, so the counts read as a lower bound.
func merge(queries []targeting.Query, results []targeting.Targets, maxRepos int) Output {
	order := make([]string, 0)
	byRepo := make(map[string]*Target)
	truncated := false

	for _, res := range results {
		if res.Truncated {
			truncated = true
		}

		for _, item := range res.Items {
			target, seen := byRepo[item.Repo]
			if !seen {
				target = &Target{Repo: item.Repo}
				byRepo[item.Repo] = target
				order = append(order, item.Repo)
			}

			target.OccurrenceCount += item.OccurrenceCount
			addSamplePaths(target, item.SamplePaths)
		}
	}

	totalRepos := len(order)
	if len(order) > maxRepos {
		order = order[:maxRepos]
		truncated = true
	}

	targets := make([]Target, len(order))
	for i, repo := range order {
		targets[i] = *byRepo[repo]
	}

	normalized := make([]string, len(queries))
	for i, q := range queries {
		normalized[i] = q.String()
	}

	return Output{
		Targets:           targets,
		NormalizedQueries: normalized,
		TotalRepos:        totalRepos,
		Truncated:         truncated,
	}
}

// addSamplePaths appends paths to the target, skipping ones it already carries
// and stopping at the targeting.MaxSamplePaths cap — re-applied because the
// union combines paths that were each capped per query.
func addSamplePaths(target *Target, paths []string) {
	for _, path := range paths {
		if len(target.SamplePaths) >= targeting.MaxSamplePaths {
			return
		}

		already := false
		for _, existing := range target.SamplePaths {
			if existing == path {
				already = true
				break
			}
		}
		if already {
			continue
		}

		target.SamplePaths = append(target.SamplePaths, path)
	}
}
