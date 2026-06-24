// Package preview is the dry-run use case: it resolves what a batch spec would
// touch, without touching anything. Target resolution runs for real against the
// public API; step execution is an Enterprise surface and is out of scope — the
// boundary is stated in every response.
package preview

import (
	"context"
	"errors"
	"sort"

	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/apperr"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/batchspec"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/targeting"
)

// boundaryNote is returned verbatim on every preview: the public-API boundary,
// declared where the user sees it.
const boundaryNote = "target resolution runs against the public API; step execution requires Enterprise executors and is out of scope"

// DefaultPhaseSize is the changesets-per-phase assumption behind estimated_phases.
// It mirrors bc_request_publish's default staged-rollout initial_batch so a
// preview's phase estimate matches what governed publication would plan. It is a
// planning constant only — preview never executes a rollout. (Use cases don't
// import each other, so the value is restated here, not shared.)
const DefaultPhaseSize = 5

// Contract errors (tool contract §4). INVALID_SPEC carries client-facing detail
// (malformed or invalid spec) so the agent can fix it.
var (
	ErrInvalidSpec = apperr.New("INVALID_SPEC", "invalid spec")
	ErrUpstream    = apperr.New("UPSTREAM_UNAVAILABLE", "upstream unavailable")
)

// Resolution is what the resolver found for a query: the matched repositories
// and whether the underlying search was capped (so more may exist than listed).
type Resolution struct {
	Repos     []string
	Truncated bool
}

// Resolver is the port over code intelligence: given a normalized query, return
// the repositories it matches and whether the result was truncated. It takes a
// targeting.Query (not a raw string) so query construction — and its failure
// mode — stays in the use case, where a bad query is an INVALID_SPEC rather than
// a transport error. An adapter at the composition root binds this to sgclient.
type Resolver interface {
	ResolveRepos(ctx context.Context, query targeting.Query) (Resolution, error)
}

// Input is the tool input (bc_preview).
type Input struct {
	SpecYAML string
}

// Validation reports the spec's non-fatal concerns surfaced at preview time.
type Validation struct {
	Valid  bool     `json:"valid"`
	Issues []string `json:"issues"`
}

// Output is the tool output (bc_preview). Truncated reports that target
// resolution was capped by the search limit, so a real run may touch more repos
// than listed — estimated_changesets and estimated_phases are then lower bounds.
// EstimatedPhases is a planning estimate (changesets / DefaultPhaseSize, rounded
// up) of how many staged-rollout phases publication would take; preview never
// executes a rollout.
type Output struct {
	ResolvedRepos       []string   `json:"resolved_repos"`
	EstimatedChangesets int        `json:"estimated_changesets"`
	EstimatedPhases     int        `json:"estimated_phases"`
	Truncated           bool       `json:"truncated"`
	Validation          Validation `json:"validation"`
	BoundaryNote        string     `json:"boundary_note"`
}

// Execute parses the spec, resolves the repos its on-query would match (union
// across rules, deduplicated), and reports what the run would touch. It never
// executes a step.
func Execute(ctx context.Context, resolver Resolver, in Input) (Output, error) {
	spec, err := batchspec.Parse(in.SpecYAML)
	if err != nil {
		var ve *batchspec.ValidationError
		if errors.As(err, &ve) {
			return Output{}, ErrInvalidSpec.WithMessage(ve.Error())
		}
		// Malformed YAML: the precise parse error goes to the logs, not the client.
		return Output{}, ErrInvalidSpec.Wrap(err)
	}

	seen := make(map[string]struct{})
	truncated := false
	for _, raw := range spec.Queries() {
		q, err := targeting.NewQuery(raw)
		if err != nil {
			// A spec whose query can't be normalized is an invalid spec, not an
			// upstream failure.
			return Output{}, ErrInvalidSpec.WithMessage("on.repositoriesMatchingQuery: " + err.Error())
		}
		res, err := resolver.ResolveRepos(ctx, q)
		if err != nil {
			return Output{}, ErrUpstream.Wrap(err)
		}
		truncated = truncated || res.Truncated
		for _, r := range res.Repos {
			seen[r] = struct{}{}
		}
	}

	resolved := make([]string, 0, len(seen))
	for r := range seen {
		resolved = append(resolved, r)
	}
	sort.Strings(resolved)

	issues := spec.Warnings()
	if len(resolved) == 0 {
		issues = append(issues, "on.repositoriesMatchingQuery matched no repositories")
	}
	if truncated {
		issues = append(issues, "target resolution was truncated by the search limit; a real run may touch more repositories than listed")
	}
	if issues == nil {
		issues = []string{}
	}

	changesets := len(resolved) // one changeset (PR) per matched repo; a lower bound when truncated

	return Output{
		ResolvedRepos:       resolved,
		EstimatedChangesets: changesets,
		EstimatedPhases:     phases(changesets),
		Truncated:           truncated,
		Validation:          Validation{Valid: true, Issues: issues},
		BoundaryNote:        boundaryNote,
	}, nil
}

// phases is the ceiling division of changesets into DefaultPhaseSize-sized
// staged-rollout phases: how many batches publication would run.
func phases(changesets int) int {
	return (changesets + DefaultPhaseSize - 1) / DefaultPhaseSize
}
