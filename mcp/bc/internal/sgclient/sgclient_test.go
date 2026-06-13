package sgclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/targeting"
)

// capturingServer returns an httptest server that records the last request it
// saw and replies with the given status and body. The recorded request lets a
// test assert that the client serialized the GraphQL request correctly — the
// transport is exercised for real, not stubbed at the method boundary.
type capturedRequest struct {
	method string
	auth   string
	ctype  string
	body   gqlBody
}

type gqlBody struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

func newServer(t *testing.T, status int, respBody string) (*httptest.Server, *capturedRequest) {
	t.Helper()
	captured := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.auth = r.Header.Get("Authorization")
		captured.ctype = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &captured.body)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

func mustQuery(t *testing.T, raw string) targeting.Query {
	t.Helper()
	q, err := targeting.NewQuery(raw)
	if err != nil {
		t.Fatalf("NewQuery(%q): %v", raw, err)
	}
	return q
}

const searchTwoRepos = `{
  "data": {
    "search": {
      "results": {
        "matchCount": 4,
        "limitHit": false,
        "results": [
          {"__typename": "FileMatch", "repository": {"name": "github.com/a/one"}, "file": {"path": "a/x.go"}},
          {"__typename": "FileMatch", "repository": {"name": "github.com/a/one"}, "file": {"path": "a/y.go"}},
          {"__typename": "Repository", "repository": {"name": "github.com/ignored"}, "file": {"path": ""}},
          {"__typename": "FileMatch", "repository": {"name": "github.com/b/two"}, "file": {"path": "b/z.go"}},
          {"__typename": "FileMatch", "repository": {"name": "github.com/a/one"}, "file": {"path": "a/w.go"}}
        ]
      }
    }
  }
}`

func TestSearch_AggregatesByRepo(t *testing.T) {
	srv, captured := newServer(t, http.StatusOK, searchTwoRepos)
	c := New(srv.URL, WithToken("secret"))

	targets, err := c.Search(context.Background(), mustQuery(t, "needle"), 25)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// Request was serialized correctly and authenticated.
	if captured.method != http.MethodPost {
		t.Errorf("method = %s, want POST", captured.method)
	}
	if captured.ctype != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", captured.ctype)
	}
	if captured.auth != "token secret" {
		t.Errorf("Authorization = %q, want %q", captured.auth, "token secret")
	}
	if captured.body.Variables["query"] != "needle" {
		t.Errorf("query variable = %v, want %q", captured.body.Variables["query"], "needle")
	}
	if !strings.Contains(captured.body.Query, "search(") {
		t.Errorf("GraphQL query missing search field: %q", captured.body.Query)
	}

	if targets.TotalRepos != 2 {
		t.Errorf("TotalRepos = %d, want 2", targets.TotalRepos)
	}
	if targets.Truncated {
		t.Error("Truncated = true, want false")
	}
	if len(targets.Items) != 2 {
		t.Fatalf("Items len = %d, want 2", len(targets.Items))
	}
	// First-seen ordering preserves Sourcegraph's ranking.
	first := targets.Items[0]
	if first.Repo != "github.com/a/one" {
		t.Errorf("Items[0].Repo = %q, want github.com/a/one", first.Repo)
	}
	if first.OccurrenceCount != 3 {
		t.Errorf("Items[0].OccurrenceCount = %d, want 3", first.OccurrenceCount)
	}
	if got := strings.Join(first.SamplePaths, ","); got != "a/x.go,a/y.go,a/w.go" {
		t.Errorf("Items[0].SamplePaths = %q, want a/x.go,a/y.go,a/w.go", got)
	}
	if targets.Items[1].Repo != "github.com/b/two" || targets.Items[1].OccurrenceCount != 1 {
		t.Errorf("Items[1] = %+v, want {github.com/b/two, count 1}", targets.Items[1])
	}
}

func TestSearch_TruncatesToMaxRepos(t *testing.T) {
	srv, _ := newServer(t, http.StatusOK, searchTwoRepos)
	c := New(srv.URL)

	targets, err := c.Search(context.Background(), mustQuery(t, "needle"), 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !targets.Truncated {
		t.Error("Truncated = false, want true")
	}
	if len(targets.Items) != 1 {
		t.Errorf("Items len = %d, want 1", len(targets.Items))
	}
	if targets.TotalRepos != 2 {
		t.Errorf("TotalRepos = %d, want 2 (count is of all distinct repos)", targets.TotalRepos)
	}
}

func TestSearch_LimitHitMarksTruncated(t *testing.T) {
	const body = `{"data":{"search":{"results":{"matchCount":1,"limitHit":true,"results":[
	  {"__typename":"FileMatch","repository":{"name":"github.com/a/one"},"file":{"path":"x.go"}}
	]}}}}`
	srv, _ := newServer(t, http.StatusOK, body)
	c := New(srv.URL)

	targets, err := c.Search(context.Background(), mustQuery(t, "needle"), 25)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !targets.Truncated {
		t.Error("Truncated = false, want true when search limitHit is set")
	}
}

func TestSearch_NoTokenSendsNoAuthHeader(t *testing.T) {
	srv, captured := newServer(t, http.StatusOK, searchTwoRepos)
	c := New(srv.URL)
	if _, err := c.Search(context.Background(), mustQuery(t, "needle"), 25); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if captured.auth != "" {
		t.Errorf("Authorization = %q, want empty when no token configured", captured.auth)
	}
}

func TestFetchFile_Success(t *testing.T) {
	const body = `{"data":{"repository":{"commit":{"oid":"abc123","file":{
	  "path":"dir/f.go","byteSize":42,"content":"package main\n"}}}}}`
	srv, captured := newServer(t, http.StatusOK, body)
	c := New(srv.URL)

	fc, err := c.FetchFile(context.Background(), "github.com/a/one", "dir/f.go", "")
	if err != nil {
		t.Fatalf("FetchFile: %v", err)
	}
	if fc.Content != "package main\n" {
		t.Errorf("Content = %q", fc.Content)
	}
	if fc.RevResolved != "abc123" {
		t.Errorf("RevResolved = %q, want abc123", fc.RevResolved)
	}
	if fc.SizeBytes != 42 {
		t.Errorf("SizeBytes = %d, want 42", fc.SizeBytes)
	}
	// Empty rev should default to HEAD on the wire.
	if captured.body.Variables["rev"] != "HEAD" {
		t.Errorf("rev variable = %v, want HEAD", captured.body.Variables["rev"])
	}
	if captured.body.Variables["repo"] != "github.com/a/one" || captured.body.Variables["path"] != "dir/f.go" {
		t.Errorf("repo/path variables wrong: %+v", captured.body.Variables)
	}
}

func TestFetchFile_NotFound(t *testing.T) {
	cases := map[string]string{
		"null repository": `{"data":{"repository":null}}`,
		"null commit":     `{"data":{"repository":{"commit":null}}}`,
		"null file":       `{"data":{"repository":{"commit":{"oid":"abc","file":null}}}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			srv, _ := newServer(t, http.StatusOK, body)
			c := New(srv.URL)
			_, err := c.FetchFile(context.Background(), "github.com/a/one", "missing.go", "HEAD")
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestDo_UpstreamStatusError(t *testing.T) {
	srv, _ := newServer(t, http.StatusInternalServerError, `boom`)
	c := New(srv.URL)
	_, err := c.Search(context.Background(), mustQuery(t, "needle"), 25)
	if !errors.Is(err, ErrUpstreamUnavailable) {
		t.Errorf("err = %v, want ErrUpstreamUnavailable", err)
	}
}

func TestDo_GraphQLErrorsSurfaced(t *testing.T) {
	const body = `{"errors":[{"message":"invalid query: bad filter"},{"message":"second"}],"data":null}`
	srv, _ := newServer(t, http.StatusOK, body)
	c := New(srv.URL)

	_, err := c.Search(context.Background(), mustQuery(t, "needle"), 25)
	var gqlErr *GraphQLError
	if !errors.As(err, &gqlErr) {
		t.Fatalf("err = %v, want *GraphQLError", err)
	}
	if len(gqlErr.Messages) != 2 || gqlErr.Messages[0] != "invalid query: bad filter" {
		t.Errorf("GraphQLError.Messages = %v", gqlErr.Messages)
	}
	if !strings.Contains(err.Error(), "invalid query") {
		t.Errorf("error string should include the message: %v", err)
	}
}

func TestDo_TransportError(t *testing.T) {
	// Point at a closed server to force a transport-level failure.
	srv, _ := newServer(t, http.StatusOK, searchTwoRepos)
	url := srv.URL
	srv.Close()

	c := New(url)
	_, err := c.Search(context.Background(), mustQuery(t, "needle"), 25)
	if !errors.Is(err, ErrUpstreamUnavailable) {
		t.Errorf("err = %v, want ErrUpstreamUnavailable", err)
	}
}

func TestDo_RespectsContextCancellation(t *testing.T) {
	srv, _ := newServer(t, http.StatusOK, searchTwoRepos)
	c := New(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Search(ctx, mustQuery(t, "needle"), 25)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled in chain", err)
	}
}
