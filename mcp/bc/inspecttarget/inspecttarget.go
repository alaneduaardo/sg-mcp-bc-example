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

// File is the content this use case needs from code intelligence, in its own
// terms — so the use case stays decoupled from any particular transport.
type File struct {
	Content     string
	RevResolved string
	SizeBytes   int
}

// Fetcher is the port over code intelligence, declared here in the use case's
// own vocabulary (no transport types). found == false means the file does not
// exist — a normal outcome, not an error; a non-nil error is a transport
// failure. An adapter at the composition root binds this to sgclient.
type Fetcher interface {
	FetchFile(ctx context.Context, repo, path, rev string) (file File, found bool, err error)
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

	file, found, err := fetcher.FetchFile(ctx, ref.repo, ref.path, ref.rev)
	if err != nil {
		return Output{}, ErrUpstream.Wrap(err)
	}
	if !found {
		return Output{}, ErrNotFound.Wrap(fmt.Errorf("%s: %s", ref.repo, ref.path))
	}

	if file.SizeBytes > MaxFileBytes {
		return Output{}, ErrTooLarge.Wrap(fmt.Errorf("%d bytes exceeds limit of %d", file.SizeBytes, MaxFileBytes))
	}

	// File and Output share a shape here; the explicit conversion makes the
	// mapping a compile-time check if they ever diverge.
	return Output(file), nil
}
