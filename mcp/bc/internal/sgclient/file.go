package sgclient

import (
	"context"
	"fmt"
)

// fileQuery fetches a single file's content at a revision, plus the resolved
// commit oid and byte size. Shaped to inspection's need: one file, in full.
const fileQuery = `query BatchInspectFile($repo: String!, $rev: String!, $path: String!) {
  repository(name: $repo) {
    commit(rev: $rev) {
      oid
      file(path: $path) {
        path
        byteSize
        content
      }
    }
  }
}`

type fileData struct {
	Repository *struct {
		Commit *struct {
			OID  string `json:"oid"`
			File *struct {
				Path     string `json:"path"`
				ByteSize int    `json:"byteSize"`
				Content  string `json:"content"`
			} `json:"file"`
		} `json:"commit"`
	} `json:"repository"`
}

// FileContent is one file's content together with the revision it resolved to.
type FileContent struct {
	Path        string
	Content     string
	RevResolved string
	SizeBytes   int
}

// FetchFile reads one file at rev (empty rev defaults to HEAD). A missing repo,
// revision or file maps to ErrNotFound — Sourcegraph returns nulls along the
// chain rather than a GraphQL error for these.
//
// This method satisfies the consumer-side interface the inspecttarget use case
// declares.
func (c *Client) FetchFile(ctx context.Context, repo, path, rev string) (FileContent, error) {
	if rev == "" {
		rev = "HEAD"
	}
	vars := map[string]any{"repo": repo, "rev": rev, "path": path}

	var data fileData
	if err := c.do(ctx, fileQuery, vars, &data); err != nil {
		return FileContent{}, fmt.Errorf("sgclient: fetch %s@%s:%s: %w", repo, rev, path, err)
	}

	if data.Repository == nil || data.Repository.Commit == nil || data.Repository.Commit.File == nil {
		return FileContent{}, fmt.Errorf("sgclient: fetch %s@%s:%s: %w", repo, rev, path, ErrNotFound)
	}

	f := data.Repository.Commit.File
	return FileContent{
		Path:        f.Path,
		Content:     f.Content,
		RevResolved: data.Repository.Commit.OID,
		SizeBytes:   f.ByteSize,
	}, nil
}
