package sgclient

import (
	"context"
	"fmt"
	"strings"

	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/targeting"
)

// searchQuery asks for file matches and the repository/path of each. Shaped to
// discovery's need: enough to aggregate per-repo counts and a few sample paths,
// nothing more. Full content is bc_inspect_target's job (FetchFile).
const searchQuery = `query BatchTargets($query: String!) {
  search(query: $query, version: V3) {
    results {
      matchCount
      limitHit
      results {
        __typename
        ... on FileMatch {
          repository { name }
          file { path }
        }
      }
    }
  }
}`

type searchData struct {
	Search struct {
		Results struct {
			MatchCount int  `json:"matchCount"`
			LimitHit   bool `json:"limitHit"`
			Results    []struct {
				Typename   string `json:"__typename"`
				Repository struct {
					Name string `json:"name"`
				} `json:"repository"`
				File struct {
					Path string `json:"path"`
				} `json:"file"`
			} `json:"results"`
		} `json:"results"`
	} `json:"search"`
}

// Search runs q against the instance and folds the file matches into per-repo
// targeting.Targets, preserving Sourcegraph's result ranking (first-seen order).
// maxRepos caps how many repos are returned; if more distinct repos matched, or
// the search itself hit its limit, the result is marked Truncated. maxRepos <= 0
// means no cap.
//
// This method satisfies the consumer-side Searcher interface that the
// findtargets use case declares.
func (c *Client) Search(ctx context.Context, q targeting.Query, maxRepos int) (targeting.Targets, error) {
	return c.search(ctx, q.String(), maxRepos)
}

// SearchAll runs q with count:all so resolution is complete rather than bounded
// by Sourcegraph's default result limit — preview needs an accurate repo set and
// changeset estimate, not a ranked sample. Even count:all has an instance hard
// cap, so the result can still come back Truncated; the count remains a lower
// bound in that case. count:all is not appended if q already carries a count:
// filter (the caller's intent wins).
func (c *Client) SearchAll(ctx context.Context, q targeting.Query) (targeting.Targets, error) {
	query := q.String()
	if !strings.Contains(query, "count:") {
		query += " count:all"
	}
	return c.search(ctx, query, 0)
}

// search is the shared resolution path: run the GraphQL query, aggregate file
// matches into per-repo Targets, and apply the optional repo cap.
func (c *Client) search(ctx context.Context, query string, maxRepos int) (targeting.Targets, error) {
	var data searchData
	if err := c.do(ctx, searchQuery, map[string]any{"query": query}, &data); err != nil {
		return targeting.Targets{}, fmt.Errorf("sgclient: search %q: %w", query, err)
	}

	// Aggregate file matches into per-repo accumulators, keeping first-seen
	// order so the ranking survives.
	type acc struct {
		count int
		paths []string
	}
	order := make([]string, 0)
	byRepo := make(map[string]*acc)
	for _, r := range data.Search.Results.Results {
		if r.Typename != "FileMatch" {
			continue
		}
		repo := r.Repository.Name
		a, ok := byRepo[repo]
		if !ok {
			a = &acc{}
			byRepo[repo] = a
			order = append(order, repo)
		}
		a.count++
		if len(a.paths) < targeting.MaxSamplePaths && r.File.Path != "" {
			a.paths = append(a.paths, r.File.Path)
		}
	}

	totalRepos := len(order)
	truncated := data.Search.Results.LimitHit
	if maxRepos > 0 && totalRepos > maxRepos {
		order = order[:maxRepos]
		truncated = true
	}

	items := make([]targeting.Target, len(order))
	for i, repo := range order {
		a := byRepo[repo]
		items[i] = targeting.NewTarget(repo, a.count, a.paths)
	}

	return targeting.Targets{
		Items:      items,
		TotalRepos: totalRepos,
		Truncated:  truncated,
	}, nil
}
