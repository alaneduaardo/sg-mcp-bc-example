package sgclient

import (
	"context"
	"fmt"

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
	var data searchData
	if err := c.do(ctx, searchQuery, map[string]any{"query": q.String()}, &data); err != nil {
		return targeting.Targets{}, fmt.Errorf("sgclient: search %q: %w", q.String(), err)
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
