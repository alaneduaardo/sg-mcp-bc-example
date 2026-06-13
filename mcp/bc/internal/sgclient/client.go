// Package sgclient is the Sourcegraph transport: HTTP, auth, GraphQL plumbing
// and error mapping. It carries zero use-case logic — it knows how to talk to a
// Sourcegraph GraphQL endpoint and how to turn wire responses into the
// targeting value objects, nothing about why a caller wants them.
//
// It imports nothing internal except targeting (the shared vocabulary it
// deserializes into). Each operation lives in its own file with its own query
// (search.go, file.go); only the transport plumbing in this file is shared.
package sgclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Sentinel errors callers branch on. Operation methods wrap these with context
// via fmt.Errorf("...: %w", err); use errors.Is to test against them.
var (
	// ErrUpstreamUnavailable covers every way the Sourcegraph instance fails to
	// give us a usable response: transport failure, non-2xx status, or a
	// response body we cannot decode.
	ErrUpstreamUnavailable = errors.New("sgclient: upstream unavailable")
	// ErrNotFound is returned when a requested repository, revision or file
	// does not exist.
	ErrNotFound = errors.New("sgclient: not found")
)

// GraphQLError carries the messages from a GraphQL `errors` array. It is
// distinct from a transport failure: the request reached Sourcegraph and was
// answered, but the query itself was rejected (bad filter, unknown field).
// Callers recover it with errors.As and may map specific messages to their own
// domain errors (e.g. an invalid search query).
type GraphQLError struct {
	Messages []string
}

func (e *GraphQLError) Error() string {
	return "sgclient: graphql error: " + strings.Join(e.Messages, "; ")
}

// Client talks to a single Sourcegraph GraphQL endpoint.
type Client struct {
	endpoint   string
	token      string
	httpClient *http.Client
}

// Option configures a Client at construction. Explicit functional options keep
// the constructor honest: no hidden globals, deps wired by the caller.
type Option func(*Client)

// WithToken sets the access token sent as `Authorization: token <token>`. The
// public instance needs none; an enterprise instance does.
func WithToken(token string) Option {
	return func(c *Client) { c.token = token }
}

// WithHTTPClient injects the underlying *http.Client (timeouts, transport,
// test doubles). Defaults to a client with a 30s timeout.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// New builds a Client for the given GraphQL endpoint
// (e.g. https://sourcegraph.com/.api/graphql).
func New(endpoint string, opts ...Option) *Client {
	c := &Client{
		endpoint:   endpoint,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// gqlEnvelope is the standard GraphQL response shape.
type gqlEnvelope struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// do executes one GraphQL request and unmarshals its `data` field into out. It
// is the single shared transport path: every operation method funnels through
// here so auth and error mapping live in exactly one place.
func (c *Client) do(ctx context.Context, query string, vars map[string]any, out any) error {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return fmt.Errorf("sgclient: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("sgclient: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "token "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Join keeps the underlying cause (context.Canceled, DNS, refused
		// connection) matchable via errors.Is alongside the sentinel.
		return fmt.Errorf("sgclient: post %s: %w", c.endpoint, errors.Join(ErrUpstreamUnavailable, err))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return fmt.Errorf("sgclient: status %d: %s: %w", resp.StatusCode, strings.TrimSpace(string(body)), ErrUpstreamUnavailable)
	}

	var env gqlEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("sgclient: decode response: %w", errors.Join(ErrUpstreamUnavailable, err))
	}
	if len(env.Errors) > 0 {
		msgs := make([]string, len(env.Errors))
		for i, e := range env.Errors {
			msgs[i] = e.Message
		}
		return &GraphQLError{Messages: msgs}
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("sgclient: decode data: %w", errors.Join(ErrUpstreamUnavailable, err))
	}
	return nil
}
