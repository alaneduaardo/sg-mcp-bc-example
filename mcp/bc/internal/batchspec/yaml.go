package batchspec

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// dto is the on-the-wire shape of a Batch Changes spec. Field order here defines
// the canonical YAML ordering; yaml tags match Sourcegraph's batch spec schema.
type dto struct {
	Name              string               `yaml:"name"`
	Description       string               `yaml:"description,omitempty"`
	On                []onRule             `yaml:"on"`
	Steps             []stepDTO            `yaml:"steps"`
	ChangesetTemplate changesetTemplateDTO `yaml:"changesetTemplate"`
}

type onRule struct {
	RepositoriesMatchingQuery string `yaml:"repositoriesMatchingQuery"`
}

type stepDTO struct {
	Run       string `yaml:"run"`
	Container string `yaml:"container"`
}

type changesetTemplateDTO struct {
	Title  string    `yaml:"title"`
	Body   string    `yaml:"body"`
	Branch string    `yaml:"branch"`
	Commit commitDTO `yaml:"commit"`
}

type commitDTO struct {
	Message string `yaml:"message"`
}

func (s Spec) toDTO() dto {
	d := dto{
		Name:        s.name,
		Description: s.description,
		ChangesetTemplate: changesetTemplateDTO{
			Title:  s.template.Title,
			Body:   s.template.Body,
			Branch: s.template.Branch,
			Commit: commitDTO{Message: s.template.Commit.Message},
		},
	}
	for _, q := range s.queries {
		d.On = append(d.On, onRule{RepositoriesMatchingQuery: q})
	}
	for _, step := range s.steps {
		d.Steps = append(d.Steps, stepDTO(step)) // identical shape; conversion is compile-checked
	}
	return d
}

// YAML renders the spec as canonical Batch Changes YAML — the human-reviewable
// artifact and the contract format.
func (s Spec) YAML() (string, error) {
	var node yaml.Node
	if err := node.Encode(s.toDTO()); err != nil {
		return "", fmt.Errorf("batchspec: encode yaml: %w", err)
	}
	// yaml.v3 quotes the `on` key because `on` is a YAML 1.1 boolean literal;
	// force it back to a plain string so the output matches Sourcegraph's
	// idiomatic unquoted `on:`. (Parsing accepts both forms.)
	forcePlainKey(&node, "on")

	out, err := yaml.Marshal(&node)
	if err != nil {
		return "", fmt.Errorf("batchspec: marshal yaml: %w", err)
	}
	return string(out), nil
}

// forcePlainKey re-tags a top-level mapping key as a plain string scalar, so the
// emitter renders it unquoted even when it would otherwise be a reserved word.
func forcePlainKey(node *yaml.Node, key string) {
	m := node
	if m.Kind == yaml.DocumentNode && len(m.Content) > 0 {
		m = m.Content[0]
	}
	if m.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i].Tag = "!!str"
			m.Content[i].Style = 0 // plain
		}
	}
}

// Parse deserializes Batch Changes YAML into a validated Spec. Malformed YAML is
// a parse error; well-formed-but-invalid content returns a *ValidationError —
// the same field-level detail New produces.
//
// Parsing is intentionally lenient about unknown keys (yaml.v3 ignores them):
// a misspelled *required* key leaves its field empty and is caught by New's
// validation, and unknown optional keys are harmless. A POC trade — sharper
// "unknown field" diagnostics (yaml.Decoder.KnownFields) are deferred.
func Parse(s string) (Spec, error) {
	var d dto
	if err := yaml.Unmarshal([]byte(s), &d); err != nil {
		return Spec{}, fmt.Errorf("batchspec: parse yaml: %w", err)
	}

	if len(d.On) > 1 {
		return Spec{}, &ValidationError{Issues: []string{"only a single on.repositoriesMatchingQuery is supported in v1"}}
	}

	p := Params{
		Name:        d.Name,
		Description: d.Description,
		Template: ChangesetTemplate{
			Title:  d.ChangesetTemplate.Title,
			Body:   d.ChangesetTemplate.Body,
			Branch: d.ChangesetTemplate.Branch,
			Commit: Commit{Message: d.ChangesetTemplate.Commit.Message},
		},
	}
	if len(d.On) == 1 {
		p.Query = d.On[0].RepositoriesMatchingQuery
	}
	for _, step := range d.Steps {
		p.Steps = append(p.Steps, Step(step)) // identical shape; conversion is compile-checked
	}

	return New(p)
}
