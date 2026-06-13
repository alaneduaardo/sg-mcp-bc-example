// Package targeting holds the value objects that connect discovery to
// composition: the normalized search Query (ready to drop into a batch spec's
// on.repositoriesMatchingQuery) and the Targets it resolves to.
//
// It imports nothing internal. The types here are the shared vocabulary that
// flows from bc_find_targets through bc_create_spec.
package targeting

import (
	"errors"
	"strings"
)

// ErrEmptyQuery is returned by NewQuery when the input normalizes to nothing.
var ErrEmptyQuery = errors.New("targeting: query is empty")

// Query is a normalized Sourcegraph search query. Normalization is total and
// idempotent so the same intent always produces the same string — important
// because the value lands verbatim in on.repositoriesMatchingQuery, where a
// stray newline or double space would be carried into the batch spec.
type Query struct {
	value string
}

// NewQuery normalizes raw (trim, collapse internal whitespace runs to a single
// space) and rejects input that is empty once normalized.
func NewQuery(raw string) (Query, error) {
	normalized := strings.Join(strings.Fields(raw), " ")
	if normalized == "" {
		return Query{}, ErrEmptyQuery
	}
	return Query{value: normalized}, nil
}

// String returns the normalized query text.
func (q Query) String() string { return q.value }

// IsZero reports whether q is the zero value (carries no query).
func (q Query) IsZero() bool { return q.value == "" }

// MaxSamplePaths bounds how many example paths a Target carries. Discovery is
// broad and cheap: it ships fragments (counts + a few sample paths), never full
// content — that is bc_inspect_target's job. The cap is a token-economics
// guardrail enforced at construction.
const MaxSamplePaths = 5

// Target is one repository matched by a Query, summarized for spec composition
// rather than browsing: how many times the pattern occurs and a few example
// paths to ground a follow-up inspection.
type Target struct {
	Repo            string
	OccurrenceCount int
	SamplePaths     []string
}

// NewTarget builds a Target, defensively copying samplePaths and capping them at
// MaxSamplePaths.
func NewTarget(repo string, occurrences int, samplePaths []string) Target {
	n := len(samplePaths)
	if n > MaxSamplePaths {
		n = MaxSamplePaths
	}
	paths := make([]string, n)
	copy(paths, samplePaths[:n])
	return Target{
		Repo:            repo,
		OccurrenceCount: occurrences,
		SamplePaths:     paths,
	}
}

// Targets is the resolved result of a discovery query: the per-repo summaries
// plus enough metadata for a caller to know whether it is seeing everything.
type Targets struct {
	Items      []Target
	TotalRepos int
	Truncated  bool
}
