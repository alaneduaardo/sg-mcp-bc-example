// Command server is the MCP entry point: it wires the Sourcegraph client and
// registers the bc_* tools over stdio. Concrete dependencies are wired here
// explicitly — no DI framework.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/findtargets"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/inspecttarget"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/sgclient"
)

const defaultEndpoint = "https://sourcegraph.com/.api/graphql"

func main() {
	endpoint := flag.String("endpoint", defaultEndpoint, "Sourcegraph GraphQL endpoint")
	flag.Parse()

	client := sgclient.New(*endpoint, sgclient.WithToken(os.Getenv("SRC_ACCESS_TOKEN")))

	s := server.NewMCPServer("bc", "0.1.0",
		server.WithToolCapabilities(true),
	)

	s.AddTool(findTargetsTool(), findTargetsHandler(client))
	s.AddTool(inspectTargetTool(), inspectTargetHandler(client))

	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("server error: %v", err)
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

func findTargetsHandler(client *sgclient.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError("INVALID_QUERY: " + err.Error()), nil
		}
		maxRepos := req.GetInt("max_repos", findtargets.DefaultMaxRepos)

		out, err := findtargets.Execute(ctx, client, findtargets.Input{Query: query, MaxRepos: maxRepos})
		if err != nil {
			switch {
			case errors.Is(err, findtargets.ErrInvalidQuery):
				return mcp.NewToolResultError("INVALID_QUERY: " + err.Error()), nil
			case errors.Is(err, findtargets.ErrUpstream):
				return mcp.NewToolResultError("UPSTREAM_UNAVAILABLE: " + err.Error()), nil
			default:
				return mcp.NewToolResultError(err.Error()), nil
			}
		}
		res, err := mcp.NewToolResultJSON(out)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("failed to encode result", err), nil
		}
		return res, nil
	}
}

func inspectTargetHandler(client *sgclient.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		repo, err := req.RequireString("repo")
		if err != nil {
			return mcp.NewToolResultError("INVALID_INPUT: " + err.Error()), nil
		}
		path, err := req.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError("INVALID_INPUT: " + err.Error()), nil
		}
		rev := req.GetString("rev", "")

		out, err := inspecttarget.Execute(ctx, client, inspecttarget.Input{Repo: repo, Path: path, Rev: rev})
		if err != nil {
			switch {
			case errors.Is(err, inspecttarget.ErrNotFound):
				return mcp.NewToolResultError("NOT_FOUND: " + err.Error()), nil
			case errors.Is(err, inspecttarget.ErrTooLarge):
				return mcp.NewToolResultError("TOO_LARGE: " + err.Error()), nil
			case errors.Is(err, inspecttarget.ErrUpstream):
				return mcp.NewToolResultError("UPSTREAM_UNAVAILABLE: " + err.Error()), nil
			default:
				return mcp.NewToolResultError(err.Error()), nil
			}
		}
		res, err := mcp.NewToolResultJSON(out)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("failed to encode result", err), nil
		}
		return res, nil
	}
}
