// Package batchspec models the Batch Changes spec — the product's central
// artifact and the unit of human review. The spec IS YAML (the contract
// format), so (de)serialization lives here with the aggregate, as domain
// representation rather than presentation.
//
// Invariants are guarded at construction: an invalid Spec cannot be created
// (New returns a *ValidationError carrying field-level detail). Warnings
// surfaces non-fatal concerns — a broad query, a missing PR body, a step that
// looks non-deterministic — which the v1 contract reports without rejecting.
//
// It imports nothing internal: it is pure domain. Use cases translate its
// errors into their own contract codes.
package batchspec

import (
	"fmt"
	"regexp"
	"strings"
)

// Field rules. Names and branches are constrained enough to be safe to drop into
// YAML and a git ref without surprises. branchRe is a deliberately pragmatic
// subset of git's ref rules (it blocks whitespace, control chars and the
// dangerous metacharacters; it does not implement the full check-ref-format
// ruleset) — a conscious POC trade, since the full rules are ~40 cases.
var (
	nameRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	branchRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
)

// suspiciousStep flags run commands that read as non-deterministic or networked
// — exactly what v1's "deterministic steps only" rule is meant to keep out.
// This is a substring heuristic, not a guarantee (step determinism is
// undecidable in general); it only raises a non-fatal warning. A POC trade.
var suspiciousStep = []string{
	"curl ", "wget ", "git clone", "$RANDOM", "uuidgen", "$(date", "`date",
	"npm install", "pip install", "go get ", "apt-get", "apk add",
}

// Step is one deterministic transformation: a container command.
type Step struct {
	Run       string
	Container string
}

// Commit is the commit metadata a changeset carries.
type Commit struct {
	Message string
}

// ChangesetTemplate describes the changeset (PR) each affected repo receives.
type ChangesetTemplate struct {
	Title  string
	Body   string
	Branch string
	Commit Commit
}

// Params is the raw input to New — the conversation's proposal before it is
// validated into a Spec.
type Params struct {
	Name        string
	Description string
	Query       string // on.repositoriesMatchingQuery
	Steps       []Step
	Template    ChangesetTemplate
}

// Spec is the validated Batch Changes aggregate. Fields are unexported: the only
// way to obtain a Spec is through New or Parse, both of which enforce invariants.
type Spec struct {
	name        string
	description string
	queries     []string // on[].repositoriesMatchingQuery
	steps       []Step
	template    ChangesetTemplate
}

// ValidationError is the field-level detail behind a rejected spec. Use cases map
// it to their contract code (VALIDATION_FAILED / INVALID_SPEC).
type ValidationError struct {
	Issues []string
}

func (e *ValidationError) Error() string {
	return "invalid batch spec: " + strings.Join(e.Issues, "; ")
}

// New validates p and returns the aggregate, or a *ValidationError listing every
// field problem at once (so a caller fixes them in one pass, not one per round).
func New(p Params) (Spec, error) {
	var issues []string

	name := strings.TrimSpace(p.Name)
	switch {
	case name == "":
		issues = append(issues, "name is required")
	case !nameRe.MatchString(name):
		issues = append(issues, fmt.Sprintf("name %q must be alphanumeric with . _ - separators", name))
	}

	query := strings.TrimSpace(p.Query)
	if query == "" {
		issues = append(issues, "on.repositoriesMatchingQuery is required")
	}

	if len(p.Steps) == 0 {
		issues = append(issues, "at least one step is required")
	}
	steps := make([]Step, 0, len(p.Steps))
	for i, s := range p.Steps {
		run, container := strings.TrimSpace(s.Run), strings.TrimSpace(s.Container)
		if run == "" {
			issues = append(issues, fmt.Sprintf("steps[%d].run is required", i))
		}
		if container == "" {
			issues = append(issues, fmt.Sprintf("steps[%d].container is required", i))
		}
		steps = append(steps, Step{Run: run, Container: container})
	}

	tmpl := p.Template
	tmpl.Title = strings.TrimSpace(tmpl.Title)
	tmpl.Branch = strings.TrimSpace(tmpl.Branch)
	tmpl.Commit.Message = strings.TrimSpace(tmpl.Commit.Message)
	if tmpl.Title == "" {
		issues = append(issues, "changesetTemplate.title is required")
	}
	switch {
	case tmpl.Branch == "":
		issues = append(issues, "changesetTemplate.branch is required")
	case !branchRe.MatchString(tmpl.Branch) || strings.Contains(tmpl.Branch, "..") || strings.HasSuffix(tmpl.Branch, "/"):
		issues = append(issues, fmt.Sprintf("changesetTemplate.branch %q is not a valid git branch name", tmpl.Branch))
	}
	if tmpl.Commit.Message == "" {
		issues = append(issues, "changesetTemplate.commit.message is required")
	}

	if len(issues) > 0 {
		return Spec{}, &ValidationError{Issues: issues}
	}

	return Spec{
		name:        name,
		description: strings.TrimSpace(p.Description),
		queries:     []string{query},
		steps:       steps,
		template:    tmpl,
	}, nil
}

// Name returns the spec name.
func (s Spec) Name() string { return s.name }

// Queries returns the on.repositoriesMatchingQuery values — what a preview
// resolves against to find affected repos.
func (s Spec) Queries() []string {
	out := make([]string, len(s.queries))
	copy(out, s.queries)
	return out
}

// Warnings reports non-fatal concerns. These never reject a spec; they make the
// human review sharper.
func (s Spec) Warnings() []string {
	var w []string
	for _, q := range s.queries {
		if !strings.Contains(q, "repo:") && !strings.Contains(q, "repository:") {
			w = append(w, fmt.Sprintf("query %q has no repo: filter and may match a very large number of repositories", q))
		}
	}
	if strings.TrimSpace(s.template.Body) == "" {
		w = append(w, "changesetTemplate.body is empty; reviewers will see no PR description")
	}
	for i, step := range s.steps {
		for _, bad := range suspiciousStep {
			if strings.Contains(step.Run, bad) {
				w = append(w, fmt.Sprintf("steps[%d] runs %q, which looks non-deterministic; v1 expects deterministic steps", i, bad))
				break
			}
		}
	}
	return w
}
