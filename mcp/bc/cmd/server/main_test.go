package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/apperr"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/sgclient"
)

// --- helpers ---------------------------------------------------------------

func callReq(name string, args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{Params: mcp.CallToolParams{Name: name, Arguments: args}}
}

// resultText returns the text payload of the first content block.
func resultText(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	if r == nil {
		t.Fatal("nil result")
	}
	if len(r.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := r.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want mcp.TextContent", r.Content[0])
	}
	return tc.Text
}

// parseClientError decodes the {code, message} a tool error returns to the client.
func parseClientError(t *testing.T, r *mcp.CallToolResult) errorBody {
	t.Helper()
	if !r.IsError {
		t.Fatalf("result.IsError = false, want true; text=%q", resultText(t, r))
	}
	var eb errorBody
	if err := json.Unmarshal([]byte(resultText(t, r)), &eb); err != nil {
		t.Fatalf("client error is not JSON: %q (%v)", resultText(t, r), err)
	}
	return eb
}

// lastLogLine decodes the most recent JSON log record written to buf.
func lastLogLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	last := lines[len(lines)-1]
	if len(last) == 0 {
		t.Fatal("no log line captured")
	}
	var m map[string]any
	if err := json.Unmarshal(last, &m); err != nil {
		t.Fatalf("log line is not JSON: %q (%v)", last, err)
	}
	return m
}

func bufLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// canned GraphQL responses.
const (
	searchOneRepo = `{"data":{"search":{"results":{"matchCount":1,"limitHit":false,"results":[` +
		`{"__typename":"FileMatch","repository":{"name":"github.com/a/one"},"file":{"path":"x.go"}}]}}}}`
	fileOK   = `{"data":{"repository":{"commit":{"oid":"abc123","file":{"path":"f.go","byteSize":12,"content":"package main\n"}}}}}`
	fileNull = `{"data":{"repository":null}}`
)

// gqlServer returns an httptest server that always replies with body, and a flag
// reporting whether it was hit.
func gqlServer(t *testing.T, status int, body string) (*sgclient.Client, *bool) {
	t.Helper()
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return sgclient.New(srv.URL), &hit
}

// --- tool definitions ------------------------------------------------------

func TestToolDefinitions(t *testing.T) {
	tests := []struct {
		name         string
		tool         mcp.Tool
		wantName     string
		wantRequired []string
		wantProps    []string
	}{
		{"find", findTargetsTool(), "bc_find_targets", []string{"query"}, []string{"query", "max_repos"}},
		{"inspect", inspectTargetTool(), "bc_inspect_target", []string{"repo", "path"}, []string{"repo", "path", "rev"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.tool.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", tc.tool.Name, tc.wantName)
			}
			if h := tc.tool.Annotations.ReadOnlyHint; h == nil || !*h {
				t.Error("ReadOnlyHint should be true")
			}

			raw, err := json.Marshal(tc.tool.InputSchema)
			if err != nil {
				t.Fatalf("marshal schema: %v", err)
			}
			var schema struct {
				Required   []string                   `json:"required"`
				Properties map[string]json.RawMessage `json:"properties"`
			}
			if err := json.Unmarshal(raw, &schema); err != nil {
				t.Fatalf("unmarshal schema: %v", err)
			}
			for _, r := range tc.wantRequired {
				if !contains(schema.Required, r) {
					t.Errorf("required missing %q; got %v", r, schema.Required)
				}
			}
			for _, p := range tc.wantProps {
				if _, ok := schema.Properties[p]; !ok {
					t.Errorf("properties missing %q", p)
				}
			}
		})
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// --- clientError -----------------------------------------------------------

func TestClientError(t *testing.T) {
	res := clientError("NOT_FOUND", "not found")
	if !res.IsError {
		t.Error("IsError = false, want true")
	}
	if got, want := resultText(t, res), `{"code":"NOT_FOUND","message":"not found"}`; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

// --- respondError ----------------------------------------------------------

func TestRespondError_Coded(t *testing.T) {
	var buf bytes.Buffer
	log := bufLogger(&buf).With(slog.Group("request", "tool", "bc_find_targets"))

	err := apperr.New("INVALID_QUERY", "invalid query").Wrap(errors.New("query is empty"))
	res := respondError(log, err)

	// Client sees the decoupled, clean form — never the cause.
	eb := parseClientError(t, res)
	if eb.Code != "INVALID_QUERY" || eb.Message != "invalid query" {
		t.Errorf("client error = %+v", eb)
	}
	if got := resultText(t, res); got != `{"code":"INVALID_QUERY","message":"invalid query"}` {
		t.Errorf("client must not leak the cause: %q", got)
	}

	// Log carries the request context plus a nested error group incl. the cause.
	line := lastLogLine(t, &buf)
	if line["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", line["level"])
	}
	req, _ := line["request"].(map[string]any)
	if req["tool"] != "bc_find_targets" {
		t.Errorf("request.tool = %v", req["tool"])
	}
	errGroup, _ := line["error"].(map[string]any)
	if errGroup["code"] != "INVALID_QUERY" || errGroup["message"] != "invalid query" {
		t.Errorf("error group = %v", errGroup)
	}
	if errGroup["cause"] != "query is empty" {
		t.Errorf("error.cause = %v, want 'query is empty'", errGroup["cause"])
	}
}

func TestRespondError_NonCoded(t *testing.T) {
	var buf bytes.Buffer
	res := respondError(bufLogger(&buf), errors.New("boom"))

	eb := parseClientError(t, res)
	if eb.Code != "INTERNAL" || eb.Message != "boom" {
		t.Errorf("client error = %+v, want INTERNAL/boom", eb)
	}
	errGroup, _ := lastLogLine(t, &buf)["error"].(map[string]any)
	if errGroup["code"] != "INTERNAL" {
		t.Errorf("error.code = %v, want INTERNAL", errGroup["code"])
	}
	if _, hasCause := errGroup["cause"]; hasCause {
		t.Error("non-coded error should have no cause field")
	}
}

// --- respondJSON -----------------------------------------------------------

func TestRespondJSON_Success(t *testing.T) {
	res, err := respondJSON(discardLogger(), map[string]string{"hello": "world"})
	if err != nil {
		t.Fatalf("respondJSON: %v", err)
	}
	if res.IsError {
		t.Error("IsError = true, want false")
	}
	if got := resultText(t, res); got != `{"hello":"world"}` {
		t.Errorf("text = %q", got)
	}
}

func TestRespondJSON_EncodeFailure(t *testing.T) {
	var buf bytes.Buffer
	res, err := respondJSON(bufLogger(&buf), make(chan int)) // channels can't be JSON-encoded
	if err == nil {
		t.Fatal("expected encode error")
	}
	if res != nil {
		t.Error("result should be nil on encode failure")
	}
	if lastLogLine(t, &buf)["msg"] != "encode failed" {
		t.Error("encode failure should be logged")
	}
}

// --- handlers (driven through a real sgclient over httptest) ---------------

func TestFindTargetsHandler_Success(t *testing.T) {
	client, hit := gqlServer(t, http.StatusOK, searchOneRepo)
	h := findTargetsHandler(client, discardLogger())

	res, err := h(context.Background(), callReq("bc_find_targets", map[string]any{"query": "needle"}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !*hit {
		t.Error("expected the upstream to be queried")
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %q", resultText(t, res))
	}

	var out struct {
		NormalizedQuery string `json:"normalized_query"`
		Targets         []struct {
			Repo string `json:"repo"`
		} `json:"targets"`
	}
	if err := json.Unmarshal([]byte(resultText(t, res)), &out); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	if out.NormalizedQuery != "needle" {
		t.Errorf("normalized_query = %q", out.NormalizedQuery)
	}
	if len(out.Targets) != 1 || out.Targets[0].Repo != "github.com/a/one" {
		t.Errorf("targets = %+v", out.Targets)
	}
}

func TestFindTargetsHandler_InvalidQueryShortCircuits(t *testing.T) {
	client, hit := gqlServer(t, http.StatusOK, searchOneRepo)
	h := findTargetsHandler(client, discardLogger())

	res, err := h(context.Background(), callReq("bc_find_targets", map[string]any{"query": "   "}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if eb := parseClientError(t, res); eb.Code != "INVALID_QUERY" {
		t.Errorf("code = %q, want INVALID_QUERY", eb.Code)
	}
	if *hit {
		t.Error("an invalid query must not reach the upstream")
	}
}

func TestFindTargetsHandler_Upstream(t *testing.T) {
	client, _ := gqlServer(t, http.StatusInternalServerError, "boom")
	h := findTargetsHandler(client, discardLogger())

	res, err := h(context.Background(), callReq("bc_find_targets", map[string]any{"query": "needle"}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if eb := parseClientError(t, res); eb.Code != "UPSTREAM_UNAVAILABLE" {
		t.Errorf("code = %q, want UPSTREAM_UNAVAILABLE", eb.Code)
	}
}

func TestInspectTargetHandler_Success(t *testing.T) {
	client, _ := gqlServer(t, http.StatusOK, fileOK)
	h := inspectTargetHandler(sgFetcher{client: client}, discardLogger())

	res, err := h(context.Background(), callReq("bc_inspect_target", map[string]any{
		"repo": "github.com/a/one", "path": "f.go",
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %q", resultText(t, res))
	}
	var out struct {
		Content     string `json:"content"`
		RevResolved string `json:"rev_resolved"`
		SizeBytes   int    `json:"size_bytes"`
	}
	if err := json.Unmarshal([]byte(resultText(t, res)), &out); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	if out.Content != "package main\n" || out.RevResolved != "abc123" || out.SizeBytes != 12 {
		t.Errorf("output = %+v", out)
	}
}

func TestInspectTargetHandler_InvalidInputShortCircuits(t *testing.T) {
	client, hit := gqlServer(t, http.StatusOK, fileOK)
	h := inspectTargetHandler(sgFetcher{client: client}, discardLogger())

	res, err := h(context.Background(), callReq("bc_inspect_target", map[string]any{"path": "f.go"})) // no repo
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if eb := parseClientError(t, res); eb.Code != "INVALID_INPUT" {
		t.Errorf("code = %q, want INVALID_INPUT", eb.Code)
	}
	if *hit {
		t.Error("invalid input must not reach the upstream")
	}
}

func TestCreateSpecHandler_Success(t *testing.T) {
	h := createSpecHandler(discardLogger())
	res, err := h(context.Background(), callReq("bc_create_spec", map[string]any{
		"name":        "wrap-errors",
		"description": "wrap errors with %w",
		"on":          map[string]any{"repositoriesMatchingQuery": "repo:foo lang:go fmt.Errorf"},
		"steps":       []any{map[string]any{"run": "gofmt -w .", "container": "golang:1.25"}},
		"changeset_template": map[string]any{
			"title": "chore: wrap", "body": "automated", "branch": "batch/wrap",
			"commit": map[string]any{"message": "chore: wrap"},
		},
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %q", resultText(t, res))
	}
	var out struct {
		SpecYAML string `json:"spec_yaml"`
		Valid    bool   `json:"valid"`
	}
	if err := json.Unmarshal([]byte(resultText(t, res)), &out); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	if !out.Valid || !strings.Contains(out.SpecYAML, "name: wrap-errors") {
		t.Errorf("out = %+v", out)
	}
}

func TestCreateSpecHandler_ValidationFailed(t *testing.T) {
	h := createSpecHandler(discardLogger())
	res, err := h(context.Background(), callReq("bc_create_spec", map[string]any{
		// name omitted → VALIDATION_FAILED
		"on":                 map[string]any{"repositoriesMatchingQuery": "repo:foo"},
		"steps":              []any{map[string]any{"run": "x", "container": "y"}},
		"changeset_template": map[string]any{"title": "t", "branch": "feat/x", "commit": map[string]any{"message": "m"}},
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	eb := parseClientError(t, res)
	if eb.Code != "VALIDATION_FAILED" {
		t.Errorf("code = %q, want VALIDATION_FAILED", eb.Code)
	}
	if !strings.Contains(eb.Message, "name is required") {
		t.Errorf("client message must carry field detail, got %q", eb.Message)
	}
}

const previewSpecYAML = "name: x\n" +
	"on:\n  - repositoriesMatchingQuery: repo:foo lang:go\n" +
	"steps:\n  - run: gofmt -w .\n    container: golang:1.25\n" +
	"changesetTemplate:\n  title: t\n  body: b\n  branch: feat/x\n  commit:\n    message: m\n"

func TestPreviewHandler_Success(t *testing.T) {
	client, hit := gqlServer(t, http.StatusOK, searchOneRepo)
	h := previewHandler(sgResolver{client: client}, discardLogger())

	res, err := h(context.Background(), callReq("bc_preview", map[string]any{"spec_yaml": previewSpecYAML}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !*hit {
		t.Error("expected the resolver to query the upstream")
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %q", resultText(t, res))
	}
	var out struct {
		ResolvedRepos       []string `json:"resolved_repos"`
		EstimatedChangesets int      `json:"estimated_changesets"`
		Truncated           bool     `json:"truncated"`
		BoundaryNote        string   `json:"boundary_note"`
	}
	if err := json.Unmarshal([]byte(resultText(t, res)), &out); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	if len(out.ResolvedRepos) != 1 || out.ResolvedRepos[0] != "github.com/a/one" {
		t.Errorf("ResolvedRepos = %v", out.ResolvedRepos)
	}
	if out.EstimatedChangesets != 1 || out.BoundaryNote == "" || out.Truncated {
		t.Errorf("out = %+v", out)
	}
}

func TestPreviewHandler_SurfacesTruncation(t *testing.T) {
	// A search that hit its limit (limitHit:true) must surface truncated:true.
	const truncatedSearch = `{"data":{"search":{"results":{"matchCount":1,"limitHit":true,"results":[` +
		`{"__typename":"FileMatch","repository":{"name":"github.com/a/one"},"file":{"path":"x.go"}}]}}}}`
	client, _ := gqlServer(t, http.StatusOK, truncatedSearch)
	h := previewHandler(sgResolver{client: client}, discardLogger())

	res, err := h(context.Background(), callReq("bc_preview", map[string]any{"spec_yaml": previewSpecYAML}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	var out struct {
		Truncated  bool `json:"truncated"`
		Validation struct {
			Issues []string `json:"issues"`
		} `json:"validation"`
	}
	if err := json.Unmarshal([]byte(resultText(t, res)), &out); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	if !out.Truncated {
		t.Error("truncated = false, want true when the search hit its limit")
	}
	if !strings.Contains(strings.Join(out.Validation.Issues, " "), "truncated") {
		t.Errorf("validation issues should note truncation: %v", out.Validation.Issues)
	}
}

func TestPreviewHandler_InvalidSpec(t *testing.T) {
	client, _ := gqlServer(t, http.StatusOK, searchOneRepo)
	h := previewHandler(sgResolver{client: client}, discardLogger())

	res, err := h(context.Background(), callReq("bc_preview", map[string]any{"spec_yaml": "name: [oops"}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if eb := parseClientError(t, res); eb.Code != "INVALID_SPEC" {
		t.Errorf("code = %q, want INVALID_SPEC", eb.Code)
	}
}

func TestRequestPublishHandler_Refuses(t *testing.T) {
	h := requestPublishHandler(discardLogger())
	res, err := h(context.Background(), callReq("bc_request_publish", map[string]any{
		"spec_yaml": "name: x\n",
		"approval":  map[string]any{"approver": "alice@example.com", "token": "tok"},
		"rollout":   map[string]any{"mode": "staged", "initial_batch": 5, "halt_on_failure_rate": 0.2},
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if res.IsError {
		t.Fatalf("should be a structured refusal, not an error: %q", resultText(t, res))
	}
	var out struct {
		Status     string `json:"status"`
		Governance struct {
			Default string `json:"default"`
		} `json:"governance_semantics"`
	}
	if err := json.Unmarshal([]byte(resultText(t, res)), &out); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	if out.Status != "NOT_IMPLEMENTED" {
		t.Errorf("status = %q, want NOT_IMPLEMENTED", out.Status)
	}
	if !strings.Contains(out.Governance.Default, "invariant") {
		t.Errorf("governance default should state the invariant, got %q", out.Governance.Default)
	}
}

func TestInspectTargetHandler_NotFound(t *testing.T) {
	client, _ := gqlServer(t, http.StatusOK, fileNull)
	h := inspectTargetHandler(sgFetcher{client: client}, discardLogger())

	res, err := h(context.Background(), callReq("bc_inspect_target", map[string]any{
		"repo": "github.com/a/one", "path": "missing.go",
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if eb := parseClientError(t, res); eb.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", eb.Code)
	}
}
