// Package inspecttarget is the inspection use case: full file content in the
// context of an identified target, so an agent can ground the transformation it
// will propose. Separated from discovery to avoid context explosion and to
// mirror how agents work — search, then read.
package inspecttarget

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/apperr"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/sgclient"
)

// MaxFileBytes caps the content this tool will return (tool contract §2).
const MaxFileBytes = 1 << 20 // 1 MiB

// Coded sentinel errors. errors.Is still matches; the code rides along for the
// handler to render. INVALID_INPUT extends the §2 contract (repo/path are
// required) — see 03-tool-contracts.md.
var (
	ErrInvalidInput = apperr.New("INVALID_INPUT", "invalid input")
	ErrNotFound     = apperr.New("NOT_FOUND", "not found")
	ErrTooLarge     = apperr.New("TOO_LARGE", "file too large")
	ErrUpstream     = apperr.New("UPSTREAM_UNAVAILABLE", "upstream unavailable")
)

// fileRef identifies the file to inspect and guards its own invariants: repo and
// path are required; rev defaults to HEAD. An invalid ref cannot be constructed,
// so Execute never reaches upstream with empty inputs — the inspection analogue
// of targeting.Query for discovery.
type fileRef struct {
	repo string
	path string
	rev  string
}

func newFileRef(repo, path, rev string) (fileRef, error) {
	if strings.TrimSpace(repo) == "" {
		return fileRef{}, ErrInvalidInput.Wrap(errors.New("repo is required"))
	}
	if strings.TrimSpace(path) == "" {
		return fileRef{}, ErrInvalidInput.Wrap(errors.New("path is required"))
	}
	if rev == "" {
		rev = "HEAD"
	}
	return fileRef{repo: repo, path: path, rev: rev}, nil
}

// Fetcher is what this use case needs from code intelligence. *sgclient.Client
// satisfies it.
type Fetcher interface {
	FetchFile(ctx context.Context, repo, path, rev string) (sgclient.FileContent, error)
}

// Input is the tool input (bc_inspect_target).
type Input struct {
	Repo string
	Path string
	Rev  string
}

// Output is the tool output (bc_inspect_target).
type Output struct {
	Content     string `json:"content"`
	RevResolved string `json:"rev_resolved"`
	SizeBytes   int    `json:"size_bytes"`
}

// Execute fetches one file and returns its content, refusing files over
// MaxFileBytes.
func Execute(ctx context.Context, fetcher Fetcher, in Input) (Output, error) {
	ref, err := newFileRef(in.Repo, in.Path, in.Rev)
	if err != nil {
		return Output{}, err
	}

	fc, err := fetcher.FetchFile(ctx, ref.repo, ref.path, ref.rev)
	if err != nil {
		if errors.Is(err, sgclient.ErrNotFound) {
			return Output{}, ErrNotFound.Wrap(err)
		}

		return Output{}, ErrUpstream.Wrap(err)
	}

	if fc.SizeBytes > MaxFileBytes {
		return Output{}, ErrTooLarge.Wrap(fmt.Errorf("%d bytes exceeds limit of %d", fc.SizeBytes, MaxFileBytes))
	}

	return Output{
		Content:     fc.Content,
		RevResolved: fc.RevResolved,
		SizeBytes:   fc.SizeBytes,
	}, nil
}
