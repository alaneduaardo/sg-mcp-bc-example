package inspecttarget

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/sgclient"
)

type fakeFetcher struct {
	gotRepo, gotPath, gotRev string
	ret                      sgclient.FileContent
	err                      error
}

func (f *fakeFetcher) FetchFile(_ context.Context, repo, path, rev string) (sgclient.FileContent, error) {
	f.gotRepo, f.gotPath, f.gotRev = repo, path, rev
	return f.ret, f.err
}

func TestExecute_HappyPath(t *testing.T) {
	f := &fakeFetcher{ret: sgclient.FileContent{
		Path:        "dir/f.go",
		Content:     "package main\n",
		RevResolved: "abc123",
		SizeBytes:   12,
	}}
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

func TestExecute_NotFound(t *testing.T) {
	f := &fakeFetcher{err: sgclient.ErrNotFound}
	_, err := Execute(context.Background(), f, Input{Repo: "r", Path: "missing.go"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestExecute_UpstreamError(t *testing.T) {
	f := &fakeFetcher{err: sgclient.ErrUpstreamUnavailable}
	_, err := Execute(context.Background(), f, Input{Repo: "r", Path: "f.go"})
	if !errors.Is(err, ErrUpstream) {
		t.Errorf("err = %v, want ErrUpstream", err)
	}
}

func TestExecute_TooLarge(t *testing.T) {
	f := &fakeFetcher{ret: sgclient.FileContent{
		Content:   strings.Repeat("x", 10),
		SizeBytes: MaxFileBytes + 1,
	}}
	_, err := Execute(context.Background(), f, Input{Repo: "r", Path: "big.go"})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Errorf("TOO_LARGE error should state the limit: %v", err)
	}
}
