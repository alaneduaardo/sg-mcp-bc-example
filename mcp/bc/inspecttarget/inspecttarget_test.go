package inspecttarget

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/apperr"
)

type fakeFetcher struct {
	gotRepo, gotPath, gotRev string
	calls                    int
	file                     File
	found                    bool
	err                      error
}

func (f *fakeFetcher) FetchFile(_ context.Context, repo, path, rev string) (File, bool, error) {
	f.calls++
	f.gotRepo, f.gotPath, f.gotRev = repo, path, rev
	return f.file, f.found, f.err
}

func TestExecute_HappyPath(t *testing.T) {
	f := &fakeFetcher{
		file:  File{Content: "package main\n", RevResolved: "abc123", SizeBytes: 12},
		found: true,
	}
	out, err := Execute(context.Background(), f, Input{Repo: "github.com/a/one", Path: "dir/f.go"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Content != "package main\n" || out.RevResolved != "abc123" || out.SizeBytes != 12 {
		t.Errorf("Output = %+v", out)
	}
	if f.gotRepo != "github.com/a/one" || f.gotPath != "dir/f.go" {
		t.Errorf("fetcher got repo/path = %q/%q", f.gotRepo, f.gotPath)
	}
}

func TestExecute_InvalidInput(t *testing.T) {
	cases := map[string]Input{
		"empty repo": {Repo: "", Path: "f.go"},
		"empty path": {Repo: "r", Path: ""},
		"blank repo": {Repo: "   ", Path: "f.go"},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeFetcher{}
			_, err := Execute(context.Background(), f, in)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
			if code, _ := apperr.Code(err); code != "INVALID_INPUT" {
				t.Errorf("contract code = %q, want INVALID_INPUT", code)
			}
			if f.calls != 0 {
				t.Errorf("fetcher called %d times; invalid input must not reach upstream", f.calls)
			}
		})
	}
}

func TestExecute_NotFound(t *testing.T) {
	f := &fakeFetcher{found: false} // file does not exist
	_, err := Execute(context.Background(), f, Input{Repo: "r", Path: "missing.go"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if code, _ := apperr.Code(err); code != "NOT_FOUND" {
		t.Errorf("contract code = %q, want NOT_FOUND", code)
	}
}

func TestExecute_UpstreamError(t *testing.T) {
	f := &fakeFetcher{err: errors.New("connection refused")}
	_, err := Execute(context.Background(), f, Input{Repo: "r", Path: "f.go"})
	if !errors.Is(err, ErrUpstream) {
		t.Errorf("err = %v, want ErrUpstream", err)
	}
	if code, _ := apperr.Code(err); code != "UPSTREAM_UNAVAILABLE" {
		t.Errorf("contract code = %q, want UPSTREAM_UNAVAILABLE", code)
	}
}

func TestExecute_TooLarge(t *testing.T) {
	f := &fakeFetcher{
		file:  File{SizeBytes: MaxFileBytes + 1},
		found: true,
	}
	_, err := Execute(context.Background(), f, Input{Repo: "r", Path: "big.go"})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if code, _ := apperr.Code(err); code != "TOO_LARGE" {
		t.Errorf("contract code = %q, want TOO_LARGE", code)
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Errorf("TOO_LARGE error should state the limit: %v", err)
	}
}
