// Package inspecttarget is the inspection use case: full file content in the
// context of an identified target, so an agent can ground the transformation it
// will propose. Separated from discovery to avoid context explosion and to
// mirror how agents work — search, then read.
package inspecttarget

import (
	"context"
	"errors"
	"fmt"

	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/sgclient"
)

// MaxFileBytes caps the content this tool will return (tool contract §2).
const MaxFileBytes = 1 << 20 // 1 MiB

var (
	ErrNotFound = errors.New("inspecttarget: not found")
	ErrTooLarge = errors.New("inspecttarget: file too large")
	ErrUpstream = errors.New("inspecttarget: upstream unavailable")
)

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
	fc, err := fetcher.FetchFile(ctx, in.Repo, in.Path, in.Rev)
	if err != nil {
		if errors.Is(err, sgclient.ErrNotFound) {
			return Output{}, fmt.Errorf("%w: %s:%s", ErrNotFound, in.Repo, in.Path)
		}
		return Output{}, fmt.Errorf("%w: %v", ErrUpstream, err)
	}

	if fc.SizeBytes > MaxFileBytes {
		return Output{}, fmt.Errorf("%w: %d bytes exceeds limit of %d", ErrTooLarge, fc.SizeBytes, MaxFileBytes)
	}

	return Output{
		Content:     fc.Content,
		RevResolved: fc.RevResolved,
		SizeBytes:   fc.SizeBytes,
	}, nil
}
