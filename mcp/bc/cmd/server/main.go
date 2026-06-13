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
	"flag"
	"log/slog"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/findtargets"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/inspecttarget"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/apperr"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/sgclient"
)

const defaultEndpoint = "https://sourcegraph.com/.api/graphql"

func main() {
	endpoint := flag.String("endpoint", defaultEndpoint, "Sourcegraph GraphQL endpoint")
	flag.Parse()

	// stdout is the JSON-RPC channel; logs are JSON on stderr so the two streams
	// stay segregated and the protocol stream is never corrupted.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	client := sgclient.New(*endpoint, sgclient.WithToken(os.Getenv("SRC_ACCESS_TOKEN")))

	s := server.NewMCPServer("bc", "0.1.0",
		server.WithToolCapabilities(true),
	)

	s.AddTool(findTargetsTool(), findTargetsHandler(client, logger))
	s.AddTool(inspectTargetTool(), inspectTargetHandler(client, logger))

	if err := server.ServeStdio(s); err != nil {
		logger.Error("server stopped", "error", err.Error())
		os.Exit(1)
	}
}

func findTargetsTool() mcp.Tool {
	return mcp.NewTool("bc_find_targets",
		mcp.WithDescription("Turn a Sourcegraph search query into batch-change targeting. "+
			"Returns per-repo occurrence counts and sample paths plus a normalized query ready "+
			"for on.repositoriesMatchingQuery. Not a generic search tool."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("query", mcp.Required(), mcp.Description("Sourcegraph search syntax")),
		mcp.WithInteger("max_repos",
			mcp.Description("max repositories to return"),
			mcp.DefaultNumber(findtargets.DefaultMaxRepos),
			mcp.Min(1), mcp.Max(findtargets.MaxAllowedRepos)),
	)
}

func findTargetsHandler(client *sgclient.Client, logger *slog.Logger) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := req.GetString("query", "")
		maxRepos := req.GetInt("max_repos", findtargets.DefaultMaxRepos)

		log := logger.With(slog.Group("request",
			"tool", "bc_find_targets",
			"query", query,
			"max_repos", maxRepos,
		))
		log.Debug("tool call")

		out, err := findtargets.Execute(ctx, client, findtargets.Input{Query: query, MaxRepos: maxRepos})
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

func inspectTargetHandler(client *sgclient.Client, logger *slog.Logger) server.ToolHandlerFunc {
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

		out, err := inspecttarget.Execute(ctx, client, inspecttarget.Input{Repo: repo, Path: path, Rev: rev})
		if err != nil {
			return respondError(log, err), nil
		}

		return respondJSON(log, out)
	}
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
