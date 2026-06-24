// Command server is the MCP entry point: it wires the Sourcegraph client and
// registers the bc_* tools over stdio. Concrete dependencies are wired here
// explicitly — no DI framework.
//
// Error handling is split by altitude: the domain owns error semantics (each
// use-case error carries its contract code), and the entrypoint owns only
// entrypoint concerns — rendering the error to the wire as JSON and recording it
// in structured logs enriched with which tool and request it came from. The
// entrypoint classifies nothing; it reads the code the domain attached.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/createspec"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/findtargets"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/inspecttarget"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/apperr"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/sgclient"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/targeting"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/preview"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/requestpublish"
)

func main() {
	endpoint := flag.String("endpoint", os.Getenv("SG_BASE_URL"), "Sourcegraph GraphQL endpoint (or SG_BASE_URL)")
	flag.Parse()

	// stdout is the JSON-RPC channel; logs are JSON on stderr so the two streams
	// stay segregated and the protocol stream is never corrupted.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	// Fail fast on missing config: an empty endpoint would otherwise start the
	// server and surface as a misleading UPSTREAM_UNAVAILABLE on every call.
	if *endpoint == "" {
		logger.Error("missing Sourcegraph endpoint: set SG_BASE_URL or pass -endpoint " +
			"(e.g. https://sourcegraph.com/.api/graphql)")
		os.Exit(1)
	}

	client := sgclient.New(*endpoint, sgclient.WithToken(os.Getenv("SG_ACCESS_TOKEN")))

	s := server.NewMCPServer("bc", "0.1.0",
		server.WithToolCapabilities(true),
	)

	s.AddTool(findTargetsTool(), findTargetsHandler(client, logger))
	s.AddTool(inspectTargetTool(), inspectTargetHandler(sgFetcher{client: client}, logger))
	s.AddTool(createSpecTool(), createSpecHandler(logger))
	s.AddTool(previewTool(), previewHandler(sgResolver{client: client}, logger))
	s.AddTool(requestPublishTool(), requestPublishHandler(logger))

	if err := server.ServeStdio(s); err != nil {
		logger.Error("server stopped", "error", err.Error())
		os.Exit(1)
	}
}

func findTargetsTool() mcp.Tool {
	return mcp.NewTool("bc_find_targets",
		mcp.WithDescription("Turn Sourcegraph search queries into batch-change targeting. "+
			"Takes one or more queries (a spec's on: clause is a list of rules), searches them "+
			"in parallel, and returns the merged per-repo occurrence counts and sample paths plus "+
			"the normalized queries ready for on.repositoriesMatchingQuery. Not a generic search tool."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithArray("queries", mcp.Required(),
			mcp.Description("one or more Sourcegraph search queries"),
			mcp.Items(map[string]any{"type": "string"})),
		mcp.WithInteger("max_repos",
			mcp.Description("max repositories to return"),
			mcp.DefaultNumber(findtargets.DefaultMaxRepos),
			mcp.Min(1), mcp.Max(findtargets.MaxAllowedRepos)),
	)
}

func findTargetsHandler(searcher findtargets.Searcher, logger *slog.Logger) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		queries := req.GetStringSlice("queries", nil)
		maxRepos := req.GetInt("max_repos", findtargets.DefaultMaxRepos)

		log := logger.With(slog.Group("request",
			"tool", "bc_find_targets",
			"queries", queries,
			"max_repos", maxRepos,
		))
		log.Debug("tool call")

		out, err := findtargets.Execute(ctx, searcher, findtargets.Input{Queries: queries, MaxRepos: maxRepos})
		if err != nil {
			return respondError(log, err), nil
		}

		return respondJSON(log, out)
	}
}

func inspectTargetTool() mcp.Tool {
	return mcp.NewTool("bc_inspect_target",
		mcp.WithDescription("Fetch full file content in the context of an identified target, "+
			"so a transformation can be grounded before it is proposed."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("repo", mcp.Required(), mcp.Description("repository name, e.g. github.com/org/name")),
		mcp.WithString("path", mcp.Required(), mcp.Description("file path within the repository")),
		mcp.WithString("rev", mcp.Description("revision; defaults to HEAD")),
	)
}

func inspectTargetHandler(fetcher inspecttarget.Fetcher, logger *slog.Logger) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		repo := req.GetString("repo", "")
		path := req.GetString("path", "")
		rev := req.GetString("rev", "")

		log := logger.With(slog.Group("request",
			"tool", "bc_inspect_target",
			"repo", repo,
			"path", path,
			"rev", rev,
		))
		log.Debug("tool call")

		out, err := inspecttarget.Execute(ctx, fetcher, inspecttarget.Input{Repo: repo, Path: path, Rev: rev})
		if err != nil {
			return respondError(log, err), nil
		}

		return respondJSON(log, out)
	}
}

func createSpecTool() mcp.Tool {
	return mcp.NewTool("bc_create_spec",
		mcp.WithDescription("Assemble and validate the declarative Batch Changes spec from the conversation. "+
			"Composition only — never executes. Returns canonical YAML, a valid flag, and warnings. "+
			"v1: deterministic container steps only."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("name", mcp.Required(), mcp.Description("batch change name (alphanumeric with . _ - separators)")),
		mcp.WithString("description", mcp.Description("human description of the change")),
		mcp.WithObject("on", mcp.Required(),
			mcp.Description("repository targeting"),
			mcp.Properties(map[string]any{
				"repositoriesMatchingQuery": map[string]any{
					"type":        "string",
					"description": "Sourcegraph search query, from bc_find_targets.normalized_query",
				},
			})),
		mcp.WithArray("steps", mcp.Required(),
			mcp.Description("deterministic container steps (v1)"),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"run":       map[string]any{"type": "string", "description": "container command"},
					"container": map[string]any{"type": "string", "description": "container image"},
				},
				"required": []string{"run", "container"},
			})),
		mcp.WithObject("changeset_template", mcp.Required(),
			mcp.Description("the changeset (PR) each affected repo receives"),
			mcp.Properties(map[string]any{
				"title":  map[string]any{"type": "string"},
				"body":   map[string]any{"type": "string"},
				"branch": map[string]any{"type": "string", "description": "git branch name"},
				"commit": map[string]any{
					"type":       "object",
					"properties": map[string]any{"message": map[string]any{"type": "string"}},
					"required":   []string{"message"},
				},
			})),
	)
}

// createSpecArgs mirrors the bc_create_spec input schema for BindArguments.
type createSpecArgs struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	On          struct {
		RepositoriesMatchingQuery string `json:"repositoriesMatchingQuery"`
	} `json:"on"`
	Steps []struct {
		Run       string `json:"run"`
		Container string `json:"container"`
	} `json:"steps"`
	ChangesetTemplate struct {
		Title  string `json:"title"`
		Body   string `json:"body"`
		Branch string `json:"branch"`
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
	} `json:"changeset_template"`
}

func createSpecHandler(logger *slog.Logger) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args createSpecArgs
		if err := req.BindArguments(&args); err != nil {
			log := logger.With(slog.Group("request", "tool", "bc_create_spec"))
			return respondError(log, createspec.ErrValidationFailed.WithMessage("invalid arguments: "+err.Error())), nil
		}

		log := logger.With(slog.Group("request", "tool", "bc_create_spec", "name", args.Name))
		log.Debug("tool call")

		steps := make([]createspec.Step, len(args.Steps))
		for i, s := range args.Steps {
			steps[i] = createspec.Step{Run: s.Run, Container: s.Container}
		}
		out, err := createspec.Execute(ctx, createspec.Input{
			Name:        args.Name,
			Description: args.Description,
			Query:       args.On.RepositoriesMatchingQuery,
			Steps:       steps,
			Template: createspec.ChangesetTemplate{
				Title:         args.ChangesetTemplate.Title,
				Body:          args.ChangesetTemplate.Body,
				Branch:        args.ChangesetTemplate.Branch,
				CommitMessage: args.ChangesetTemplate.Commit.Message,
			},
		})
		if err != nil {
			return respondError(log, err), nil
		}

		return respondJSON(log, out)
	}
}

func previewTool() mcp.Tool {
	return mcp.NewTool("bc_preview",
		mcp.WithDescription("Resolve what a batch spec would touch, without touching anything. "+
			"Returns the repos the spec's on-query matches (resolved completely via count:all, not a "+
			"ranked sample), an estimated changeset count, an estimated staged-rollout phase count, "+
			"validation, and a boundary note. Target resolution is real; step execution is out of scope."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("spec_yaml", mcp.Required(), mcp.Description("canonical batch spec YAML, e.g. from bc_create_spec")),
	)
}

func previewHandler(resolver preview.Resolver, logger *slog.Logger) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		specYAML := req.GetString("spec_yaml", "")

		log := logger.With(slog.Group("request", "tool", "bc_preview"))
		log.Debug("tool call")

		out, err := preview.Execute(ctx, resolver, preview.Input{SpecYAML: specYAML})
		if err != nil {
			return respondError(log, err), nil
		}

		return respondJSON(log, out)
	}
}

func requestPublishTool() mcp.Tool {
	return mcp.NewTool("bc_request_publish",
		mcp.WithDescription("Governed publication request. v1 returns NOT_IMPLEMENTED plus the governance "+
			"semantics publication would require — the refusal is the deliverable, and human approval is an "+
			"invariant, not a feature flag. (v1 has no side effects.)"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("spec_yaml", mcp.Required(), mcp.Description("the spec proposed for publication")),
		mcp.WithObject("approval", mcp.Required(),
			mcp.Description("human authorization — no agent self-approval"),
			mcp.Properties(map[string]any{
				"approver": map[string]any{"type": "string", "description": "human identity (required)"},
				"token":    map[string]any{"type": "string", "description": "out-of-band approval token"},
			})),
		mcp.WithObject("rollout",
			mcp.Description("staged-rollout configuration"),
			mcp.Properties(map[string]any{
				"mode":                 map[string]any{"type": "string", "enum": []string{"staged"}, "default": "staged"},
				"initial_batch":        map[string]any{"type": "integer", "default": 5},
				"halt_on_failure_rate": map[string]any{"type": "number", "default": 0.2},
			})),
	)
}

// requestPublishArgs mirrors the bc_request_publish input schema for BindArguments.
type requestPublishArgs struct {
	SpecYAML string `json:"spec_yaml"`
	Approval struct {
		Approver string `json:"approver"`
		Token    string `json:"token"`
	} `json:"approval"`
	Rollout struct {
		Mode              string  `json:"mode"`
		InitialBatch      int     `json:"initial_batch"`
		HaltOnFailureRate float64 `json:"halt_on_failure_rate"`
	} `json:"rollout"`
}

func requestPublishHandler(logger *slog.Logger) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args requestPublishArgs
		_ = req.BindArguments(&args) // the response is fixed; bind only to record the attempt

		log := logger.With(slog.Group("request", "tool", "bc_request_publish", "approver", args.Approval.Approver))
		log.Debug("tool call")

		out, err := requestpublish.Execute(ctx, requestpublish.Input{
			SpecYAML: args.SpecYAML,
			Approval: requestpublish.Approval{Approver: args.Approval.Approver, Token: args.Approval.Token},
			Rollout: requestpublish.Rollout{
				Mode:              args.Rollout.Mode,
				InitialBatch:      args.Rollout.InitialBatch,
				HaltOnFailureRate: args.Rollout.HaltOnFailureRate,
			},
		})
		if err != nil {
			return respondError(log, err), nil
		}

		return respondJSON(log, out)
	}
}

// sgResolver adapts the Sourcegraph client to preview's Resolver port: it runs
// the search and projects results down to repo names. Query construction lives
// in the use case, so this adapter can only fail on transport.
type sgResolver struct {
	client *sgclient.Client
}

func (r sgResolver) ResolveRepos(ctx context.Context, query targeting.Query) (preview.Resolution, error) {
	targets, err := r.client.SearchAll(ctx, query) // count:all: resolve completely, not a ranked sample
	if err != nil {
		return preview.Resolution{}, err
	}
	repos := make([]string, 0, len(targets.Items))
	for _, t := range targets.Items {
		repos = append(repos, t.Repo)
	}
	return preview.Resolution{Repos: repos, Truncated: targets.Truncated}, nil
}

// sgFetcher adapts the Sourcegraph client to inspecttarget's Fetcher port. It is
// the anti-corruption boundary: it translates sgclient's types and its
// not-found sentinel into the use case's own vocabulary, which is why
// inspecttarget imports no transport package.
type sgFetcher struct {
	client *sgclient.Client
}

func (f sgFetcher) FetchFile(ctx context.Context, repo, path, rev string) (inspecttarget.File, bool, error) {
	fc, err := f.client.FetchFile(ctx, repo, path, rev)
	switch {
	case errors.Is(err, sgclient.ErrNotFound):
		return inspecttarget.File{}, false, nil
	case err != nil:
		return inspecttarget.File{}, false, err
	}
	return inspecttarget.File{
		Content:     fc.Content,
		RevResolved: fc.RevResolved,
		SizeBytes:   fc.SizeBytes,
	}, true, nil
}

// errorBody is the decoupled error the client receives: code and message as
// separate fields (no stutter, no internal chain). The cause stays in the logs.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// respondError logs the error as a structured group and returns the decoupled
// form to the client. The domain set the code/message/cause; the entrypoint only
// reads and renders. The Error log carries the request context (via the logger's
// "request" group) plus a nested "error" group {code, message, cause} — full
// diagnostic context, not a flat string. A non-coded error is recorded as
// INTERNAL.
func respondError(log *slog.Logger, err error) *mcp.CallToolResult {
	e := apperr.As(err)
	if e == nil {
		log.Error("tool call failed", slog.Group("error", "code", "INTERNAL", "message", err.Error()))
		return clientError("INTERNAL", err.Error())
	}

	attrs := []any{"code", e.Code(), "message", e.Message()}
	if cause := e.Cause(); cause != nil {
		attrs = append(attrs, "cause", cause.Error())
	}
	log.Error("tool call failed", slog.Group("error", attrs...))

	return clientError(e.Code(), e.Message())
}

// clientError renders the decoupled {code, message} JSON the client sees.
func clientError(code, message string) *mcp.CallToolResult {
	body, _ := json.Marshal(errorBody{Code: code, Message: message})
	return mcp.NewToolResultError(string(body))
}

// respondJSON encodes the use-case output as a JSON tool result, logging and
// surfacing an encode failure as an internal error.
func respondJSON(log *slog.Logger, out any) (*mcp.CallToolResult, error) {
	res, err := mcp.NewToolResultJSON(out)
	if err != nil {
		log.Error("encode failed", slog.Group("error", "code", "INTERNAL", "message", err.Error()))
		return nil, err
	}
	return res, nil
}
