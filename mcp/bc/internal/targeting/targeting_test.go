package targeting

import (
	"errors"
	"testing"
)

func TestNewQuery_Normalization(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr error
	}{
		{name: "plain", raw: "repo:foo lang:go", want: "repo:foo lang:go"},
		{name: "trims surrounding space", raw: "   repo:foo   ", want: "repo:foo"},
		{name: "collapses internal runs", raw: "repo:foo    lang:go", want: "repo:foo lang:go"},
		{name: "collapses tabs and newlines", raw: "repo:foo\t\tlang:go\npattern", want: "repo:foo lang:go pattern"},
		{name: "mixed whitespace everywhere", raw: "\n  repo:foo \t lang:go \n ", want: "repo:foo lang:go"},
		{name: "empty is rejected", raw: "", wantErr: ErrEmptyQuery},
		{name: "whitespace only is rejected", raw: "   \t\n ", wantErr: ErrEmptyQuery},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q, err := NewQuery(tc.raw)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("NewQuery(%q) err = %v, want %v", tc.raw, err, tc.wantErr)
				}
				if !q.IsZero() {
					t.Errorf("NewQuery(%q) returned non-zero query on error: %q", tc.raw, q.String())
				}
				return
			}
			if err != nil {
				t.Fatalf("NewQuery(%q) unexpected err = %v", tc.raw, err)
			}
			if q.String() != tc.want {
				t.Errorf("NewQuery(%q) = %q, want %q", tc.raw, q.String(), tc.want)
			}
		})
	}
}

func TestNewTarget_CapsSamplePaths(t *testing.T) {
	paths := []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go"}
	tgt := NewTarget("github.com/x/y", 12, paths)

	if got := len(tgt.SamplePaths); got != MaxSamplePaths {
		t.Fatalf("SamplePaths len = %d, want %d", got, MaxSamplePaths)
	}
	for i := range tgt.SamplePaths {
		if tgt.SamplePaths[i] != paths[i] {
			t.Errorf("SamplePaths[%d] = %q, want %q", i, tgt.SamplePaths[i], paths[i])
		}
	}
}

func TestNewTarget_DoesNotAliasInput(t *testing.T) {
	paths := []string{"a.go", "b.go"}
	tgt := NewTarget("github.com/x/y", 2, paths)

	paths[0] = "mutated.go"
	if tgt.SamplePaths[0] == "mutated.go" {
		t.Error("NewTarget aliased the caller's slice; expected a defensive copy")
	}
}

func TestNewTarget_FewerThanMaxKeepsAll(t *testing.T) {
	tgt := NewTarget("github.com/x/y", 1, []string{"only.go"})
	if len(tgt.SamplePaths) != 1 {
		t.Fatalf("SamplePaths len = %d, want 1", len(tgt.SamplePaths))
	}
	if tgt.OccurrenceCount != 1 || tgt.Repo != "github.com/x/y" {
		t.Errorf("unexpected target fields: %+v", tgt)
	}
}
